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
	case event.MsgImage:
		data, dlErr := kc.Main.Bridge.Bot.DownloadMedia(ctx, msg.Content.URL, msg.Content.File)
		if dlErr != nil {
			return nil, fmt.Errorf("failed to download image from Matrix: %w", dlErr)
		}
		fileName := msg.Content.FileName
		if fileName == "" {
			fileName = msg.Content.Body
		}
		err = kc.call(ctx, "sendPhoto", map[string]any{
			"chat_id":  chatID,
			"data_b64": base64.StdEncoding.EncodeToString(data),
			"filename": fileName,
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
