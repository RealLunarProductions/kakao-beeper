package connector

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// The emoticon shelf is a private room the bridge fills with every emoticon of
// the user's KakaoTalk packs. Beeper has no sticker picker for bridges, so the
// user long-presses an emoticon there and forwards it to a KakaoTalk chat;
// HandleMatrixMessage then sends the real emoticon. The same packs are also
// posted as custom emoji the user can add to Beeper's emoji picker (emojipack.go). Packs the user has sent an
// emoticon from are added automatically; "!kakao emoticons add <pack>" adds others.

const (
	shelfPortalID  = "__emoticon_shelf__"
	maxPackItems   = 99
	maxEmoticonLen = 10 << 20
	// Body of shelf stickers: Beeper sends it as a text after a forwarded sticker,
	// and takeEmoticonEcho swallows exactly this placeholder.
	shelfStickerBody = "(이모티콘)"
)

var packExtensions = []string{"webp", "png", "gif"}

func emoticonPath(pack string, n int, ext string) string {
	return fmt.Sprintf("%s.emot_%03d.%s", pack, n, ext)
}

// fetchEmoticon downloads and decodes one emoticon image; found is false when
// the CDN has no such item (the end of a pack).
func fetchEmoticon(ctx context.Context, path string) (data []byte, found bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, emoticonCDN+path, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusForbidden:
		return nil, false, nil
	default:
		return nil, false, fmt.Errorf("unexpected status %d downloading %s", resp.StatusCode, path)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxEmoticonLen))
	if err != nil {
		return nil, false, err
	}
	return decryptEmoticon(data), true, nil
}

// packFormat finds a pack's file extension and message type: from an emoticon of
// the pack the bridge has seen, else by probing the CDN for its first item.
func (kc *KakaoClient) packFormat(ctx context.Context, pack string) (string, int, error) {
	if ext, kind, ok, err := kc.Main.emoticons.packFormat(ctx, pack); err != nil {
		return "", 0, err
	} else if ok {
		return ext, kind, nil
	}
	for _, ext := range packExtensions {
		_, found, err := fetchEmoticon(ctx, emoticonPath(pack, 1, ext))
		if err != nil {
			return "", 0, err
		} else if found {
			return ext, map[string]int{"webp": typeAnimEmote, "png": typeEmoticon, "gif": typeSticker}[ext], nil
		}
	}
	return "", 0, fmt.Errorf("KakaoTalk has no emoticon pack %s", pack)
}

// shelfFeatures are the emoticon room's features: Beeper hides reactions in rooms
// without reaction support, and the custom emoji sheet's reactions are what the
// user right-clicks to add an emoticon to their custom emoji.
func shelfFeatures() *event.RoomFeatures {
	f := roomCaps.Clone()
	f.ID = "kakaotalk.emoticon_room.2026_11_11"
	f.Reaction = event.CapLevelFullySupported
	f.CustomEmojiReactions = true
	return f
}

// setShelfFeatures (re)sends the emoticon room's features.
func (kc *KakaoClient) setShelfFeatures(ctx context.Context, roomID id.RoomID) error {
	_, err := kc.Main.Bridge.Bot.SendState(ctx, roomID, event.StateBeeperRoomFeatures, "", &event.Content{Parsed: shelfFeatures()}, time.Now())
	return err
}

func (kc *KakaoClient) ensureShelfRoom(ctx context.Context) (id.RoomID, error) {
	loginID := string(kc.UserLogin.ID)
	if roomID, err := kc.Main.emoticons.shelfRoom(ctx, loginID); err != nil || roomID != "" {
		return roomID, err
	}
	br := kc.Main.Bridge
	name := kc.Main.GetName()
	req := &mautrix.ReqCreateRoom{
		Visibility: "private",
		Name:       "KakaoTalk 이모티콘",
		Topic:      "Your KakaoTalk emoticons. Long-press one and forward it to a KakaoTalk chat to send it.",
		InitialState: []*event.Event{{
			Type:    event.StateBeeperRoomFeatures,
			Content: event.Content{Parsed: shelfFeatures()},
		}, {
			Type:    event.StateRoomAvatar,
			Content: event.Content{Parsed: &event.RoomAvatarEventContent{URL: name.NetworkIcon}},
		}, {
			Type: event.StateBridge,
			Content: event.Content{Parsed: &event.BridgeEventContent{
				BridgeBot: br.Bot.GetMXID(),
				Protocol:  name.AsBridgeInfoSection(),
				Channel: event.BridgeInfoSection{
					ID:          shelfPortalID,
					DisplayName: "KakaoTalk 이모티콘",
					Receiver:    loginID,
				},
			}},
		}},
		PowerLevelOverride: &event.PowerLevelsEventContent{
			Users: map[id.UserID]int{
				br.Bot.GetMXID():      9001,
				kc.UserLogin.UserMXID: 50,
			},
		},
		Invite: []id.UserID{kc.UserLogin.UserMXID},
		BeeperLocalRoomID: br.Matrix.GenerateDeterministicRoomID(networkid.PortalKey{
			ID:       shelfPortalID,
			Receiver: kc.UserLogin.ID,
		}),
	}
	autoJoin := br.Matrix.GetCapabilities().AutoJoinInvites
	if autoJoin {
		req.BeeperInitialMembers = []id.UserID{kc.UserLogin.UserMXID}
		req.BeeperAutoJoinInvites = true
	}
	roomID, err := br.Bot.CreateRoom(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to create the emoticon room: %w", err)
	}
	log := zerolog.Ctx(ctx)
	if !autoJoin {
		if dp := kc.UserLogin.User.DoublePuppet(ctx); dp != nil {
			if err = dp.EnsureJoined(ctx, roomID); err != nil {
				log.Warn().Err(err).Msg("Failed to auto-join the emoticon room")
			}
		}
	}
	if err = kc.Main.emoticons.setShelfRoom(ctx, loginID, roomID); err != nil {
		return "", err
	}
	if space, err := kc.UserLogin.GetSpaceRoom(ctx); err != nil {
		log.Warn().Err(err).Msg("Failed to get space room for the emoticon room")
	} else if space != "" {
		_, err = br.Bot.SendState(ctx, space, event.StateSpaceChild, roomID.String(), &event.Content{
			Parsed: &event.SpaceChildEventContent{Via: []string{br.Matrix.ServerName()}},
		}, time.Now())
		if err != nil {
			log.Warn().Err(err).Msg("Failed to add the emoticon room to the space")
		}
	}
	log.Info().Stringer("room_id", roomID).Msg("Created the emoticon room")
	return roomID, nil
}

// addPackToShelf posts every emoticon of a pack to the shelf room and returns how
// many it posted (or how many were already there).
func (kc *KakaoClient) addPackToShelf(ctx context.Context, pack string) (int, error) {
	if kc.Main.emoticons == nil {
		return 0, fmt.Errorf("the emoticon table is unavailable")
	}
	kc.shelfLock.Lock()
	defer kc.shelfLock.Unlock()
	loginID := string(kc.UserLogin.ID)
	packs, err := kc.Main.emoticons.shelfPacks(ctx, loginID)
	if err != nil {
		return 0, err
	}
	for _, p := range packs {
		if p.Pack == pack {
			return p.Count, nil
		}
	}
	ext, kind, err := kc.packFormat(ctx, pack)
	if err != nil {
		return 0, err
	}
	roomID, err := kc.ensureShelfRoom(ctx)
	if err != nil {
		return 0, err
	}
	log := zerolog.Ctx(ctx).With().Str("pack", pack).Stringer("room_id", roomID).Logger()
	bot := kc.Main.Bridge.Bot
	header := &event.MessageEventContent{MsgType: event.MsgNotice, Body: "📦 이모티콘 " + pack}
	if _, err = bot.SendMessage(ctx, roomID, event.EventMessage, &event.Content{Parsed: header}, nil); err != nil {
		return 0, fmt.Errorf("failed to post to the emoticon room: %w", err)
	}
	count := 0
	for n := 1; n <= maxPackItems; n++ {
		path := emoticonPath(pack, n, ext)
		data, found, err := fetchEmoticon(ctx, path)
		if err != nil {
			return count, err
		} else if !found {
			break
		}
		hash := imageHash(data) // before UploadMedia, which encrypts data in place
		mime := http.DetectContentType(data)
		width, height := imageSize(data)
		size := len(data)
		mxc, file, err := bot.UploadMedia(ctx, roomID, data, path, mime)
		if err != nil {
			return count, fmt.Errorf("failed to upload %s: %w", path, err)
		}
		content := &event.MessageEventContent{
			Body:     shelfStickerBody,
			FileName: path,
			URL:      mxc,
			File:     file,
			Info:     &event.FileInfo{MimeType: mime, Size: size, Width: width, Height: height},
		}
		if _, err = bot.SendMessage(ctx, roomID, event.EventSticker, &event.Content{Parsed: content}, nil); err != nil {
			return count, fmt.Errorf("failed to post %s: %w", path, err)
		}
		att := map[string]any{"emoticonItemPath": path, "name": "(Emoticons)", "path": path, "type": "image/" + ext}
		if err = kc.Main.emoticons.put(ctx, hash, contentMXC(content), kind, att, false); err != nil {
			log.Warn().Err(err).Str("path", path).Msg("Failed to remember shelf emoticon")
		}
		count++
	}
	if count == 0 {
		return 0, fmt.Errorf("pack %s has no emoticons", pack)
	}
	if err = kc.Main.emoticons.addShelfPack(ctx, loginID, pack, count); err != nil {
		return count, err
	}
	log.Info().Int("count", count).Msg("Added emoticon pack to the shelf")
	if err = kc.postMissingSheets(log.WithContext(ctx)); err != nil {
		log.Warn().Err(err).Msg("Failed to post the custom emoji sheet")
	}
	return count, nil
}

// queueShelfPack adds an owned pack to the shelf in the background, once.
func (kc *KakaoClient) queueShelfPack(pack string) {
	if !kc.Main.Config.EmoticonShelf || pack == "" {
		return
	}
	if _, busy := kc.shelfQueued.LoadOrStore(pack, true); busy {
		return
	}
	go func() {
		log := kc.UserLogin.Log.With().Str("component", "emoticon shelf").Str("pack", pack).Logger()
		ctx, cancel := context.WithTimeout(log.WithContext(context.Background()), 10*time.Minute)
		defer cancel()
		if _, err := kc.addPackToShelf(ctx, pack); err != nil {
			log.Err(err).Msg("Failed to add emoticon pack to the shelf")
			kc.shelfQueued.Delete(pack) // try again next time
		}
	}()
}

// syncShelf adds every owned pack that isn't on the shelf yet.
func (kc *KakaoClient) syncShelf(ctx context.Context) {
	if !kc.Main.Config.EmoticonShelf || kc.Main.emoticons == nil {
		return
	}
	if roomID, err := kc.Main.emoticons.shelfRoom(ctx, string(kc.UserLogin.ID)); err == nil && roomID != "" {
		if err = kc.setShelfFeatures(ctx, roomID); err != nil {
			zerolog.Ctx(ctx).Warn().Err(err).Msg("Failed to set the emoticon room's features")
		}
	}
	kc.shelfLock.Lock()
	err := kc.postMissingSheets(ctx)
	kc.shelfLock.Unlock()
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Msg("Failed to post custom emoji sheets")
	}
	packs, err := kc.Main.emoticons.ownedPacks(ctx)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to list owned emoticon packs")
		return
	}
	for _, pack := range packs {
		kc.queueShelfPack(pack)
	}
}
