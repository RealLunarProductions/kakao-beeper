package connector

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
)

var httpClient = &http.Client{Timeout: 2 * time.Minute}

const maxDownloadSize = 100 << 20

func downloadURL(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("unexpected status %d downloading media", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxDownloadSize {
		return nil, "", fmt.Errorf("media is larger than %d bytes", maxDownloadSize)
	}
	return data, resp.Header.Get("Content-Type"), nil
}

func makeAvatar(url *string) *bridgev2.Avatar {
	if url == nil || *url == "" {
		return nil
	}
	u := *url
	return &bridgev2.Avatar{
		ID: networkid.AvatarID(u),
		Get: func(ctx context.Context) ([]byte, error) {
			data, _, err := downloadURL(ctx, u)
			return data, err
		},
	}
}

func (kc *KakaoClient) getChat(ctx context.Context, chatID string) (*KakaoChat, error) {
	kc.cacheLock.RLock()
	chat, ok := kc.chats[chatID]
	kc.cacheLock.RUnlock()
	if ok {
		return chat, nil
	}
	chat = &KakaoChat{}
	if err := kc.call(ctx, "getChat", map[string]any{"chat_id": chatID}, chat); err != nil {
		return nil, err
	}
	kc.cacheChat(chat)
	return chat, nil
}

func (kc *KakaoClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	chatID := string(portal.ID)
	chat, err := kc.getChat(ctx, chatID)
	if err != nil {
		return nil, fmt.Errorf("failed to get chat: %w", err)
	}
	var members []KakaoMember
	membersFull := true
	if err = kc.call(ctx, "getMembers", map[string]any{"chat_id": chatID}, &members); err != nil {
		// Still bridge the chat; senders get added as their messages arrive.
		zerolog.Ctx(ctx).Warn().Err(err).Str("chat_id", chatID).Msg("Failed to get members, bridging chat without full member list")
		members = nil
		membersFull = false
	}

	info := &bridgev2.ChatInfo{
		Members: &bridgev2.ChatMemberList{
			IsFull:           membersFull,
			TotalMemberCount: len(members),
			MemberMap:        make(bridgev2.ChatMemberMap, len(members)),
		},
		CanBackfill: true,
	}
	kc.cacheLock.Lock()
	for _, member := range members {
		kc.members[member.UserID] = member
	}
	kc.cacheLock.Unlock()
	hasMe := false
	for _, member := range members {
		uid, err := strconv.ParseInt(member.UserID, 10, 64)
		if err != nil {
			continue
		}
		sender := kc.makeEventSender(uid)
		hasMe = hasMe || sender.IsFromMe
		info.Members.MemberMap[sender.Sender] = bridgev2.ChatMember{
			EventSender: sender,
			Membership:  event.MembershipJoin,
			UserInfo:    kc.memberUserInfo(member),
		}
		if !sender.IsFromMe {
			info.Members.OtherUserID = sender.Sender
		}
	}
	if !hasMe {
		// The member list leaves out the logged-in user; add them so Beeper shows the chat.
		uid, _ := strconv.ParseInt(string(kc.UserLogin.ID), 10, 64)
		me := kc.makeEventSender(uid)
		info.Members.MemberMap[me.Sender] = bridgev2.ChatMember{EventSender: me, Membership: event.MembershipJoin}
	}

	switch chat.TypeName() {
	case "DirectChat", "OD":
		info.Type = ptr.Ptr(database.RoomTypeDM)
	case "MemoChat":
		info.Type = ptr.Ptr(database.RoomTypeDefault)
		info.Name = ptr.Ptr("나와의 채팅")
		info.Members.OtherUserID = ""
	default:
		info.Type = ptr.Ptr(database.RoomTypeDefault)
		info.Members.OtherUserID = ""
		if chat.Title != nil && *chat.Title != "" {
			info.Name = chat.Title
		} else if chat.DisplayName != nil {
			info.Name = chat.DisplayName
		}
	}
	return info, nil
}

func (kc *KakaoClient) memberUserInfo(member KakaoMember) *bridgev2.UserInfo {
	return &bridgev2.UserInfo{
		Identifiers: []string{"kakaotalk:" + member.UserID},
		Name:        ptr.Ptr(member.Nickname),
		Avatar:      makeAvatar(member.ProfileImageURL),
	}
}

func (kc *KakaoClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	kc.cacheLock.RLock()
	member, ok := kc.members[string(ghost.ID)]
	kc.cacheLock.RUnlock()
	if !ok {
		// Profiles are only available per chat; GetChatInfo fills them in later.
		return nil, nil
	}
	return kc.memberUserInfo(member), nil
}

var roomCaps = &event.RoomFeatures{
	ID:            "kakaotalk.capabilities.2026_10_28",
	MaxTextLength: 10000,
	ReadReceipts:  true,
	File: event.FileFeatureMap{
		event.MsgImage: {
			MimeTypes: map[string]event.CapabilitySupportLevel{
				"image/jpeg": event.CapLevelFullySupported,
				"image/png":  event.CapLevelFullySupported,
				"image/gif":  event.CapLevelFullySupported,
				"image/*":    event.CapLevelPartialSupport, // sent as a file
			},
			Caption: event.CapLevelRejected,
			MaxSize: 30 << 20,
		},
		event.MsgVideo: {
			MimeTypes: map[string]event.CapabilitySupportLevel{
				"video/mp4": event.CapLevelFullySupported,
				"video/*":   event.CapLevelPartialSupport, // sent as a file
			},
			Caption: event.CapLevelRejected,
			MaxSize: 100 << 20,
		},
		event.MsgAudio: {
			MimeTypes: map[string]event.CapabilitySupportLevel{
				"audio/mp4":   event.CapLevelFullySupported,
				"audio/x-m4a": event.CapLevelFullySupported,
				"audio/aac":   event.CapLevelFullySupported,
				"audio/*":     event.CapLevelPartialSupport, // sent as a file
			},
			Caption: event.CapLevelRejected,
			MaxSize: 100 << 20,
		},
		event.MsgFile: {
			MimeTypes: map[string]event.CapabilitySupportLevel{
				"*/*": event.CapLevelFullySupported,
			},
			Caption: event.CapLevelRejected,
			MaxSize: 100 << 20,
		},
	},
}

func (kc *KakaoClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	return roomCaps
}
