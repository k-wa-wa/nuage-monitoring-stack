package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

// ActiveAlertsFetcher は Alertmanager からアクティブアラートを取得するインターフェースである。
type ActiveAlertsFetcher interface {
	GetActiveAlerts(ctx context.Context) ([]push.Alert, error)
}

// UnifiedNotification はアラートおよび一般通知を統合したレスポンス項目である。
type UnifiedNotification struct {
	UID         string     `json:"uid"`
	ID          int64      `json:"id"`
	Kind        string     `json:"kind"`   // "alert" | "notification"
	Source      string     `json:"source"` // "alertmanager" | "keel" | "argocd" | "generic" | ...
	Title       string     `json:"title"`
	Body        string     `json:"body"`
	Description string     `json:"description,omitempty"`
	Status      string     `json:"status"` // "firing" | "resolved" | "notice"
	Level       string     `json:"level"`  // "error" | "warning" | "info" | "success"
	URL         string     `json:"url,omitempty"`
	Details     string     `json:"details,omitempty"`
	Timestamp   time.Time  `json:"timestamp"`
	FiredAt     *time.Time `json:"fired_at,omitempty"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// HistoryHandler は通知履歴 API のリクエストを処理する構造体である。
type HistoryHandler struct {
	db       *push.DB
	amClient ActiveAlertsFetcher
}

func NewHistoryHandler(db *push.DB, amClient ActiveAlertsFetcher) *HistoryHandler {
	return &HistoryHandler{
		db:       db,
		amClient: amClient,
	}
}

func parseLimitParam(c echo.Context, defaultLimit, maxLimit int) int {
	limitStr := c.QueryParam("limit")
	limit := defaultLimit
	if limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
			if limit > maxLimit {
				limit = maxLimit
			}
		}
	}
	return limit
}

// History はデータベースに保存された通知履歴を取得する。
func (h *HistoryHandler) History(c echo.Context) error {
	limit := parseLimitParam(c, 50, 100)

	notes, err := h.db.ListNotifications(limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch notification history")
	}

	if notes == nil {
		notes = []push.Notification{}
	}

	return c.JSON(http.StatusOK, notes)
}

// ActiveAlerts は現在 firing 状態のアラート一覧を Alertmanager からリアルタイム取得する。
func (h *HistoryHandler) ActiveAlerts(c echo.Context) error {
	alerts, err := h.amClient.GetActiveAlerts(c.Request().Context())
	if err != nil {
		slog.Error("failed to fetch active alerts from alertmanager", "error", err)
		return c.JSON(http.StatusOK, []push.Alert{})
	}

	if alerts == nil {
		alerts = []push.Alert{}
	}

	return c.JSON(http.StatusOK, alerts)
}

// AlertHistory はスレッド化された全アラート履歴を取得する。
func (h *HistoryHandler) AlertHistory(c echo.Context) error {
	limit := parseLimitParam(c, 50, 100)

	alerts, err := h.db.ListAlertHistory(limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch alert history")
	}

	if alerts == nil {
		alerts = []push.Alert{}
	}

	return c.JSON(http.StatusOK, alerts)
}

// UnifiedNotifications は Alertmanager のアラートと一般通知を統合した一覧を返す。
func (h *HistoryHandler) UnifiedNotifications(c echo.Context) error {
	limit := parseLimitParam(c, 50, 100)

	var activeAlerts []push.Alert
	if h.amClient != nil {
		var err error
		activeAlerts, err = h.amClient.GetActiveAlerts(c.Request().Context())
		if err != nil {
			slog.Error("failed to fetch active alerts from alertmanager", "error", err)
			activeAlerts = nil
		}
	}

	dbAlerts, err := h.db.ListAlertHistory(limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch alert history")
	}

	notes, err := h.db.ListNotifications(limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch notification history")
	}

	dbAlertByFP := make(map[string]push.Alert, len(dbAlerts))
	for _, a := range dbAlerts {
		dbAlertByFP[a.Fingerprint] = a
	}

	activeFPSet := make(map[string]bool, len(activeAlerts))
	firingItems := make([]UnifiedNotification, 0, len(activeAlerts))
	for _, active := range activeAlerts {
		activeFPSet[active.Fingerprint] = true
		if dbAlert, ok := dbAlertByFP[active.Fingerprint]; ok {
			// DB レコードの ID を引き継ぎ、ディープリンク (/alerts/:id) と一致させる
			merged := active
			merged.ID = dbAlert.ID
			merged.Status = "firing"
			merged.ResolvedAt = nil
			firingItems = append(firingItems, alertToUnified(merged, fmt.Sprintf("alert-%d", dbAlert.ID)))
		} else {
			firingItems = append(firingItems, alertToUnified(active, fmt.Sprintf("active-%d", active.ID)))
		}
	}

	historyItems := make([]UnifiedNotification, 0, len(dbAlerts)+len(notes))
	for _, a := range dbAlerts {
		if activeFPSet[a.Fingerprint] {
			continue
		}
		item := alertToUnified(a, fmt.Sprintf("alert-%d", a.ID))
		if a.Status == "firing" {
			firingItems = append(firingItems, item)
		} else {
			historyItems = append(historyItems, item)
		}
	}

	for _, n := range notes {
		if isLegacyAlertmanagerNotification(n) {
			continue
		}
		historyItems = append(historyItems, notificationToUnified(n))
	}

	sort.Slice(firingItems, func(i, j int) bool {
		return firingItems[i].Timestamp.After(firingItems[j].Timestamp)
	})
	sort.Slice(historyItems, func(i, j int) bool {
		return historyItems[i].Timestamp.After(historyItems[j].Timestamp)
	})

	result := make([]UnifiedNotification, 0, len(firingItems)+len(historyItems))
	result = append(result, firingItems...)
	result = append(result, historyItems...)
	if len(result) > limit {
		result = result[:limit]
	}

	return c.JSON(http.StatusOK, result)
}

// GetNotificationDetail は種別 (alert / notification) と ID を指定して単一の通知詳細を返す。
func (h *HistoryHandler) GetNotificationDetail(c echo.Context) error {
	kind := c.Param("kind")
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid notification id")
	}

	switch kind {
	case "alert":
		a, err := h.db.GetAlertByID(id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return echo.NewHTTPError(http.StatusNotFound, "alert not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch alert")
		}
		return c.JSON(http.StatusOK, alertToUnified(*a, fmt.Sprintf("alert-%d", a.ID)))
	case "notification":
		n, err := h.db.GetNotificationByID(id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return echo.NewHTTPError(http.StatusNotFound, "notification not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch notification")
		}
		return c.JSON(http.StatusOK, notificationToUnified(*n))
	default:
		return echo.NewHTTPError(http.StatusBadRequest, "invalid notification kind")
	}
}

func alertToUnified(a push.Alert, uid string) UnifiedNotification {
	ts := a.UpdatedAt
	if ts.IsZero() {
		ts = a.FiredAt
	}
	firedAt := a.FiredAt
	title := a.Alertname
	if title == "" {
		title = a.Summary
	}
	return UnifiedNotification{
		UID:         uid,
		ID:          a.ID,
		Kind:        "alert",
		Source:      "alertmanager",
		Title:       title,
		Body:        a.Summary,
		Description: a.Description,
		Status:      a.Status,
		Level:       a.Level,
		URL:         a.URL,
		Details:     a.Details,
		Timestamp:   ts,
		FiredAt:     &firedAt,
		ResolvedAt:  a.ResolvedAt,
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
	}
}

func notificationToUnified(n push.Notification) UnifiedNotification {
	source := n.Source
	if source == "" {
		if strings.HasPrefix(n.Title, "[Keel]") {
			source = "keel"
		} else if strings.Contains(strings.ToLower(n.Title), "argo") {
			source = "argocd"
		} else {
			source = "generic"
		}
	}
	return UnifiedNotification{
		UID:       fmt.Sprintf("notification-%d", n.ID),
		ID:        n.ID,
		Kind:      "notification",
		Source:    source,
		Title:     n.Title,
		Body:      n.Body,
		Status:    "notice",
		Level:     n.Level,
		URL:       n.URL,
		Details:   n.Details,
		Timestamp: n.CreatedAt,
		CreatedAt: n.CreatedAt,
		UpdatedAt: n.CreatedAt,
	}
}

// isLegacyAlertmanagerNotification は過去の二重保存によって notifications テーブルに入った Alertmanager 通知を判定する。
func isLegacyAlertmanagerNotification(n push.Notification) bool {
	if n.Source == "alertmanager" {
		return true
	}
	if n.Source != "" {
		return false
	}
	if !strings.HasPrefix(n.Title, "[firing] ") && !strings.HasPrefix(n.Title, "[resolved] ") {
		return false
	}
	if n.Details == "" {
		return false
	}
	var parsed struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	}
	if err := json.Unmarshal([]byte(n.Details), &parsed); err != nil {
		return false
	}
	return len(parsed.Labels) > 0 || len(parsed.Annotations) > 0
}
