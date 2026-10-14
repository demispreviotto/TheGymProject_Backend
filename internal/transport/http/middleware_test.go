package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeVerifier struct {
	sub string
	err error
}

func (f fakeVerifier) Verify(context.Context, string) (string, error) { return f.sub, f.err }

func TestRequireAuth(t *testing.T) {
	var gotUser string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = userIDFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	cases := []struct {
		name     string
		header   string
		verifier fakeVerifier
		wantCode int
		wantUser string
	}{
		{"no header", "", fakeVerifier{sub: "u1"}, http.StatusUnauthorized, ""},
		{"wrong scheme", "Basic abc", fakeVerifier{sub: "u1"}, http.StatusUnauthorized, ""},
		{"empty bearer", "Bearer ", fakeVerifier{sub: "u1"}, http.StatusUnauthorized, ""},
		{"verifier rejects", "Bearer tok", fakeVerifier{err: errors.New("bad")}, http.StatusUnauthorized, ""},
		{"valid", "Bearer tok", fakeVerifier{sub: "u1"}, http.StatusOK, "u1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotUser = ""
			req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			requireAuth(tc.verifier)(next).ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantCode)
			}
			if gotUser != tc.wantUser {
				t.Fatalf("user = %q, want %q", gotUser, tc.wantUser)
			}
		})
	}
}
