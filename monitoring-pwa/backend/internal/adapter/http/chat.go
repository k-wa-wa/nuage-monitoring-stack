package httpadapter

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

// ChatHandler は人間向けチャット UI API (`/api/channels/*`, `/api/search` 等) を処理する。
type ChatHandler struct {
	chatUC *usecase.ChatUsecase
}

// NewChatHandler は新しい ChatHandler を生成する。
func NewChatHandler(chatUC *usecase.ChatUsecase) *ChatHandler {
	return &ChatHandler{chatUC: chatUC}
}

// Bootstrap はログイン後の初期画面に必要なユーザー・チャンネル・bot 状態を一括返却する。
func (h *ChatHandler) Bootstrap(c echo.Context) error {
	user := GetUser(c)
	res, err := h.chatUC.Bootstrap(*user)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load channels")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"user":     toAuthUserDTO(&res.User),
		"channels": toChannelDTOs(res.Channels),
		"bot": map[string]interface{}{
			"id":      res.Bot.ID,
			"user_id": res.Bot.UserID,
			"name":    res.Bot.Name,
			"online":  res.Bot.Online,
		},
	})
}

// ListChannels はユーザーがアクセス可能なチャンネル一覧と未読数を返す。
func (h *ChatHandler) ListChannels(c echo.Context) error {
	user := GetUser(c)
	channels, err := h.chatUC.ListChannelsForUser(user.ID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list channels")
	}
	return c.JSON(http.StatusOK, toChannelDTOs(channels))
}

func mapChannelError(err error, fallback string) error {
	switch {
	case errors.Is(err, usecase.ErrNotFound):
		return echo.NewHTTPError(http.StatusNotFound, "channel not found")
	case errors.Is(err, usecase.ErrForbidden):
		return echo.NewHTTPError(http.StatusForbidden, "access denied to another user's DM")
	default:
		return echo.NewHTTPError(http.StatusInternalServerError, fallback)
	}
}

// ListMessages はチャンネルのメッセージ一覧（古い→新しい順、before ページネーション対応）を返す。
func (h *ChatHandler) ListMessages(c echo.Context) error {
	user := GetUser(c)
	limit := parseLimitParam(c, 40, 100)
	beforeTS := strings.TrimSpace(c.QueryParam("before"))

	ch, msgs, hasMore, err := h.chatUC.ListMessages(user.ID, c.Param("id"), beforeTS, limit)
	if err != nil {
		return mapChannelError(err, "failed to list messages")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"channel":  toChannelDTO(*ch),
		"messages": toMessageDTOs(msgs),
		"has_more": hasMore,
	})
}

// GetThread は指定スレッドの親メッセージと返信一覧を返す。
func (h *ChatHandler) GetThread(c echo.Context) error {
	user := GetUser(c)
	ch, parent, replies, err := h.chatUC.GetThreadForUser(user.ID, c.Param("id"), c.Param("ts"))
	if err != nil {
		if errors.Is(err, usecase.ErrNotFound) {
			return echo.NewHTTPError(http.StatusNotFound, "thread not found")
		}
		if errors.Is(err, usecase.ErrForbidden) {
			return echo.NewHTTPError(http.StatusForbidden, "access denied to another user's DM")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to load thread")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"channel": toChannelDTO(*ch),
		"parent":  toMessageDTO(*parent),
		"replies": toMessageDTOs(replies),
	})
}

// PostMessage は人間ユーザーからのメッセージを投稿し、Socket Mode で bot へ配信する。
func (h *ChatHandler) PostMessage(c echo.Context) error {
	user := GetUser(c)
	var req struct {
		Text     string `json:"text"`
		ThreadTS string `json:"thread_ts,omitempty"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid message payload")
	}

	res, err := h.chatUC.PostHumanMessage(c.Request().Context(), *user, c.Param("id"), req.Text, req.ThreadTS)
	if err != nil {
		switch {
		case errors.Is(err, usecase.ErrInvalidInput):
			return echo.NewHTTPError(http.StatusBadRequest, "text is required")
		case errors.Is(err, usecase.ErrThreadNotFound):
			return echo.NewHTTPError(http.StatusNotFound, "thread parent not found")
		default:
			return mapChannelError(err, "failed to save message")
		}
	}

	return c.JSON(http.StatusCreated, map[string]interface{}{
		"message":          toMessageDTO(res.Message),
		"bot_online":       res.BotOnline,
		"delivered_to_bot": res.DeliveredToBot,
	})
}

// MarkRead はチャンネルを既読にする。
func (h *ChatHandler) MarkRead(c echo.Context) error {
	user := GetUser(c)
	var req struct {
		TS string `json:"ts"`
	}
	_ = c.Bind(&req)

	if err := h.chatUC.MarkRead(user.ID, c.Param("id"), req.TS); err != nil {
		return mapChannelError(err, "failed to mark read")
	}
	return c.NoContent(http.StatusNoContent)
}

// Search はキーワード検索を実行する。
func (h *ChatHandler) Search(c echo.Context) error {
	user := GetUser(c)
	q := strings.TrimSpace(c.QueryParam("q"))
	channelID := strings.TrimSpace(c.QueryParam("channel_id"))
	limit := parseLimitParam(c, 30, 100)

	results, err := h.chatUC.Search(user.ID, q, channelID, limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to search messages")
	}
	return c.JSON(http.StatusOK, toSearchResultDTOs(results))
}

// parseLimitParam は limit クエリを読む。無い・不正なら既定値、上限を超えたら上限にする。
func parseLimitParam(c echo.Context, defaultLimit, maxLimit int) int {
	raw := strings.TrimSpace(c.QueryParam("limit"))
	if raw == "" {
		return defaultLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return defaultLimit
	}
	if n > maxLimit {
		return maxLimit
	}
	return n
}
