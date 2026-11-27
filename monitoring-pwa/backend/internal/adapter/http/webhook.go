package httpadapter

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

// WebhookHandler は Alertmanager・Argo CD・Keel・汎用 Webhook とテスト通知を受け取る。
type WebhookHandler struct {
	alertUC  *usecase.AlertUsecase
	notifyUC *usecase.NotificationUsecase
	subUC    *usecase.SubscriptionUsecase
}

// NewWebhookHandler は新しい WebhookHandler を生成する。
func NewWebhookHandler(
	alertUC *usecase.AlertUsecase,
	notifyUC *usecase.NotificationUsecase,
	subUC *usecase.SubscriptionUsecase,
) *WebhookHandler {
	return &WebhookHandler{
		alertUC:  alertUC,
		notifyUC: notifyUC,
		subUC:    subUC,
	}
}

type alertmanagerAlertDTO struct {
	Fingerprint  string            `json:"fingerprint"`
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	GeneratorURL string            `json:"generatorURL"`
}

type alertmanagerWebhookDTO struct {
	Status      string                 `json:"status"`
	ExternalURL string                 `json:"externalURL"`
	Alerts      []alertmanagerAlertDTO `json:"alerts"`
}

type genericWebhookDTO struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url,omitempty"`
	TargetURL  string `json:"target_url,omitempty"`
	DirectOpen bool   `json:"direct_open,omitempty"`
	Level      string `json:"level,omitempty"`
	Details    string `json:"details,omitempty"`
	Source     string `json:"source,omitempty"`
}

type keelWebhookDTO struct {
	Name    string `json:"name"`
	Message string `json:"message"`
	Type    string `json:"type"`
	Event   *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"event,omitempty"`
}

type testNotifyRequestDTO struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url,omitempty"`
	TargetURL  string `json:"target_url,omitempty"`
	DirectOpen bool   `json:"direct_open,omitempty"`
	Level      string `json:"level,omitempty"`
	Tag        string `json:"tag,omitempty"`
}

// AlertmanagerWebhook は Alertmanager の通知を受け、アラートごとにその回のスレッドへ反映する。
func (h *WebhookHandler) AlertmanagerWebhook(c echo.Context) error {
	var wh alertmanagerWebhookDTO
	if err := c.Bind(&wh); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid alertmanager payload")
	}
	failed := 0
	for _, alert := range wh.Alerts {
		details, _ := json.Marshal(alert)
		in := domain.IncomingAlert{
			Fingerprint:  alert.Fingerprint,
			Status:       alert.Status,
			Labels:       alert.Labels,
			Annotations:  alert.Annotations,
			GeneratorURL: alert.GeneratorURL,
			Details:      string(details),
		}
		if err := h.alertUC.HandleAlert(c.Request().Context(), in); err != nil {
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
	var req genericWebhookDTO
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
	if _, err := h.notifyUC.PostNotification(c.Request().Context(), domain.Notification{
		Source:     domain.InferNotificationSource(req.Title, req.Source),
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
	var req keelWebhookDTO
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
	if _, err := h.notifyUC.PostNotification(c.Request().Context(), domain.Notification{
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
	var req testNotifyRequestDTO
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	h.subUC.SendTestNotification(c.Request().Context(), domain.PushNotification{
		Title:      req.Title,
		Body:       req.Body,
		URL:        req.URL,
		TargetURL:  req.TargetURL,
		DirectOpen: req.DirectOpen,
		Level:      req.Level,
		Tag:        req.Tag,
	})
	return c.NoContent(http.StatusOK)
}
