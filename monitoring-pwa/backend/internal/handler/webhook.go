package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/realtime"
)

// PushSender は Web Push 通知を送信するインターフェースである。
type PushSender interface {
	Broadcast(payload []byte)
}

// WebhookHandler は Alertmanager および汎用 Webhook リクエストを処理する構造体である。
type WebhookHandler struct {
	sender PushSender
	db     *push.DB
	hub    *realtime.Hub
}

func NewWebhookHandler(sender PushSender, db *push.DB) *WebhookHandler {
	return &WebhookHandler{
		sender: sender,
		db:     db,
	}
}

// NewWebhookHandlerWithHub はリアルタイム配信 Hub 付きの WebhookHandler を生成する。
func NewWebhookHandlerWithHub(sender PushSender, db *push.DB, hub *realtime.Hub) *WebhookHandler {
	return &WebhookHandler{
		sender: sender,
		db:     db,
		hub:    hub,
	}
}

// notificationPayload はサービスワーカーが受信するプッシュ通知ペイロードの形式である。
type notificationPayload struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	URL        string `json:"url,omitempty"`
	TargetURL  string `json:"target_url,omitempty"`
	DirectOpen bool   `json:"direct_open,omitempty"`
	Level      string `json:"level,omitempty"` // info, warning, error, success
	Tag        string `json:"tag,omitempty"`
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

// checkAuth は Webhook の認証チェックを行う。
func (h *WebhookHandler) checkAuth(c echo.Context) error {
	return nil
}

// AlertmanagerWebhook は Alertmanager の通知を受信し、状態保存およびプッシュ通知を処理する。
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

		fingerprint := push.GenerateFingerprint(alert.Labels)

		title := fmt.Sprintf("[%s] %s", alert.Status, name)
		summary := alert.Annotations["summary"]
		if summary == "" {
			summary = alert.Annotations["description"]
		}
		if summary == "" {
			summary = name
		}
		description := alert.Annotations["description"]

		level := "warning"
		if alert.Status == "firing" {
			if alert.Labels["severity"] == "critical" {
				level = "error"
			}
		} else if alert.Status == "resolved" {
			level = "success"
		}

		alertDetails, _ := json.Marshal(alert)

		existing, findErr := h.db.GetAlertByFingerprint(fingerprint)
		isNewCycle := findErr != nil || existing == nil || (alert.Status == "firing" && existing.Status != "firing")

		// DBにアラート状態を保存・更新（統合一覧と /alerts/:id ディープリンクのソース）
		savedAlert, err := h.db.UpsertAlert(
			fingerprint,
			name,
			alert.Status,
			level,
			summary,
			description,
			alert.GeneratorURL,
			string(alertDetails),
		)
		if err != nil {
			slog.Error("failed to upsert alert", "fingerprint", fingerprint, "error", err)
		} else if savedAlert != nil {
			msg, parent, chatErr := h.db.PostAlertChatMessage(savedAlert, isNewCycle, name, summary, string(alertDetails), alert.GeneratorURL)
			if chatErr != nil {
				slog.Error("failed to post alert chat message", "fingerprint", fingerprint, "error", chatErr)
			} else {
				h.emitSystemMessage(push.ChannelAlertmanager, msg, parent)
			}
		}

		// resolved の場合は Push 通知（バイブ・通知音）を送信しない
		if alert.Status == "resolved" {
			continue
		}

		// firing の場合のみ Push 通知をブロードキャストする
		payloadObj := notificationPayload{
			Title:     title,
			Body:      summary,
			URL:       "/",
			TargetURL: alert.GeneratorURL,
			Level:     level,
		}
		if savedAlert != nil {
			payloadObj.URL = fmt.Sprintf("/alerts/%d", savedAlert.ID)
			payloadObj.Tag = fmt.Sprintf("alert-%d", savedAlert.ID)
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
	targetURL := strings.TrimSpace(req.URL)
	if targetURL == "" {
		targetURL = strings.TrimSpace(req.TargetURL)
	}
	source := push.InferNotificationSource(req.Title, req.Source)

	// DBに履歴を保存
	id, err := h.db.SaveNotificationWithSource(req.Title, req.Body, targetURL, level, req.Details, source)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save notification history")
	}

	h.saveAndEmitNotificationChat(id, source, req.Title, req.Body, level, req.Details, targetURL)

	payloadObj := notificationPayload{
		Title:      req.Title,
		Body:       req.Body,
		URL:        fmt.Sprintf("/notifications/%d", id),
		TargetURL:  targetURL,
		DirectOpen: req.DirectOpen,
		Level:      level,
		Tag:        fmt.Sprintf("notification-%d", id),
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
	id, err := h.db.SaveNotificationWithSource(title, body, "", level, string(detailsObj), "keel")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save notification history")
	}

	h.saveAndEmitNotificationChat(id, "keel", title, body, level, string(detailsObj), "")

	payloadObj := notificationPayload{
		Title: title,
		Body:  body,
		URL:   fmt.Sprintf("/notifications/%d", id),
		Level: level,
		Tag:   fmt.Sprintf("notification-%d", id),
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

func (h *WebhookHandler) saveAndEmitNotificationChat(noteID int64, source, title, body, level, details, targetURL string) {
	chID := push.ChannelForSource(source)
	username := "Nuage Notify"
	switch source {
	case "argocd":
		username = "Argo CD"
	case "keel":
		username = "Keel"
	}

	text := fmt.Sprintf("*%s*\n%s", strings.TrimSpace(title), strings.TrimSpace(body))
	if strings.TrimSpace(body) == "" || strings.TrimSpace(body) == strings.TrimSpace(title) {
		text = fmt.Sprintf("*%s*", strings.TrimSpace(title))
	}

	msg, err := h.db.CreateMessage(push.CreateMessageParams{
		ChannelID:  chID,
		UserID:     push.SystemBotUserID,
		BotID:      push.SystemBotID,
		Username:   username,
		Text:       text,
		Level:      level,
		SourceKind: "notification",
		SourceID:   noteID,
		Details:    details,
		URL:        targetURL,
	})
	if err != nil {
		slog.Error("failed to post notification chat message", "note_id", noteID, "error", err)
		return
	}
	h.emitSystemMessage(chID, msg, nil)
}

func (h *WebhookHandler) emitSystemMessage(channelID string, msg, updatedParent *push.Message) {
	if h.hub == nil || msg == nil {
		return
	}
	h.hub.PublishBrowser("", realtime.BrowserEvent{
		Type:      "message_created",
		ChannelID: channelID,
		Payload:   msg,
	})
	if updatedParent != nil && updatedParent.TS != msg.TS {
		h.hub.PublishBrowser("", realtime.BrowserEvent{
			Type:      "message_updated",
			ChannelID: channelID,
			Payload:   updatedParent,
		})
	}
	ev := map[string]interface{}{
		"type":         "message",
		"channel":      channelID,
		"channel_type": "channel",
		"user":         msg.UserID,
		"bot_id":       msg.BotID,
		"text":         msg.Text,
		"ts":           msg.TS,
		"event_ts":     msg.TS,
	}
	if msg.ThreadTS != "" {
		ev["thread_ts"] = msg.ThreadTS
	}
	h.hub.SendEventCallback(ev)
}
