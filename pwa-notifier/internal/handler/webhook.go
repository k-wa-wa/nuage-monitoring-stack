package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/pwa-notifier/internal/push"
)

type WebhookHandler struct {
	sender       *push.Sender
	webhookToken string
}

func NewWebhookHandler(sender *push.Sender, webhookToken string) *WebhookHandler {
	return &WebhookHandler{
		sender:       sender,
		webhookToken: webhookToken,
	}
}

// notificationPayload is what the service worker (web/sw.js) expects on
// the "push" event.
type notificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
}

// alertmanagerWebhook mirrors the subset of Alertmanager's webhook_config
// payload (https://prometheus.io/docs/alerting/latest/configuration/#webhook_config)
// that is needed to build a notification.
type alertmanagerWebhook struct {
	Status      string `json:"status"`
	ExternalURL string `json:"externalURL"`
	Alerts      []struct {
		Status       string            `json:"status"`
		Labels       map[string]string `json:"labels"`
		Annotations  map[string]string `json:"annotations"`
		GeneratorURL string            `json:"generatorURL"`
	} `json:"alerts"`
}

// AlertmanagerWebhook receives Alertmanager's webhook_config POST and fans
// each alert out as a push notification to every subscribed browser.
func (h *WebhookHandler) AlertmanagerWebhook(c echo.Context) error {
	if h.webhookToken != "" {
		token := c.Request().Header.Get("Authorization")
		if token != "Bearer "+h.webhookToken {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid webhook token")
		}
	}

	var wh alertmanagerWebhook
	if err := c.Bind(&wh); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid alertmanager payload")
	}

	if len(wh.Alerts) == 0 {
		return c.NoContent(http.StatusOK)
	}

	for _, alert := range wh.Alerts {
		name := alert.Labels["alertname"]
		if name == "" {
			name = "Alert"
		}

		title := fmt.Sprintf("[%s] %s", alert.Status, name)
		body := alert.Annotations["summary"]
		if body == "" {
			body = alert.Annotations["description"]
		}

		payload, err := json.Marshal(notificationPayload{
			Title: title,
			Body:  body,
			URL:   alert.GeneratorURL,
		})
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode notification")
		}

		h.sender.Broadcast(payload)
	}

	return c.NoContent(http.StatusOK)
}

// TestNotify sends an arbitrary title/body notification to every
// subscriber, bypassing Alertmanager entirely. It exists to verify the
// subscribe -> push -> service worker path independently of alerting.
func (h *WebhookHandler) TestNotify(c echo.Context) error {
	var req notificationPayload
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Title == "" {
		req.Title = "Test notification"
	}
	if req.Body == "" {
		req.Body = "pwa-notifier is working"
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode notification")
	}

	h.sender.Broadcast(payload)
	return c.NoContent(http.StatusOK)
}
