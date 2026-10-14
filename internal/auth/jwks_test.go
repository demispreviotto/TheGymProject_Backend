package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testAudience = "authenticated"

type jwksServer struct {
	*httptest.Server
	mu    sync.Mutex
	keys  map[string]*ecdsa.PrivateKey
	calls atomic.Int32
}

func newJWKSServer(t *testing.T, keys map[string]*ecdsa.PrivateKey) *jwksServer {
	t.Helper()
	s := &jwksServer{keys: keys}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.calls.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		doc := jwkSet{}
		for kid, k := range s.keys {
			doc.Keys = append(doc.Keys, jwk{
				Kty: "EC", Crv: "P-256", Alg: "ES256", Kid: kid,
				X: b64(k.PublicKey.X.FillBytes(make([]byte, 32))),
				Y: b64(k.PublicKey.Y.FillBytes(make([]byte, 32))),
			})
		}
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jwksServer) setKeys(keys map[string]*ecdsa.PrivateKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keys = keys
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func sign(t *testing.T, key *ecdsa.PrivateKey, kid string, claims jwt.RegisteredClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = kid
	raw, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func validClaims() jwt.RegisteredClaims {
	return jwt.RegisteredClaims{
		Subject:   "user-1",
		Audience:  jwt.ClaimStrings{testAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}
}

func TestVerify_ValidToken(t *testing.T) {
	key := newKey(t)
	srv := newJWKSServer(t, map[string]*ecdsa.PrivateKey{"k1": key})
	v := NewVerifier(srv.URL, testAudience)

	sub, err := v.Verify(context.Background(), sign(t, key, "k1", validClaims()))
	if err != nil {
		t.Fatalf("expected valid token, got %v", err)
	}
	if sub != "user-1" {
		t.Fatalf("subject = %q, want user-1", sub)
	}
}

func TestVerify_Rejections(t *testing.T) {
	key := newKey(t)
	other := newKey(t)
	srv := newJWKSServer(t, map[string]*ecdsa.PrivateKey{"k1": key})
	v := NewVerifier(srv.URL, testAudience)

	expired := validClaims()
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	wrongAud := validClaims()
	wrongAud.Audience = jwt.ClaimStrings{"someone-else"}
	noExp := validClaims()
	noExp.ExpiresAt = nil
	noSub := validClaims()
	noSub.Subject = ""

	hs256 := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims())
	hs256.Header["kid"] = "k1"
	hsRaw, err := hs256.SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"expired":             sign(t, key, "k1", expired),
		"wrong audience":      sign(t, key, "k1", wrongAud),
		"missing expiry":      sign(t, key, "k1", noExp),
		"missing subject":     sign(t, key, "k1", noSub),
		"signed by other key": sign(t, other, "k1", validClaims()),
		"unknown kid":         sign(t, key, "nope", validClaims()),
		"hs256 algorithm":     hsRaw,
		"garbage":             "not-a-jwt",
		"empty":               "",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(context.Background(), raw); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestVerify_KeyRotationTriggersRefresh(t *testing.T) {
	oldKey, newK := newKey(t), newKey(t)
	srv := newJWKSServer(t, map[string]*ecdsa.PrivateKey{"old": oldKey})
	v := NewVerifier(srv.URL, testAudience)
	v.minRefresh = 0

	if _, err := v.Verify(context.Background(), sign(t, oldKey, "old", validClaims())); err != nil {
		t.Fatal(err)
	}
	srv.setKeys(map[string]*ecdsa.PrivateKey{"new": newK})

	if _, err := v.Verify(context.Background(), sign(t, newK, "new", validClaims())); err != nil {
		t.Fatalf("expected refresh to pick up rotated key, got %v", err)
	}
}

func TestVerify_UnknownKidIsRateLimited(t *testing.T) {
	key := newKey(t)
	srv := newJWKSServer(t, map[string]*ecdsa.PrivateKey{"k1": key})
	v := NewVerifier(srv.URL, testAudience) // default minRefresh

	bad := sign(t, key, "forged", validClaims())
	for range 20 {
		_, _ = v.Verify(context.Background(), bad)
	}
	if got := srv.calls.Load(); got != 1 {
		t.Fatalf("JWKS fetched %d times, want 1 (unknown kid must not hammer the issuer)", got)
	}
}

func TestVerify_JWKSUnavailable(t *testing.T) {
	key := newKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	v := NewVerifier(srv.URL, testAudience)

	if _, err := v.Verify(context.Background(), sign(t, key, "k1", validClaims())); err == nil {
		t.Fatal("expected error when JWKS endpoint fails")
	}
}
