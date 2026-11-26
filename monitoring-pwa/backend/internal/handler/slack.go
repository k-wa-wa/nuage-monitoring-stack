package handler

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/realtime"
)

// SlackHandler は Slack 互換 Web API (`/slack/api/*`) および Socket Mode (`/slack/socket`) を処理する。
type SlackHandler struct {
	cfg      config.Config
	db       *push.DB
	hub      *realtime.Hub
	sender   PushSender
	upgrader websocket.Upgrader
}

// NewSlackHandler は新しい SlackHandler を生成する。
func NewSlackHandler(cfg config.Config, db *push.DB, hub *realtime.Hub, sender PushSender) *SlackHandler {
	return &SlackHandler{
		cfg:    cfg,
		db:     db,
		hub:    hub,
		sender: sender,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func slackOK(c echo.Context, fields map[string]interface{}) error {
	res := map[string]interface{}{"ok": true}
	for k, v := range fields {
		res[k] = v
	}
	return c.JSON(http.StatusOK, res)
}

func slackErr(c echo.Context, errCode string) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"ok":    false,
		"error": errCode,
	})
}

func extractBearerToken(c echo.Context) string {
	authz := strings.TrimSpace(c.Request().Header.Get("Authorization"))
	if len(authz) > 7 && strings.EqualFold(authz[:7], "Bearer ") {
		return strings.TrimSpace(authz[7:])
	}
	return ""
}

func tokenMatches(actual, expected string) bool {
	if actual == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

// parseSlackParams は JSON ボディ・Form・Query のいずれの形式で送られたパラメータも統一的に読み取る。
func parseSlackParams(c echo.Context) map[string]interface{} {
	params := make(map[string]interface{})

	for k, vals := range c.QueryParams() {
		if len(vals) > 0 {
			params[k] = vals[0]
		}
	}

	ct := strings.ToLower(c.Request().Header.Get(echo.HeaderContentType))
	if strings.Contains(ct, "application/json") {
		var body map[string]interface{}
		if err := json.NewDecoder(c.Request().Body).Decode(&body); err == nil {
			for k, v := range body {
				params[k] = v
			}
		}
		return params
	}

	if form, err := c.FormParams(); err == nil {
		for k, vals := range form {
			if len(vals) > 0 {
				params[k] = vals[0]
			}
		}
	}
	return params
}

func paramString(params map[string]interface{}, key string) string {
	v, ok := params[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	default:
		return fmt.Sprintf("%v", s)
	}
}

func paramInt(params map[string]interface{}, key string, def int) int {
	v, ok := params[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		if parsed, err := strconv.Atoi(n); err == nil {
			return parsed
		}
	}
	return def
}

func paramBool(params map[string]interface{}, key string) bool {
	v, ok := params[key]
	if !ok || v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true" || b == "1"
	}
	return false
}

// DispatchMethod は `/slack/api/:method` のディスパッチャである。
func (h *SlackHandler) DispatchMethod(c echo.Context) error {
	method := c.Param("method")
	token := extractBearerToken(c)
	if token == "" {
		// form の token パラメータも一応確認
		params := parseSlackParams(c)
		token = paramString(params, "token")
		if method == "apps.connections.open" {
			return h.handleAppsConnectionsOpen(c, token)
		}
		return h.dispatchBotMethod(c, method, token, params)
	}

	if method == "apps.connections.open" {
		return h.handleAppsConnectionsOpen(c, token)
	}
	params := parseSlackParams(c)
	return h.dispatchBotMethod(c, method, token, params)
}

func (h *SlackHandler) handleAppsConnectionsOpen(c echo.Context, token string) error {
	if !tokenMatches(token, h.cfg.SlackAppToken) {
		return slackErr(c, "invalid_auth")
	}
	ticket := h.hub.IssueTicket()

	scheme := "ws"
	if c.Request().TLS != nil || strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "wss"
	}
	host := c.Request().Host
	if host == "" {
		host = "localhost:" + h.cfg.Port
	}
	wsURL := fmt.Sprintf("%s://%s/slack/socket?ticket=%s", scheme, host, ticket)
	return slackOK(c, map[string]interface{}{"url": wsURL})
}

func (h *SlackHandler) dispatchBotMethod(c echo.Context, method, token string, params map[string]interface{}) error {
	if !tokenMatches(token, h.cfg.SlackBotToken) {
		return slackErr(c, "invalid_auth")
	}

	switch method {
	case "auth.test":
		return h.authTest(c)
	case "conversations.list":
		return h.conversationsList(c, params)
	case "conversations.info":
		return h.conversationsInfo(c, params)
	case "conversations.history":
		return h.conversationsHistory(c, params)
	case "conversations.replies":
		return h.conversationsReplies(c, params)
	case "chat.postMessage":
		return h.chatPostMessage(c, params)
	case "chat.update":
		return h.chatUpdate(c, params)
	case "users.info":
		return h.usersInfo(c, params)
	default:
		return slackErr(c, "unknown_method")
	}
}

func (h *SlackHandler) authTest(c echo.Context) error {
	scheme := "http"
	if c.Request().TLS != nil || strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s/", scheme, c.Request().Host)
	return slackOK(c, map[string]interface{}{
		"url":                   baseURL,
		"team":                  "Nuage",
		"user":                  push.SlackBotName,
		"team_id":               "T01NUAGE",
		"user_id":               push.SlackBotUserID,
		"bot_id":                push.SlackBotID,
		"is_enterprise_install": false,
	})
}

func formatSlackChannel(ch push.Channel) map[string]interface{} {
	m := map[string]interface{}{
		"id":          ch.ID,
		"name":        ch.Name,
		"is_channel":  ch.IsChannel,
		"is_group":    false,
		"is_im":       ch.IsIM,
		"is_private":  false,
		"is_archived": false,
		"is_member":   true,
		"created":     ch.CreatedAt.Unix(),
		"topic": map[string]interface{}{
			"value":    ch.Topic,
			"creator":  push.SlackBotUserID,
			"last_set": ch.CreatedAt.Unix(),
		},
		"purpose": map[string]interface{}{
			"value":    ch.Purpose,
			"creator":  push.SlackBotUserID,
			"last_set": ch.CreatedAt.Unix(),
		},
	}
	if ch.IsIM {
		m["user"] = ch.DMUserID
	}
	return m
}

func formatSlackMessage(m push.Message) map[string]interface{} {
	obj := map[string]interface{}{
		"type":     "message",
		"ts":       m.TS,
		"text":     m.Text,
		"username": m.Username,
	}
	if m.UserID != "" {
		obj["user"] = m.UserID
	}
	if m.BotID != "" {
		obj["bot_id"] = m.BotID
		obj["subtype"] = "bot_message"
	}
	if m.ThreadTS != "" {
		obj["thread_ts"] = m.ThreadTS
	} else if m.ReplyCount > 0 {
		obj["thread_ts"] = m.TS
		obj["reply_count"] = m.ReplyCount
		obj["latest_reply"] = m.LatestReply
	}
	if len(m.Blocks) > 0 {
		var blocks interface{}
		if err := json.Unmarshal(m.Blocks, &blocks); err == nil {
			obj["blocks"] = blocks
		}
	}
	return obj
}

func (h *SlackHandler) conversationsList(c echo.Context, params map[string]interface{}) error {
	typesParam := paramString(params, "types")
	if typesParam == "" {
		typesParam = "public_channel"
	}
	includePublic := false
	includeIM := false
	for _, t := range strings.Split(typesParam, ",") {
		switch strings.TrimSpace(t) {
		case "public_channel":
			includePublic = true
		case "im":
			includeIM = true
		}
	}

	channels, err := h.db.ListAllChannels(includePublic, includeIM)
	if err != nil {
		return slackErr(c, "internal_error")
	}

	limit := paramInt(params, "limit", 100)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	if len(channels) > limit {
		channels = channels[:limit]
	}

	out := make([]map[string]interface{}, 0, len(channels))
	for _, ch := range channels {
		out = append(out, formatSlackChannel(ch))
	}

	return slackOK(c, map[string]interface{}{
		"channels":          out,
		"response_metadata": map[string]string{"next_cursor": ""},
	})
}

func (h *SlackHandler) conversationsInfo(c echo.Context, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	if chID == "" {
		return slackErr(c, "channel_not_found")
	}
	ch, err := h.db.GetChannelByID(chID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return slackErr(c, "channel_not_found")
		}
		return slackErr(c, "internal_error")
	}
	return slackOK(c, map[string]interface{}{
		"channel": formatSlackChannel(*ch),
	})
}

func (h *SlackHandler) conversationsHistory(c echo.Context, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	if chID == "" {
		return slackErr(c, "channel_not_found")
	}
	if _, err := h.db.GetChannelByID(chID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return slackErr(c, "channel_not_found")
		}
		return slackErr(c, "internal_error")
	}

	limit := paramInt(params, "limit", 100)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	oldest := paramString(params, "oldest")
	latest := paramString(params, "latest")
	inclusive := paramBool(params, "inclusive")

	msgs, hasMore, err := h.db.ListChannelMessagesDesc(chID, oldest, latest, inclusive, limit)
	if err != nil {
		return slackErr(c, "internal_error")
	}

	out := make([]map[string]interface{}, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, formatSlackMessage(m))
	}

	return slackOK(c, map[string]interface{}{
		"messages":          out,
		"has_more":          hasMore,
		"response_metadata": map[string]string{"next_cursor": ""},
	})
}

func (h *SlackHandler) conversationsReplies(c echo.Context, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	ts := paramString(params, "ts")
	if chID == "" || ts == "" {
		return slackErr(c, "thread_not_found")
	}

	parent, replies, err := h.db.GetThreadMessages(chID, ts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return slackErr(c, "thread_not_found")
		}
		return slackErr(c, "internal_error")
	}

	limit := paramInt(params, "limit", 200)
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	out := make([]map[string]interface{}, 0, 1+len(replies))
	parentObj := formatSlackMessage(*parent)
	parentObj["thread_ts"] = parent.TS
	parentObj["reply_count"] = len(replies)
	out = append(out, parentObj)

	for _, r := range replies {
		out = append(out, formatSlackMessage(r))
	}
	if len(out) > limit {
		out = out[:limit]
	}

	return slackOK(c, map[string]interface{}{
		"messages":          out,
		"has_more":          false,
		"response_metadata": map[string]string{"next_cursor": ""},
	})
}

func (h *SlackHandler) chatPostMessage(c echo.Context, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	if chID == "" {
		return slackErr(c, "channel_not_found")
	}
	ch, err := h.db.GetChannelByID(chID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return slackErr(c, "channel_not_found")
		}
		return slackErr(c, "internal_error")
	}

	text := paramString(params, "text")
	blocksJSON := extractBlocksJSON(params["blocks"])
	if strings.TrimSpace(text) == "" && blocksJSON == "" {
		return slackErr(c, "no_text")
	}
	if strings.TrimSpace(text) == "" && blocksJSON != "" {
		text = extractTextFromBlocks(blocksJSON)
	}

	threadTS := paramString(params, "thread_ts")
	if threadTS != "" {
		if _, err := h.db.GetMessageByTS(chID, threadTS); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return slackErr(c, "thread_not_found")
			}
			return slackErr(c, "internal_error")
		}
	}

	username := paramString(params, "username")
	if username == "" {
		username = push.SlackBotName
	}

	msg, err := h.db.CreateMessage(push.CreateMessageParams{
		ChannelID: chID,
		ThreadTS:  threadTS,
		UserID:    push.SlackBotUserID,
		BotID:     push.SlackBotID,
		Username:  username,
		Text:      text,
		Blocks:    blocksJSON,
		Level:     "info",
	})
	if err != nil {
		slog.Error("chat.postMessage CreateMessage failed", "channel", chID, "error", err)
		return slackErr(c, "internal_error")
	}

	// ブラウザへリアルタイム配信
	h.hub.PublishBrowser(ch.DMUserID, realtime.BrowserEvent{
		Type:      "message_created",
		ChannelID: chID,
		Payload:   msg,
	})

	// Socket Mode へ bot_id 付きイベント配信（bot 側が無視できるようにする）
	ev := map[string]interface{}{
		"type":         "message",
		"channel":      chID,
		"channel_type": channelTypeFor(ch),
		"user":         push.SlackBotUserID,
		"bot_id":       push.SlackBotID,
		"text":         msg.Text,
		"ts":           msg.TS,
		"event_ts":     msg.TS,
	}
	if msg.ThreadTS != "" {
		ev["thread_ts"] = msg.ThreadTS
	}
	h.hub.SendEventCallback(ev)

	// Web Push 通知を送信
	if h.sender != nil {
		pushURL := fmt.Sprintf("/channels/%s", chID)
		if msg.ThreadTS != "" {
			pushURL = fmt.Sprintf("/channels/%s/threads/%s", chID, msg.ThreadTS)
		}
		pushTitle := fmt.Sprintf("%s (#%s)", username, ch.Name)
		if ch.IsIM {
			pushTitle = fmt.Sprintf("%s (DM)", username)
		}
		payloadObj := notificationPayload{
			Title: pushTitle,
			Body:  text,
			URL:   pushURL,
			Level: "info",
			Tag:   fmt.Sprintf("msg-%s-%s", chID, msg.TS),
		}
		if payload, err := json.Marshal(payloadObj); err == nil {
			if ch.IsIM && ch.DMUserID != "" {
				h.sender.BroadcastToUser(ch.DMUserID, payload)
			} else {
				h.sender.Broadcast(payload)
			}
		}
	}

	return slackOK(c, map[string]interface{}{
		"channel": chID,
		"ts":      msg.TS,
		"message": formatSlackMessage(*msg),
	})
}

func (h *SlackHandler) chatUpdate(c echo.Context, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	ts := paramString(params, "ts")
	if chID == "" || ts == "" {
		return slackErr(c, "message_not_found")
	}

	ch, err := h.db.GetChannelByID(chID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return slackErr(c, "channel_not_found")
		}
		return slackErr(c, "internal_error")
	}

	text := paramString(params, "text")
	blocksJSON := extractBlocksJSON(params["blocks"])
	if strings.TrimSpace(text) == "" && blocksJSON == "" {
		return slackErr(c, "no_text")
	}
	if strings.TrimSpace(text) == "" && blocksJSON != "" {
		text = extractTextFromBlocks(blocksJSON)
	}

	updated, err := h.db.UpdateMessage(chID, ts, text, blocksJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return slackErr(c, "message_not_found")
		}
		return slackErr(c, "internal_error")
	}

	h.hub.PublishBrowser(ch.DMUserID, realtime.BrowserEvent{
		Type:      "message_updated",
		ChannelID: chID,
		Payload:   updated,
	})

	return slackOK(c, map[string]interface{}{
		"channel": chID,
		"ts":      updated.TS,
		"text":    updated.Text,
		"message": formatSlackMessage(*updated),
	})
}

func (h *SlackHandler) usersInfo(c echo.Context, params map[string]interface{}) error {
	userID := paramString(params, "user")
	if userID == "" {
		return slackErr(c, "user_not_found")
	}

	if userID == push.SlackBotUserID || userID == push.SystemBotUserID {
		name := push.SlackBotName
		if userID == push.SystemBotUserID {
			name = "nuage-notify"
		}
		return slackOK(c, map[string]interface{}{
			"user": map[string]interface{}{
				"id":        userID,
				"team_id":   "T01NUAGE",
				"name":      name,
				"real_name": name,
				"is_bot":    true,
				"profile": map[string]interface{}{
					"display_name": name,
					"real_name":    name,
				},
			},
		})
	}

	for _, u := range h.cfg.Users {
		if u.ID == userID {
			return slackOK(c, map[string]interface{}{
				"user": map[string]interface{}{
					"id":        u.ID,
					"team_id":   "T01NUAGE",
					"name":      u.EffectiveName(),
					"real_name": u.DisplayName,
					"is_bot":    false,
					"profile": map[string]interface{}{
						"display_name": u.DisplayName,
						"real_name":    u.DisplayName,
					},
				},
			})
		}
	}

	return slackErr(c, "user_not_found")
}

// SocketModeWS は `/slack/socket` の WebSocket 接続を受け付け、hello・events_api・ack・disconnect を処理する。
func (h *SlackHandler) SocketModeWS(c echo.Context) error {
	ticket := c.QueryParam("ticket")
	token := extractBearerToken(c)
	if !h.hub.ConsumeTicket(ticket) && !tokenMatches(token, h.cfg.SlackAppToken) {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid socket mode ticket")
	}

	conn, err := h.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	connID, cleanup, err := h.hub.RegisterSocket(conn)
	if err != nil {
		slog.Error("failed to register socket mode connection", "error", err)
		return nil
	}
	defer cleanup()

	slog.Info("socket mode client connected", "conn_id", connID)

	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	conn.SetPingHandler(func(appData string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
	})

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			slog.Info("socket mode client disconnected", "conn_id", connID, "error", err)
			return nil
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		if envID, ok := h.hub.HandleAck(msg); ok {
			slog.Debug("received socket mode ack", "conn_id", connID, "envelope_id", envID)
		}
	}
}

func channelTypeFor(ch *push.Channel) string {
	if ch.IsIM {
		return "im"
	}
	return "channel"
}

func extractBlocksJSON(raw interface{}) string {
	if raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" || trimmed == "null" {
			return ""
		}
		return trimmed
	default:
		b, err := json.Marshal(v)
		if err != nil || string(b) == "null" {
			return ""
		}
		return string(b)
	}
}

func extractTextFromBlocks(blocksJSON string) string {
	var blocks []struct {
		Text *struct {
			Text string `json:"text"`
		} `json:"text"`
	}
	if err := json.Unmarshal([]byte(blocksJSON), &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Text != nil && b.Text.Text != "" {
			parts = append(parts, b.Text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
