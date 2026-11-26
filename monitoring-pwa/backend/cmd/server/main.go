package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/alertmanager"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/auth"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/handler"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/realtime"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	// SQLite データベースの初期化
	db, err := push.InitDB(cfg.DBPath)
	if err != nil {
		slog.Error("failed to initialize SQLite database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.EnsureUserChannels(cfg.Users); err != nil {
		slog.Error("failed to initialize user DM channels", "error", err)
		os.Exit(1)
	}

	hub := realtime.NewHub()
	amClient := alertmanager.NewClient(cfg.AlertmanagerURL)
	sender := push.NewSender(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject, db)

	e := NewRouter(cfg, db, hub, sender, amClient)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("starting server", "port", cfg.Port, "db_path", cfg.DBPath)
		if err := e.Start(":" + cfg.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server stopped", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down server")
	hub.DisconnectAll("server_shutdown")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
}

// NewRouter は認証・チャット・Slack 互換 API・Webhook を含む Echo ルーターを構築する。
func NewRouter(cfg config.Config, db *push.DB, hub *realtime.Hub, sender handler.PushSender, amClient handler.ActiveAlertsFetcher) *echo.Echo {
	subscribeH := handler.NewSubscribeHandler(cfg.VAPIDPublicKey, db)
	webhookH := handler.NewWebhookHandlerWithHub(sender, db, hub)
	historyH := handler.NewHistoryHandler(db, amClient)
	authH := handler.NewAuthHandler([]byte(cfg.CookieSigningKey), cfg.Users)
	chatH := handler.NewChatHandler(cfg, db, hub)
	slackH := handler.NewSlackHandler(cfg, db, hub, sender)

	e := echo.New()
	e.HideBanner = true
	e.IPExtractor = buildTrustedIPExtractor()
	e.Use(middleware.RequestID())
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOriginFunc: func(origin string) (bool, error) {
			u, err := url.Parse(origin)
			if err != nil || u.Scheme != "http" {
				return false, nil
			}
			host := u.Hostname()
			return host == "localhost" || host == "127.0.0.1", nil
		},
		AllowCredentials: true,
	}))

	// 認証不要の公開エンドポイント
	e.GET("/health", handler.Health)
	e.POST("/webhook/alertmanager", webhookH.AlertmanagerWebhook)
	e.POST("/webhook/generic", webhookH.GenericWebhook)
	e.POST("/webhook/keel", webhookH.KeelWebhook)
	e.POST("/api/auth/login", authH.Login)
	e.POST("/api/auth/logout", authH.Logout)

	// 人間向け API (署名付きセッション Cookie 必須)
	api := e.Group("/api")
	api.Use(auth.RequireSessionMiddleware([]byte(cfg.CookieSigningKey), cfg.Users))
	api.GET("/auth/me", authH.Me)
	api.GET("/bootstrap", chatH.Bootstrap)
	api.GET("/channels", chatH.ListChannels)
	api.GET("/channels/:id/messages", chatH.ListMessages)
	api.GET("/channels/:id/threads/:ts", chatH.GetThread)
	api.POST("/channels/:id/messages", chatH.PostMessage)
	api.POST("/channels/:id/read", chatH.MarkRead)
	api.GET("/search", chatH.Search)
	api.GET("/deeplink/:kind/:id", chatH.DeepLink)
	api.GET("/events", chatH.Events)

	// 既存 Push / 履歴 API
	api.GET("/vapid-public-key", subscribeH.VAPIDPublicKey)
	api.POST("/subscribe", subscribeH.Subscribe)
	api.POST("/unsubscribe", subscribeH.Unsubscribe)
	api.POST("/test-notify", webhookH.TestNotify)
	api.GET("/history", historyH.History)
	api.GET("/alerts/active", historyH.ActiveAlerts)
	api.GET("/alerts/history", historyH.AlertHistory)
	api.GET("/notifications", historyH.UnifiedNotifications)
	api.GET("/notifications/:kind/:id", historyH.GetNotificationDetail)

	// Slack 互換 API (bot / app トークン必須) & Socket Mode WebSocket
	e.POST("/slack/api/:method", slackH.DispatchMethod)
	e.GET("/slack/socket", slackH.SocketModeWS)

	return e
}

// buildTrustedIPExtractor はクラスタ内プロキシ (Cilium Pod CIDR と Loopback) からの
// X-Forwarded-For のみを信頼して実クライアント IP を抽出する。
// HAProxy (mode tcp, PROXY protocol なし) の 10.20.1.x は信頼対象に含めず、
// XFF 左端の偽装を防ぐ。
func buildTrustedIPExtractor() echo.IPExtractor {
	opts := []echo.TrustOption{
		echo.TrustLoopback(true),
		echo.TrustLinkLocal(false),
		echo.TrustPrivateNet(false),
	}
	if _, ipNet, err := net.ParseCIDR("10.244.0.0/16"); err == nil {
		opts = append(opts, echo.TrustIPRange(ipNet))
	}
	return echo.ExtractIPFromXFFHeader(opts...)
}
