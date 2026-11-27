package realtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

type browserSub struct {
	id     uint64
	userID string
	ch     chan domain.BrowserEvent
}

// SocketEnvelope は Slack Socket Mode の events_api エンベロープを表す。
type SocketEnvelope struct {
	EnvelopeID             string                 `json:"envelope_id"`
	Type                   string                 `json:"type"`
	AcceptsResponsePayload bool                   `json:"accepts_response_payload"`
	Payload                map[string]interface{} `json:"payload"`
}

type socketConn struct {
	id   uint64
	sess usecase.SocketSession
}

type ticketEntry struct {
	expiresAt time.Time
}

// Hub はブラウザ向け SSE と Slack Socket Mode WebSocket 接続を管理する。
type Hub struct {
	mu          sync.RWMutex
	browserSubs map[uint64]browserSub
	socketConns map[uint64]*socketConn
	tickets     map[string]ticketEntry
	pendingAcks map[string]chan struct{}
	ackedIDs    map[string]time.Time
	nextID      atomic.Uint64
	rrIndex     atomic.Uint64
}

// NewHub は新しい Hub を初期化する。
func NewHub() *Hub {
	return &Hub{
		browserSubs: make(map[uint64]browserSub),
		socketConns: make(map[uint64]*socketConn),
		tickets:     make(map[string]ticketEntry),
		pendingAcks: make(map[string]chan struct{}),
		ackedIDs:    make(map[string]time.Time),
	}
}

// SubscribeBrowser はログイン済みユーザーの SSE 購読を登録する。
func (h *Hub) SubscribeBrowser(userID string) (<-chan domain.BrowserEvent, func()) {
	id := h.nextID.Add(1)
	ch := make(chan domain.BrowserEvent, 64)

	h.mu.Lock()
	h.browserSubs[id] = browserSub{id: id, userID: userID, ch: ch}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		if sub, ok := h.browserSubs[id]; ok {
			delete(h.browserSubs, id)
			close(sub.ch)
		}
		h.mu.Unlock()
	}
	return ch, cancel
}

// PublishBrowser はブラウザ SSE 購読者にイベントを配信する。
// targetUserID が空でない場合は、そのユーザーのセッションにのみ配信する (DM 保護)。
func (h *Hub) PublishBrowser(targetUserID string, ev domain.BrowserEvent) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.browserSubs {
		if targetUserID != "" && sub.userID != targetUserID {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			slog.Warn("dropping slow browser SSE event", "user_id", sub.userID, "type", ev.Type)
		}
	}
}

// PublishMessageCreated は新規メッセージを SSE と Socket Mode へ配信する。
func (h *Hub) PublishMessageCreated(ch domain.Channel, msg domain.Message) bool {
	msgCopy := msg
	h.PublishBrowser(ch.DMUserID, domain.BrowserEvent{
		Type:      domain.BrowserEventMessageCreated,
		ChannelID: ch.ID,
		Message:   &msgCopy,
	})

	_, delivered := h.SendEventCallback(buildSocketEvent("message", ch, msg))
	if msg.BotID == "" && ch.IsChannel && domain.MentionsBot(msg.Text) {
		_, mentionDelivered := h.SendEventCallback(buildSocketEvent("app_mention", ch, msg))
		delivered = delivered || mentionDelivered
	}
	return delivered
}

// PublishMessageUpdated は更新済みメッセージを SSE へ配信する。
func (h *Hub) PublishMessageUpdated(ch domain.Channel, msg domain.Message) {
	msgCopy := msg
	h.PublishBrowser(ch.DMUserID, domain.BrowserEvent{
		Type:      domain.BrowserEventMessageUpdated,
		ChannelID: ch.ID,
		Message:   &msgCopy,
	})
}

func buildSocketEvent(eventType string, ch domain.Channel, msg domain.Message) map[string]interface{} {
	ev := map[string]interface{}{
		"type":         eventType,
		"channel":      ch.ID,
		"channel_type": ch.ChannelType(),
		"user":         msg.UserID,
		"text":         msg.Text,
		"ts":           msg.TS,
		"event_ts":     msg.TS,
	}
	if msg.BotID != "" {
		ev["bot_id"] = msg.BotID
	}
	if msg.ThreadTS != "" {
		ev["thread_ts"] = msg.ThreadTS
	}
	return ev
}

// IssueTicket は Socket Mode WebSocket 接続用のワンタイムチケットを発行する。
func (h *Hub) IssueTicket() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	ticket := hex.EncodeToString(buf)

	now := time.Now()
	h.mu.Lock()
	for k, v := range h.tickets {
		if v.expiresAt.Before(now) {
			delete(h.tickets, k)
		}
	}
	h.tickets[ticket] = ticketEntry{expiresAt: now.Add(60 * time.Second)}
	h.mu.Unlock()
	return ticket
}

// ConsumeTicket はワンタイムチケットを検証して消費する。
func (h *Hub) ConsumeTicket(ticket string) bool {
	if ticket == "" {
		return false
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.tickets[ticket]
	if !ok {
		return false
	}
	delete(h.tickets, ticket)
	return entry.expiresAt.After(now)
}

// IsBotOnline は Socket Mode で 1 本以上の bot 接続がアクティブかを返す。
func (h *Hub) IsBotOnline() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.socketConns) > 0
}

// SocketConnectionCount は現在の Socket Mode 接続数を返す。
func (h *Hub) SocketConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.socketConns)
}

// RegisterSocket は Socket Mode 接続を登録し、hello を送信してクリーンアップ関数を返す。
func (h *Hub) RegisterSocket(sess usecase.SocketSession) (uint64, func(), error) {
	id := h.nextID.Add(1)
	sc := &socketConn{id: id, sess: sess}

	h.mu.Lock()
	wasOnline := len(h.socketConns) > 0
	h.socketConns[id] = sc
	numConns := len(h.socketConns)
	h.mu.Unlock()

	hello := map[string]interface{}{
		"type":            "hello",
		"num_connections": numConns,
		"debug_info": map[string]interface{}{
			"host":                        "nuage-monitoring-pwa",
			"started_connecting":          time.Now().UTC().Format(time.RFC3339),
			"build_number":                1,
			"approximate_connection_time": 1800,
		},
		"connection_info": map[string]interface{}{
			"app_id": domain.AppID,
		},
	}
	if err := sc.sess.SendJSON(hello); err != nil {
		h.mu.Lock()
		delete(h.socketConns, id)
		h.mu.Unlock()
		return 0, nil, err
	}

	if !wasOnline {
		online := true
		h.PublishBrowser("", domain.BrowserEvent{
			Type:      domain.BrowserEventBotStatus,
			BotOnline: &online,
		})
	}

	cleanup := func() {
		h.mu.Lock()
		delete(h.socketConns, id)
		nowOnline := len(h.socketConns) > 0
		h.mu.Unlock()

		if !nowOnline {
			online := false
			h.PublishBrowser("", domain.BrowserEvent{
				Type:      domain.BrowserEventBotStatus,
				BotOnline: &online,
			})
		}
	}
	return id, cleanup, nil
}

// SendEventCallback は Slack events_api のイベントエンベロープを接続中の Socket Mode bot へ送信し、envelope_id を返す。
func (h *Hub) SendEventCallback(event map[string]interface{}) (string, bool) {
	h.mu.RLock()
	conns := make([]*socketConn, 0, len(h.socketConns))
	for _, sc := range h.socketConns {
		conns = append(conns, sc)
	}
	h.mu.RUnlock()

	if len(conns) == 0 {
		return "", false
	}

	envID := randomID("env")
	eventID := randomID("Ev")
	envelope := SocketEnvelope{
		EnvelopeID:             envID,
		Type:                   "events_api",
		AcceptsResponsePayload: false,
		Payload: map[string]interface{}{
			"token":      "nuage-socket-mode",
			"team_id":    domain.TeamID,
			"api_app_id": domain.AppID,
			"type":       "event_callback",
			"event_id":   eventID,
			"event_time": time.Now().Unix(),
			"event":      event,
		},
	}

	ackCh := make(chan struct{})
	h.mu.Lock()
	h.pendingAcks[envID] = ackCh
	h.mu.Unlock()

	start := int(h.rrIndex.Add(1)) % len(conns)
	for i := 0; i < len(conns); i++ {
		sc := conns[(start+i)%len(conns)]
		if err := sc.sess.SendJSON(envelope); err == nil {
			return envID, true
		} else {
			slog.Warn("failed to send socket mode envelope", "conn_id", sc.id, "error", err)
		}
	}

	h.mu.Lock()
	delete(h.pendingAcks, envID)
	h.mu.Unlock()
	return "", false
}

// HandleAck は bot から返された envelope_id の ack を処理する。
func (h *Hub) HandleAck(raw []byte) (string, bool) {
	var ack struct {
		EnvelopeID string `json:"envelope_id"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil || ack.EnvelopeID == "" {
		return "", false
	}

	h.mu.Lock()
	if ch, ok := h.pendingAcks[ack.EnvelopeID]; ok {
		delete(h.pendingAcks, ack.EnvelopeID)
		close(ch)
	}
	h.ackedIDs[ack.EnvelopeID] = time.Now()
	// 古い ack 記録を掃除
	if len(h.ackedIDs) > 1000 {
		cutoff := time.Now().Add(-5 * time.Minute)
		for k, ts := range h.ackedIDs {
			if ts.Before(cutoff) {
				delete(h.ackedIDs, k)
			}
		}
	}
	h.mu.Unlock()
	return ack.EnvelopeID, true
}

// IsAcked は指定 envelope_id が既に ack されたかを返す（テスト・監視用）。
func (h *Hub) IsAcked(envelopeID string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.ackedIDs[envelopeID]
	return ok
}

// DisconnectAll は接続中の全 Socket Mode クライアントへ disconnect 予告を送信する。
func (h *Hub) DisconnectAll(reason string) {
	if reason == "" {
		reason = "refresh_requested"
	}
	h.mu.RLock()
	conns := make([]*socketConn, 0, len(h.socketConns))
	for _, sc := range h.socketConns {
		conns = append(conns, sc)
	}
	h.mu.RUnlock()

	msg := map[string]interface{}{
		"type":   "disconnect",
		"reason": reason,
		"debug_info": map[string]interface{}{
			"host": "nuage-monitoring-pwa",
		},
	}
	for _, sc := range conns {
		_ = sc.sess.SendJSON(msg)
	}
}

func randomID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}
