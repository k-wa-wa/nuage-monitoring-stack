package main

import (
	"log/slog"
	"os"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/k-wa-wa/nuage-monitoring-stack/pwa-notifier/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/pwa-notifier/internal/handler"
	"github.com/k-wa-wa/nuage-monitoring-stack/pwa-notifier/internal/push"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	if cfg.VAPIDPublicKey == "" || cfg.VAPIDPrivateKey == "" {
		slog.Error("VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY must be set (see: go run ./cmd/genvapid)")
		os.Exit(1)
	}

	store := push.NewStore()
	sender := push.NewSender(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject, store)

	subscribeH := handler.NewSubscribeHandler(cfg.VAPIDPublicKey, store)
	webhookH := handler.NewWebhookHandler(sender, cfg.WebhookToken)

	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.RequestID())
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	e.GET("/health", handler.Health)

	api := e.Group("/api")
	api.GET("/vapid-public-key", subscribeH.VAPIDPublicKey)
	api.POST("/subscribe", subscribeH.Subscribe)
	api.POST("/unsubscribe", subscribeH.Unsubscribe)
	api.POST("/test-notify", webhookH.TestNotify)

	e.POST("/webhook/alertmanager", webhookH.AlertmanagerWebhook)

	// Serve the PWA (manifest, service worker, static assets) last so it
	// doesn't shadow the API routes above.
	e.Static("/", "web")

	slog.Info("starting server", "port", cfg.Port)
	if err := e.Start(":" + cfg.Port); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
