package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

// WebhookHandler は Alertmanager および汎用 Webhook リクエストを処理する構造体である。
type WebhookHandler struct {
	sender *push.Sender
	db     *push.DB
}

func NewWebhookHandler(sender *push.Sender, db *push.DB) *WebhookHandler {
	return &WebhookHandler{
		sender: sender,
		db:     db,
	}
}

// notificationPayload はサービスワーカーが受信するプッシュ通知ペイロードの形式である。
type notificationPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
	Level string `json:"level,omitempty"` // info, warning, error, success
}

// alertmanagerWebhook は Alertmanager の webhook_config からのペイロードを表現する。
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

// genericWebhook は汎用通知 Webhook のリクエストボディを表現する。
type genericWebhook struct {
	Title   string `json:"title"`
	Body    string `json:"body"`
	URL     string `json:"url,omitempty"`
	Level   string `json:"level,omitempty"`
	Details string `json:"details,omitempty"`
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

// checkAuth は Webhook の認証チェックを行う。
func (h *WebhookHandler) checkAuth(c echo.Context) error {
	return nil
}

// AlertmanagerWebhook は Alertmanager の通知を受信し、プッシュ通知をブロードキャストする。
func (h *WebhookHandler) AlertmanagerWebhook(c echo.Context) error {
	if err := h.checkAuth(c); err != nil {
		return err
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

		level := "warning"
		if alert.Status == "firing" {
			if alert.Labels["severity"] == "critical" {
				level = "error"
			}
		} else if alert.Status == "resolved" {
			level = "success"
		}

		alertDetails, _ := json.Marshal(alert)

		// DBに履歴を保存
		id, err := h.db.SaveNotification(title, body, alert.GeneratorURL, level, string(alertDetails))
		if err != nil {
			id = 0
		}

		payloadObj := notificationPayload{
			Title: title,
			Body:  body,
			URL:   "/",
			Level: level,
		}
		if id > 0 {
			payloadObj.URL = fmt.Sprintf("/history/%d", id)
		}

		payload, err := json.Marshal(payloadObj)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode notification")
		}

		h.sender.Broadcast(payload)
	}

	return c.NoContent(http.StatusOK)
}

// GenericWebhook は外部からの汎用通知を受信し、プッシュ通知をブロードキャストする。
func (h *WebhookHandler) GenericWebhook(c echo.Context) error {
	if err := h.checkAuth(c); err != nil {
		return err
	}

	var req genericWebhook
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid generic webhook payload")
	}

	if req.Title == "" || req.Body == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "title and body are required")
	}

	level := req.Level
	if level == "" {
		level = "info"
	}

	// DBに履歴を保存
	id, err := h.db.SaveNotification(req.Title, req.Body, req.URL, level, req.Details)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save notification history")
	}

	payloadObj := notificationPayload{
		Title: req.Title,
		Body:  req.Body,
		URL:   fmt.Sprintf("/history/%d", id),
		Level: level,
	}

	payload, err := json.Marshal(payloadObj)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode notification")
	}

	h.sender.Broadcast(payload)
	return c.NoContent(http.StatusOK)
}

// KeelWebhook は Keel からの自動更新通知を受信し、プッシュ通知をブロードキャストする。
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

	title := fmt.Sprintf("[Keel] %s 更新通知", name)
	level := "info"

	detailsObj, _ := json.Marshal(req)

	// DBに履歴を保存
	id, err := h.db.SaveNotification(title, body, "", level, string(detailsObj))
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save notification history")
	}

	payloadObj := notificationPayload{
		Title: title,
		Body:  body,
		URL:   fmt.Sprintf("/history/%d", id),
		Level: level,
	}

	payload, err := json.Marshal(payloadObj)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode notification")
	}

	h.sender.Broadcast(payload)
	return c.NoContent(http.StatusOK)
}

// TestNotify は疎通確認のためのテスト通知を送信する。
func (h *WebhookHandler) TestNotify(c echo.Context) error {
	var req notificationPayload
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

	payload, err := json.Marshal(req)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to encode notification")
	}

	h.sender.Broadcast(payload)
	return c.NoContent(http.StatusOK)
}
