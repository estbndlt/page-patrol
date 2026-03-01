package web

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"page-patrol/internal/config"
)

func TestSecurityMiddlewareRedirectsHTTPToHTTPS(t *testing.T) {
	s := &Server{
		cfg: config.Config{
			AppBaseURL:        "https://pagepatrol.estbndlt.com",
			TrustProxyHeaders: true,
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
			AppBaseURL:        "https://pagepatrol.estbndlt.com",
			TrustProxyHeaders: true,
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
	if got := rr.Header().Get("Content-Security-Policy"); got != s.contentSecurityPolicy() {
		t.Fatalf("unexpected Content-Security-Policy header: %q", got)
	}
	if got := rr.Header().Get("Permissions-Policy"); got != permissionsPolicy {
		t.Fatalf("unexpected Permissions-Policy header: %q", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("unexpected X-Frame-Options header: %q", got)
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

func TestSecurityMiddlewareIgnoresForwardedHeadersWithoutProxyTrust(t *testing.T) {
	s := &Server{
		cfg: config.Config{
			AppBaseURL: "https://pagepatrol.estbndlt.com",
		},
	}

	req := httptest.NewRequest(http.MethodGet, "http://pagepatrol.estbndlt.com/login", nil)
	req.Header.Set("X-Forwarded-Proto", "https")

	rr := httptest.NewRecorder()

	handler := s.securityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusPermanentRedirect {
		t.Fatalf("expected status %d, got %d", http.StatusPermanentRedirect, rr.Code)
	}
}

func TestClientIPUsesTrustedProxyHeaders(t *testing.T) {
	s := &Server{
		cfg: config.Config{
			TrustProxyHeaders: true,
		},
	}

	req := httptest.NewRequest(http.MethodGet, "https://pagepatrol.estbndlt.com/login", nil)
	req.RemoteAddr = "10.0.0.10:1234"
	req.Header.Set("CF-Connecting-IP", "198.51.100.8")

	if got := s.clientIP(req); got != "198.51.100.8" {
		t.Fatalf("unexpected client IP: %q", got)
	}
}

func TestClientIPFallsBackToRemoteAddr(t *testing.T) {
	s := &Server{}

	req := httptest.NewRequest(http.MethodGet, "https://pagepatrol.estbndlt.com/login", nil)
	req.RemoteAddr = "198.51.100.9:4567"
	req.Header.Set("CF-Connecting-IP", "203.0.113.10")

	if got := s.clientIP(req); got != "198.51.100.9" {
		t.Fatalf("unexpected client IP: %q", got)
	}
}

func TestHandleRequestMagicLinkRedirectsWhenRateLimited(t *testing.T) {
	limiter := newMagicLinkRateLimiter(config.Config{
		MagicLinkRateLimitWindow:      time.Minute,
		MagicLinkRateLimitMaxPerIP:    1,
		MagicLinkRateLimitMaxPerEmail: 10,
	})
	now := time.Now()
	if decision := limiter.Allow("198.51.100.20", "member@example.com", now); !decision.Allowed {
		t.Fatalf("unexpected seed decision: %+v", decision)
	}

	s := &Server{
		cfg:              config.Config{},
		logger:           log.New(io.Discard, "", 0),
		magicLinkLimiter: limiter,
	}

	form := url.Values{
		"email":      {"member@example.com"},
		"csrf_token": {"csrf-token"},
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/request-link", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "198.51.100.20:5555"
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "csrf-token"})

	rr := httptest.NewRecorder()
	s.handleRequestMagicLink(rr, req)

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("expected status %d, got %d", http.StatusSeeOther, rr.Code)
	}
	if got := rr.Header().Get("Location"); got != "/login?sent=1" {
		t.Fatalf("unexpected redirect location: %q", got)
	}
}

func TestContentSecurityPolicyAllowsInlineWhenConfigured(t *testing.T) {
	s := &Server{
		cfg: config.Config{
			CSPAllowUnsafeInline: true,
		},
	}

	if got := s.contentSecurityPolicy(); !strings.Contains(got, "'unsafe-inline'") {
		t.Fatalf("expected unsafe-inline in policy, got %q", got)
	}
}
