package handler

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/auth"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
)

// AuthHandler はログイン・ログアウト・現在のユーザー情報 API を処理する。
type AuthHandler struct {
	signingKey []byte
	users      []config.UserConfig
}

// NewAuthHandler は新しい AuthHandler を生成する。
func NewAuthHandler(signingKey []byte, users []config.UserConfig) *AuthHandler {
	return &AuthHandler{
		signingKey: signingKey,
		users:      users,
	}
}

type authUserDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

func toAuthUserDTO(u *config.UserConfig) authUserDTO {
	return authUserDTO{
		ID:          u.ID,
		Name:        u.EffectiveName(),
		DisplayName: u.DisplayName,
	}
}

// Login は合言葉を検証し、署名付きセッション Cookie を発行する。
func (h *AuthHandler) Login(c echo.Context) error {
	var req struct {
		Passphrase string `json:"passphrase"`
		User       string `json:"user,omitempty"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	user, err := auth.VerifyPassphrase(h.users, req.Passphrase, req.User)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "合言葉が正しくない")
	}

	token, err := auth.IssueSessionToken(h.signingKey, user.ID, time.Now())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue session")
	}

	auth.SetSessionCookie(c, token)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"user": toAuthUserDTO(user),
	})
}

// Logout はセッション Cookie を破棄する。
func (h *AuthHandler) Logout(c echo.Context) error {
	auth.ClearSessionCookie(c)
	return c.NoContent(http.StatusNoContent)
}

// Me は現在ログイン中のユーザー情報を返す。
func (h *AuthHandler) Me(c echo.Context) error {
	user := auth.GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"user": toAuthUserDTO(user),
	})
}
