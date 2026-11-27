package httpadapter

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

// ActivityHandler は通知タブ向け API (`/api/activity`) を処理する。
type ActivityHandler struct {
	activityUC *usecase.ActivityUsecase
}

// NewActivityHandler は新しい ActivityHandler を生成する。
func NewActivityHandler(activityUC *usecase.ActivityUsecase) *ActivityHandler {
	return &ActivityHandler{activityUC: activityUC}
}

// Activity はアラート・通知のスレッドの親メッセージを新しい順に返す。
// filter: all (既定) / firing / resolved / notice、before: この ts より古いものだけ、limit: 件数。
func (h *ActivityHandler) Activity(c echo.Context) error {
	filter, ok := domain.ParseActivityFilter(c.QueryParam("filter"))
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid filter")
	}
	limit := parseLimitParam(c, 50, 200)
	res, err := h.activityUC.List(filter, c.QueryParam("before"), limit)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list activity")
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"items":        toActivityItemDTOs(res.Items),
		"has_more":     res.HasMore,
		"firing_count": res.FiringCount,
	})
}
