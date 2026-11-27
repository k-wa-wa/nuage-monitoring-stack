package usecase

import (
	"context"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

func TestPassphraseAndSessionToken(t *testing.T) {
	hash1, _ := bcrypt.GenerateFromPassword([]byte("alpha-pass"), bcrypt.MinCost)
	hash2, _ := bcrypt.GenerateFromPassword([]byte("beta-pass"), bcrypt.MinCost)
	users := []domain.User{
		{ID: "U01ALPHA", Name: "alpha", DisplayName: "Alpha", PassphraseHash: string(hash1)},
		{ID: "U02BETA", Name: "beta", DisplayName: "Beta", PassphraseHash: string(hash2)},
	}
	signingKey := []byte("0123456789abcdef0123456789abcdef")
	uc := NewAuthUsecase(signingKey, users)
	now := time.Unix(1700000000, 0)

	t.Run("複数ユーザーから合言葉のみで正しくユーザーを特定できること", func(t *testing.T) {
		u, err := uc.VerifyPassphrase("beta-pass", "")
		if err != nil {
			t.Fatalf("VerifyPassphrase failed: %v", err)
		}
		if u.ID != "U02BETA" {
			t.Fatalf("expected U02BETA, got %s", u.ID)
		}

		if _, err := uc.VerifyPassphrase("wrong-pass", ""); err == nil {
			t.Fatal("expected error for wrong passphrase")
		}
	})

	t.Run("署名付きセッショントークンの発行と検証ができること", func(t *testing.T) {
		token, err := uc.IssueSessionToken("U01ALPHA", now)
		if err != nil {
			t.Fatalf("IssueSessionToken failed: %v", err)
		}

		u, err := uc.VerifySessionToken(token, now.Add(30*24*time.Hour))
		if err != nil {
			t.Fatalf("VerifySessionToken failed: %v", err)
		}
		if u.ID != "U01ALPHA" {
			t.Fatalf("expected U01ALPHA, got %s", u.ID)
		}

		// 別の鍵では検証失敗すること
		otherUC := NewAuthUsecase([]byte("different-key-different-key-1234"), users)
		if _, err := otherUC.VerifySessionToken(token, now); err == nil {
			t.Fatal("expected failure with different signing key")
		}

		// 期限切れ (90日超) は検証失敗すること
		if _, err := uc.VerifySessionToken(token, now.Add(91*24*time.Hour)); err == nil {
			t.Fatal("expected failure for expired token")
		}

		// SOPS から消されたユーザー ID は検証失敗すること
		reducedUC := NewAuthUsecase(signingKey, users[1:])
		if _, err := reducedUC.VerifySessionToken(token, now); err == nil {
			t.Fatal("expected failure for removed user")
		}
	})
}

func TestLoginRateLimit(t *testing.T) {
	hash, _ := bcrypt.GenerateFromPassword([]byte("valid-pass"), bcrypt.MinCost)
	uc := NewAuthUsecase([]byte(strings.Repeat("k", 32)), []domain.User{
		{ID: "U01ADMIN", DisplayName: "Admin", PassphraseHash: string(hash)},
	})
	now := time.Unix(1700000000, 0)
	uc.nowFunc = func() time.Time { return now }

	var waits []time.Duration
	uc.sleepFunc = func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}

	// 1. 失敗回数が増えるにつれてバックオフ遅延が増加すること (2回までは 0ms)
	for i := 0; i < 6; i++ {
		if _, _, err := uc.Login(context.Background(), "198.51.100.10", "wrong", ""); err != ErrInvalidPassphrase {
			t.Fatalf("attempt %d: expected ErrInvalidPassphrase, got %v", i, err)
		}
	}
	// 3回目失敗後の4回目(i=3)に500ms、5回目(i=4)に1s、6回目(i=5)に2s が設定されること
	if len(waits) != 3 || waits[0] != 500*time.Millisecond || waits[1] != 1*time.Second || waits[2] != 2*time.Second {
		t.Fatalf("unexpected backoff waits: %v", waits)
	}

	// 2. 失敗が連続した後でも、正規の合言葉を送れば締め出されずログインできること
	if u, tok, err := uc.Login(context.Background(), "198.51.100.10", "valid-pass", ""); err != nil || u == nil || tok == "" {
		t.Fatalf("expected valid login after failures, got user=%v tok=%q err=%v", u, tok, err)
	}

	// 3. 待機キューが上限 (loginMaxQueueWait) を超える集中攻撃のみ ErrTooManyRequests で弾かれること
	waits = nil
	for i := 0; i < 6; i++ {
		_, _, _ = uc.Login(context.Background(), "198.51.100.11", "wrong", "")
	}
	gotTooMany := false
	for i := 0; i < 6; i++ {
		if _, _, err := uc.Login(context.Background(), "198.51.100.11", "wrong", ""); err == ErrTooManyRequests {
			gotTooMany = true
			break
		}
	}
	if !gotTooMany {
		t.Fatal("expected ErrTooManyRequests when concurrent/burst queue wait exceeds limit")
	}

	// 過負荷時は readCredentials (リクエスト Bind) より先に ErrTooManyRequests で弾かれること
	called := false
	if _, _, err := uc.LoginWithReader(context.Background(), "198.51.100.11", func() (string, string, error) {
		called = true
		return "", "", ErrInvalidInput
	}); err != ErrTooManyRequests {
		t.Fatalf("expected ErrTooManyRequests before reader, got %v", err)
	}
	if called {
		t.Fatal("readCredentials must not be called when overloaded")
	}

	// 別 IP は遅延なくログイン可能であること
	waitsBefore := len(waits)
	if u, _, err := uc.Login(context.Background(), "198.51.100.20", "valid-pass", ""); err != nil || u == nil {
		t.Fatalf("expected success from another IP, got user=%v err=%v", u, err)
	}
	if len(waits) != waitsBefore {
		t.Fatalf("expected no wait for another IP, got %v", waits[waitsBefore:])
	}
}
