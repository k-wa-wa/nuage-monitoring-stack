package spiffe

import (
	"context"
	"crypto"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	defaultRefreshCooldown = 5 * time.Second
	defaultCacheTTL        = 5 * time.Minute
	defaultClockLeeway     = 30 * time.Second
	defaultHTTPTimeout     = 5 * time.Second
	maxJWKSBodyBytes       = 1 << 20
)

var allowedSigningMethods = []string{
	"ES256", "ES384", "ES512",
	"RS256", "RS384", "RS512",
	"PS256", "PS384", "PS512",
}

type jwkEntry struct {
	pub crypto.PublicKey
	alg string
}

type jwksDocument struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type inflightFetch struct {
	done chan struct{}
	err  error
}

// Verifier は SPIRE OIDC Discovery Provider の JWKS を用いて JWT-SVID を検証する。
type Verifier struct {
	jwksURL  string
	audience string
	client   *http.Client
	cooldown time.Duration
	cacheTTL time.Duration
	leeway   time.Duration
	nowFunc  func() time.Time

	mu          sync.Mutex
	keys        map[string]jwkEntry
	lastSuccess time.Time
	lastAttempt time.Time
	inflight    *inflightFetch
}

// Option は Verifier の設定オプションである。
type Option func(*Verifier)

// WithHTTPClient はテスト等向けに HTTP クライアントを差し替える。
func WithHTTPClient(c *http.Client) Option {
	return func(v *Verifier) {
		if c != nil {
			v.client = c
		}
	}
}

// WithCooldown は JWKS 再取得失敗・未知 kid 受信時の最短再取得間隔を設定する。
func WithCooldown(d time.Duration) Option {
	return func(v *Verifier) {
		if d >= 0 {
			v.cooldown = d
		}
	}
}

// WithCacheTTL はキャッシュ済み JWKS を信頼する有効期限を設定する。
func WithCacheTTL(d time.Duration) Option {
	return func(v *Verifier) {
		if d > 0 {
			v.cacheTTL = d
		}
	}
}

// WithNowFunc はテスト等向けに現在時刻関数を差し替える。
func WithNowFunc(fn func() time.Time) Option {
	return func(v *Verifier) {
		if fn != nil {
			v.nowFunc = fn
		}
	}
}

// NewVerifier は新しい JWT-SVID 検証器を生成する。
func NewVerifier(jwksURL, audience string, opts ...Option) *Verifier {
	v := &Verifier{
		jwksURL:  strings.TrimSpace(jwksURL),
		audience: strings.TrimSpace(audience),
		client:   &http.Client{Timeout: defaultHTTPTimeout},
		cooldown: defaultRefreshCooldown,
		cacheTTL: defaultCacheTTL,
		leeway:   defaultClockLeeway,
		nowFunc:  time.Now,
		keys:     make(map[string]jwkEntry),
	}
	for _, opt := range opts {
		opt(v)
	}
	return v
}

// Verify は JWT-SVID の署名・有効期限・対象者 (aud) を検証し、呼び出し元の SPIFFE ID (sub) を返す。
func (v *Verifier) Verify(ctx context.Context, rawToken string) (string, error) {
	tokenStr := strings.TrimSpace(rawToken)
	if tokenStr == "" {
		return "", errors.New("empty jwt token")
	}
	if v.jwksURL == "" || v.audience == "" {
		return "", errors.New("spiffe verifier is not configured")
	}

	var claims jwt.RegisteredClaims
	parserOpts := []jwt.ParserOption{
		jwt.WithValidMethods(allowedSigningMethods),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(v.leeway),
		jwt.WithTimeFunc(v.nowFunc),
	}

	parsed, err := jwt.ParseWithClaims(tokenStr, &claims, func(tok *jwt.Token) (interface{}, error) {
		kid, _ := tok.Header["kid"].(string)
		if kid == "" || kid != strings.TrimSpace(kid) {
			return nil, errors.New("missing or malformed kid header")
		}
		entry, err := v.getKey(ctx, kid)
		if err != nil {
			return nil, err
		}
		if entry.alg != "" && tok.Method.Alg() != entry.alg {
			return nil, fmt.Errorf("jwt alg %q does not match jwk alg %q", tok.Method.Alg(), entry.alg)
		}
		return entry.pub, nil
	}, parserOpts...)
	if err != nil || !parsed.Valid {
		if err == nil {
			err = errors.New("invalid jwt token")
		}
		return "", err
	}

	sub := claims.Subject
	if sub == "" || sub != strings.TrimSpace(sub) || !strings.HasPrefix(sub, "spiffe://") || len(sub) <= len("spiffe://") {
		return "", fmt.Errorf("invalid spiffe sub claim: %q", claims.Subject)
	}
	return sub, nil
}

func (v *Verifier) getKey(ctx context.Context, kid string) (jwkEntry, error) {
	if err := ctx.Err(); err != nil {
		return jwkEntry{}, err
	}

	v.mu.Lock()
	now := v.nowFunc()
	if v.isCacheFreshLocked(now) {
		if entry, ok := v.keys[kid]; ok {
			v.mu.Unlock()
			return entry, nil
		}
	}

	if call := v.inflight; call != nil {
		v.mu.Unlock()
		select {
		case <-call.done:
			if call.err != nil {
				return jwkEntry{}, call.err
			}
			v.mu.Lock()
			entry, ok := v.keys[kid]
			v.mu.Unlock()
			if !ok {
				return jwkEntry{}, fmt.Errorf("unknown kid %q", kid)
			}
			return entry, nil
		case <-ctx.Done():
			return jwkEntry{}, ctx.Err()
		}
	}

	if !v.lastAttempt.IsZero() && now.Sub(v.lastAttempt) < v.cooldown {
		v.mu.Unlock()
		return jwkEntry{}, fmt.Errorf("jwks refresh cooldown active for kid %q", kid)
	}

	call := &inflightFetch{done: make(chan struct{})}
	v.inflight = call
	v.mu.Unlock()

	fetchCtx, cancel := context.WithTimeout(context.Background(), v.fetchTimeout())
	newKeys, err := v.fetchJWKS(fetchCtx)
	cancel()

	v.mu.Lock()
	doneAt := v.nowFunc()
	v.lastAttempt = doneAt
	if err == nil {
		v.keys = newKeys
		v.lastSuccess = doneAt
	} else {
		err = fmt.Errorf("failed to fetch jwks: %w", err)
		if !v.isCacheFreshLocked(doneAt) {
			v.keys = make(map[string]jwkEntry)
		}
	}
	call.err = err
	v.inflight = nil
	close(call.done)

	if err != nil {
		v.mu.Unlock()
		return jwkEntry{}, err
	}
	entry, ok := v.keys[kid]
	v.mu.Unlock()
	if !ok {
		return jwkEntry{}, fmt.Errorf("unknown kid %q", kid)
	}
	return entry, nil
}

func (v *Verifier) isCacheFreshLocked(now time.Time) bool {
	return len(v.keys) > 0 && !v.lastSuccess.IsZero() && now.Sub(v.lastSuccess) < v.cacheTTL
}

func (v *Verifier) fetchTimeout() time.Duration {
	if v.client != nil && v.client.Timeout > 0 {
		return v.client.Timeout
	}
	return defaultHTTPTimeout
}

func (v *Verifier) fetchJWKS(ctx context.Context) (map[string]jwkEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jwks endpoint returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxJWKSBodyBytes))
	if err != nil {
		return nil, err
	}

	var doc jwksDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}

	out := make(map[string]jwkEntry, len(doc.Keys))
	for _, k := range doc.Keys {
		kid := strings.TrimSpace(k.Kid)
		if kid == "" {
			continue
		}
		if k.Use != "" && k.Use != "sig" {
			continue
		}
		pub, err := parseJWKPublicKey(k)
		if err != nil {
			continue
		}
		out[kid] = jwkEntry{pub: pub, alg: strings.TrimSpace(k.Alg)}
	}
	if len(out) == 0 {
		return nil, errors.New("jwks contained no usable signing keys")
	}
	return out, nil
}

func parseJWKPublicKey(k jwkKey) (crypto.PublicKey, error) {
	switch k.Kty {
	case "EC":
		return parseECJWK(k)
	case "RSA":
		return parseRSAJWK(k)
	default:
		return nil, fmt.Errorf("unsupported kty: %s", k.Kty)
	}
}

func parseECJWK(k jwkKey) (*ecdsa.PublicKey, error) {
	var (
		curve     elliptic.Curve
		ecdhCurve ecdh.Curve
		coordSize int
	)
	switch k.Crv {
	case "P-256":
		curve = elliptic.P256()
		ecdhCurve = ecdh.P256()
		coordSize = 32
	case "P-384":
		curve = elliptic.P384()
		ecdhCurve = ecdh.P384()
		coordSize = 48
	case "P-521":
		curve = elliptic.P521()
		ecdhCurve = ecdh.P521()
		coordSize = 66
	default:
		return nil, fmt.Errorf("unsupported EC curve: %s", k.Crv)
	}

	xb, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	yb, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, err
	}
	if len(xb) > coordSize || len(yb) > coordSize {
		return nil, errors.New("invalid EC coordinate size")
	}

	uncompressed := make([]byte, 1+2*coordSize)
	uncompressed[0] = 0x04
	copy(uncompressed[1+coordSize-len(xb):1+coordSize], xb)
	copy(uncompressed[1+2*coordSize-len(yb):1+2*coordSize], yb)

	if _, err := ecdhCurve.NewPublicKey(uncompressed); err != nil {
		return nil, fmt.Errorf("invalid EC point: %w", err)
	}

	return &ecdsa.PublicKey{
		Curve: curve,
		X:     new(big.Int).SetBytes(xb),
		Y:     new(big.Int).SetBytes(yb),
	}, nil
}

func parseRSAJWK(k jwkKey) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, err
	}
	n := new(big.Int).SetBytes(nb)
	e := new(big.Int).SetBytes(eb)
	if n.Sign() <= 0 || !e.IsInt64() || e.Int64() < 2 {
		return nil, errors.New("invalid RSA modulus or exponent")
	}
	return &rsa.PublicKey{
		N: n,
		E: int(e.Int64()),
	}, nil
}
