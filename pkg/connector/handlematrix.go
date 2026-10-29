package connector

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
)

var (
	_ bridgev2.ReadReceiptHandlingNetworkAPI = (*KakaoClient)(nil)
	_ bridgev2.TypingHandlingNetworkAPI      = (*KakaoClient)(nil)
)

func (kc *KakaoClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	chatID := string(msg.Portal.ID)
	var res KakaoSendResult
	var err error
	switch msg.Content.MsgType {
	case event.MsgText, event.MsgNotice, event.MsgEmote:
		text := msg.Content.Body
		if msg.Content.MsgType == event.MsgEmote {
			text = "* " + text
		}
		err = kc.call(ctx, "sendMessage", map[string]any{"chat_id": chatID, "text": text}, &res)
	case event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile:
		data, dlErr := kc.Main.Bridge.Bot.DownloadMedia(ctx, msg.Content.URL, msg.Content.File)
		if dlErr != nil {
			return nil, fmt.Errorf("failed to download %s from Matrix: %w", msg.Content.MsgType, dlErr)
		}
		fileName := msg.Content.FileName
		if fileName == "" {
			fileName = msg.Content.Body
		}
		var mimeType string
		if msg.Content.Info != nil {
			mimeType = msg.Content.Info.MimeType
		}
		err = kc.call(ctx, "sendMedia", map[string]any{
			"chat_id":  chatID,
			"kind":     mediaKind(msg.Content.MsgType, mimeType),
			"data_b64": base64.StdEncoding.EncodeToString(data),
			"filename": fileName,
			"mime":     mimeType,
		}, &res)
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
