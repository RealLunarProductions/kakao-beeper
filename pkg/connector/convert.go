package connector

import (
	"context"
	"encoding/json"
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
	typeFeed       = 0
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
	typeCall       = 51

	// Set on the type of a message its sender deleted for everyone.
	flagDeleted = 16384
)

// Feed (system) message types; the payload is JSON in the message text.
const (
	feedInvite       = 1
	feedLeave        = 2
	feedSecretLeave  = 3
	feedOpenJoin     = 4
	feedOpenKicked   = 6
	feedDeleteToAll  = 14
	feedHandOverHost = 15
)

// Emoticon attachments carry a CDN path like "4412207.emot_001.webp".
const emoticonCDN = "https://item.kakaocdn.net/dw/"

type feedMember struct {
	NickName string `json:"nickName"`
}

type kakaoFeed struct {
	FeedType int          `json:"feedType"`
	LogID    json.Number  `json:"logId"`
	Hidden   bool         `json:"hidden"`
	Inviter  *feedMember  `json:"inviter"`
	Member   *feedMember  `json:"member"`
	Members  []feedMember `json:"members"`
}

// parseFeed returns the payload of a feed message, or nil for anything else.
func parseFeed(msg *KakaoMessage) *kakaoFeed {
	if msg.BaseKind() != typeFeed || msg.Message == "" {
		return nil
	}
	var feed kakaoFeed
	if json.Unmarshal([]byte(msg.Message), &feed) != nil {
		return nil
	}
	return &feed
}

func feedNames(feed *kakaoFeed) string {
	names := make([]string, 0, len(feed.Members)+1)
	for _, m := range feed.Members {
		names = append(names, m.NickName)
	}
	if feed.Member != nil {
		names = append(names, feed.Member.NickName)
	}
	return strings.Join(names, ", ")
}

// feedText mirrors the system lines KakaoTalk shows in the chat.
func feedText(feed *kakaoFeed) string {
	names := feedNames(feed)
	switch feed.FeedType {
	case feedInvite:
		if feed.Inviter != nil && feed.Inviter.NickName != "" {
			return fmt.Sprintf("%s님이 %s님을 초대했습니다.", feed.Inviter.NickName, names)
		}
		return names + "님이 들어왔습니다."
	case feedLeave, feedSecretLeave:
		return names + "님이 나갔습니다."
	case feedOpenJoin:
		return names + "님이 들어왔습니다."
	case feedOpenKicked:
		return names + "님을 내보냈습니다."
	case feedHandOverHost:
		return names + "님이 방장이 되었습니다."
	}
	return fmt.Sprintf("[KakaoTalk 알림 %d]", feed.FeedType)
}

// callText turns a VoIP log entry ({"type":"v_bye","duration":886,...}) into a line
// like the ones KakaoTalk shows.
func callText(msg *KakaoMessage, att map[string]any) string {
	kind, secs := attStr(att, "type"), attInt(att, "duration")
	if kind == "" {
		var info struct {
			Type     string `json:"type"`
			Duration int    `json:"duration"`
		}
		_ = json.Unmarshal([]byte(msg.Message), &info)
		kind, secs = info.Type, info.Duration
	}
	switch kind {
	case "v_invite":
		return "📞 통화 시작"
	case "v_bye":
		if secs > 0 {
			return "📞 통화 종료 (" + formatDuration(secs) + ")"
		}
		return "📞 통화 종료"
	case "v_canceled":
		return "📞 통화 취소"
	case "v_noanswer", "v_missed":
		return "📞 부재중 통화"
	case "v_decline", "v_deny", "v_reject":
		return "📞 통화 거절"
	case "v_busy":
		return "📞 상대방이 통화 중"
	}
	return "📞 통화"
}

func formatDuration(secs int) string {
	if secs >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", secs/3600, secs/60%60, secs%60)
	}
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

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
	switch msg.BaseKind() {
	case typeFeed:
		body := msg.Message
		if feed := parseFeed(msg); feed != nil {
			body = feedText(feed)
		}
		cm.Parts = []*bridgev2.ConvertedMessagePart{textPart(event.MsgNotice, body)}
	case typeCall:
		cm.Parts = []*bridgev2.ConvertedMessagePart{textPart(event.MsgNotice, callText(msg, att))}
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
		zerolog.Ctx(ctx).Debug().Interface("attachment", att).Msg("KakaoTalk emoticon")
		if sticker := kc.emoticonPart(ctx, portal, intent, att); sticker != nil {
			cm.Parts = []*bridgev2.ConvertedMessagePart{sticker}
			if msg.Message != "" {
				caption := textPart(event.MsgText, msg.Message)
				caption.ID = "caption"
				cm.Parts = append(cm.Parts, caption)
			}
			break
		}
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
				textPart(event.MsgNotice, fmt.Sprintf("[Unsupported KakaoTalk message type %d]", msg.BaseKind())),
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

// emoticonPart bridges a KakaoTalk emoticon as a Matrix sticker, or returns nil if
// it has no image path or the image can't be fetched.
func (kc *KakaoClient) emoticonPart(
	ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, att map[string]any,
) *bridgev2.ConvertedMessagePart {
	p := attStr(att, "path")
	if p == "" {
		return nil
	}
	part := kc.mediaPart(ctx, portal, intent, event.MsgImage, emoticonCDN+p, p, attInt(att, "width"), attInt(att, "height"))
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
