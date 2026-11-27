package slackapi

import (
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"
)

type wsSocketSession struct {
	conn  *websocket.Conn
	write sync.Mutex
}

func (s *wsSocketSession) SendJSON(v interface{}) error {
	s.write.Lock()
	defer s.write.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return s.conn.WriteJSON(v)
}

// SocketModeWS は `/slack/socket` の WebSocket 接続を受け付け、hello・events_api・ack・disconnect を処理する。
func (h *Handler) SocketModeWS(c echo.Context) error {
	ticket := c.QueryParam("ticket")
	token := extractBearerToken(c)
	if !h.socketUC.AuthorizeSocket(ticket, token) {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid socket mode ticket")
	}

	conn, err := h.upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	connID, cleanup, err := h.socketUC.RegisterSocket(&wsSocketSession{conn: conn})
	if err != nil {
		slog.Error("failed to register socket mode connection", "error", err)
		return nil
	}
	defer cleanup()

	slog.Info("socket mode client connected", "conn_id", connID)

	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	conn.SetPingHandler(func(appData string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
	})

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			slog.Info("socket mode client disconnected", "conn_id", connID, "error", err)
			return nil
		}
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		if envID, ok := h.socketUC.HandleAck(msg); ok {
			slog.Debug("received socket mode ack", "conn_id", connID, "envelope_id", envID)
		}
	}
}
