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

// Verifier は SPIRE OIDC Discovery Provider の JWKS を用いて JWT-SVID を検証する。
type Verifier struct {
	jwksURL  string
	audience string
	client   *http.Client
	cooldown time.Duration
	leeway   time.Duration
	nowFunc  func() time.Time

	mu        sync.Mutex
	keys      map[string]jwkEntry
	lastFetch time.Time
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

// WithCooldown は未知 kid 受信時の JWKS 再取得クールダウン間隔を設定する。
func WithCooldown(d time.Duration) Option {
	return func(v *Verifier) {
		if d >= 0 {
			v.cooldown = d
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
		kid = strings.TrimSpace(kid)
		if kid == "" {
			return nil, errors.New("missing kid header")
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

	sub := strings.TrimSpace(claims.Subject)
	if !strings.HasPrefix(sub, "spiffe://") || len(sub) <= len("spiffe://") {
		return "", fmt.Errorf("invalid spiffe sub claim: %q", claims.Subject)
	}
	return sub, nil
}

func (v *Verifier) getKey(ctx context.Context, kid string) (jwkEntry, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if entry, ok := v.keys[kid]; ok {
		return entry, nil
	}

	now := v.nowFunc()
	hadKeys := len(v.keys) > 0
	if hadKeys && !v.lastFetch.IsZero() && now.Sub(v.lastFetch) < v.cooldown {
		return jwkEntry{}, fmt.Errorf("unknown kid %q (jwks refresh cooldown active)", kid)
	}

	newKeys, err := v.fetchJWKS(ctx)
	if err != nil {
		if hadKeys {
			v.lastFetch = now
		}
		return jwkEntry{}, fmt.Errorf("failed to fetch jwks: %w", err)
	}
	v.keys = newKeys
	v.lastFetch = now

	entry, ok := v.keys[kid]
	if !ok {
		return jwkEntry{}, fmt.Errorf("unknown kid %q", kid)
	}
	return entry, nil
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
