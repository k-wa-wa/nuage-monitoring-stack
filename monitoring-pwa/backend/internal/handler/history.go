package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/alertmanager"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

// HistoryHandler は通知履歴 API のリクエストを処理する構造体である。
type HistoryHandler struct {
	db       *push.DB
	amClient *alertmanager.Client
}

func NewHistoryHandler(db *push.DB, amClient *alertmanager.Client) *HistoryHandler {
	return &HistoryHandler{
		db:       db,
		amClient: amClient,
	}
}

// History はデータベースに保存された通知履歴を取得する。
func (h *HistoryHandler) History(c echo.Context) error {
	limitStr := c.QueryParam("limit")
	limit := 50
	if limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
			if limit > 100 {
				limit = 100
			}
		}
	}

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
	limitStr := c.QueryParam("limit")
	limit := 50
	if limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil && val > 0 {
			limit = val
			if limit > 100 {
				limit = 100
			}
		}
	}

	alerts, err := h.db.ListAlertHistory(limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch alert history")
	}

	if alerts == nil {
		alerts = []push.Alert{}
	}

	return c.JSON(http.StatusOK, alerts)
}
