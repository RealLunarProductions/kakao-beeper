package connector

import (
	"bytes"
	"context"
	"encoding/binary"
	"net/http"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

// emoticonPart bridges a KakaoTalk emoticon as a Matrix sticker, or returns nil if
// it has no image path or the image can't be fetched.
func (kc *KakaoClient) emoticonPart(
	ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, att map[string]any,
) *bridgev2.ConvertedMessagePart {
	p := attStr(att, "path")
	if p == "" {
		return nil
	}
	data, _, err := downloadURL(ctx, emoticonCDN+p)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to download KakaoTalk emoticon")
		return nil
	}
	data = decryptEmoticon(data)
	mime := http.DetectContentType(data)
	width, height := imageSize(data)
	part := kc.uploadPart(ctx, portal, intent, event.MsgImage, data, mime, p, width, height)
	if part.Content.MsgType != event.MsgImage {
		return nil
	}
	part.Type = event.EventSticker
	part.Content.MsgType = ""
	part.Content.Body = "(이모티콘)"
	if alt := attStr(att, "alt"); alt != "" {
		part.Content.Body = alt
	}
	return part
}

// The item CDN serves emoticons with their first 128 bytes XORed with an LFSR
// keystream seeded from a fixed key (as decoded by KakaoTalkEmoticonDownloader
// and sticker-convert). Files that already look like an image are left alone.
const (
	emoticonKey        = "a271730728cbe141e47fd9d677e9006d"
	emoticonCryptBytes = 128
)

func decryptEmoticon(data []byte) []byte {
	if len(data) < emoticonCryptBytes || looksLikeImage(data) {
		return data
	}
	out := bytes.Clone(data)
	k := []byte(emoticonKey)
	s := [3]uint32{
		binary.BigEndian.Uint32(k[0:4]),
		binary.BigEndian.Uint32(k[4:8]),
		binary.BigEndian.Uint32(k[8:12]),
	}
	for i := range emoticonCryptBytes {
		out[i] ^= emoticonKeyByte(&s)
	}
	return out
}

func emoticonKeyByte(s *[3]uint32) byte {
	flag1, flag2 := uint32(1), uint32(0)
	var result uint32
	for range 8 {
		if s[0]&1 != 0 {
			s[0] = s[0]>>1 ^ 0xC0000031
			if s[1]&1 != 0 {
				s[1] = (s[1]>>1 | 0xC0000000) ^ 0x20000010
				flag1 = 1
			} else {
				s[1] = s[1] >> 1 & 0x3FFFFFFF
				flag1 = 0
			}
		} else {
			s[0] >>= 1
			if s[2]&1 != 0 {
				s[2] = (s[2]>>1 | 0xF0000000) ^ 0x08000001
				flag2 = 1
			} else {
				s[2] = s[2] >> 1 & 0x0FFFFFFF
				flag2 = 0
			}
		}
		result = (flag1 ^ flag2) | result<<1
	}
	return byte(result)
}

func looksLikeImage(data []byte) bool {
	return bytes.HasPrefix(data, []byte("RIFF")) || bytes.HasPrefix(data, []byte("GIF8")) ||
		bytes.HasPrefix(data, []byte("\x89PNG"))
}

// imageSize reads the canvas size from a WebP, GIF or PNG header (0, 0 if unknown).
func imageSize(data []byte) (int, int) {
	switch {
	case len(data) >= 30 && bytes.HasPrefix(data, []byte("RIFF")) && string(data[12:16]) == "VP8X":
		w := int(data[24]) | int(data[25])<<8 | int(data[26])<<16
		h := int(data[27]) | int(data[28])<<8 | int(data[29])<<16
		return w + 1, h + 1
	case len(data) >= 10 && bytes.HasPrefix(data, []byte("GIF8")):
		return int(binary.LittleEndian.Uint16(data[6:8])), int(binary.LittleEndian.Uint16(data[8:10]))
	case len(data) >= 24 && bytes.HasPrefix(data, []byte("\x89PNG")):
		return int(binary.BigEndian.Uint32(data[16:20])), int(binary.BigEndian.Uint32(data[20:24]))
	}
	return 0, 0
}
