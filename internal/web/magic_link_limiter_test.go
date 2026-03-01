package web

import (
	"testing"
	"time"

	"page-patrol/internal/config"
)

func TestMagicLinkRateLimiterLimitsByIP(t *testing.T) {
	limiter := newMagicLinkRateLimiter(config.Config{
		MagicLinkRateLimitWindow:      15 * time.Minute,
		MagicLinkRateLimitMaxPerIP:    2,
		MagicLinkRateLimitMaxPerEmail: 10,
	})
	now := time.Now()

	if decision := limiter.Allow("198.51.100.1", "first@example.com", now); !decision.Allowed {
		t.Fatalf("unexpected first decision: %+v", decision)
	}
	if decision := limiter.Allow("198.51.100.1", "second@example.com", now.Add(time.Second)); !decision.Allowed {
		t.Fatalf("unexpected second decision: %+v", decision)
	}

	decision := limiter.Allow("198.51.100.1", "third@example.com", now.Add(2*time.Second))
	if decision.Allowed {
		t.Fatal("expected IP limit to reject third request")
	}
	if decision.Reason != "ip_window" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}

func TestMagicLinkRateLimiterLimitsByEmail(t *testing.T) {
	limiter := newMagicLinkRateLimiter(config.Config{
		MagicLinkRateLimitWindow:      15 * time.Minute,
		MagicLinkRateLimitMaxPerIP:    10,
		MagicLinkRateLimitMaxPerEmail: 2,
	})
	now := time.Now()

	if decision := limiter.Allow("198.51.100.1", "member@example.com", now); !decision.Allowed {
		t.Fatalf("unexpected first decision: %+v", decision)
	}
	if decision := limiter.Allow("198.51.100.2", "member@example.com", now.Add(time.Second)); !decision.Allowed {
		t.Fatalf("unexpected second decision: %+v", decision)
	}

	decision := limiter.Allow("198.51.100.3", "member@example.com", now.Add(2*time.Second))
	if decision.Allowed {
		t.Fatal("expected email limit to reject third request")
	}
	if decision.Reason != "email_window" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
}

func TestMagicLinkRateLimiterEnforcesCooldown(t *testing.T) {
	limiter := newMagicLinkRateLimiter(config.Config{
		MagicLinkRateLimitWindow:      15 * time.Minute,
		MagicLinkRateLimitMaxPerIP:    10,
		MagicLinkRateLimitMaxPerEmail: 10,
		MagicLinkResendCooldown:       time.Minute,
	})
	now := time.Now()

	if decision := limiter.Allow("198.51.100.1", "member@example.com", now); !decision.Allowed {
		t.Fatalf("unexpected first decision: %+v", decision)
	}

	decision := limiter.Allow("198.51.100.2", "member@example.com", now.Add(30*time.Second))
	if decision.Allowed {
		t.Fatal("expected cooldown to reject second request")
	}
	if decision.Reason != "email_cooldown" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}

	decision = limiter.Allow("198.51.100.3", "member@example.com", now.Add(61*time.Second))
	if !decision.Allowed {
		t.Fatalf("expected request after cooldown to be allowed, got %+v", decision)
	}
}
