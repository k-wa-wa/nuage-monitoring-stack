package httpadapter

import (
	"net"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

// SlackRoutes は Slack 互換 Web API と Socket Mode WebSocket のハンドラーインターフェースである。
type SlackRoutes interface {
	DispatchMethod(c echo.Context) error
	SocketModeWS(c echo.Context) error
}

// Dependencies はルーターの組み立てに必要なユースケースとハンドラー依存をまとめる。
type Dependencies struct {
	Auth         *usecase.AuthUsecase
	Chat         *usecase.ChatUsecase
	Activity     *usecase.ActivityUsecase
	Alert        *usecase.AlertUsecase
	Notification *usecase.NotificationUsecase
	Subscription *usecase.SubscriptionUsecase
	Slack        SlackRoutes
}

// NewRouter は認証・チャット・Slack 互換 API・Webhook を含む Echo ルーターを構築する。
func NewRouter(deps Dependencies) *echo.Echo {
	subscribeH := NewSubscribeHandler(deps.Subscription)
	webhookH := NewWebhookHandler(deps.Alert, deps.Notification, deps.Subscription)
	authH := NewAuthHandler(deps.Auth)
	chatH := NewChatHandler(deps.Chat)
	activityH := NewActivityHandler(deps.Activity)

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
	e.GET("/health", Health)
	e.POST("/webhook/alertmanager", webhookH.AlertmanagerWebhook)
	e.POST("/webhook/generic", webhookH.GenericWebhook)
	e.POST("/webhook/keel", webhookH.KeelWebhook)
	e.POST("/api/auth/login", authH.Login)
	e.POST("/api/auth/logout", authH.Logout)

	// 人間向け API (署名付きセッション Cookie 必須)
	api := e.Group("/api")
	api.Use(RequireSessionMiddleware(deps.Auth))
	api.GET("/auth/me", authH.Me)
	api.GET("/bootstrap", chatH.Bootstrap)
	api.GET("/channels", chatH.ListChannels)
	api.GET("/channels/:id/messages", chatH.ListMessages)
	api.GET("/channels/:id/threads/:ts", chatH.GetThread)
	api.POST("/channels/:id/messages", chatH.PostMessage)
	api.POST("/channels/:id/read", chatH.MarkRead)
	api.GET("/search", chatH.Search)
	api.GET("/activity", activityH.Activity)
	api.GET("/events", chatH.Events)

	// Web Push
	api.GET("/vapid-public-key", subscribeH.VAPIDPublicKey)
	api.POST("/subscribe", subscribeH.Subscribe)
	api.POST("/unsubscribe", subscribeH.Unsubscribe)
	api.POST("/test-notify", webhookH.TestNotify)

	// Slack 互換 API (bot / app トークン必須) & Socket Mode WebSocket
	if deps.Slack != nil {
		e.POST("/slack/api/:method", deps.Slack.DispatchMethod)
		e.GET("/slack/socket", deps.Slack.SocketModeWS)
	}

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
