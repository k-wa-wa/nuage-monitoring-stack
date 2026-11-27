package httpadapter

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

const contextUserKey = "auth_user"

// IsSecureRequest はリクエストが HTTPS 経由かを判定する。
func IsSecureRequest(c echo.Context) bool {
	if c.Request().TLS != nil {
		return true
	}
	return strings.EqualFold(c.Request().Header.Get("X-Forwarded-Proto"), "https")
}

// SetSessionCookie はレスポンスに署名付きセッション Cookie を設定する。
func SetSessionCookie(c echo.Context, token string) {
	c.SetCookie(&http.Cookie{
		Name:     usecase.SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(usecase.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   IsSecureRequest(c),
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie はセッション Cookie を削除する。
func ClearSessionCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     usecase.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   IsSecureRequest(c),
		SameSite: http.SameSiteLaxMode,
	})
}

// RequireSessionMiddleware は署名付きセッション Cookie を必須とする Echo ミドルウェアを返す。
func RequireSessionMiddleware(authUC *usecase.AuthUsecase) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cookie, err := c.Cookie(usecase.SessionCookieName)
			if err != nil || cookie.Value == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			user, err := authUC.AuthenticateSession(cookie.Value)
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
			}
			c.Set(contextUserKey, user)
			return next(c)
		}
	}
}

// GetUser はリクエストコンテキストから認証済みユーザーを取得する。
func GetUser(c echo.Context) *domain.User {
	u, _ := c.Get(contextUserKey).(*domain.User)
	return u
}
