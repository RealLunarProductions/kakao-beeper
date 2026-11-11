package connector

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Every KakaoTalk emoticon the bridge turns into a Matrix sticker is remembered
// here, keyed by the hash of the decoded image and by its mxc URI. When the user
// sends or forwards that sticker (or the same image) from Beeper, the bridge
// sends the original emoticon instead of a picture of it. Emoticons the user
// sent from their own KakaoTalk app are marked as owned.
const emoticonSchema = `
CREATE TABLE IF NOT EXISTS kakaotalk_emoticon (
	hash       TEXT PRIMARY KEY,
	mxc        TEXT NOT NULL,
	kind       INTEGER NOT NULL,
	attachment TEXT NOT NULL,
	pack       TEXT NOT NULL,
	owned      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS kakaotalk_emoticon_mxc ON kakaotalk_emoticon (mxc);
-- Every mxc URI an emoticon has had: a newer bridged copy replaces mxc above,
-- but custom emoji and older stickers still point at the earlier one.
CREATE TABLE IF NOT EXISTS kakaotalk_emoticon_uri (
	mxc  TEXT PRIMARY KEY,
	hash TEXT NOT NULL
);
INSERT INTO kakaotalk_emoticon_uri (mxc, hash) SELECT mxc, hash FROM kakaotalk_emoticon WHERE mxc<>''
	ON CONFLICT (mxc) DO NOTHING;
CREATE TABLE IF NOT EXISTS kakaotalk_shelf (
	login_id TEXT PRIMARY KEY,
	room_id  TEXT NOT NULL
);
DROP TABLE IF EXISTS kakaotalk_shelf_sheet;
CREATE TABLE IF NOT EXISTS kakaotalk_emoji_sheet (
	login_id TEXT NOT NULL,
	pack     TEXT NOT NULL,
	PRIMARY KEY (login_id, pack)
);
CREATE TABLE IF NOT EXISTS kakaotalk_shelf_pack (
	login_id TEXT NOT NULL,
	pack     TEXT NOT NULL,
	count    INTEGER NOT NULL,
	PRIMARY KEY (login_id, pack)
);
`

type storedEmoticon struct {
	Kind       int
	Attachment map[string]any
}

type emoticonStore struct {
	db *dbutil.Database
}

func newEmoticonStore(ctx context.Context, db *dbutil.Database) (*emoticonStore, error) {
	if _, err := db.Exec(ctx, emoticonSchema); err != nil {
		return nil, err
	}
	return &emoticonStore{db: db}, nil
}

func imageHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// emoticonPack is the pack ID in a path like "4446367.emot_003.webp".
func emoticonPack(path string) string {
	pack, _, _ := strings.Cut(path, ".")
	return pack
}

// sendableAttachment keeps the fields a KakaoTalk client puts in an emoticon
// message (path, name, type, plus alt/sound/size when present).
func sendableAttachment(att map[string]any) map[string]any {
	out := map[string]any{}
	for _, k := range []string{"path", "name", "type", "alt", "s", "sound", "width", "height", "emoticonItemPath"} {
		if v, ok := att[k]; ok && v != nil {
			out[k] = v
		}
	}
	return out
}

func (s *emoticonStore) put(ctx context.Context, hash string, mxc id.ContentURIString, kind int, att map[string]any, owned bool) error {
	raw, err := json.Marshal(sendableAttachment(att))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO kakaotalk_emoticon (hash, mxc, kind, attachment, pack, owned) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (hash) DO UPDATE SET mxc=excluded.mxc, kind=excluded.kind, attachment=excluded.attachment,
			pack=excluded.pack, owned=MAX(kakaotalk_emoticon.owned, excluded.owned)`,
		hash, string(mxc), kind, string(raw), emoticonPack(attStr(att, "path")), owned)
	if err != nil || mxc == "" {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO kakaotalk_emoticon_uri (mxc, hash) VALUES ($1, $2)
		ON CONFLICT (mxc) DO UPDATE SET hash=excluded.hash`, string(mxc), hash)
	return err
}

func (s *emoticonStore) get(ctx context.Context, column, value string) (*storedEmoticon, error) {
	if value == "" {
		return nil, nil
	}
	var kind int
	var raw string
	err := s.db.QueryRow(ctx, `SELECT kind, attachment FROM kakaotalk_emoticon WHERE `+column+`=$1 LIMIT 1`, value).Scan(&kind, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	emo := &storedEmoticon{Kind: kind}
	if err = json.Unmarshal([]byte(raw), &emo.Attachment); err != nil {
		return nil, err
	}
	return emo, nil
}

func (s *emoticonStore) byMXC(ctx context.Context, mxc id.ContentURIString) (*storedEmoticon, error) {
	if mxc == "" {
		return nil, nil
	}
	var hash string
	err := s.db.QueryRow(ctx, `SELECT hash FROM kakaotalk_emoticon_uri WHERE mxc=$1`, string(mxc)).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return s.get(ctx, "mxc", string(mxc))
	} else if err != nil {
		return nil, err
	}
	return s.get(ctx, "hash", hash)
}

type packImage struct {
	Path string
	MXC  id.ContentURIString
}

// packImages are the emoticons of a pack the bridge has, ordered by path.
func (s *emoticonStore) packImages(ctx context.Context, pack string) ([]packImage, error) {
	rows, err := s.db.Query(ctx, `SELECT attachment, mxc FROM kakaotalk_emoticon WHERE pack=$1`, pack)
	if err != nil {
		return nil, err
	}
	all, err := dbutil.NewRowIterWithError(rows, func(row dbutil.Scannable) (img packImage, err error) {
		var raw string
		if err = row.Scan(&raw, &img.MXC); err != nil {
			return
		}
		var att struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(raw), &att)
		img.Path = att.Path
		return
	}, err).AsList()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := all[:0]
	for _, img := range all {
		if emoticonFileName.MatchString(img.Path) && img.MXC != "" && !seen[img.Path] {
			seen[img.Path] = true
			out = append(out, img)
		}
	}
	slices.SortFunc(out, func(a, b packImage) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}

func (s *emoticonStore) byHash(ctx context.Context, hash string) (*storedEmoticon, error) {
	return s.get(ctx, "hash", hash)
}

// contentMXC is the media URI of an event, encrypted or not.
func contentMXC(c *event.MessageEventContent) id.ContentURIString {
	if c.File != nil && c.File.URL != "" {
		return c.File.URL
	}
	return c.URL
}

// emoticonFileName matches the file name a bridged emoticon keeps in Beeper (its
// KakaoTalk path), e.g. "4446367.emot_005.webp".
var emoticonFileName = regexp.MustCompile(`^(\d+)\.emot_\d{3}\.(webp|png|gif)$`)

// byFileName rebuilds an emoticon from a bridged sticker's file name, for
// stickers bridged before the bridge remembered them. The message type comes
// from another emoticon of the same pack when there is one, else the extension.
func (s *emoticonStore) byFileName(ctx context.Context, fileName string) (*storedEmoticon, error) {
	m := emoticonFileName.FindStringSubmatch(fileName)
	if m == nil {
		return nil, nil
	}
	pack, ext := m[1], m[2]
	kind := map[string]int{"webp": typeAnimEmote, "png": typeEmoticon, "gif": typeSticker}[ext]
	var known int
	err := s.db.QueryRow(ctx, `SELECT kind FROM kakaotalk_emoticon WHERE pack=$1 LIMIT 1`, pack).Scan(&known)
	if err == nil {
		kind = known
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return &storedEmoticon{Kind: kind, Attachment: map[string]any{
		"emoticonItemPath": fileName,
		"name":             "(Emoticons)",
		"path":             fileName,
		"type":             "image/" + ext,
	}}, nil
}

// packFormat is the file extension and message type of a pack the bridge has
// seen, or ok=false.
func (s *emoticonStore) packFormat(ctx context.Context, pack string) (ext string, kind int, ok bool, err error) {
	var raw string
	err = s.db.QueryRow(ctx, `SELECT kind, attachment FROM kakaotalk_emoticon WHERE pack=$1 LIMIT 1`, pack).Scan(&kind, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false, nil
	} else if err != nil {
		return "", 0, false, err
	}
	var att struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal([]byte(raw), &att)
	m := emoticonFileName.FindStringSubmatch(att.Path)
	if m == nil {
		return "", 0, false, nil
	}
	return m[2], kind, true, nil
}

// ownedPacks are the packs the user has sent an emoticon from.
func (s *emoticonStore) ownedPacks(ctx context.Context) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT DISTINCT pack FROM kakaotalk_emoticon WHERE owned=1 AND pack<>'' ORDER BY pack`)
	if err != nil {
		return nil, err
	}
	return dbutil.NewRowIterWithError(rows, dbutil.ScanSingleColumn[string], err).AsList()
}

func (s *emoticonStore) shelfRoom(ctx context.Context, loginID string) (id.RoomID, error) {
	var roomID string
	err := s.db.QueryRow(ctx, `SELECT room_id FROM kakaotalk_shelf WHERE login_id=$1`, loginID).Scan(&roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id.RoomID(roomID), err
}

func (s *emoticonStore) setShelfRoom(ctx context.Context, loginID string, roomID id.RoomID) error {
	_, err := s.db.Exec(ctx, `INSERT INTO kakaotalk_shelf (login_id, room_id) VALUES ($1, $2)
		ON CONFLICT (login_id) DO UPDATE SET room_id=excluded.room_id`, loginID, string(roomID))
	return err
}

type shelfPack struct {
	Pack  string
	Count int
}

func (s *emoticonStore) shelfPacks(ctx context.Context, loginID string) ([]shelfPack, error) {
	rows, err := s.db.Query(ctx, `SELECT pack, count FROM kakaotalk_shelf_pack WHERE login_id=$1 ORDER BY pack`, loginID)
	if err != nil {
		return nil, err
	}
	return dbutil.NewRowIterWithError(rows, func(row dbutil.Scannable) (p shelfPack, err error) {
		err = row.Scan(&p.Pack, &p.Count)
		return
	}, err).AsList()
}

func (s *emoticonStore) addShelfPack(ctx context.Context, loginID, pack string, count int) error {
	_, err := s.db.Exec(ctx, `INSERT INTO kakaotalk_shelf_pack (login_id, pack, count) VALUES ($1, $2, $3)
		ON CONFLICT (login_id, pack) DO UPDATE SET count=excluded.count`, loginID, pack, count)
	return err
}

// sheetPosted reports whether a pack's custom emoji sheet is in the shelf room.
func (s *emoticonStore) sheetPosted(ctx context.Context, loginID, pack string) (bool, error) {
	var one int
	err := s.db.QueryRow(ctx, `SELECT 1 FROM kakaotalk_emoji_sheet WHERE login_id=$1 AND pack=$2`, loginID, pack).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *emoticonStore) setSheetPosted(ctx context.Context, loginID, pack string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO kakaotalk_emoji_sheet (login_id, pack) VALUES ($1, $2)
		ON CONFLICT (login_id, pack) DO NOTHING`, loginID, pack)
	return err
}

func (s *emoticonStore) clearSheets(ctx context.Context, loginID string) error {
	_, err := s.db.Exec(ctx, `DELETE FROM kakaotalk_emoji_sheet WHERE login_id=$1`, loginID)
	return err
}

func (s *emoticonStore) clearShelf(ctx context.Context, loginID string) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM kakaotalk_shelf_pack WHERE login_id=$1`, loginID); err != nil {
		return err
	}
	if _, err := s.db.Exec(ctx, `DELETE FROM kakaotalk_emoji_sheet WHERE login_id=$1`, loginID); err != nil {
		return err
	}
	_, err := s.db.Exec(ctx, `DELETE FROM kakaotalk_shelf WHERE login_id=$1`, loginID)
	return err
}
