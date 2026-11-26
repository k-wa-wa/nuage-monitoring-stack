package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/notify"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

// PushSender は Web Push 通知を送信するインターフェースである。
type PushSender interface {
	Broadcast(payload []byte)
	BroadcastToUser(userID string, payload []byte)
}

// WebhookHandler は Alertmanager・Argo CD・Keel・汎用 Webhook を受け、notify.Service へ渡す。
type WebhookHandler struct {
	svc    *notify.Service
	sender PushSender
}

// NewWebhookHandler は WebhookHandler を作る。
func NewWebhookHandler(svc *notify.Service, sender PushSender) *WebhookHandler {
	return &WebhookHandler{svc: svc, sender: sender}
}

// alertmanagerWebhook は Alertmanager の webhook_config からのペイロードを表現する。
type alertmanagerWebhook struct {
	Status      string                 `json:"status"`
	ExternalURL string                 `json:"externalURL"`
	Alerts      []notify.IncomingAlert `json:"alerts"`
}

// genericWebhook は汎用通知 Webhook のリクエストボディを表現する。
type genericWebhook struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url,omitempty"`
	TargetURL  string `json:"target_url,omitempty"`
	DirectOpen bool   `json:"direct_open,omitempty"`
	Level      string `json:"level,omitempty"`
	Details    string `json:"details,omitempty"`
	Source     string `json:"source,omitempty"`
}

// keelWebhook は Keel からの通知 Webhook のリクエストボディを表現する。
type keelWebhook struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Type    string `json:"type"`
	Event   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"event,omitempty"`
}

// AlertmanagerWebhook は Alertmanager の通知を受け、アラートごとにその回のスレッドへ反映する。
func (h *WebhookHandler) AlertmanagerWebhook(c echo.Context) error {
	var wh alertmanagerWebhook
	if err := c.Bind(&wh); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid alertmanager payload")
	}
	failed := 0
	for _, alert := range wh.Alerts {
		if err := h.svc.HandleAlert(alert); err != nil {
			failed++
			slog.Error("failed to handle alert", "alertname", alert.Labels["alertname"], "status", alert.Status, "error", err)
		}
	}
	if failed > 0 {
		// Alertmanager に失敗を返し、再送してもらう
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("failed to handle %d alert(s)", failed))
	}
	return c.NoContent(http.StatusOK)
}

// GenericWebhook は外部からの汎用通知を受け、送り手のチャンネルに投稿する。
func (h *WebhookHandler) GenericWebhook(c echo.Context) error {
	var req genericWebhook
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid generic webhook payload")
	}
	if req.Title == "" || req.Body == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "title and body are required")
	}
	targetURL := strings.TrimSpace(req.URL)
	if targetURL == "" {
		targetURL = strings.TrimSpace(req.TargetURL)
	}
	if _, err := h.svc.PostNotification(notify.Notification{
		Source:     push.InferNotificationSource(req.Title, req.Source),
		Title:      req.Title,
		Body:       req.Body,
		Level:      req.Level,
		Details:    req.Details,
		URL:        targetURL,
		DirectOpen: req.DirectOpen,
	}); err != nil {
		slog.Error("failed to post notification", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save notification")
	}
	return c.NoContent(http.StatusOK)
}

// KeelWebhook は Keel からの自動更新通知を受け、#keel に投稿する。
func (h *WebhookHandler) KeelWebhook(c echo.Context) error {
	var req keelWebhook
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid keel webhook payload")
	}
	name := req.Name
	if name == "" {
		name = "Keel"
	}
	body := req.Message
	if body == "" && req.Event != nil {
		body = req.Event.Message
	}
	if body == "" {
		body = fmt.Sprintf("%s のイメージ更新が実行された。", name)
	}
	details, _ := json.Marshal(req)
	if _, err := h.svc.PostNotification(notify.Notification{
		Source:  "keel",
		Title:   fmt.Sprintf("[Keel] %s 更新通知", name),
		Body:    body,
		Level:   "info",
		Details: string(details),
	}); err != nil {
		slog.Error("failed to post keel notification", "error", err)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save notification")
	}
	return c.NoContent(http.StatusOK)
}

// TestNotify は疎通確認のためのテスト通知を送信する (履歴には残さない)。
func (h *WebhookHandler) TestNotify(c echo.Context) error {
	var req notify.PushPayload
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if req.Title == "" {
		req.Title = "テスト通知"
	}
	if req.Body == "" {
		req.Body = "monitoring-pwa からのテスト通知である。"
	}
	if req.Level == "" {
		req.Level = "info"
	}
	if req.URL == "" {
		req.URL = "/"
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode notification")
	}
	h.sender.Broadcast(payload)
	return c.NoContent(http.StatusOK)
}
