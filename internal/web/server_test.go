package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"page-patrol/internal/config"
)

func TestSecurityMiddlewareRedirectsHTTPToHTTPS(t *testing.T) {
	s := &Server{
		cfg: config.Config{
			AppBaseURL: "https://pagepatrol.estbndlt.com",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "http://pagepatrol.estbndlt.com/login?sent=1", nil)
	req.Header.Set("X-Forwarded-Proto", "http")

	rr := httptest.NewRecorder()
	nextCalled := false

	handler := s.securityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(rr, req)

	if nextCalled {
		t.Fatal("expected insecure request to be redirected before reaching next handler")
	}
	if rr.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected status %d, got %d", http.StatusPermanentRedirect, rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "https://pagepatrol.estbndlt.com/login?sent=1" {
		t.Fatalf("unexpected redirect location: %q", got)
	}
}

func TestSecurityMiddlewareAllowsHTTPSAndSetsHeaders(t *testing.T) {
	s := &Server{
		cfg: config.Config{
			AppBaseURL: "https://pagepatrol.estbndlt.com",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "https://pagepatrol.estbndlt.com/login", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	rr := httptest.NewRecorder()

	handler := s.securityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
	if got := rr.Header().Get("Strict-Transport-Security"); got == "" {
		t.Fatal("expected Strict-Transport-Security header to be set")
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("unexpected X-Content-Type-Options header: %q", got)
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("unexpected Referrer-Policy header: %q", got)
	}
}

func TestSecurityMiddlewareDoesNotRedirectLocalHTTP(t *testing.T) {
	s := &Server{
		cfg: config.Config{
			AppBaseURL: "http://localhost:3000",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "http://localhost:3000/login", nil)
	rr := httptest.NewRecorder()
	nextCalled := false

	handler := s.securityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Fatal("expected local HTTP request to reach next handler")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}
	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("expected no Strict-Transport-Security header for local HTTP, got %q", got)
	}
}
