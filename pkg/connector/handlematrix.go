package connector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
)

var (
	_ bridgev2.ReadReceiptHandlingNetworkAPI = (*KakaoClient)(nil)
	_ bridgev2.TypingHandlingNetworkAPI      = (*KakaoClient)(nil)
)

// Images up to this size are checked against the stored KakaoTalk emoticons.
const maxEmoticonCheckSize = 4 << 20

func (kc *KakaoClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	chatID := string(msg.Portal.ID)
	var res KakaoSendResult
	var err error
	switch msg.Content.MsgType {
	case event.MsgText, event.MsgNotice, event.MsgEmote:
		if echo := kc.takeEmoticonEcho(chatID, msg.Content.Body); echo != nil {
			zerolog.Ctx(ctx).Debug().Str("log_id", echo.logID).Msg("Not bridging the text Beeper sent with a forwarded emoticon")
			uid, _ := strconv.ParseInt(string(kc.UserLogin.ID), 10, 64)
			return &bridgev2.MatrixMessageResponse{DB: &database.Message{
				ID:        makeMessageID(chatID, echo.logID),
				PartID:    "caption",
				SenderID:  makeUserID(uid),
				Timestamp: time.Unix(echo.sentAt, 0),
			}}, nil
		}
		if mxc, text, ok := singleCustomEmoji(msg.Content); ok && kc.Main.emoticons != nil {
			// One of the user's KakaoTalk emoticons picked from Beeper's custom emoji.
			if emo, lerr := kc.Main.emoticons.byMXC(ctx, mxc); lerr != nil {
				zerolog.Ctx(ctx).Warn().Err(lerr).Msg("Failed to look up custom emoji")
			} else if emo != nil {
				if r, done, serr := kc.trySendEmoticon(ctx, chatID, emo, text); done {
					res, err = r, serr
					break
				}
			}
		}
		text := msg.Content.Body
		if msg.Content.MsgType == event.MsgEmote {
			text = "* " + text
		}
		err = kc.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, &res)
	case event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile, event.CapMsgSticker:
		res, err = kc.sendMatrixMedia(ctx, chatID, msg.Content)
	default:
		return nil, fmt.Errorf("%w %s", bridgev2.ErrUnsupportedMessageType, msg.Content.MsgType)
	}
	if err != nil {
		return nil, err
	}
	if !res.Success || res.LogID == "" {
		return nil, fmt.Errorf("KakaoTalk did not accept the message")
	}
	uid, _ := strconv.ParseInt(string(kc.UserLogin.ID), 10, 64)
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        makeMessageID(chatID, res.LogID),
			SenderID:  makeUserID(uid),
			Timestamp: time.Unix(res.SentAt, 0),
		},
		StreamOrder: logStreamOrder(res.LogID),
	}, nil
}

// sendMatrixMedia sends a Matrix attachment to KakaoTalk. A sticker or image that
// is a known KakaoTalk emoticon goes out as that emoticon; anything else is
// uploaded, and a caption follows as its own text message (KakaoTalk media has
// no captions).
func (kc *KakaoClient) sendMatrixMedia(ctx context.Context, chatID string, content *event.MessageEventContent) (KakaoSendResult, error) {
	log := zerolog.Ctx(ctx)
	var res KakaoSendResult
	mimeType := ""
	size := 0
	if content.Info != nil {
		mimeType, size = content.Info.MimeType, content.Info.Size
	}
	isImage := content.MsgType == event.MsgImage || content.MsgType == event.CapMsgSticker
	var matched *storedEmoticon
	defer func() {
		if matched != nil && res.Success && res.LogID != "" {
			kc.emoticonEchoes.Store(chatID, &emoticonEcho{at: time.Now(), logID: res.LogID, sentAt: res.SentAt, emo: matched})
		}
	}()

	if isImage && kc.Main.emoticons != nil {
		if emo, err := kc.Main.emoticons.byMXC(ctx, contentMXC(content)); err != nil {
			log.Warn().Err(err).Msg("Failed to look up emoticon by mxc")
		} else if emo != nil {
			matched = emo
			if r, done, err := kc.trySendEmoticon(ctx, chatID, emo, ""); done {
				res = r
				return res, err
			}
		}
	}

	data, err := kc.Main.Bridge.Bot.DownloadMedia(ctx, content.URL, content.File)
	if err != nil {
		return res, fmt.Errorf("failed to download %s from Matrix: %w", content.MsgType, err)
	}
	if isImage && kc.Main.emoticons != nil && len(data) <= maxEmoticonCheckSize {
		// Beeper may re-upload a forwarded sticker, so match on the image itself too.
		if emo, err := kc.Main.emoticons.byHash(ctx, imageHash(data)); err != nil {
			log.Warn().Err(err).Msg("Failed to look up emoticon by hash")
		} else if emo != nil {
			matched = emo
			if r, done, err := kc.trySendEmoticon(ctx, chatID, emo, ""); done {
				res = r
				return res, err
			}
		}
	}
	if isImage && matched == nil && kc.Main.emoticons != nil {
		if emo, err := kc.Main.emoticons.byFileName(ctx, content.FileName); err != nil {
			log.Warn().Err(err).Msg("Failed to look up emoticon by file name")
		} else if emo != nil {
			matched = emo
			if r, done, err := kc.trySendEmoticon(ctx, chatID, emo, ""); done {
				res = r
				return res, err
			}
		}
	}
	if size == 0 {
		size = len(data)
	}

	fileName := content.FileName
	if fileName == "" {
		fileName = content.Body
	}
	if fileName == "" {
		fileName = "file"
	}
	caption := ""
	if content.FileName != "" && content.Body != content.FileName && content.MsgType != event.CapMsgSticker {
		caption = strings.TrimSpace(content.Body)
	}

	// Hand the bytes to the sidecar through a private temp file, not base64 in the
	// JSON request: that took ~4 copies of a large file in memory.
	path, err := kc.writeOutbox(data)
	if err != nil {
		return res, err
	}
	defer os.Remove(path)
	data = nil

	msgType := content.MsgType
	if msgType == event.CapMsgSticker {
		msgType = event.MsgImage
	}
	kind := mediaKind(msgType, mimeType)
	if kind == "file" && isVoiceRecording(fileName) {
		// Phone recordings (.m4a) often arrive labelled video/3gpp or as files;
		// KakaoTalk plays them as voice messages.
		kind = "audio"
	}
	params := map[string]any{
		"chat_id":  chatID,
		"kind":     kind,
		"path":     path,
		"filename": fileName,
		"mime":     mimeType,
	}
	err = kc.call(ctx, "sendMedia", params, &res)
	// KakaoTalk refuses some photos/videos as media (e.g. long videos); a plain
	// file still gets through.
	if params["kind"] != "file" && !errors.Is(err, ErrSidecarExited) && (err != nil || !res.Success || res.LogID == "") {
		log.Warn().Err(err).Any("kind", params["kind"]).Int("size", size).Msg("KakaoTalk rejected the media, sending it as a file")
		params["kind"] = "file"
		res = KakaoSendResult{}
		err = kc.call(ctx, "sendMedia", params, &res)
	}
	if err == nil && res.Success && caption != "" {
		var capRes KakaoSendResult
		if capErr := kc.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": caption}, &capRes); capErr != nil {
			log.Warn().Err(capErr).Msg("Failed to send media caption")
		}
	}
	return res, err
}

// trySendEmoticon sends a stored emoticon, with text when given. done is false when there is nothing to
// send or KakaoTalk refused it (e.g. a pack the user doesn't own), so the caller
// sends the picture instead.
func (kc *KakaoClient) trySendEmoticon(ctx context.Context, chatID string, emo *storedEmoticon, text string) (res KakaoSendResult, done bool, err error) {
	if emo == nil {
		return res, false, nil
	}
	log := zerolog.Ctx(ctx)
	log.Debug().Int("kind", emo.Kind).Any("attachment", emo.Attachment).Msg("Sending KakaoTalk emoticon")
	params := map[string]any{
		"chat_id":    chatID,
		"type":       emo.Kind,
		"attachment": emo.Attachment,
	}
	if text != "" {
		params["text"] = text
	}
	err = kc.call(ctx, "sendEmoticon", params, &res)
	if errors.Is(err, ErrSidecarExited) {
		return res, true, err
	}
	if err != nil || !res.Success || res.LogID == "" {
		log.Warn().Err(err).Any("attachment", emo.Attachment).Msg("KakaoTalk refused the emoticon, sending the picture")
		return KakaoSendResult{}, false, nil
	}
	return res, true, nil
}

func (kc *KakaoClient) writeOutbox(data []byte) (string, error) {
	dir := filepath.Join(kc.Main.Config.StateDir, "outbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create outbox: %w", err)
	}
	f, err := os.CreateTemp(dir, "upload-*")
	if err != nil {
		return "", fmt.Errorf("failed to create outbox file: %w", err)
	}
	if _, err = f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", fmt.Errorf("failed to write outbox file: %w", err)
	}
	if err = f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func (kc *KakaoClient) HandleMatrixReadReceipt(ctx context.Context, msg *bridgev2.MatrixReadReceipt) error {
	if msg.ExactMessage == nil {
		return nil
	}
	chatID, logID := parseMessageID(msg.ExactMessage.ID)
	if logID == "" {
		return nil
	}
	return kc.call(ctx, "markRead", map[string]any{"chat_id": chatID, "log_id": logID}, nil)
}

func (kc *KakaoClient) HandleMatrixTyping(ctx context.Context, msg *bridgev2.MatrixTyping) error {
	if !msg.IsTyping {
		return nil
	}
	return kc.call(ctx, "sendTyping", map[string]any{"chat_id": string(msg.Portal.ID)}, nil)
}

// mediaKind picks the KakaoTalk message type for a Matrix attachment. KakaoTalk
// clients only render a few formats inline (photos, mp4 video, m4a voice notes);
// anything else goes out as a plain file so the recipient can still open it.
func mediaKind(msgType event.MessageType, mimeType string) string {
	switch {
	case msgType == event.MsgImage && kakaoPhotoMimes[mimeType]:
		return "photo"
	case msgType == event.MsgVideo && mimeType == "video/mp4":
		return "video"
	case msgType == event.MsgAudio && kakaoAudioMimes[mimeType]:
		return "audio"
	default:
		return "file"
	}
}

var kakaoPhotoMimes = map[string]bool{"image/jpeg": true, "image/png": true, "image/gif": true}

var kakaoAudioMimes = map[string]bool{"audio/mp4": true, "audio/x-m4a": true, "audio/m4a": true, "audio/aac": true}

func isVoiceRecording(fileName string) bool {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".m4a", ".aac":
		return true
	}
	return false
}

// Beeper forwards a sticker as an image followed by its body ("(이모티콘)") as a
// separate text message. emoticonEcho remembers the emoticon just sent so that
// text can be recorded as part of it instead of being sent to KakaoTalk.
type emoticonEcho struct {
	at     time.Time
	logID  string
	sentAt int64
	emo    *storedEmoticon
}

const emoticonEchoWindow = 20 * time.Second

func (kc *KakaoClient) takeEmoticonEcho(chatID, body string) *emoticonEcho {
	v, ok := kc.emoticonEchoes.Load(chatID)
	if !ok {
		return nil
	}
	echo := v.(*emoticonEcho)
	if time.Since(echo.at) > emoticonEchoWindow {
		kc.emoticonEchoes.CompareAndDelete(chatID, v)
		return nil
	}
	body = strings.TrimSpace(body)
	placeholders := []string{"(이모티콘)", "(Emoticons)", "(emoticon)", attStr(echo.emo.Attachment, "name"), attStr(echo.emo.Attachment, "alt")}
	for _, p := range placeholders {
		if p != "" && body == p {
			kc.emoticonEchoes.CompareAndDelete(chatID, v)
			return echo
		}
	}
	return nil
}
