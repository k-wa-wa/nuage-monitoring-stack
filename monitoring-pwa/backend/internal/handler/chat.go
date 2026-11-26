package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/auth"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/realtime"
)

var botMentionAliasRe = regexp.MustCompile(`(?i)@(?:bot|nuage-bot)\b`)

// ChatHandler は人間向けチャット UI API (`/api/channels/*`, `/api/events` 等) を処理する。
type ChatHandler struct {
	cfg config.Config
	db  *push.DB
	hub *realtime.Hub
}

// NewChatHandler は新しい ChatHandler を生成する。
func NewChatHandler(cfg config.Config, db *push.DB, hub *realtime.Hub) *ChatHandler {
	return &ChatHandler{
		cfg: cfg,
		db:  db,
		hub: hub,
	}
}

// Bootstrap はログイン後の初期画面に必要なユーザー・チャンネル・bot 状態を一括返却する。
func (h *ChatHandler) Bootstrap(c echo.Context) error {
	user := auth.GetUser(c)
	channels, err := h.db.ListChannelsForUser(user.ID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load channels")
	}
	if channels == nil {
		channels = []push.Channel{}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"user":     toAuthUserDTO(user),
		"channels": channels,
		"bot": map[string]interface{}{
			"id":      push.SlackBotID,
			"user_id": push.SlackBotUserID,
			"name":    push.SlackBotName,
			"online":  h.hub.IsBotOnline(),
		},
	})
}

// ListChannels はユーザーがアクセス可能なチャンネル一覧と未読数を返す。
func (h *ChatHandler) ListChannels(c echo.Context) error {
	user := auth.GetUser(c)
	channels, err := h.db.ListChannelsForUser(user.ID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list channels")
	}
	if channels == nil {
		channels = []push.Channel{}
	}
	return c.JSON(http.StatusOK, channels)
}

func (h *ChatHandler) authorizeChannelAccess(c echo.Context, chID string) (*push.Channel, *config.UserConfig, error) {
	user := auth.GetUser(c)
	ch, err := h.db.GetChannelByID(chID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, echo.NewHTTPError(http.StatusNotFound, "channel not found")
		}
		return nil, nil, echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch channel")
	}
	if ch.IsIM && ch.DMUserID != user.ID {
		return nil, nil, echo.NewHTTPError(http.StatusForbidden, "access denied to another user's DM")
	}
	return ch, user, nil
}

// ListMessages はチャンネルのメッセージ一覧（古い→新しい順、before ページネーション対応）を返す。
func (h *ChatHandler) ListMessages(c echo.Context) error {
	ch, _, err := h.authorizeChannelAccess(c, c.Param("id"))
	if err != nil {
		return err
	}

	limit := parseLimitParam(c, 40, 100)
	beforeTS := strings.TrimSpace(c.QueryParam("before"))

	msgs, hasMore, err := h.db.ListChannelMessagesAsc(ch.ID, beforeTS, limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list messages")
	}
	if msgs == nil {
		msgs = []push.Message{}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"channel":  ch,
		"messages": msgs,
		"has_more": hasMore,
	})
}

// GetThread は指定スレッドの親メッセージと返信一覧を返す。
func (h *ChatHandler) GetThread(c echo.Context) error {
	ch, _, err := h.authorizeChannelAccess(c, c.Param("id"))
	if err != nil {
		return err
	}
	ts := c.Param("ts")
	parent, replies, err := h.db.GetThreadMessages(ch.ID, ts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "thread not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load thread")
	}
	if replies == nil {
		replies = []push.Message{}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"channel": ch,
		"parent":  parent,
		"replies": replies,
	})
}

// PostMessage は人間ユーザーからのメッセージを投稿し、Socket Mode で bot へ配信する。
func (h *ChatHandler) PostMessage(c echo.Context) error {
	ch, user, err := h.authorizeChannelAccess(c, c.Param("id"))
	if err != nil {
		return err
	}

	var req struct {
		Text     string `json:"text"`
		ThreadTS string `json:"thread_ts,omitempty"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid message payload")
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "text is required")
	}

	// @bot / @nuage-bot を Slack 互換メンション形式 <@U01BOT> に正規化する
	normalizedText := botMentionAliasRe.ReplaceAllString(text, "<@"+push.SlackBotUserID+">")

	threadTS := strings.TrimSpace(req.ThreadTS)
	if threadTS != "" {
		if _, err := h.db.GetMessageByTS(ch.ID, threadTS); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return echo.NewHTTPError(http.StatusNotFound, "thread parent not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to verify thread parent")
		}
	}

	msg, err := h.db.CreateMessage(push.CreateMessageParams{
		ChannelID: ch.ID,
		ThreadTS:  threadTS,
		UserID:    user.ID,
		Username:  user.DisplayName,
		Text:      normalizedText,
	})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save message")
	}

	_ = h.db.MarkChannelRead(user.ID, ch.ID, msg.TS)

	// ブラウザ SSE へ配信
	h.hub.PublishBrowser(ch.DMUserID, realtime.BrowserEvent{
		Type:      "message_created",
		ChannelID: ch.ID,
		Payload:   msg,
	})

	// Socket Mode へイベント送信
	baseEvent := map[string]interface{}{
		"type":         "message",
		"user":         user.ID,
		"text":         msg.Text,
		"ts":           msg.TS,
		"channel":      ch.ID,
		"channel_type": channelTypeFor(ch),
		"event_ts":     msg.TS,
	}
	if msg.ThreadTS != "" {
		baseEvent["thread_ts"] = msg.ThreadTS
	}
	_, delivered := h.hub.SendEventCallback(baseEvent)

	// チャンネル上で bot にメンションしている場合は app_mention も送る (Slack と同じ)
	if ch.IsChannel && strings.Contains(msg.Text, "<@"+push.SlackBotUserID+">") {
		mentionEvent := map[string]interface{}{
			"type":         "app_mention",
			"user":         user.ID,
			"text":         msg.Text,
			"ts":           msg.TS,
			"channel":      ch.ID,
			"channel_type": "channel",
			"event_ts":     msg.TS,
		}
		if msg.ThreadTS != "" {
			mentionEvent["thread_ts"] = msg.ThreadTS
		}
		_, mentionDelivered := h.hub.SendEventCallback(mentionEvent)
		delivered = delivered || mentionDelivered
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":          msg,
		"bot_online":       h.hub.IsBotOnline(),
		"delivered_to_bot": delivered,
	})
}

// MarkRead はチャンネルを既読にする。
func (h *ChatHandler) MarkRead(c echo.Context) error {
	ch, user, err := h.authorizeChannelAccess(c, c.Param("id"))
	if err != nil {
		return err
	}
	var req struct {
		TS string `json:"ts"`
	}
	_ = c.Bind(&req)

	if err := h.db.MarkChannelRead(user.ID, ch.ID, req.TS); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to mark read")
	}
	return c.NoContent(http.StatusNoContent)
}

// Search はキーワード検索を実行する。
func (h *ChatHandler) Search(c echo.Context) error {
	user := auth.GetUser(c)
	q := strings.TrimSpace(c.QueryParam("q"))
	if q == "" {
		return c.JSON(http.StatusOK, []push.SearchResult{})
	}
	channelID := strings.TrimSpace(c.QueryParam("channel_id"))
	limit := parseLimitParam(c, 30, 100)

	results, err := h.db.SearchMessages(q, channelID, user.ID, limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to search messages")
	}
	if results == nil {
		results = []push.SearchResult{}
	}
	return c.JSON(http.StatusOK, results)
}

// DeepLink は `/alerts/:id` や `/notifications/:id` からチャット上のチャンネル・メッセージ座標を返す。
func (h *ChatHandler) DeepLink(c echo.Context) error {
	kind := c.Param("kind")
	if kind != "alert" && kind != "notification" {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid deeplink kind")
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid deeplink id")
	}

	msg, err := h.db.GetMessageBySourceRef(kind, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "message not found for reference")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to resolve deeplink")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"channel_id": msg.ChannelID,
		"ts":         msg.TS,
		"message":    msg,
	})
}

// Events はログイン済みブラウザ向けに Server-Sent Events (SSE) で新着・状態変化を配信する。
func (h *ChatHandler) Events(c echo.Context) error {
	user := auth.GetUser(c)
	ch, cancel := h.hub.SubscribeBrowser(user.ID)
	defer cancel()

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeEvent := func(ev realtime.BrowserEvent) error {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, string(b)); err != nil {
			return err
		}
		w.Flush()
		return nil
	}

	// 初回に接続状態を送る
	if err := writeEvent(realtime.BrowserEvent{
		Type:    "bot_status",
		Payload: map[string]bool{"online": h.hub.IsBotOnline()},
	}); err != nil {
		return nil
	}

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			w.Flush()
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := writeEvent(ev); err != nil {
				return nil
			}
		}
	}
}
