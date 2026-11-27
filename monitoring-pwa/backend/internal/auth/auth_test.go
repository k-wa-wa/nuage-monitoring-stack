package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
)

func TestPassphraseAndSessionCookie(t *testing.T) {
	hash1, _ := bcrypt.GenerateFromPassword([]byte("alpha-pass"), bcrypt.MinCost)
	hash2, _ := bcrypt.GenerateFromPassword([]byte("beta-pass"), bcrypt.MinCost)
	users := []config.UserConfig{
		{ID: "U01ALPHA", Name: "alpha", DisplayName: "Alpha", PassphraseHash: string(hash1)},
		{ID: "U02BETA", Name: "beta", DisplayName: "Beta", PassphraseHash: string(hash2)},
	}
	signingKey := []byte("0123456789abcdef0123456789abcdef")
	now := time.Unix(1700000000, 0)

	t.Run("複数ユーザーから合言葉のみで正しくユーザーを特定できること", func(t *testing.T) {
		u, err := VerifyPassphrase(users, "beta-pass", "")
		if err != nil {
			t.Fatalf("VerifyPassphrase failed: %v", err)
		}
		if u.ID != "U02BETA" {
			t.Fatalf("expected U02BETA, got %s", u.ID)
		}

		if _, err := VerifyPassphrase(users, "wrong-pass", ""); err == nil {
			t.Fatal("expected error for wrong passphrase")
		}
	})

	t.Run("署名付きセッショントークンの発行と検証ができること", func(t *testing.T) {
		token, err := IssueSessionToken(signingKey, "U01ALPHA", now)
		if err != nil {
			t.Fatalf("IssueSessionToken failed: %v", err)
		}

		u, err := VerifySessionToken(signingKey, users, token, now.Add(30*24*time.Hour))
		if err != nil {
			t.Fatalf("VerifySessionToken failed: %v", err)
		}
		if u.ID != "U01ALPHA" {
			t.Fatalf("expected U01ALPHA, got %s", u.ID)
		}

		// 別の鍵では検証失敗すること
		if _, err := VerifySessionToken([]byte("different-key-different-key-1234"), users, token, now); err == nil {
			t.Fatal("expected failure with different signing key")
		}

		// 期限切れ (90日超) は検証失敗すること
		if _, err := VerifySessionToken(signingKey, users, token, now.Add(91*24*time.Hour)); err == nil {
			t.Fatal("expected failure for expired token")
		}

		// SOPS から消されたユーザー ID は検証失敗すること
		if _, err := VerifySessionToken(signingKey, users[1:], token, now); err == nil {
			t.Fatal("expected failure for removed user")
		}
	})

	t.Run("Echoミドルウェアが未認証を401で拒否し認証済みを通すこと", func(t *testing.T) {
		e := echo.New()
		mw := RequireSessionMiddleware(signingKey, users)
		protected := mw(func(c echo.Context) error {
			u := GetUser(c)
			return c.String(http.StatusOK, u.ID)
		})

		// 未認証
		req1 := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		rec1 := httptest.NewRecorder()
		err1 := protected(e.NewContext(req1, rec1))
		if err1 == nil {
			t.Fatal("expected 401 error")
		}
		httpErr, ok := err1.(*echo.HTTPError)
		if !ok || httpErr.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 HTTPError, got %v", err1)
		}

		// 認証済み
		token, _ := IssueSessionToken(signingKey, "U02BETA", time.Now())
		req2 := httptest.NewRequest(http.MethodGet, "/api/test", nil)
		req2.AddCookie(&http.Cookie{Name: SessionCookieName, Value: token})
		rec2 := httptest.NewRecorder()
		if err := protected(e.NewContext(req2, rec2)); err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if rec2.Body.String() != "U02BETA" {
			t.Fatalf("expected U02BETA, got %s", rec2.Body.String())
		}
	})
}
