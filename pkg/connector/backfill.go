package connector

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
)

var _ bridgev2.BackfillingNetworkAPI = (*KakaoClient)(nil)

// FetchMessages only supports forward backfill: LOCO pages forward from a log ID.
func (kc *KakaoClient) FetchMessages(ctx context.Context, params bridgev2.FetchMessagesParams) (*bridgev2.FetchMessagesResponse, error) {
	if !params.Forward {
		return nil, fmt.Errorf("backward backfill is not supported")
	}
	chatID := string(params.Portal.ID)
	var msgs []*KakaoMessage
	if params.AnchorMessage != nil {
		_, anchorLogID := parseMessageID(params.AnchorMessage.ID)
		var page struct {
			Messages []*KakaoMessage `json:"messages"`
		}
		err := kc.call(ctx, "getMessagePage", map[string]any{
			"chat_id": chatID, "from": anchorLogID, "count": min(max(params.Count, 1), 100),
		}, &page)
		if err != nil {
			return nil, err
		}
		anchorOrder := logStreamOrder(anchorLogID)
		for _, msg := range page.Messages {
			if logStreamOrder(msg.LogID) > anchorOrder {
				msgs = append(msgs, msg)
			}
		}
	} else {
		count := min(max(params.Count, 1), kc.Main.Config.InitialBackfill)
		err := kc.call(ctx, "getMessages", map[string]any{"chat_id": chatID, "count": count}, &msgs)
		if err != nil {
			return nil, err
		}
	}

	converted := make([]*bridgev2.BackfillMessage, 0, len(msgs))
	for _, msg := range msgs {
		msg.ChatID = chatID
		cm, err := kc.convertMessage(ctx, params.Portal, params.Portal.Bridge.Bot, msg)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).Str("log_id", msg.LogID).Msg("Failed to convert backfill message")
			continue
		}
		converted = append(converted, &bridgev2.BackfillMessage{
			ConvertedMessage: cm,
			Sender:           kc.makeEventSender(msg.AuthorID),
			ID:               makeMessageID(chatID, msg.LogID),
			Timestamp:        time.Unix(msg.SentAt, 0),
			StreamOrder:      logStreamOrder(msg.LogID),
		})
	}
	chat, _ := params.BundledData.(*KakaoChat)
	return &bridgev2.FetchMessagesResponse{
		Messages: converted,
		Forward:  true,
		MarkRead: chat != nil && chat.UnreadCount == 0,
	}, nil
}
