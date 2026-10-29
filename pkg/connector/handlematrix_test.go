package connector

import (
	"testing"

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
