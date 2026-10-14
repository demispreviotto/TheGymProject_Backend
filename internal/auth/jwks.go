// Package auth verifies Supabase-issued access tokens (ES256) against the
// issuer's JWKS endpoint.
package auth

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// defaultMinRefresh bounds how often an unknown `kid` may trigger a JWKS fetch,
// so forged tokens cannot be used to hammer the issuer.
const (
	defaultMinRefresh = 30 * time.Second
	retryAfterFailure = 2 * time.Second
)

var ErrInvalidToken = errors.New("invalid token")

type jwk struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

type jwkSet struct {
	Keys []jwk `json:"keys"`
}

// Verifier validates ES256 tokens using keys fetched lazily from a JWKS URL.
type Verifier struct {
	jwksURL    string
	audience   string
	client     *http.Client
	minRefresh time.Duration

	mu        sync.RWMutex
	keys      map[string]*ecdsa.PublicKey
	lastFetch time.Time
}

func NewVerifier(jwksURL, audience string) *Verifier {
	return &Verifier{
		jwksURL:    jwksURL,
		audience:   audience,
		client:     &http.Client{Timeout: 5 * time.Second},
		minRefresh: defaultMinRefresh,
		keys:       map[string]*ecdsa.PublicKey{},
	}
}

// Verify checks signature, expiry and audience and returns the token subject.
func (v *Verifier) Verify(ctx context.Context, raw string) (string, error) {
	claims := jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(raw, &claims,
		func(t *jwt.Token) (any, error) { return v.keyFor(ctx, t) },
		jwt.WithValidMethods([]string{jwt.SigningMethodES256.Alg()}),
		jwt.WithAudience(v.audience),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	if claims.Subject == "" {
		return "", fmt.Errorf("%w: missing subject", ErrInvalidToken)
	}
	return claims.Subject, nil
}

func (v *Verifier) keyFor(ctx context.Context, t *jwt.Token) (*ecdsa.PublicKey, error) {
	kid, _ := t.Header["kid"].(string)
	if kid == "" {
		return nil, errors.New("missing kid")
	}
	if k := v.lookup(kid); k != nil {
		return k, nil
	}
	if err := v.refresh(ctx); err != nil {
		return nil, err
	}
	if k := v.lookup(kid); k != nil {
		return k, nil
	}
	return nil, fmt.Errorf("unknown kid %q", kid)
}

func (v *Verifier) lookup(kid string) *ecdsa.PublicKey {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.keys[kid]
}

func (v *Verifier) refresh(ctx context.Context) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.lastFetch.IsZero() && time.Since(v.lastFetch) < v.minRefresh {
		return nil // another request refreshed (or we are rate limited); caller re-checks the cache
	}
	v.lastFetch = time.Now()

	keys, err := v.fetch(ctx)
	if err != nil {
		// Retry soon rather than waiting a full minRefresh: an outage at startup
		// must not lock out valid tokens for long, but must not be hammered either.
		v.lastFetch = time.Now().Add(retryAfterFailure - v.minRefresh)
		return err
	}
	v.keys = keys
	return nil
}

func (v *Verifier) fetch(ctx context.Context) (map[string]*ecdsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch jwks: status %d", res.StatusCode)
	}

	var set jwkSet
	if err := json.NewDecoder(res.Body).Decode(&set); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	keys := make(map[string]*ecdsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		pub, err := k.publicKey()
		if err != nil {
			continue // skip unsupported or malformed keys; others may still be usable
		}
		keys[k.Kid] = pub
	}
	return keys, nil
}

func (k jwk) publicKey() (*ecdsa.PublicKey, error) {
	if k.Kty != "EC" || k.Crv != "P-256" || k.Kid == "" {
		return nil, errors.New("unsupported key")
	}
	x, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, err
	}
	y, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, err
	}
	if len(x) != 32 || len(y) != 32 {
		return nil, errors.New("bad coordinate length")
	}
	// ecdh validates that the point is on the curve.
	uncompressed := append([]byte{0x04}, append(x, y...)...)
	if _, err := ecdh.P256().NewPublicKey(uncompressed); err != nil {
		return nil, err
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
}
