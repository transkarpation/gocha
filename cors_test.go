package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newCORSHandler(origins []string) http.Handler {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	return corsMiddleware(origins)(next)
}

func TestCORSAllowedOrigin(t *testing.T) {
	h := newCORSHandler([]string{"http://localhost:5173"})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Allow-Origin = %q, want the reflected origin", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Errorf("request should pass through: code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestCORSPreflightShortCircuits(t *testing.T) {
	h := newCORSHandler([]string{"http://localhost:5173"})

	req := httptest.NewRequest(http.MethodOptions, "/login", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight code = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Error("preflight is missing Access-Control-Allow-Methods")
	}
	if rec.Body.Len() != 0 {
		t.Errorf("preflight should not reach the handler, got body %q", rec.Body.String())
	}
}

func TestCORSDisallowedOrigin(t *testing.T) {
	h := newCORSHandler([]string{"http://localhost:5173"})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want none for a disallowed origin", got)
	}
}

func TestCORSWildcardNoCredentials(t *testing.T) {
	h := newCORSHandler([]string{"*"})

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Origin", "http://anything.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Errorf("Allow-Credentials = %q, want empty with wildcard", got)
	}
}
