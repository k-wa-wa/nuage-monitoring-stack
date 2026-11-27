package spiffe_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/infra/spiffe"
)

type jwksStubServer struct {
	mu      sync.Mutex
	keys    []map[string]interface{}
	status  int
	delay   time.Duration
	calls   atomic.Int32
	srv     *httptest.Server
	started chan struct{}
}

func newJWKSStubServer(t *testing.T) *jwksStubServer {
	t.Helper()
	s := &jwksStubServer{status: http.StatusOK}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		s.mu.Lock()
		delay := s.delay
		status := s.status
		keys := append([]map[string]interface{}{}, s.keys...)
		started := s.started
		s.mu.Unlock()

		if started != nil {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": keys})
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *jwksStubServer) setKeys(keys ...map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = http.StatusOK
	s.keys = keys
}

func (s *jwksStubServer) setStatus(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status = status
}

func ecJWK(kid, alg string, pub *ecdsa.PublicKey) map[string]interface{} {
	coordSize := 32
	if pub.Curve == elliptic.P384() {
		coordSize = 48
	}
	xb := make([]byte, coordSize)
	yb := make([]byte, coordSize)
	pub.X.FillBytes(xb)
	pub.Y.FillBytes(yb)
	return map[string]interface{}{
		"kty": "EC",
		"kid": kid,
		"use": "sig",
		"alg": alg,
		"crv": pub.Curve.Params().Name,
		"x":   base64.RawURLEncoding.EncodeToString(xb),
		"y":   base64.RawURLEncoding.EncodeToString(yb),
	}
}

func rsaJWK(kid, alg string, pub *rsa.PublicKey) map[string]interface{} {
	return map[string]interface{}{
		"kty": "RSA",
		"kid": kid,
		"use": "sig",
		"alg": alg,
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

func signJWT(t *testing.T, kid string, method jwt.SigningMethod, key interface{}, claims jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("SignedString failed: %v", err)
	}
	return signed
}

func TestVerifier_VerifyAndKeyRotation(t *testing.T) {
	now := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	curTime := now
	nowFn := func() time.Time { return curTime }

	ecKey1, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa keygen failed: %v", err)
	}
	ecKey2, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa keygen 2 failed: %v", err)
	}
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa keygen failed: %v", err)
	}

	jwksSrv := newJWKSStubServer(t)
	jwksSrv.setKeys(
		ecJWK("kid-ec-1", "ES256", &ecKey1.PublicKey),
		rsaJWK("kid-rsa-1", "RS256", &rsaKey.PublicKey),
	)

	verifier := spiffe.NewVerifier(
		jwksSrv.srv.URL,
		"monitoring-pwa",
		spiffe.WithCooldown(5*time.Second),
		spiffe.WithCacheTTL(1*time.Minute),
		spiffe.WithNowFunc(nowFn),
	)

	ctx := context.Background()

	t.Run("ECDSAとRSAの正常JWTを検証できること", func(t *testing.T) {
		tokEC := signJWT(t, "kid-ec-1", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
			Subject:   "spiffe://nuage/host/atelier/bot",
			Audience:  jwt.ClaimStrings{"monitoring-pwa"},
			ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
			NotBefore: jwt.NewNumericDate(curTime.Add(-10 * time.Second)),
		})
		sub, err := verifier.Verify(ctx, tokEC)
		if err != nil || sub != "spiffe://nuage/host/atelier/bot" {
			t.Fatalf("Verify ES256 failed: sub=%q err=%v", sub, err)
		}

		tokRSA := signJWT(t, "kid-rsa-1", jwt.SigningMethodRS256, rsaKey, jwt.RegisteredClaims{
			Subject:   "spiffe://nuage/ns/nuage-monitoring-stack-pr-42/sa/echo-bot",
			Audience:  jwt.ClaimStrings{"monitoring-pwa"},
			ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
		})
		sub, err = verifier.Verify(ctx, tokRSA)
		if err != nil || sub != "spiffe://nuage/ns/nuage-monitoring-stack-pr-42/sa/echo-bot" {
			t.Fatalf("Verify RS256 failed: sub=%q err=%v", sub, err)
		}
		if jwksSrv.calls.Load() != 1 {
			t.Fatalf("expected 1 JWKS fetch (cached), got %d", jwksSrv.calls.Load())
		}
	})

	t.Run("未知kidはクールダウン中は再取得せず、経過後に再取得して鍵ローテーションへ追従すること", func(t *testing.T) {
		jwksSrv.setKeys(ecJWK("kid-ec-2", "ES256", &ecKey2.PublicKey))
		tokNew := signJWT(t, "kid-ec-2", jwt.SigningMethodES256, ecKey2, jwt.RegisteredClaims{
			Subject:   "spiffe://nuage/host/atelier/bot",
			Audience:  jwt.ClaimStrings{"monitoring-pwa"},
			ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
		})

		// クールダウン (5s) 以内は再取得せず拒否される
		curTime = now.Add(2 * time.Second)
		if _, err := verifier.Verify(ctx, tokNew); err == nil {
			t.Fatal("expected failure during JWKS refresh cooldown")
		}
		if jwksSrv.calls.Load() != 1 {
			t.Fatalf("expected no JWKS fetch during cooldown, got %d", jwksSrv.calls.Load())
		}

		// クールダウン経過後は再取得して成功する
		curTime = now.Add(6 * time.Second)
		sub, err := verifier.Verify(ctx, tokNew)
		if err != nil || sub != "spiffe://nuage/host/atelier/bot" {
			t.Fatalf("expected Verify after rotation to succeed, sub=%q err=%v", sub, err)
		}
		if jwksSrv.calls.Load() != 2 {
			t.Fatalf("expected 2 JWKS fetches after rotation, got %d", jwksSrv.calls.Load())
		}
	})

	t.Run("キャッシュTTL経過後は既知kidであってもJWKSを再取得し、撤回済み鍵を拒否すること", func(t *testing.T) {
		// 直前に kid-ec-2 が取得されているが、配布元から kid-ec-2 を撤回し別鍵のみにする
		jwksSrv.setKeys(ecJWK("kid-ec-1", "ES256", &ecKey1.PublicKey))

		// TTL (1m) 未満ならキャッシュが使われる
		tokRevoked := signJWT(t, "kid-ec-2", jwt.SigningMethodES256, ecKey2, jwt.RegisteredClaims{
			Subject:   "spiffe://nuage/host/atelier/bot",
			Audience:  jwt.ClaimStrings{"monitoring-pwa"},
			ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
		})
		if _, err := verifier.Verify(ctx, tokRevoked); err != nil {
			t.Fatalf("expected cached key before TTL expiry, got %v", err)
		}

		// TTL 経過後は既知 kid-ec-2 でも再取得が走り、撤回済みのため拒否される
		curTime = curTime.Add(61 * time.Second)
		if _, err := verifier.Verify(ctx, tokRevoked); err == nil {
			t.Fatal("expected revoked kid to be rejected after cache TTL expiry")
		}
		if jwksSrv.calls.Load() != 3 {
			t.Fatalf("expected 3 JWKS fetches after TTL expiry, got %d", jwksSrv.calls.Load())
		}
	})

	t.Run("許可alg・既知kid・正当クレームでも署名鍵違い、期限切れ、aud不一致、空白付きsub等を拒否すること", func(t *testing.T) {
		// 30 秒クロックスキュー以内なら許容
		tokWithinLeeway := signJWT(t, "kid-ec-1", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
			Subject:   "spiffe://nuage/host/atelier/bot",
			Audience:  jwt.ClaimStrings{"monitoring-pwa"},
			ExpiresAt: jwt.NewNumericDate(curTime.Add(-20 * time.Second)),
		})
		if _, err := verifier.Verify(ctx, tokWithinLeeway); err != nil {
			t.Fatalf("expected leeway-valid token to pass, got %v", err)
		}

		badTokens := []struct {
			name string
			tok  string
		}{
			{
				name: "既知kid・同一alg・正当クレームで署名鍵違い",
				tok: signJWT(t, "kid-ec-1", jwt.SigningMethodES256, ecKey2, jwt.RegisteredClaims{
					Subject:   "spiffe://nuage/host/atelier/bot",
					Audience:  jwt.ClaimStrings{"monitoring-pwa"},
					ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
				}),
			},
			{
				name: "期限切れ(31秒前)",
				tok: signJWT(t, "kid-ec-1", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
					Subject:   "spiffe://nuage/host/atelier/bot",
					Audience:  jwt.ClaimStrings{"monitoring-pwa"},
					ExpiresAt: jwt.NewNumericDate(curTime.Add(-31 * time.Second)),
				}),
			},
			{
				name: "exp欠落",
				tok: signJWT(t, "kid-ec-1", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
					Subject:  "spiffe://nuage/host/atelier/bot",
					Audience: jwt.ClaimStrings{"monitoring-pwa"},
				}),
			},
			{
				name: "nbfが未来すぎ(31秒先)",
				tok: signJWT(t, "kid-ec-1", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
					Subject:   "spiffe://nuage/host/atelier/bot",
					Audience:  jwt.ClaimStrings{"monitoring-pwa"},
					NotBefore: jwt.NewNumericDate(curTime.Add(31 * time.Second)),
					ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
				}),
			},
			{
				name: "他環境のaud不一致",
				tok: signJWT(t, "kid-ec-1", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
					Subject:   "spiffe://nuage/ns/nuage-monitoring-stack-pr-42/sa/echo-bot",
					Audience:  jwt.ClaimStrings{"monitoring-pwa.nuage-monitoring-stack-pr-42"},
					ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
				}),
			},
			{
				name: "subがspiffe://でない",
				tok: signJWT(t, "kid-ec-1", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
					Subject:   "https://example.com/bot",
					Audience:  jwt.ClaimStrings{"monitoring-pwa"},
					ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
				}),
			},
			{
				name: "subに前後空白がある",
				tok: signJWT(t, "kid-ec-1", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
					Subject:   " spiffe://nuage/host/atelier/bot ",
					Audience:  jwt.ClaimStrings{"monitoring-pwa"},
					ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
				}),
			},
			{
				name: "kid欠落",
				tok: signJWT(t, "", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
					Subject:   "spiffe://nuage/host/atelier/bot",
					Audience:  jwt.ClaimStrings{"monitoring-pwa"},
					ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
				}),
			},
			{
				name: "HS256(対称鍵alg拒否)",
				tok: signJWT(t, "kid-ec-1", jwt.SigningMethodHS256, []byte("secret"), jwt.RegisteredClaims{
					Subject:   "spiffe://nuage/host/atelier/bot",
					Audience:  jwt.ClaimStrings{"monitoring-pwa"},
					ExpiresAt: jwt.NewNumericDate(curTime.Add(5 * time.Minute)),
				}),
			},
		}

		for _, tc := range badTokens {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := verifier.Verify(ctx, tc.tok); err == nil {
					t.Fatalf("expected error for %s", tc.name)
				}
			})
		}
	})
}

func TestVerifier_InitialFailureCooldownAndNonBlockingCacheHit(t *testing.T) {
	baseTime := time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	curTime := baseTime
	nowFn := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return curTime
	}
	advance := func(d time.Duration) {
		mu.Lock()
		curTime = curTime.Add(d)
		mu.Unlock()
	}

	ecKey1, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecKey2, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)

	jwksSrv := newJWKSStubServer(t)
	jwksSrv.setStatus(http.StatusInternalServerError)

	verifier := spiffe.NewVerifier(
		jwksSrv.srv.URL,
		"monitoring-pwa",
		spiffe.WithCooldown(5*time.Second),
		spiffe.WithCacheTTL(5*time.Minute),
		spiffe.WithNowFunc(nowFn),
	)

	tok1 := signJWT(t, "kid-1", jwt.SigningMethodES256, ecKey1, jwt.RegisteredClaims{
		Subject:   "spiffe://nuage/host/atelier/bot",
		Audience:  jwt.ClaimStrings{"monitoring-pwa"},
		ExpiresAt: jwt.NewNumericDate(baseTime.Add(10 * time.Minute)),
	})

	// 1. 初回取得失敗にもクールダウンが適用されること
	if _, err := verifier.Verify(context.Background(), tok1); err == nil {
		t.Fatal("expected initial JWKS failure")
	}
	if jwksSrv.calls.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", jwksSrv.calls.Load())
	}

	advance(2 * time.Second)
	if _, err := verifier.Verify(context.Background(), tok1); err == nil {
		t.Fatal("expected cooldown error after initial failure")
	}
	if jwksSrv.calls.Load() != 1 {
		t.Fatalf("expected cooldown to suppress immediate retry after initial failure, got %d calls", jwksSrv.calls.Load())
	}

	// クールダウン経過後は復旧してキャッシュに載る
	advance(4 * time.Second)
	jwksSrv.setKeys(ecJWK("kid-1", "ES256", &ecKey1.PublicKey))
	if _, err := verifier.Verify(context.Background(), tok1); err != nil {
		t.Fatalf("expected recovery after cooldown, got %v", err)
	}

	// 2. 未知 kid で JWKS 取得が遅れていても、既知 kid のキャッシュヒットはブロックされないこと
	advance(10 * time.Second)
	jwksSrv.mu.Lock()
	jwksSrv.delay = 2 * time.Second
	jwksSrv.started = make(chan struct{}, 1)
	jwksSrv.mu.Unlock()

	tokUnknown := signJWT(t, "kid-2", jwt.SigningMethodES256, ecKey2, jwt.RegisteredClaims{
		Subject:   "spiffe://nuage/host/atelier/bot",
		Audience:  jwt.ClaimStrings{"monitoring-pwa"},
		ExpiresAt: jwt.NewNumericDate(baseTime.Add(10 * time.Minute)),
	})

	slowDone := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(context.Background(), tokUnknown)
		slowDone <- err
	}()

	<-jwksSrv.started

	fastDone := make(chan error, 1)
	go func() {
		_, err := verifier.Verify(context.Background(), tok1)
		fastDone <- err
	}()

	select {
	case err := <-fastDone:
		if err != nil {
			t.Fatalf("cached Verify failed during slow JWKS fetch: %v", err)
		}
	case <-time.After(300 * time.Millisecond):
		t.Fatal("cached Verify was blocked by slow JWKS fetch")
	}

	<-slowDone
}
