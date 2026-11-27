package usecase

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/internal/domain"
)

const (
	// SessionCookieName はログインセッションを保持する署名付き Cookie 名である。
	SessionCookieName = "nuage_notify_session"
	// SessionTTL はセッション Cookie の有効期限（90 日）である。
	SessionTTL = 90 * 24 * time.Hour

	loginPenaltyWindow       = 1 * time.Minute
	loginPenaltyFreeAttempts = 2
	loginPenaltyBaseDelay    = 500 * time.Millisecond
	loginPenaltyMaxDelay     = 4 * time.Second
	loginMaxQueueWait        = 12 * time.Second
)

type sessionPayload struct {
	UserID string `json:"uid"`
	Issued int64  `json:"iat"`
	Expiry int64  `json:"exp"`
}

type ipPenaltyState struct {
	failures    []time.Time
	nextAllowed time.Time
}

// AuthUsecase は合言葉照合・試行制限・セッショントークンの発行と検証を扱う。
type AuthUsecase struct {
	signingKey []byte
	users      []domain.User
	mu         sync.Mutex
	states     map[string]*ipPenaltyState
	nowFunc    func() time.Time
	sleepFunc  func(ctx context.Context, d time.Duration) error
}

// NewAuthUsecase は新しい AuthUsecase を生成する。
func NewAuthUsecase(signingKey []byte, users []domain.User) *AuthUsecase {
	return &AuthUsecase{
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

// Login は IP ごとのバックオフ試行制限を適用しつつ合言葉を検証し、セッショントークンを返す。
func (u *AuthUsecase) Login(ctx context.Context, clientIP, passphrase, usernameOrID string) (*domain.User, string, error) {
	return u.LoginWithReader(ctx, clientIP, func() (string, string, error) {
		return passphrase, usernameOrID, nil
	})
}

// LoginWithReader は IP ごとのバックオフ試行制限を予約・待機した後に readCredentials を呼び出して合言葉を検証する。
// リクエスト解析 (Bind) 前に試行制限を適用することで、過負荷時や遅延待機中の順序を維持する。
func (u *AuthUsecase) LoginWithReader(
	ctx context.Context,
	clientIP string,
	readCredentials func() (passphrase, usernameOrID string, err error),
) (*domain.User, string, error) {
	now := u.nowFunc()
	wait, overloaded := u.reserveAttempt(clientIP, now)
	if overloaded {
		return nil, "", ErrTooManyRequests
	}
	if wait > 0 {
		if err := u.sleepFunc(ctx, wait); err != nil {
			return nil, "", ErrRequestCancelled
		}
	}

	passphrase, usernameOrID, err := readCredentials()
	if err != nil {
		return nil, "", err
	}

	user, err := u.VerifyPassphrase(passphrase, usernameOrID)
	if err != nil {
		u.recordFailure(clientIP, u.nowFunc())
		return nil, "", err
	}

	u.clearFailures(clientIP)
	token, err := u.IssueSessionToken(user.ID, now)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// VerifyPassphrase は宣言されたユーザー一覧から合言葉が一致するユーザーを特定する。
// usernameOrID が指定された場合は該当ユーザーのみを照合する。
func (u *AuthUsecase) VerifyPassphrase(passphrase, usernameOrID string) (*domain.User, error) {
	if passphrase == "" {
		return nil, ErrInvalidPassphrase
	}
	target := strings.TrimSpace(usernameOrID)
	for i := range u.users {
		usr := &u.users[i]
		if target != "" && usr.ID != target && !strings.EqualFold(usr.EffectiveName(), target) && !strings.EqualFold(usr.DisplayName, target) {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(usr.PassphraseHash), []byte(passphrase)); err == nil {
			copyUser := *usr
			return &copyUser, nil
		}
	}
	return nil, ErrInvalidPassphrase
}

// IssueSessionToken は署名付きセッショントークンを発行する。
func (u *AuthUsecase) IssueSessionToken(userID string, now time.Time) (string, error) {
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
	sig := signToken(u.signingKey, encoded)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// VerifySessionToken は署名付きセッショントークンを検証し、宣言済みユーザーを返す。
func (u *AuthUsecase) VerifySessionToken(token string, now time.Time) (*domain.User, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, ErrInvalidSession
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalidSession
	}
	expectedSig := signToken(u.signingKey, parts[0])
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

	for i := range u.users {
		if u.users[i].ID == payload.UserID {
			copyUser := u.users[i]
			return &copyUser, nil
		}
	}
	return nil, ErrInvalidSession
}

// AuthenticateSession は現在時刻でセッショントークンを検証する。
func (u *AuthUsecase) AuthenticateSession(token string) (*domain.User, error) {
	return u.VerifySessionToken(token, u.nowFunc())
}

// FindUserByID は宣言された人間ユーザーを ID で検索する。
func (u *AuthUsecase) FindUserByID(userID string) (*domain.User, bool) {
	for i := range u.users {
		if u.users[i].ID == userID {
			copyUser := u.users[i]
			return &copyUser, true
		}
	}
	return nil, false
}

func signToken(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
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

func (u *AuthUsecase) reserveAttempt(ip string, now time.Time) (time.Duration, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	state, ok := u.states[ip]
	if !ok {
		return 0, false
	}

	cutoff := now.Add(-loginPenaltyWindow)
	state.failures = pruneFailures(state.failures, cutoff)
	if len(state.failures) == 0 && !state.nextAllowed.After(now) {
		delete(u.states, ip)
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

func (u *AuthUsecase) recordFailure(ip string, now time.Time) {
	u.mu.Lock()
	defer u.mu.Unlock()

	cutoff := now.Add(-loginPenaltyWindow)
	if len(u.states) > 512 {
		for k, s := range u.states {
			s.failures = pruneFailures(s.failures, cutoff)
			if len(s.failures) == 0 && !s.nextAllowed.After(now) {
				delete(u.states, k)
			}
		}
	}

	state, ok := u.states[ip]
	if !ok {
		state = &ipPenaltyState{}
		u.states[ip] = state
	}
	state.failures = append(pruneFailures(state.failures, cutoff), now)
	if delay := backoffForFailures(len(state.failures)); delay > 0 {
		next := now.Add(delay)
		if next.After(state.nextAllowed) {
			state.nextAllowed = next
		}
	}
}

func (u *AuthUsecase) clearFailures(ip string) {
	u.mu.Lock()
	delete(u.states, ip)
	u.mu.Unlock()
}
