package httpadapter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"

	httpadapter "github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/adapter/http"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/realtime"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/sqlite"
	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/usecase"
)

type noopSender struct{}

func (noopSender) Broadcast(context.Context, domain.PushNotification)                 {}
func (noopSender) BroadcastToUser(context.Context, string, domain.PushNotification) {}

func TestEndToEndAuthBoundary(t *testing.T) {
	hashAlice, _ := bcrypt.GenerateFromPassword([]byte("alice-secret"), bcrypt.MinCost)
	hashBob, _ := bcrypt.GenerateFromPassword([]byte("bob-secret"), bcrypt.MinCost)

	users := []domain.User{
		{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: string(hashAlice)},
		{ID: "U02BOB", Name: "bob", DisplayName: "Bob", PassphraseHash: string(hashBob)},
	}

	db, err := sqlite.InitDB(filepath.Join(t.TempDir(), "e2e.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()
	if err := db.EnsureUserChannels(users); err != nil {
		t.Fatalf("EnsureUserChannels failed: %v", err)
	}

	hub := realtime.NewHub()
	authUC := usecase.NewAuthUsecase([]byte(strings.Repeat("s", 32)), users)
	chatUC := usecase.NewChatUsecase(db, db, hub, hub, noopSender{})
	e := httpadapter.NewRouter(httpadapter.Dependencies{
		Auth:         authUC,
		Chat:         chatUC,
		Activity:     usecase.NewActivityUsecase(db),
		Alert:        usecase.NewAlertUsecase(db, db, nil, noopSender{}, realtime.NewLocalLocker()),
		Notification: usecase.NewNotificationUsecase(db, nil, noopSender{}),
		Subscription: usecase.NewSubscriptionUsecase("pub", db, noopSender{}),
	})

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
	for _, path := range []string{"/api/auth/me", "/api/bootstrap", "/api/activity", "/api/channels"} {
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

	// 4. Alice でログイン -> Cookie で /api/bootstrap が通る
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
	bobDMReq := httptest.NewRequest(http.MethodGet, "/api/channels/"+domain.DMChannelID("U02BOB")+"/messages", nil)
	bobDMReq.AddCookie(sessionCookie)
	bobDMRec := httptest.NewRecorder()
	e.ServeHTTP(bobDMRec, bobDMReq)
	if bobDMRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 accessing another user's DM, got %d", bobDMRec.Code)
	}

	// 6. CORS は localhost のみ許可し、*.wpcapp.net 等の外部 Origin には ACAO を返さない
	crossReq := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	crossReq.Header.Set("Origin", "https://monitoring-pr-1.wpcapp.net")
	crossReq.AddCookie(sessionCookie)
	crossRec := httptest.NewRecorder()
	e.ServeHTTP(crossRec, crossReq)
	if acao := crossRec.Header().Get("Access-Control-Allow-Origin"); acao != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin for preview subdomain, got %q", acao)
	}

	localReq := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	localReq.Header.Set("Origin", "http://localhost:3000")
	localReq.AddCookie(sessionCookie)
	localRec := httptest.NewRecorder()
	e.ServeHTTP(localRec, localReq)
	if acao := localRec.Header().Get("Access-Control-Allow-Origin"); acao != "http://localhost:3000" {
		t.Fatalf("expected localhost ACAO, got %q", acao)
	}
}

func TestLoginRateLimitBehindTrustedProxy(t *testing.T) {
	hashAlice, _ := bcrypt.GenerateFromPassword([]byte("alice-secret"), bcrypt.MinCost)
	users := []domain.User{
		{ID: "U01ALICE", Name: "alice", DisplayName: "Alice", PassphraseHash: string(hashAlice)},
	}

	db, err := sqlite.InitDB(filepath.Join(t.TempDir(), "ratelimit.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()
	if err := db.EnsureUserChannels(users); err != nil {
		t.Fatalf("EnsureUserChannels failed: %v", err)
	}

	hub := realtime.NewHub()
	authUC := usecase.NewAuthUsecase([]byte(strings.Repeat("s", 32)), users)
	e := httpadapter.NewRouter(httpadapter.Dependencies{
		Auth:         authUC,
		Chat:         usecase.NewChatUsecase(db, db, hub, hub, noopSender{}),
		Activity:     usecase.NewActivityUsecase(db),
		Alert:        usecase.NewAlertUsecase(db, db, nil, noopSender{}, realtime.NewLocalLocker()),
		Notification: usecase.NewNotificationUsecase(db, nil, noopSender{}),
		Subscription: usecase.NewSubscriptionUsecase("pub", db, noopSender{}),
	})

	// 1. HAProxy (10.20.1.x) -> Cilium Envoy (10.244.x.x) の実経路で、
	// クライアントが偽装した XFF 左端は無視され、lb の IP (10.20.1.21) が返ること。
	reqSpoof := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	reqSpoof.RemoteAddr = "10.244.2.7:45678"
	reqSpoof.Header.Set("X-Forwarded-For", "1.2.3.4, 10.20.1.21")
	if got := e.IPExtractor(reqSpoof); got != "10.20.1.21" {
		t.Fatalf("expected lb IP 10.20.1.21 (ignoring spoofed left XFF), got %q", got)
	}

	// 非信頼 IP (198.51.100.5) からの直接接続では XFF 偽装が効かないこと
	reqDirect := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	reqDirect.RemoteAddr = "198.51.100.5:1111"
	reqDirect.Header.Set("X-Forwarded-For", "203.0.113.200")
	if got := e.IPExtractor(reqDirect); got != "198.51.100.5" {
		t.Fatalf("expected direct IP 198.51.100.5, got %q", got)
	}

	sendLogin := func(remoteAddr, xff, passphrase string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"passphrase":"`+passphrase+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = remoteAddr
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	// 2. XFF 左端を変えながら複数回失敗しても、同一の lb IP (10.20.1.21) 枠で扱い、
	// かつその直後に正規の合言葉を送った本人は締め出されず 200 でログインできること。
	for i := 0; i < 3; i++ {
		xff := "198.51.100." + string(rune('1'+i)) + ", 10.20.1.21"
		if code := sendLogin("10.244.2.7:45678", xff, "wrong"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i, code)
		}
	}
	if code := sendLogin("10.244.2.7:45679", "10.20.1.21", "alice-secret"); code != http.StatusOK {
		t.Fatalf("expected 200 for valid passphrase after failures via lb, got %d", code)
	}
}

func TestSessionMiddleware(t *testing.T) {
	hash1, _ := bcrypt.GenerateFromPassword([]byte("alpha-pass"), bcrypt.MinCost)
	hash2, _ := bcrypt.GenerateFromPassword([]byte("beta-pass"), bcrypt.MinCost)
	users := []domain.User{
		{ID: "U01ALPHA", Name: "alpha", DisplayName: "Alpha", PassphraseHash: string(hash1)},
		{ID: "U02BETA", Name: "beta", DisplayName: "Beta", PassphraseHash: string(hash2)},
	}
	authUC := usecase.NewAuthUsecase([]byte("0123456789abcdef0123456789abcdef"), users)

	e := echo.New()
	mw := httpadapter.RequireSessionMiddleware(authUC)
	protected := mw(func(c echo.Context) error {
		u := httpadapter.GetUser(c)
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
	token, _ := authUC.IssueSessionToken("U02BETA", time.Now())
	req2 := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req2.AddCookie(&http.Cookie{Name: usecase.SessionCookieName, Value: token})
	rec2 := httptest.NewRecorder()
	if err := protected(e.NewContext(req2, rec2)); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if rec2.Body.String() != "U02BETA" {
		t.Fatalf("expected U02BETA, got %s", rec2.Body.String())
	}
}
