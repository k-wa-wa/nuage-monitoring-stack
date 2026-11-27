package httpadapter

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

// SubscribeHandler は Push 購読関連のリクエストを処理する。
type SubscribeHandler struct {
	subUC *usecase.SubscriptionUsecase
}

// NewSubscribeHandler は新しい SubscribeHandler を生成する。
func NewSubscribeHandler(subUC *usecase.SubscriptionUsecase) *SubscribeHandler {
	return &SubscribeHandler{subUC: subUC}
}

type subscribeRequestDTO struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// VAPIDPublicKey は PWA がプッシュ通知購読を開始するために必要な公開鍵を返す。
func (h *SubscribeHandler) VAPIDPublicKey(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"publicKey": h.subUC.VAPIDPublicKey()})
}

// Subscribe はブラウザの PushSubscription をデータベースに保存する。
func (h *SubscribeHandler) Subscribe(c echo.Context) error {
	var req subscribeRequestDTO
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid subscription payload")
	}

	userID := ""
	if u := GetUser(c); u != nil {
		userID = u.ID
	}
	err := h.subUC.Subscribe(domain.PushSubscription{
		Endpoint: req.Endpoint,
		P256dh:   req.Keys.P256dh,
		Auth:     req.Keys.Auth,
		UserID:   userID,
	}, userID)
	if err != nil {
		if errors.Is(err, usecase.ErrInvalidInput) {
			return echo.NewHTTPError(http.StatusBadRequest, "endpoint and keys are required")
		}
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

	if err := h.subUC.Unsubscribe(req.Endpoint); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "endpoint is required")
	}
	return c.NoContent(http.StatusNoContent)
}
