package handler

import (
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

// SubscribeHandler は Push 購読関連のリクエストを処理する構造体である。
type SubscribeHandler struct {
	vapidPublicKey string
	db             *push.DB
}

func NewSubscribeHandler(vapidPublicKey string, db *push.DB) *SubscribeHandler {
	return &SubscribeHandler{
		vapidPublicKey: vapidPublicKey,
		db:             db,
	}
}

// VAPIDPublicKey は PWA がプッシュ通知購読を開始するために必要な公開鍵を返す。
func (h *SubscribeHandler) VAPIDPublicKey(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"publicKey": h.vapidPublicKey})
}

// Subscribe はブラウザの PushSubscription をデータベースに保存する。
func (h *SubscribeHandler) Subscribe(c echo.Context) error {
	var sub webpush.Subscription
	if err := c.Bind(&sub); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid subscription payload")
	}
	if sub.Endpoint == "" || sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "endpoint and keys are required")
	}

	if err := h.db.Add(sub); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save subscription")
	}
	return c.NoContent(http.StatusCreated)
}

// Unsubscribe は保存されている購読情報を削除する。
func (h *SubscribeHandler) Unsubscribe(c echo.Context) error {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := c.Bind(&req); err != nil || req.Endpoint == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "endpoint is required")
	}

	h.db.Remove(req.Endpoint)
	return c.NoContent(http.StatusNoContent)
}
