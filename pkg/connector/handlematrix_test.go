package connector

import (
	"testing"
	"time"

	"maunium.net/go/mautrix/event"
)

func TestMediaKind(t *testing.T) {
	cases := []struct {
		msgType event.MessageType
		mime    string
		want    string
	}{
		{event.MsgImage, "image/jpeg", "photo"},
		{event.MsgImage, "image/webp", "file"},
		{event.MsgVideo, "video/mp4", "video"},
		{event.MsgVideo, "video/quicktime", "file"},
		{event.MsgAudio, "audio/mp4", "audio"},
		{event.MsgAudio, "audio/ogg", "file"},
		{event.MsgFile, "application/pdf", "file"},
		{event.MsgFile, "", "file"},
	}
	for _, c := range cases {
		if got := mediaKind(c.msgType, c.mime); got != c.want {
			t.Errorf("mediaKind(%s, %q) = %q, want %q", c.msgType, c.mime, got, c.want)
		}
	}
}

func TestEmoticonEcho(t *testing.T) {
	kc := &KakaoClient{}
	emo := &storedEmoticon{Kind: typeAnimEmote, Attachment: map[string]any{"name": "(Emoticons)", "path": "1.emot_001.webp"}}
	kc.emoticonEchoes.Store("chat", &emoticonEcho{at: time.Now(), logID: "42", emo: emo})
	if kc.takeEmoticonEcho("other", "(이모티콘)") != nil {
		t.Error("echo matched in another chat")
	}
	if kc.takeEmoticonEcho("chat", "hello") != nil {
		t.Error("real text was swallowed")
	}
	if echo := kc.takeEmoticonEcho("chat", " (이모티콘) "); echo == nil || echo.logID != "42" {
		t.Fatalf("placeholder not matched: %+v", echo)
	}
	if kc.takeEmoticonEcho("chat", "(이모티콘)") != nil {
		t.Error("echo used twice")
	}
	kc.emoticonEchoes.Store("chat", &emoticonEcho{at: time.Now().Add(-time.Minute), logID: "43", emo: emo})
	if kc.takeEmoticonEcho("chat", "(Emoticons)") != nil {
		t.Error("stale echo matched")
	}
}

func TestIsVoiceRecording(t *testing.T) {
	for name, want := range map[string]bool{"음성 261028_183712.m4a": true, "a.AAC": true, "clip.mp4": false, "file": false} {
		if isVoiceRecording(name) != want {
			t.Errorf("isVoiceRecording(%q) != %v", name, want)
		}
	}
}
