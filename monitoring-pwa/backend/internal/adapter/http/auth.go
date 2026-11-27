package httpadapter

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

// AuthHandler はログイン・ログアウト・現在のユーザー情報 API を処理する。
type AuthHandler struct {
	authUC *usecase.AuthUsecase
}

// NewAuthHandler は新しい AuthHandler を生成する。
func NewAuthHandler(authUC *usecase.AuthUsecase) *AuthHandler {
	return &AuthHandler{authUC: authUC}
}

// Login は合言葉を検証し、署名付きセッション Cookie を発行する。
func (h *AuthHandler) Login(c echo.Context) error {
	user, token, err := h.authUC.LoginWithReader(c.Request().Context(), c.RealIP(), func() (string, string, error) {
		var req struct {
			Passphrase string `json:"passphrase"`
			User       string `json:"user,omitempty"`
		}
		if err := c.Bind(&req); err != nil {
			return "", "", usecase.ErrInvalidInput
		}
		return req.Passphrase, req.User, nil
	})
	if err != nil {
		switch {
		case errors.Is(err, usecase.ErrTooManyRequests):
			return echo.NewHTTPError(http.StatusTooManyRequests, "ログイン試行が集中しているため、しばらく待ってから再試行してほしい")
		case errors.Is(err, usecase.ErrRequestCancelled):
			return echo.NewHTTPError(http.StatusRequestTimeout, "request cancelled")
		case errors.Is(err, usecase.ErrInvalidInput):
			return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
		case errors.Is(err, usecase.ErrInvalidPassphrase):
			return echo.NewHTTPError(http.StatusUnauthorized, "合言葉が正しくない")
		default:
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue session")
		}
	}

	SetSessionCookie(c, token)
	return c.JSON(http.StatusOK, map[string]interface{}{
		"user": toAuthUserDTO(user),
	})
}

// Logout はセッション Cookie を破棄する。
func (h *AuthHandler) Logout(c echo.Context) error {
	ClearSessionCookie(c)
	return c.NoContent(http.StatusNoContent)
}

// Me は現在ログイン中のユーザー情報を返す。
func (h *AuthHandler) Me(c echo.Context) error {
	user := GetUser(c)
	if user == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	return c.JSON(http.StatusOK, map[string]interface{}{
		"user": toAuthUserDTO(user),
	})
}
