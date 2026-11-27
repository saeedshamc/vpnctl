package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecureChecksExactHostAndOrigin(t *testing.T) {
	tests := []struct {
		name       string
		host       string
		origin     string
		wantStatus int
	}{
		{name: "local origin", host: "127.0.0.1:3210", origin: "http://127.0.0.1:3210", wantStatus: http.StatusNoContent},
		{name: "localhost origin", host: "localhost:3210", origin: "http://localhost:3210", wantStatus: http.StatusNoContent},
		{name: "host suffix attack", host: "127.0.0.1.attacker.test:3210", wantStatus: http.StatusForbidden},
		{name: "origin suffix attack", host: "127.0.0.1:3210", origin: "http://127.0.0.1.attacker.test:3210", wantStatus: http.StatusForbidden},
		{name: "origin port mismatch", host: "127.0.0.1:3210", origin: "http://127.0.0.1:3211", wantStatus: http.StatusForbidden},
		{name: "origin path", host: "127.0.0.1:3210", origin: "http://127.0.0.1:3210/path", wantStatus: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			(&Server{}).secure(next).ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestWithAuthDoesNotAcceptQueryToken(t *testing.T) {
	s := &Server{Token: "secret"}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/status?token=secret", nil)
	req.Host = "127.0.0.1:3210"
	rec := httptest.NewRecorder()
	s.withAuth(next).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
