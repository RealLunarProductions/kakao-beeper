package connector

import (
	"context"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

// LOCO message types (agent-messenger KAKAO_MESSAGE_TYPE + node-kakao KnownChatType).
const (
	typeText       = 1
	typePhoto      = 2
	typeVideo      = 3
	typeContact    = 4
	typeAudio      = 5
	typeEmoticon   = 12
	typeFile       = 18
	typeAnimEmote  = 20
	typeSticker    = 25
	typeReply      = 26
	typeMultiPhoto = 27
)

func attStr(att map[string]any, key string) string {
	if v, ok := att[key].(string); ok {
		return v
	}
	return ""
}

func attInt(att map[string]any, key string) int {
	switch v := att[key].(type) {
	case float64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

func attStrList(att map[string]any, key string) []string {
	raw, _ := att[key].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func textPart(msgType event.MessageType, body string) *bridgev2.ConvertedMessagePart {
	return &bridgev2.ConvertedMessagePart{
		Type:    event.EventMessage,
		Content: &event.MessageEventContent{MsgType: msgType, Body: body},
	}
}

func (kc *KakaoClient) convertMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, msg *KakaoMessage) (*bridgev2.ConvertedMessage, error) {
	att := msg.Attachment
	if att == nil {
		att = map[string]any{}
	}
	cm := &bridgev2.ConvertedMessage{}
	switch msg.Kind() {
	case typeText:
		cm.Parts = []*bridgev2.ConvertedMessagePart{textPart(event.MsgText, msg.Message)}
	case typeReply:
		cm.Parts = []*bridgev2.ConvertedMessagePart{textPart(event.MsgText, msg.Message)}
		if src := attSrcLogID(att); src != "" {
			cm.ReplyTo = &networkid.MessageOptionalPartID{MessageID: makeMessageID(string(portal.ID), src)}
		}
	case typePhoto:
		cm.Parts = []*bridgev2.ConvertedMessagePart{
			kc.mediaPart(ctx, portal, intent, event.MsgImage, attStr(att, "url"), "photo.jpg", attInt(att, "w"), attInt(att, "h")),
		}
	case typeMultiPhoto:
		urls := attStrList(att, "imageUrls")
		for i, u := range urls {
			part := kc.mediaPart(ctx, portal, intent, event.MsgImage, u, fmt.Sprintf("photo-%d.jpg", i+1), 0, 0)
			part.ID = networkid.PartID(strconv.Itoa(i))
			cm.Parts = append(cm.Parts, part)
		}
		if len(cm.Parts) == 0 {
			cm.Parts = []*bridgev2.ConvertedMessagePart{textPart(event.MsgNotice, "[사진 묶음]")}
		}
	case typeVideo:
		cm.Parts = []*bridgev2.ConvertedMessagePart{
			kc.mediaPart(ctx, portal, intent, event.MsgVideo, attStr(att, "url"), "video.mp4", attInt(att, "w"), attInt(att, "h")),
		}
	case typeAudio:
		cm.Parts = []*bridgev2.ConvertedMessagePart{
			kc.mediaPart(ctx, portal, intent, event.MsgAudio, attStr(att, "url"), "audio.m4a", 0, 0),
		}
	case typeFile:
		name := attStr(att, "name")
		if name == "" {
			name = "file"
		}
		cm.Parts = []*bridgev2.ConvertedMessagePart{
			kc.mediaPart(ctx, portal, intent, event.MsgFile, attStr(att, "url"), name, 0, 0),
		}
	case typeEmoticon, typeAnimEmote, typeSticker:
		body := "(이모티콘)"
		if msg.Message != "" {
			body += " " + msg.Message
		}
		cm.Parts = []*bridgev2.ConvertedMessagePart{textPart(event.MsgNotice, body)}
	default:
		if msg.Message != "" {
			cm.Parts = []*bridgev2.ConvertedMessagePart{textPart(event.MsgText, msg.Message)}
		} else {
			cm.Parts = []*bridgev2.ConvertedMessagePart{
				textPart(event.MsgNotice, fmt.Sprintf("[Unsupported KakaoTalk message type %d]", msg.Kind())),
			}
		}
	}
	return cm, nil
}

func attSrcLogID(att map[string]any) string {
	switch v := att["src_logId"].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatInt(int64(v), 10)
	}
	return ""
}

// mediaPart reuploads a KakaoTalk CDN file to Matrix; on failure it falls back to a notice.
func (kc *KakaoClient) mediaPart(
	ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI,
	msgType event.MessageType, url, fileName string, width, height int,
) *bridgev2.ConvertedMessagePart {
	if url == "" {
		return textPart(event.MsgNotice, fmt.Sprintf("[%s without URL]", msgType))
	}
	data, mime, err := downloadURL(ctx, url)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to download KakaoTalk media")
		return textPart(event.MsgNotice, "[Failed to bridge media: "+err.Error()+"]")
	}
	if mime == "" || mime == "application/octet-stream" {
		mime = http.DetectContentType(data)
	}
	if ext := path.Ext(fileName); ext == "" || strings.HasPrefix(mime, "image/png") && ext == ".jpg" {
		fileName = strings.TrimSuffix(fileName, ext) + extForMime(mime)
	}
	mxc, file, err := intent.UploadMedia(ctx, portal.MXID, data, fileName, mime)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to upload KakaoTalk media to Matrix")
		return textPart(event.MsgNotice, "[Failed to bridge media: "+err.Error()+"]")
	}
	content := &event.MessageEventContent{
		MsgType:  msgType,
		Body:     fileName,
		FileName: fileName,
		URL:      mxc,
		File:     file,
		Info: &event.FileInfo{
			MimeType: mime,
			Size:     len(data),
			Width:    width,
			Height:   height,
		},
	}
	return &bridgev2.ConvertedMessagePart{Type: event.EventMessage, Content: content}
}

func extForMime(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/png"):
		return ".png"
	case strings.HasPrefix(mime, "image/gif"):
		return ".gif"
	case strings.HasPrefix(mime, "image/webp"):
		return ".webp"
	case strings.HasPrefix(mime, "image/"):
		return ".jpg"
	case strings.HasPrefix(mime, "video/"):
		return ".mp4"
	case strings.HasPrefix(mime, "audio/"):
		return ".m4a"
	}
	return ""
}
