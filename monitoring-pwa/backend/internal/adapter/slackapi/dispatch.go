package slackapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

// Handler は Slack 互換 Web API (`/slack/api/*`) および Socket Mode (`/slack/socket`) を処理する。
type Handler struct {
	port     string
	chatUC   *usecase.ChatUsecase
	authUC   *usecase.AuthUsecase
	socketUC *usecase.SocketUsecase
	upgrader websocket.Upgrader
}

// NewHandler は新しい Slack 互換 API ハンドラーを生成する。
func NewHandler(
	port string,
	chatUC *usecase.ChatUsecase,
	authUC *usecase.AuthUsecase,
	socketUC *usecase.SocketUsecase,
) *Handler {
	return &Handler{
		port:     port,
		chatUC:   chatUC,
		authUC:   authUC,
		socketUC: socketUC,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// DispatchMethod は `/slack/api/:method` のディスパッチャである。
// SPIFFE JWT-SVID 認証では Authorization: Bearer ヘッダのみを受け付ける。
func (h *Handler) DispatchMethod(c echo.Context) error {
	method := c.Param("method")
	token := extractBearerToken(c)
	if method == "apps.connections.open" {
		return h.handleAppsConnectionsOpen(c, token)
	}
	params := parseSlackParams(c)
	return h.dispatchBotMethod(c, method, token, params)
}

func (h *Handler) handleAppsConnectionsOpen(c echo.Context, token string) error {
	ticket, err := h.socketUC.OpenConnectionTicket(c.Request().Context(), token)
	if err != nil {
		return slackErr(c, "invalid_auth")
	}

	scheme := "ws"
	if c.Request().TLS != nil || strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "wss"
	}
	host := c.Request().Host
	if host == "" {
		host = "localhost:" + h.port
	}
	wsURL := fmt.Sprintf("%s://%s/slack/socket?ticket=%s", scheme, host, ticket)
	return slackOK(c, map[string]interface{}{"url": wsURL})
}

func (h *Handler) dispatchBotMethod(c echo.Context, method, token string, params map[string]interface{}) error {
	bot, err := h.socketUC.VerifyBotToken(c.Request().Context(), token)
	if err != nil {
		return slackErr(c, "invalid_auth")
	}

	switch method {
	case "auth.test":
		return h.authTest(c, bot)
	case "conversations.list":
		return h.conversationsList(c, params)
	case "conversations.info":
		return h.conversationsInfo(c, params)
	case "conversations.history":
		return h.conversationsHistory(c, params)
	case "conversations.replies":
		return h.conversationsReplies(c, params)
	case "chat.postMessage":
		return h.chatPostMessage(c, bot, params)
	case "chat.update":
		return h.chatUpdate(c, bot, params)
	case "users.info":
		return h.usersInfo(c, params)
	default:
		return slackErr(c, "unknown_method")
	}
}

func (h *Handler) authTest(c echo.Context, bot domain.BotIdentity) error {
	scheme := "http"
	if c.Request().TLS != nil || strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	baseURL := fmt.Sprintf("%s://%s/", scheme, c.Request().Host)
	return slackOK(c, map[string]interface{}{
		"url":                   baseURL,
		"team":                  "Nuage",
		"user":                  bot.Name,
		"team_id":               domain.TeamID,
		"user_id":               bot.UserID,
		"bot_id":                bot.BotID,
		"is_enterprise_install": false,
	})
}

func (h *Handler) conversationsList(c echo.Context, params map[string]interface{}) error {
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

	channels, err := h.chatUC.ListAllChannels(includePublic, includeIM)
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

func (h *Handler) conversationsInfo(c echo.Context, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	if chID == "" {
		return slackErr(c, "channel_not_found")
	}
	ch, err := h.chatUC.GetChannel(chID)
	if err != nil {
		if errors.Is(err, usecase.ErrChannelNotFound) {
			return slackErr(c, "channel_not_found")
		}
		return slackErr(c, "internal_error")
	}
	return slackOK(c, map[string]interface{}{
		"channel": formatSlackChannel(*ch),
	})
}

func (h *Handler) conversationsHistory(c echo.Context, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	if chID == "" {
		return slackErr(c, "channel_not_found")
	}

	limit := paramInt(params, "limit", 100)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	oldest := paramString(params, "oldest")
	latest := paramString(params, "latest")
	inclusive := paramBool(params, "inclusive")

	msgs, hasMore, err := h.chatUC.ListMessagesDesc(chID, oldest, latest, inclusive, limit)
	if err != nil {
		if errors.Is(err, usecase.ErrChannelNotFound) {
			return slackErr(c, "channel_not_found")
		}
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

func (h *Handler) conversationsReplies(c echo.Context, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	ts := paramString(params, "ts")
	if chID == "" || ts == "" {
		return slackErr(c, "thread_not_found")
	}

	parent, replies, err := h.chatUC.GetThread(chID, ts)
	if err != nil {
		if errors.Is(err, usecase.ErrNotFound) {
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

func (h *Handler) chatPostMessage(c echo.Context, bot domain.BotIdentity, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	if chID == "" {
		return slackErr(c, "channel_not_found")
	}
	if _, err := h.chatUC.GetChannel(chID); err != nil {
		if errors.Is(err, usecase.ErrChannelNotFound) {
			return slackErr(c, "channel_not_found")
		}
		return slackErr(c, "internal_error")
	}

	text, blocksJSON, errCode := extractMessageContent(params)
	if errCode != "" {
		return slackErr(c, errCode)
	}

	threadTS := paramString(params, "thread_ts")
	username := paramString(params, "username")

	msg, err := h.chatUC.PostBotMessage(c.Request().Context(), bot, chID, threadTS, username, text, blocksJSON)
	if err != nil {
		switch {
		case errors.Is(err, usecase.ErrChannelNotFound):
			return slackErr(c, "channel_not_found")
		case errors.Is(err, usecase.ErrThreadNotFound):
			return slackErr(c, "thread_not_found")
		default:
			slog.Error("chat.postMessage CreateMessage failed", "channel", chID, "error", err)
			return slackErr(c, "internal_error")
		}
	}

	return slackOK(c, map[string]interface{}{
		"channel": chID,
		"ts":      msg.TS,
		"message": formatSlackMessage(*msg),
	})
}

func (h *Handler) chatUpdate(c echo.Context, bot domain.BotIdentity, params map[string]interface{}) error {
	chID := paramString(params, "channel")
	ts := paramString(params, "ts")
	if chID == "" || ts == "" {
		return slackErr(c, "message_not_found")
	}

	if _, err := h.chatUC.GetChannel(chID); err != nil {
		if errors.Is(err, usecase.ErrChannelNotFound) {
			return slackErr(c, "channel_not_found")
		}
		return slackErr(c, "internal_error")
	}

	text, blocksJSON, errCode := extractMessageContent(params)
	if errCode != "" {
		return slackErr(c, errCode)
	}

	updated, err := h.chatUC.UpdateBotMessage(c.Request().Context(), bot, chID, ts, text, blocksJSON)
	if err != nil {
		switch {
		case errors.Is(err, usecase.ErrChannelNotFound):
			return slackErr(c, "channel_not_found")
		case errors.Is(err, usecase.ErrNotFound):
			return slackErr(c, "message_not_found")
		case errors.Is(err, usecase.ErrCantUpdateMessage):
			return slackErr(c, "cant_update_message")
		default:
			return slackErr(c, "internal_error")
		}
	}

	return slackOK(c, map[string]interface{}{
		"channel": chID,
		"ts":      updated.TS,
		"text":    updated.Text,
		"message": formatSlackMessage(*updated),
	})
}

func (h *Handler) usersInfo(c echo.Context, params map[string]interface{}) error {
	userID := paramString(params, "user")
	if userID == "" {
		return slackErr(c, "user_not_found")
	}

	if userID == domain.SystemBotUserID {
		return slackOK(c, map[string]interface{}{
			"user": formatBotUserInfo(domain.SystemBotUserID, domain.SystemBotName),
		})
	}

	if bot, ok := h.socketUC.FindBotByUserID(userID); ok {
		return slackOK(c, map[string]interface{}{
			"user": formatBotUserInfo(bot.UserID, bot.Name),
		})
	}

	if u, ok := h.authUC.FindUserByID(userID); ok {
		return slackOK(c, map[string]interface{}{
			"user": map[string]interface{}{
				"id":        u.ID,
				"team_id":   domain.TeamID,
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

	return slackErr(c, "user_not_found")
}

func formatBotUserInfo(userID, name string) map[string]interface{} {
	return map[string]interface{}{
		"id":        userID,
		"team_id":   domain.TeamID,
		"name":      name,
		"real_name": name,
		"is_bot":    true,
		"profile": map[string]interface{}{
			"display_name": name,
			"real_name":    name,
		},
	}
}
