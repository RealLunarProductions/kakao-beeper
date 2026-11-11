package connector

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestEmoticonStore(t *testing.T) {
	ctx := context.Background()
	raw, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	db, err := dbutil.NewWithDB(raw, "sqlite3")
	if err != nil {
		t.Fatal(err)
	}
	store, err := newEmoticonStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	att := map[string]any{"emoticonItemPath": "4446367.emot_003.webp", "name": "(Emoticons)",
		"path": "4446367.emot_003.webp", "type": "image/webp", "url": "ignored"}
	hash := imageHash([]byte("webp bytes"))
	if err = store.put(ctx, hash, "mxc://beeper.local/a", typeAnimEmote, att, true); err != nil {
		t.Fatal(err)
	}
	// Seeing it again from someone else keeps it owned and moves it to the new mxc.
	if err = store.put(ctx, hash, "mxc://beeper.local/b", typeAnimEmote, att, false); err != nil {
		t.Fatal(err)
	}
	var owned int
	var pack string
	if err = db.QueryRow(ctx, "SELECT owned, pack FROM kakaotalk_emoticon").Scan(&owned, &pack); err != nil || owned != 1 || pack != "4446367" {
		t.Fatalf("owned=%d pack=%q err=%v", owned, pack, err)
	}

	emo, err := store.byMXC(ctx, "mxc://beeper.local/b")
	if err != nil || emo == nil {
		t.Fatalf("byMXC: %v %v", emo, err)
	}
	if emo.Kind != typeAnimEmote || emo.Attachment["path"] != "4446367.emot_003.webp" || emo.Attachment["url"] != nil {
		t.Errorf("unexpected emoticon %+v", emo)
	}
	if emo, _ = store.byHash(ctx, hash); emo == nil {
		t.Error("byHash found nothing")
	}
	// Earlier copies keep matching (custom emoji and old stickers point at them).
	if emo, _ = store.byMXC(ctx, "mxc://beeper.local/a"); emo == nil {
		t.Error("old mxc no longer matches")
	}
	if emo, _ = store.byHash(ctx, imageHash([]byte("other"))); emo != nil {
		t.Error("unknown hash matched")
	}
}

func TestContentMXC(t *testing.T) {
	plain := &event.MessageEventContent{URL: "mxc://x/plain"}
	enc := &event.MessageEventContent{File: &event.EncryptedFileInfo{URL: "mxc://x/enc"}}
	if contentMXC(plain) != "mxc://x/plain" || contentMXC(enc) != id.ContentURIString("mxc://x/enc") {
		t.Error("contentMXC picked the wrong URI")
	}
}

func TestEmoticonByFileName(t *testing.T) {
	ctx := context.Background()
	raw, _ := sql.Open("sqlite3", ":memory:")
	raw.SetMaxOpenConns(1)
	db, _ := dbutil.NewWithDB(raw, "sqlite3")
	store, err := newEmoticonStore(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if emo, _ := store.byFileName(ctx, "photo.webp"); emo != nil {
		t.Error("matched a normal file name")
	}
	emo, err := store.byFileName(ctx, "4446367.emot_005.webp")
	if err != nil || emo == nil || emo.Kind != typeAnimEmote || emo.Attachment["path"] != "4446367.emot_005.webp" || emo.Attachment["type"] != "image/webp" {
		t.Fatalf("webp: %+v %v", emo, err)
	}
	// A pack the bridge has seen uses that pack's message type.
	_ = store.put(ctx, "h", "mxc://x/y", typeEmoticon, map[string]any{"path": "4446367.emot_001.webp"}, true)
	if emo, _ = store.byFileName(ctx, "4446367.emot_005.webp"); emo == nil || emo.Kind != typeEmoticon {
		t.Errorf("pack kind not used: %+v", emo)
	}
}
