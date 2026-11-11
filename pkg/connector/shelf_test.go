package connector

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/util/dbutil"
)

func newTestStore(t *testing.T) *emoticonStore {
	t.Helper()
	raw, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	db, err := dbutil.NewWithDB(raw, "sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newEmoticonStore(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestShelfStore(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if room, err := s.shelfRoom(ctx, "1"); err != nil || room != "" {
		t.Fatalf("empty shelf room: %q %v", room, err)
	}
	if err := s.setShelfRoom(ctx, "1", "!a:beeper.local"); err != nil {
		t.Fatal(err)
	}
	if err := s.setShelfRoom(ctx, "1", "!b:beeper.local"); err != nil {
		t.Fatal(err)
	}
	if room, _ := s.shelfRoom(ctx, "1"); room != "!b:beeper.local" {
		t.Errorf("shelf room = %q", room)
	}
	_ = s.addShelfPack(ctx, "1", "4446367", 24)
	_ = s.addShelfPack(ctx, "1", "4446367", 25)
	_ = s.addShelfPack(ctx, "2", "999", 3)
	packs, err := s.shelfPacks(ctx, "1")
	if err != nil || len(packs) != 1 || packs[0] != (shelfPack{"4446367", 25}) {
		t.Fatalf("shelf packs = %+v %v", packs, err)
	}
	if err = s.clearShelf(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	if packs, _ = s.shelfPacks(ctx, "1"); len(packs) != 0 {
		t.Errorf("packs left after clear: %+v", packs)
	}
}

func TestOwnedPacksAndFormat(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.put(ctx, "h1", "mxc://x/1", typeAnimEmote, map[string]any{"path": "4446367.emot_003.webp"}, true)
	_ = s.put(ctx, "h2", "mxc://x/2", typeEmoticon, map[string]any{"path": "555.emot_001.png"}, false)
	owned, err := s.ownedPacks(ctx)
	if err != nil || len(owned) != 1 || owned[0] != "4446367" {
		t.Fatalf("owned packs = %v %v", owned, err)
	}
	ext, kind, ok, err := s.packFormat(ctx, "555")
	if err != nil || !ok || ext != "png" || kind != typeEmoticon {
		t.Errorf("packFormat(555) = %q %d %v %v", ext, kind, ok, err)
	}
	if _, _, ok, _ = s.packFormat(ctx, "777"); ok {
		t.Error("unknown pack has a format")
	}
}

func TestPackArg(t *testing.T) {
	for arg, want := range map[string]string{
		"4446367": "4446367", "4446367.emot_005.webp": "4446367", "12": "", "abc": "", "4446367.emot_5.webp": "",
	} {
		got := ""
		if m := packArg.FindStringSubmatch(arg); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("packArg(%q) = %q, want %q", arg, got, want)
		}
	}
}

func TestFetchEmoticonEndOfPack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/1.emot_001.webp" {
			_, _ = w.Write([]byte("RIFF....WEBPVP8X"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := emoticonCDN
	emoticonCDN = srv.URL + "/"
	defer func() { emoticonCDN = old }()

	data, found, err := fetchEmoticon(context.Background(), "1.emot_001.webp")
	if err != nil || !found || string(data[:4]) != "RIFF" {
		t.Fatalf("existing item: %q %v %v", data, found, err)
	}
	if _, found, err = fetchEmoticon(context.Background(), "1.emot_002.webp"); err != nil || found {
		t.Errorf("past the end: found=%v err=%v", found, err)
	}
}
