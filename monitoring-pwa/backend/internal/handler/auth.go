package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/auth"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
)

const (
	loginPenaltyWindow       = 1 * time.Minute
	loginPenaltyFreeAttempts = 2
	loginPenaltyBaseDelay    = 500 * time.Millisecond
	loginPenaltyMaxDelay     = 4 * time.Second
	loginMaxQueueWait        = 12 * time.Second
)

type ipPenaltyState struct {
	failures    []time.Time
	nextAllowed time.Time
}

// AuthHandler はログイン・ログアウト・現在のユーザー情報 API を処理する。
type AuthHandler struct {
	signingKey []byte
	users      []config.UserConfig
	mu         sync.Mutex
	states     map[string]*ipPenaltyState
	nowFunc    func() time.Time
	sleepFunc  func(ctx context.Context, d time.Duration) error
}

// NewAuthHandler は新しい AuthHandler を生成する。
func NewAuthHandler(signingKey []byte, users []config.UserConfig) *AuthHandler {
	return &AuthHandler{
		signingKey: signingKey,
		users:      users,
		states:     make(map[string]*ipPenaltyState),
		nowFunc:    time.Now,
		sleepFunc:  waitWithContext,
	}
}

func waitWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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

func backoffForFailures(count int) time.Duration {
	if count <= loginPenaltyFreeAttempts {
		return 0
	}
	shift := count - loginPenaltyFreeAttempts - 1
	if shift > 3 {
		shift = 3
	}
	d := loginPenaltyBaseDelay << shift
	if d > loginPenaltyMaxDelay {
		return loginPenaltyMaxDelay
	}
	return d
}

func pruneFailures(entries []time.Time, cutoff time.Time) []time.Time {
	kept := entries[:0]
	for _, ts := range entries {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	return kept
}

func (h *AuthHandler) reserveAttempt(ip string, now time.Time) (time.Duration, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	state, ok := h.states[ip]
	if !ok {
		return 0, false
	}

	cutoff := now.Add(-loginPenaltyWindow)
	state.failures = pruneFailures(state.failures, cutoff)
	if len(state.failures) == 0 && !state.nextAllowed.After(now) {
		delete(h.states, ip)
		return 0, false
	}

	scheduled := now
	if state.nextAllowed.After(now) {
		scheduled = state.nextAllowed
	}
	wait := scheduled.Sub(now)
	if wait > loginMaxQueueWait {
		return 0, true
	}

	delay := backoffForFailures(len(state.failures))
	if delay > 0 {
		state.nextAllowed = scheduled.Add(delay)
	}
	return wait, false
}

func (h *AuthHandler) recordFailure(ip string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	cutoff := now.Add(-loginPenaltyWindow)
	if len(h.states) > 512 {
		for k, s := range h.states {
			s.failures = pruneFailures(s.failures, cutoff)
			if len(s.failures) == 0 && !s.nextAllowed.After(now) {
				delete(h.states, k)
			}
		}
	}

	state, ok := h.states[ip]
	if !ok {
		state = &ipPenaltyState{}
		h.states[ip] = state
	}
	state.failures = append(pruneFailures(state.failures, cutoff), now)
	if delay := backoffForFailures(len(state.failures)); delay > 0 {
		next := now.Add(delay)
		if next.After(state.nextAllowed) {
			state.nextAllowed = next
		}
	}
}

func (h *AuthHandler) clearFailures(ip string) {
	h.mu.Lock()
	delete(h.states, ip)
	h.mu.Unlock()
}

// Login は合言葉を検証し、署名付きセッション Cookie を発行する。
// 失敗回数に応じたバックオフ遅延で総当たりを抑制しつつ、同一プロキシ IP 配下の正規ユーザーの締め出しを防ぐ。
func (h *AuthHandler) Login(c echo.Context) error {
	ip := c.RealIP()
	now := h.nowFunc()
	wait, overloaded := h.reserveAttempt(ip, now)
	if overloaded {
		return echo.NewHTTPError(http.StatusTooManyRequests, "ログイン試行が集中しているため、しばらく待ってから再試行してほしい")
	}
	if wait > 0 {
		if err := h.sleepFunc(c.Request().Context(), wait); err != nil {
			return echo.NewHTTPError(http.StatusRequestTimeout, "request cancelled")
		}
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
		h.recordFailure(ip, h.nowFunc())
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
