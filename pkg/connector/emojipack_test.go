package connector

import (
	"context"
	"strings"
	"testing"

	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

func TestEmojiShortcode(t *testing.T) {
	for path, want := range map[string]string{
		"4446367.emot_005.webp": "kakao4446367_05",
		"4446367.emot_024.webp": "kakao4446367_24",
		"555.emot_100.png":      "kakao555_100",
		"555.emot_001.gif":      "kakao555_01",
		"photo.jpg":             "",
	} {
		if got := emojiShortcode(path); got != want {
			t.Errorf("emojiShortcode(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestEmojiSheet(t *testing.T) {
	c := emojiSheet("4446367", []packImage{
		{Path: "4446367.emot_001.webp", MXC: "mxc://local.beeper.com/a"},
		{Path: "bogus", MXC: "mxc://x/b"},
		{Path: "4446367.emot_002.webp", MXC: "mxc://local.beeper.com/c"},
	})
	if c.MsgType != event.MsgNotice || c.Format != event.FormatHTML {
		t.Fatalf("sheet = %+v", c)
	}
	if strings.Count(c.FormattedBody, "data-mx-emoticon") != 2 || strings.Contains(c.FormattedBody, "mxc://x/b") ||
		!strings.Contains(c.FormattedBody, `src="mxc://local.beeper.com/c" alt=":kakao4446367_02:" title=":kakao4446367_02:"`) {
		t.Errorf("formatted body = %s", c.FormattedBody)
	}
	if !strings.HasSuffix(c.Body, ":kakao4446367_01: :kakao4446367_02:") {
		t.Errorf("body = %q", c.Body)
	}
}

func TestSheetPosted(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if posted, err := s.sheetPosted(ctx, "1", "4446367"); err != nil || posted {
		t.Fatalf("sheetPosted before = %v, %v", posted, err)
	}
	_ = s.setSheetPosted(ctx, "1", "4446367")
	_ = s.setSheetPosted(ctx, "1", "4446367")
	if posted, _ := s.sheetPosted(ctx, "1", "4446367"); !posted {
		t.Error("sheet not recorded")
	}
	if posted, _ := s.sheetPosted(ctx, "2", "4446367"); posted {
		t.Error("sheet recorded for another login")
	}
	_ = s.clearShelf(ctx, "1")
	if posted, _ := s.sheetPosted(ctx, "1", "4446367"); posted {
		t.Error("clearShelf kept the sheet")
	}
}

func TestSingleCustomEmoji(t *testing.T) {
	html := func(body string) *event.MessageEventContent {
		return &event.MessageEventContent{MsgType: event.MsgText, Body: "x", Format: event.FormatHTML, FormattedBody: body}
	}
	img := `<img data-mx-emoticon src="mxc://local.beeper.com/abc" alt=":kakao1_01:" title=":kakao1_01:" height="32" />`
	cases := []struct {
		content *event.MessageEventContent
		ok      bool
		text    string
	}{
		{html(img), true, ""},
		{html(" " + img + " "), true, ""},
		{html("hi &amp; bye " + img), true, "hi & bye"},
		{html("<p>line one</p><p>" + img + "two</p>"), true, "line one\ntwo"},
		{html(img + img), false, ""},
		{html(`<img src="mxc://x/y">`), false, ""},
		{html(`<img data-mx-emoticon src="https://x/y">`), false, ""},
		{&event.MessageEventContent{MsgType: event.MsgText, Body: ":kakao1_01:"}, false, ""},
	}
	for i, c := range cases {
		mxc, text, ok := singleCustomEmoji(c.content)
		if ok != c.ok || (ok && (mxc != "mxc://local.beeper.com/abc" || text != c.text)) {
			t.Errorf("case %d: got (%q, %q, %v), want ok=%v text=%q", i, mxc, text, ok, c.ok, c.text)
		}
	}
}

func TestEmoticonMXCHistory(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	att := map[string]any{"path": "4446367.emot_002.webp"}
	_ = s.put(ctx, "h2", "mxc://shelf/2", typeAnimEmote, att, false)
	_ = s.put(ctx, "h1", "mxc://shelf/1", typeAnimEmote, map[string]any{"path": "4446367.emot_001.webp"}, false)
	// The same emoticon bridged again from a chat gets a new mxc.
	_ = s.put(ctx, "h2", "mxc://chat/2", typeAnimEmote, att, true)
	for _, mxc := range []id.ContentURIString{"mxc://shelf/2", "mxc://chat/2"} {
		if emo, err := s.byMXC(ctx, mxc); err != nil || emo == nil || attStr(emo.Attachment, "path") != "4446367.emot_002.webp" {
			t.Errorf("byMXC(%s) = %+v, %v", mxc, emo, err)
		}
	}
	imgs, err := s.packImages(ctx, "4446367")
	if err != nil || len(imgs) != 2 || imgs[0].Path != "4446367.emot_001.webp" || imgs[1].MXC != "mxc://chat/2" {
		t.Errorf("packImages = %+v, %v", imgs, err)
	}
}
