package handler

import (
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/auth"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
)

const (
	loginRateWindow      = 1 * time.Minute
	loginRateMaxFailures = 10
)

// AuthHandler はログイン・ログアウト・現在のユーザー情報 API を処理する。
type AuthHandler struct {
	signingKey []byte
	users      []config.UserConfig
	mu         sync.Mutex
	failures   map[string][]time.Time
	nowFunc    func() time.Time
}

// NewAuthHandler は新しい AuthHandler を生成する。
func NewAuthHandler(signingKey []byte, users []config.UserConfig) *AuthHandler {
	return &AuthHandler{
		signingKey: signingKey,
		users:      users,
		failures:   make(map[string][]time.Time),
		nowFunc:    time.Now,
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

func (h *AuthHandler) isRateLimited(ip string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := now.Add(-loginRateWindow)
	entries := h.failures[ip]
	kept := entries[:0]
	for _, ts := range entries {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) == 0 {
		delete(h.failures, ip)
		return false
	}
	h.failures[ip] = kept
	return len(kept) >= loginRateMaxFailures
}

func (h *AuthHandler) recordFailure(ip string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := now.Add(-loginRateWindow)
	// 古い IP エントリの掃除
	if len(h.failures) > 512 {
		for k, list := range h.failures {
			if len(list) == 0 || !list[len(list)-1].After(cutoff) {
				delete(h.failures, k)
			}
		}
	}

	entries := h.failures[ip]
	kept := entries[:0]
	for _, ts := range entries {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	h.failures[ip] = append(kept, now)
}

func (h *AuthHandler) clearFailures(ip string) {
	h.mu.Lock()
	delete(h.failures, ip)
	h.mu.Unlock()
}

// Login は合言葉を検証し、署名付きセッション Cookie を発行する。
func (h *AuthHandler) Login(c echo.Context) error {
	ip := c.RealIP()
	now := h.nowFunc()
	if h.isRateLimited(ip, now) {
		return echo.NewHTTPError(http.StatusTooManyRequests, "ログイン試行回数の上限に達したため、しばらく待ってから再試行してほしい")
	}

	var req struct {
		Passphrase string `json:"passphrase"`
		User       string `json:"user,omitempty"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	user, err := auth.VerifyPassphrase(h.users, req.Passphrase, req.User)
	if err != nil {
		h.recordFailure(ip, now)
		return echo.NewHTTPError(http.StatusUnauthorized, "合言葉が正しくない")
	}

	h.clearFailures(ip)
	token, err := auth.IssueSessionToken(h.signingKey, user.ID, now)
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
