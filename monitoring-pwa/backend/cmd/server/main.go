package main

import (
	"log/slog"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/handler"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	if cfg.VAPIDPublicKey == "" || cfg.VAPIDPrivateKey == "" {
		slog.Error("VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY must be set (see: go run ./cmd/genvapid)")
		os.Exit(1)
	}

	// SQLite データベースの初期化
	db, err := push.InitDB(cfg.DBPath)
	if err != nil {
		slog.Error("failed to initialize SQLite database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	sender := push.NewSender(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject, db)

	subscribeH := handler.NewSubscribeHandler(cfg.VAPIDPublicKey, db)
	webhookH := handler.NewWebhookHandler(sender, db, cfg.WebhookToken)
	historyH := handler.NewHistoryHandler(db)
	metricsH := handler.NewMetricsHandler(cfg.PrometheusURL)

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.RequestID())
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// 開発時等のクロスオリジンアクセスに対応するため CORS を有効化
	e.Use(middleware.CORS())

	e.GET("/health", handler.Health)

	api := e.Group("/api")
	api.GET("/vapid-public-key", subscribeH.VAPIDPublicKey)
	api.POST("/subscribe", subscribeH.Subscribe)
	api.POST("/unsubscribe", subscribeH.Unsubscribe)
	api.POST("/test-notify", webhookH.TestNotify)
	api.GET("/history", historyH.History)
	api.GET("/cluster/status", metricsH.ClusterStatus)

	e.POST("/webhook/alertmanager", webhookH.AlertmanagerWebhook)
	e.POST("/webhook/generic", webhookH.GenericWebhook)

	slog.Info("starting server", "port", cfg.Port, "db_path", cfg.DBPath, "prometheus_url", cfg.PrometheusURL)
	if err := e.Start(":" + cfg.Port); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
