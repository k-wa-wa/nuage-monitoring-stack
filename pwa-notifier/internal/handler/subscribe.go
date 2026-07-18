package handler

import (
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/pwa-notifier/internal/push"
)

type SubscribeHandler struct {
	vapidPublicKey string
	store          *push.Store
}

func NewSubscribeHandler(vapidPublicKey string, store *push.Store) *SubscribeHandler {
	return &SubscribeHandler{
		vapidPublicKey: vapidPublicKey,
		store:          store,
	}
}

// VAPIDPublicKey returns the public key the PWA needs to open a push
// subscription. It is not secret.
func (h *SubscribeHandler) VAPIDPublicKey(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"publicKey": h.vapidPublicKey})
}

// Subscribe stores a browser's PushSubscription (as returned by
// PushManager.subscribe().toJSON()).
func (h *SubscribeHandler) Subscribe(c echo.Context) error {
	var sub webpush.Subscription
	if err := c.Bind(&sub); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid subscription payload")
	}
	if sub.Endpoint == "" || sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "endpoint and keys are required")
	}

	h.store.Add(sub)
	return c.NoContent(http.StatusCreated)
}

// Unsubscribe removes a previously stored subscription.
func (h *SubscribeHandler) Unsubscribe(c echo.Context) error {
	var req struct {
		Endpoint string `json:"endpoint"`
	}
	if err := c.Bind(&req); err != nil || req.Endpoint == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "endpoint is required")
	}

	h.store.Remove(req.Endpoint)
	return c.NoContent(http.StatusNoContent)
}
