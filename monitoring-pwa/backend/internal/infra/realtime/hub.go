package realtime

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

const (
	defaultSendWaitTimeout = 500 * time.Millisecond
	pendingAckTTL          = 60 * time.Second
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
	bot  domain.BotIdentity
	sess usecase.SocketSession
}

type ticketEntry struct {
	bot       domain.BotIdentity
	expiresAt time.Time
}

type pendingAck struct {
	connID    uint64
	ch        chan struct{}
	createdAt time.Time
}

// Hub はブラウザ向け SSE と Slack Socket Mode WebSocket 接続を管理する。
type Hub struct {
	mu              sync.RWMutex
	browserSubs     map[uint64]browserSub
	socketConns     map[uint64]*socketConn
	botRR           map[string]uint64
	tickets         map[string]ticketEntry
	pendingAcks     map[string]pendingAck
	ackedIDs        map[string]time.Time
	nextID          atomic.Uint64
	sendWaitTimeout time.Duration
	nowFunc         func() time.Time
}

// HubOption は Hub の動作オプションである。
type HubOption func(*Hub)

// WithSendWaitTimeout は bot ごとの並列送信完了を待つ上限時間を設定する。
func WithSendWaitTimeout(d time.Duration) HubOption {
	return func(h *Hub) {
		if d > 0 {
			h.sendWaitTimeout = d
		}
	}
}

// WithHubNowFunc はテスト等向けに現在時刻関数を差し替える。
func WithHubNowFunc(fn func() time.Time) HubOption {
	return func(h *Hub) {
		if fn != nil {
			h.nowFunc = fn
		}
	}
}

// NewHub は新しい Hub を初期化する。
func NewHub(opts ...HubOption) *Hub {
	h := &Hub{
		browserSubs:     make(map[uint64]browserSub),
		socketConns:     make(map[uint64]*socketConn),
		botRR:           make(map[string]uint64),
		tickets:         make(map[string]ticketEntry),
		pendingAcks:     make(map[string]pendingAck),
		ackedIDs:        make(map[string]time.Time),
		sendWaitTimeout: defaultSendWaitTimeout,
		nowFunc:         time.Now,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
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
// message イベントは接続中の各 bot ごとに 1 本ずつ並列配信し、
// app_mention イベントは人間投稿かつチャンネル投稿時にメンションされた bot へだけ配信する。
func (h *Hub) PublishMessageCreated(ch domain.Channel, msg domain.Message) bool {
	msgCopy := msg
	h.PublishBrowser(ch.DMUserID, domain.BrowserEvent{
		Type:      domain.BrowserEventMessageCreated,
		ChannelID: ch.ID,
		Message:   &msgCopy,
	})

	delivered := h.sendEventToBots(buildSocketEvent("message", ch, msg), nil)
	if msg.BotID == "" && ch.IsChannel {
		if mentioned := domain.ExtractMentionedUserIDs(msg.Text); len(mentioned) > 0 {
			if h.sendEventToBots(buildSocketEvent("app_mention", ch, msg), mentioned) {
				delivered = true
			}
		}
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

// IssueTicket は指定 bot に紐づく Socket Mode WebSocket 接続用のワンタイムチケットを発行する。
func (h *Hub) IssueTicket(bot domain.BotIdentity) string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	ticket := hex.EncodeToString(buf)

	now := h.nowFunc()
	h.mu.Lock()
	for k, v := range h.tickets {
		if v.expiresAt.Before(now) {
			delete(h.tickets, k)
		}
	}
	h.tickets[ticket] = ticketEntry{bot: bot, expiresAt: now.Add(60 * time.Second)}
	h.mu.Unlock()
	return ticket
}

// ConsumeTicket はワンタイムチケットを検証して消費し、紐づく bot の識別情報を返す。
func (h *Hub) ConsumeTicket(ticket string) (domain.BotIdentity, bool) {
	if ticket == "" {
		return domain.BotIdentity{}, false
	}
	now := h.nowFunc()
	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.tickets[ticket]
	if !ok {
		return domain.BotIdentity{}, false
	}
	delete(h.tickets, ticket)
	if !entry.expiresAt.After(now) {
		return domain.BotIdentity{}, false
	}
	return entry.bot, true
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

// PendingAckCount は現在応答待ちの pendingAcks 件数を返す（テスト・監視用）。
func (h *Hub) PendingAckCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.pendingAcks)
}

// RegisterSocket は bot に紐づく Socket Mode 接続を登録し、hello を送信してクリーンアップ関数を返す。
func (h *Hub) RegisterSocket(bot domain.BotIdentity, sess usecase.SocketSession) (uint64, func(), error) {
	id := h.nextID.Add(1)
	sc := &socketConn{id: id, bot: bot, sess: sess}

	h.mu.Lock()
	wasOnline := len(h.socketConns) > 0
	h.socketConns[id] = sc
	numConns := h.countBotConnsLocked(bot.UserID)
	h.mu.Unlock()

	hello := map[string]interface{}{
		"type":            "hello",
		"num_connections": numConns,
		"debug_info": map[string]interface{}{
			"host":                        "nuage-monitoring-pwa",
			"started_connecting":          h.nowFunc().UTC().Format(time.RFC3339),
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
		for envID, pa := range h.pendingAcks {
			if pa.connID == id {
				delete(h.pendingAcks, envID)
				close(pa.ch)
			}
		}
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

func (h *Hub) countBotConnsLocked(botUserID string) int {
	n := 0
	for _, sc := range h.socketConns {
		if sc.bot.UserID == botUserID {
			n++
		}
	}
	return n
}

func (h *Hub) prunePendingAcksLocked(now time.Time) {
	cutoff := now.Add(-pendingAckTTL)
	for envID, pa := range h.pendingAcks {
		if pa.createdAt.Before(cutoff) {
			delete(h.pendingAcks, envID)
			close(pa.ch)
		}
	}
}

// sendEventToBots は bot ごとに接続を束ねてイベントを並列に各 bot 1 本ずつ送信する。
// targetUserIDs が nil の場合は接続中の全 bot へ、非 nil の場合は該当 UserID の bot へだけ送信する。
func (h *Hub) sendEventToBots(event map[string]interface{}, targetUserIDs []string) bool {
	var targetSet map[string]bool
	if targetUserIDs != nil {
		targetSet = make(map[string]bool, len(targetUserIDs))
		for _, uid := range targetUserIDs {
			targetSet[uid] = true
		}
	}

	now := h.nowFunc()
	h.mu.Lock()
	h.prunePendingAcksLocked(now)
	byBot := make(map[string][]*socketConn)
	for _, sc := range h.socketConns {
		if targetSet != nil && !targetSet[sc.bot.UserID] {
			continue
		}
		byBot[sc.bot.UserID] = append(byBot[sc.bot.UserID], sc)
	}
	botIDs := make([]string, 0, len(byBot))
	startOffsets := make(map[string]int, len(byBot))
	for uid, conns := range byBot {
		botIDs = append(botIDs, uid)
		idx := h.botRR[uid]
		startOffsets[uid] = int(idx % uint64(len(conns)))
		h.botRR[uid] = idx + 1
	}
	h.mu.Unlock()

	if len(byBot) == 0 {
		return false
	}

	sort.Strings(botIDs)

	results := make(chan bool, len(botIDs))
	for _, uid := range botIDs {
		conns := byBot[uid]
		sort.Slice(conns, func(i, j int) bool { return conns[i].id < conns[j].id })
		start := startOffsets[uid]
		go func(group []*socketConn, offset int) {
			results <- h.sendToOneConnInGroup(group, offset, event)
		}(conns, start)
	}

	timer := time.NewTimer(h.sendWaitTimeout)
	defer timer.Stop()

	deliveredAny := false
	completed := 0
	for completed < len(botIDs) {
		select {
		case ok := <-results:
			completed++
			if ok {
				deliveredAny = true
			}
		case <-timer.C:
			return deliveredAny
		}
	}
	return deliveredAny
}

func (h *Hub) sendToOneConnInGroup(conns []*socketConn, start int, event map[string]interface{}) bool {
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
			"event_time": h.nowFunc().Unix(),
			"event":      event,
		},
	}

	for i := 0; i < len(conns); i++ {
		sc := conns[(start+i)%len(conns)]
		ackCh := make(chan struct{})
		h.mu.Lock()
		h.pendingAcks[envID] = pendingAck{
			connID:    sc.id,
			ch:        ackCh,
			createdAt: h.nowFunc(),
		}
		h.mu.Unlock()

		if err := sc.sess.SendJSON(envelope); err == nil {
			return true
		} else {
			h.mu.Lock()
			if pa, ok := h.pendingAcks[envID]; ok {
				delete(h.pendingAcks, envID)
				close(pa.ch)
			}
			h.mu.Unlock()
			slog.Warn("failed to send socket mode envelope", "conn_id", sc.id, "bot_user_id", sc.bot.UserID, "error", err)
		}
	}

	return false
}

// HandleAck は bot から返された envelope_id の ack を処理する。
func (h *Hub) HandleAck(raw []byte) (string, bool) {
	var ack struct {
		EnvelopeID string `json:"envelope_id"`
	}
	if err := json.Unmarshal(raw, &ack); err != nil || ack.EnvelopeID == "" {
		return "", false
	}

	now := h.nowFunc()
	h.mu.Lock()
	if pa, ok := h.pendingAcks[ack.EnvelopeID]; ok {
		delete(h.pendingAcks, ack.EnvelopeID)
		close(pa.ch)
	}
	h.prunePendingAcksLocked(now)
	h.ackedIDs[ack.EnvelopeID] = now
	// 古い ack 記録を掃除
	if len(h.ackedIDs) > 1000 {
		cutoff := now.Add(-5 * time.Minute)
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
