package connector

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/bridgev2/status"
)

type KakaoProfile struct {
	UserID          string `json:"user_id"`
	Nickname        string `json:"nickname"`
	ProfileImageURL string `json:"profile_image_url"`
}

type KakaoChat struct {
	ChatID        string          `json:"chat_id"`
	Type          json.RawMessage `json:"type"`
	DisplayName   *string         `json:"display_name"`
	Title         *string         `json:"title"`
	ActiveMembers int             `json:"active_members"`
	UnreadCount   int             `json:"unread_count"`
	LastMessage   *struct {
		SentAt int64 `json:"sent_at"`
	} `json:"last_message"`
}

// TypeName returns the chat type as a string (KakaoTalk sends either names or numbers).
func (c *KakaoChat) TypeName() string {
	var name string
	if json.Unmarshal(c.Type, &name) == nil {
		return name
	}
	return string(c.Type)
}

type KakaoMember struct {
	UserID          string  `json:"user_id"`
	Nickname        string  `json:"nickname"`
	ProfileImageURL *string `json:"profile_image_url"`
}

// KakaoMessage covers both history entries and live pushes. History entries carry
// the LOCO message type in "type"; pushes use "type" for the push kind ("MSG")
// and carry the message type in "message_type".
type KakaoMessage struct {
	ChatID      string          `json:"chat_id"`
	LogID       string          `json:"log_id"`
	Type        json.RawMessage `json:"type"`
	MessageType int             `json:"message_type"`
	AuthorID    int64          `json:"author_id"`
	AuthorName  *string        `json:"author_name"`
	Message     string         `json:"message"`
	Attachment  map[string]any `json:"attachment"`
	SentAt      int64          `json:"sent_at"`
}

func (m *KakaoMessage) Kind() int {
	if m.MessageType != 0 {
		return m.MessageType
	}
	var kind int
	_ = json.Unmarshal(m.Type, &kind)
	return kind
}

type KakaoReadEvent struct {
	ChatID    string `json:"chat_id"`
	UserID    int64  `json:"user_id"`
	Watermark string `json:"watermark"`
}

type KakaoSendResult struct {
	Success bool   `json:"success"`
	ChatID  string `json:"chat_id"`
	LogID   string `json:"log_id"`
	SentAt  int64  `json:"sent_at"`
}

type KakaoClient struct {
	Main      *KakaoConnector
	UserLogin *bridgev2.UserLogin

	scLock   sync.RWMutex
	sidecar  *Sidecar
	stopping bool

	cacheLock sync.RWMutex
	chats     map[string]*KakaoChat
	members   map[string]KakaoMember
}

var _ bridgev2.NetworkAPI = (*KakaoClient)(nil)

func (kc *KakaoClient) sc() *Sidecar {
	kc.scLock.RLock()
	defer kc.scLock.RUnlock()
	return kc.sidecar
}

func (kc *KakaoClient) call(ctx context.Context, method string, params, out any) error {
	sc := kc.sc()
	if sc == nil {
		return ErrSidecarExited
	}
	return sc.Call(ctx, method, params, out)
}

func (kc *KakaoClient) Connect(ctx context.Context) {
	log := kc.UserLogin.Log.With().Str("component", "kakaotalk").Logger()
	kc.scLock.Lock()
	kc.stopping = false
	kc.scLock.Unlock()
	kc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnecting})

	sc, err := StartSidecar(&kc.Main.Config, log, func(name string, data json.RawMessage) {
		kc.handleEvent(log.WithContext(context.Background()), name, data)
	})
	if err != nil {
		log.Err(err).Msg("Failed to start sidecar")
		kc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateUnknownError, Error: "kakao-sidecar-start-failed", Message: err.Error()})
		return
	}
	kc.scLock.Lock()
	kc.sidecar = sc
	kc.scLock.Unlock()
	go kc.watchSidecar(log, sc)

	var profile KakaoProfile
	if err = sc.Call(ctx, "start", nil, &profile); err != nil {
		log.Err(err).Msg("Failed to log in to KakaoTalk")
		kc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateBadCredentials, Error: "kakao-login-failed", Message: err.Error()})
		return
	}
	kc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
	go kc.syncChats(log.WithContext(context.WithoutCancel(ctx)))
}

// watchSidecar restarts the sidecar if it dies while we're supposed to be connected.
func (kc *KakaoClient) watchSidecar(log zerolog.Logger, sc *Sidecar) {
	<-sc.Done
	kc.scLock.Lock()
	stopping := kc.stopping
	if kc.sidecar == sc {
		kc.sidecar = nil
	}
	kc.scLock.Unlock()
	if stopping {
		return
	}
	log.Warn().Msg("Sidecar exited unexpectedly, restarting in 10 seconds")
	kc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: "kakao-sidecar-exited"})
	time.Sleep(10 * time.Second)
	kc.scLock.RLock()
	stopping = kc.stopping
	kc.scLock.RUnlock()
	if !stopping {
		kc.Connect(log.WithContext(context.Background()))
	}
}

func (kc *KakaoClient) syncChats(ctx context.Context) {
	log := zerolog.Ctx(ctx)
	var chats []*KakaoChat
	// all=false: the login snapshot is complete on the sidecar's first start and
	// only contains changed chats afterwards, which is exactly what a resync needs.
	if err := kc.call(ctx, "getChats", map[string]any{"all": false}, &chats); err != nil {
		log.Err(err).Msg("Failed to fetch chat list")
		return
	}
	log.Info().Int("count", len(chats)).Msg("Syncing chats")
	for _, chat := range chats {
		kc.cacheChat(chat)
		var latest time.Time
		if chat.LastMessage != nil {
			latest = time.Unix(chat.LastMessage.SentAt, 0)
		}
		kc.UserLogin.QueueRemoteEvent(&simplevent.ChatResync{
			EventMeta: simplevent.EventMeta{
				Type: bridgev2.RemoteEventChatResync,
				LogContext: func(c zerolog.Context) zerolog.Context {
					return c.Str("chat_id", chat.ChatID)
				},
				PortalKey:    kc.makePortalKey(chat.ChatID),
				CreatePortal: true,
			},
			LatestMessageTS:     latest,
			BundledBackfillData: chat,
		})
	}
}

func (kc *KakaoClient) cacheChat(chat *KakaoChat) {
	kc.cacheLock.Lock()
	defer kc.cacheLock.Unlock()
	if kc.chats == nil {
		kc.chats = make(map[string]*KakaoChat)
	}
	kc.chats[chat.ChatID] = chat
}

func (kc *KakaoClient) handleEvent(ctx context.Context, name string, data json.RawMessage) {
	log := zerolog.Ctx(ctx)
	switch name {
	case "message":
		var msg KakaoMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Err(err).Msg("Failed to parse message event")
			return
		}
		kc.handleMessage(&msg)
	case "read":
		var evt KakaoReadEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			log.Err(err).Msg("Failed to parse read event")
			return
		}
		kc.UserLogin.QueueRemoteEvent(&simplevent.Receipt{
			EventMeta: simplevent.EventMeta{
				Type:      bridgev2.RemoteEventReadReceipt,
				PortalKey: kc.makePortalKey(evt.ChatID),
				Sender:    kc.makeEventSender(evt.UserID),
			},
			LastTarget: makeMessageID(evt.ChatID, evt.Watermark),
		})
	case "connected":
		kc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})
	case "disconnected":
		kc.UserLogin.BridgeState.Send(status.BridgeState{StateEvent: status.StateTransientDisconnect, Error: "kakao-disconnected"})
	case "error":
		log.Warn().RawJSON("data", data).Msg("Sidecar reported an error")
	default:
		log.Debug().Str("event", name).Msg("Ignoring sidecar event")
	}
}

func (kc *KakaoClient) handleMessage(msg *KakaoMessage) {
	if msg.AuthorName != nil && *msg.AuthorName != "" {
		key := strconv.FormatInt(msg.AuthorID, 10)
		kc.cacheLock.Lock()
		if _, ok := kc.members[key]; !ok {
			kc.members[key] = KakaoMember{UserID: key, Nickname: *msg.AuthorName}
		}
		kc.cacheLock.Unlock()
	}
	kc.UserLogin.QueueRemoteEvent(&simplevent.Message[*KakaoMessage]{
		EventMeta: simplevent.EventMeta{
			Type: bridgev2.RemoteEventMessage,
			LogContext: func(c zerolog.Context) zerolog.Context {
				return c.Str("chat_id", msg.ChatID).Str("log_id", msg.LogID).Int("kakao_type", msg.Kind())
			},
			PortalKey:    kc.makePortalKey(msg.ChatID),
			Sender:       kc.makeEventSender(msg.AuthorID),
			CreatePortal: true,
			Timestamp:    time.Unix(msg.SentAt, 0),
			StreamOrder:  logStreamOrder(msg.LogID),
		},
		Data:               msg,
		ID:                 makeMessageID(msg.ChatID, msg.LogID),
		ConvertMessageFunc: kc.convertMessage,
	})
}

func (kc *KakaoClient) Disconnect() {
	kc.scLock.Lock()
	kc.stopping = true
	sc := kc.sidecar
	kc.sidecar = nil
	kc.scLock.Unlock()
	if sc != nil {
		sc.Stop()
	}
}

func (kc *KakaoClient) IsLoggedIn() bool {
	return kc.sc() != nil
}

// LogoutRemote only drops the bridge session; the tablet-slot credentials stay
// managed by `agent-kakaotalk auth logout`.
func (kc *KakaoClient) LogoutRemote(ctx context.Context) {
	kc.Disconnect()
}

func (kc *KakaoClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return string(userID) == string(kc.UserLogin.ID)
}
