package connector

import (
	"strconv"
	"strings"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
)

func makeUserID(kakaoUserID int64) networkid.UserID {
	return networkid.UserID(strconv.FormatInt(kakaoUserID, 10))
}

func makeUserLoginID(kakaoUserID string) networkid.UserLoginID {
	return networkid.UserLoginID(kakaoUserID)
}

func makePortalID(chatID string) networkid.PortalID {
	return networkid.PortalID(chatID)
}

// Log IDs are only guaranteed unique within a chat, so message IDs are chatID:logID.
func makeMessageID(chatID, logID string) networkid.MessageID {
	return networkid.MessageID(chatID + ":" + logID)
}

func parseMessageID(id networkid.MessageID) (chatID, logID string) {
	chatID, logID, _ = strings.Cut(string(id), ":")
	return
}

// Log IDs increase monotonically within a chat, which makes them a good stream order.
func logStreamOrder(logID string) int64 {
	order, _ := strconv.ParseInt(logID, 10, 64)
	return order
}

func (kc *KakaoClient) makePortalKey(chatID string) networkid.PortalKey {
	return networkid.PortalKey{
		ID:       makePortalID(chatID),
		Receiver: kc.UserLogin.ID,
	}
}

func (kc *KakaoClient) makeEventSender(kakaoUserID int64) bridgev2.EventSender {
	isMe := strconv.FormatInt(kakaoUserID, 10) == string(kc.UserLogin.ID)
	sender := bridgev2.EventSender{
		IsFromMe: isMe,
		Sender:   makeUserID(kakaoUserID),
	}
	if isMe {
		sender.SenderLogin = kc.UserLogin.ID
	}
	return sender
}
