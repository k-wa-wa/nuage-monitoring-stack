package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
)

const (
	// SessionCookieName はログインセッションを保持する署名付き Cookie 名である。
	SessionCookieName = "nuage_notify_session"
	// SessionTTL はセッション Cookie の有効期限（90 日）である。
	SessionTTL = 90 * 24 * time.Hour

	contextUserKey = "auth_user"
)

var (
	ErrInvalidPassphrase = errors.New("invalid passphrase")
	ErrInvalidSession    = errors.New("invalid or expired session")
)

type sessionPayload struct {
	UserID string `json:"uid"`
	Issued int64  `json:"iat"`
	Expiry int64  `json:"exp"`
}

// VerifyPassphrase は宣言されたユーザー一覧から合言葉が一致するユーザーを特定する。
// usernameOrID が指定された場合は該当ユーザーのみを照合する。
func VerifyPassphrase(users []config.UserConfig, passphrase, usernameOrID string) (*config.UserConfig, error) {
	if passphrase == "" {
		return nil, ErrInvalidPassphrase
	}
	target := strings.TrimSpace(usernameOrID)
	for i := range users {
		u := &users[i]
		if target != "" && u.ID != target && !strings.EqualFold(u.EffectiveName(), target) && !strings.EqualFold(u.DisplayName, target) {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(u.PassphraseHash), []byte(passphrase)); err == nil {
			copyUser := *u
			return &copyUser, nil
		}
	}
	return nil, ErrInvalidPassphrase
}

// IssueSessionToken は署名付きセッショントークンを発行する。
func IssueSessionToken(signingKey []byte, userID string, now time.Time) (string, error) {
	payload := sessionPayload{
		UserID: userID,
		Issued: now.Unix(),
		Expiry: now.Add(SessionTTL).Unix(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	sig := sign(signingKey, encoded)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// VerifySessionToken は署名付きセッショントークンを検証し、宣言済みユーザーを返す。
func VerifySessionToken(signingKey []byte, users []config.UserConfig, token string, now time.Time) (*config.UserConfig, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrInvalidSession
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidSession
	}
	expectedSig := sign(signingKey, parts[0])
	if !hmac.Equal(sig, expectedSig) {
		return nil, ErrInvalidSession
	}

	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidSession
	}
	var payload sessionPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return nil, ErrInvalidSession
	}
	if payload.UserID == "" || payload.Expiry <= now.Unix() {
		return nil, ErrInvalidSession
	}

	for i := range users {
		if users[i].ID == payload.UserID {
			copyUser := users[i]
			return &copyUser, nil
		}
	}
	return nil, ErrInvalidSession
}

func sign(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

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
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   IsSecureRequest(c),
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearSessionCookie はセッション Cookie を削除する。
func ClearSessionCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   IsSecureRequest(c),
		SameSite: http.SameSiteLaxMode,
	})
}

// RequireSessionMiddleware は署名付きセッション Cookie を必須とする Echo ミドルウェアを返す。
func RequireSessionMiddleware(signingKey []byte, users []config.UserConfig) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			cookie, err := c.Cookie(SessionCookieName)
			if err != nil || cookie.Value == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			user, err := VerifySessionToken(signingKey, users, cookie.Value, time.Now())
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired session")
			}
			c.Set(contextUserKey, user)
			return next(c)
		}
	}
}

// GetUser はリクエストコンテキストから認証済みユーザーを取得する。
func GetUser(c echo.Context) *config.UserConfig {
	u, _ := c.Get(contextUserKey).(*config.UserConfig)
	return u
}
