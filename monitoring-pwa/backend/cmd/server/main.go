package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"

	httpadapter "github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/adapter/http"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/adapter/slackapi"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/alertmanager"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/realtime"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/sqlite"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/webpush"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

// alertReconcileInterval は Alertmanager と発生中のスレッドを突き合わせる間隔である。
const alertReconcileInterval = time.Minute

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("invalid configuration", "error", err)
		os.Exit(1)
	}

	users := toDomainUsers(cfg.Users)

	// SQLite データベースの初期化
	db, err := sqlite.InitDB(cfg.DBPath)
	if err != nil {
		slog.Error("failed to initialize SQLite database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.EnsureUserChannels(users); err != nil {
		slog.Error("failed to initialize user DM channels", "error", err)
		os.Exit(1)
	}

	hub := realtime.NewHub()
	amClient := alertmanager.NewClient(cfg.AlertmanagerURL)
	sender := webpush.NewSender(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject, db)
	e, alertUC := buildApp(cfg, users, db, hub, sender)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 解決・発生の Webhook の取りこぼしを、Alertmanager との突き合わせで埋める
	if cfg.AlertReconcile {
		go alertUC.RunReconciler(ctx, amClient, alertReconcileInterval)
	} else {
		slog.Warn("alert reconcile is disabled by ALERT_RECONCILE=off; missed alertmanager webhooks will not be recovered")
	}

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

func buildApp(
	cfg config.Config,
	users []domain.User,
	db *sqlite.DB,
	hub *realtime.Hub,
	sender usecase.PushSender,
) (*echo.Echo, *usecase.AlertUsecase) {
	authUC := usecase.NewAuthUsecase([]byte(cfg.CookieSigningKey), users)
	chatUC := usecase.NewChatUsecase(db, db, hub, hub, sender)
	activityUC := usecase.NewActivityUsecase(db)
	alertUC := usecase.NewAlertUsecase(db, db, hub, sender, realtime.NewLocalLocker())
	notifyUC := usecase.NewNotificationUsecase(db, hub, sender)
	subUC := usecase.NewSubscriptionUsecase(cfg.VAPIDPublicKey, db, sender)
	socketUC := usecase.NewSocketUsecase(hub, cfg.SlackBotToken, cfg.SlackAppToken)
	slackH := slackapi.NewHandler(cfg.Port, chatUC, authUC, socketUC)

	e := httpadapter.NewRouter(httpadapter.Dependencies{
		Auth:         authUC,
		Chat:         chatUC,
		Activity:     activityUC,
		Alert:        alertUC,
		Notification: notifyUC,
		Subscription: subUC,
		Slack:        slackH,
	})
	return e, alertUC
}

func toDomainUsers(cfgUsers []config.UserConfig) []domain.User {
	users := make([]domain.User, 0, len(cfgUsers))
	for _, u := range cfgUsers {
		users = append(users, domain.User{
			ID:             u.ID,
			Name:           u.Name,
			DisplayName:    u.DisplayName,
			PassphraseHash: u.PassphraseHash,
		})
	}
	return users
}
