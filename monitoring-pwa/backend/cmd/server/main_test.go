package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/alertmanager"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/config"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/push"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/realtime"
)

type noopSender struct{}

func (noopSender) Broadcast([]byte) {}

func TestEndToEndAuthBoundary(t *testing.T) {
	hashAlice, _ := bcrypt.GenerateFromPassword([]byte("alice-secret"), bcrypt.MinCost)
	hashBob, _ := bcrypt.GenerateFromPassword([]byte("bob-secret"), bcrypt.MinCost)

	cfg := config.Config{
		Port:             "8080",
		VAPIDPublicKey:   "pub",
		VAPIDPrivateKey:  "priv",
		CookieSigningKey: strings.Repeat("s", 32),
		SlackBotToken:    "xoxb-e2e-bot-token",
		SlackAppToken:    "xapp-e2e-app-token",
		Users: []config.UserConfig{
			{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: string(hashAlice)},
			{ID: "U02BOB", Name: "bob", DisplayName: "Bob", PassphraseHash: string(hashBob)},
		},
	}

	db, err := push.InitDB(filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()
	if err := db.EnsureUserChannels(cfg.Users); err != nil {
		t.Fatalf("EnsureUserChannels failed: %v", err)
	}

	e := NewRouter(cfg, db, realtime.NewHub(), noopSender{}, alertmanager.NewClient("http://localhost:9093"))

	// 1. /health と /webhook/* は認証不要
	healthRec := httptest.NewRecorder()
	e.ServeHTTP(healthRec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /health, got %d", healthRec.Code)
	}

	whReq := httptest.NewRequest(http.MethodPost, "/webhook/generic", strings.NewReader(`{"title":"Deploy","body":"ok"}`))
	whReq.Header.Set("Content-Type", "application/json")
	whRec := httptest.NewRecorder()
	e.ServeHTTP(whRec, whReq)
	if whRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /webhook/generic, got %d", whRec.Code)
	}

	// 2. /api/* は未ログイン時 401
	for _, path := range []string{"/api/auth/me", "/api/bootstrap", "/api/notifications", "/api/channels"} {
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for unauthenticated %s, got %d", path, rec.Code)
		}
	}

	// 3. 誤った合言葉は 401
	badLogin := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"passphrase":"wrong"}`))
	badLogin.Header.Set("Content-Type", "application/json")
	badRec := httptest.NewRecorder()
	e.ServeHTTP(badRec, badLogin)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong passphrase, got %d", badRec.Code)
	}

	// 4. Alice でログイン -> Cookie で /api/bootstrap と /api/notifications が通る
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"passphrase":"alice-secret"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	e.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid login, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie on login")
	}
	sessionCookie := cookies[0]

	bootReq := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	bootReq.AddCookie(sessionCookie)
	bootRec := httptest.NewRecorder()
	e.ServeHTTP(bootRec, bootReq)
	if bootRec.Code != http.StatusOK {
		t.Fatalf("expected 200 on /api/bootstrap, got %d", bootRec.Code)
	}
	var boot map[string]interface{}
	_ = json.NewDecoder(bytes.NewReader(bootRec.Body.Bytes())).Decode(&boot)
	if boot["user"].(map[string]interface{})["id"] != "U01ALICE" {
		t.Fatalf("expected U01ALICE, got %+v", boot["user"])
	}

	// 5. Alice から Bob の DM へのアクセスは 403
	bobDMReq := httptest.NewRequest(http.MethodGet, "/api/channels/"+push.DMChannelID("U02BOB")+"/messages", nil)
	bobDMReq.AddCookie(sessionCookie)
	bobDMRec := httptest.NewRecorder()
	e.ServeHTTP(bobDMRec, bobDMReq)
	if bobDMRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 accessing another user's DM, got %d", bobDMRec.Code)
	}
}
