package httpadapter

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

// Events はログイン済みブラウザ向けに Server-Sent Events (SSE) で新着・状態変化を配信する。
func (h *ChatHandler) Events(c echo.Context) error {
	user := GetUser(c)
	ch, cancel, online := h.chatUC.SubscribeEvents(user.ID)
	defer cancel()

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	writeEvent := func(ev domain.BrowserEvent) error {
		dto := toBrowserEventDTO(ev)
		b, err := json.Marshal(dto)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", dto.Type, string(b)); err != nil {
			return err
		}
		w.Flush()
		return nil
	}

	// 初回に接続状態を送る
	if err := writeEvent(domain.BrowserEvent{
		Type:      domain.BrowserEventBotStatus,
		BotOnline: &online,
	}); err != nil {
		return nil
	}

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return nil
			}
			w.Flush()
		case ev, ok := <-ch:
			if !ok {
				return nil
			}
			if err := writeEvent(ev); err != nil {
				return nil
			}
		}
	}
}
