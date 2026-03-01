package web

import (
	"strings"
	"sync"
	"time"

	"page-patrol/internal/config"
)

type magicLinkDecision struct {
	Allowed bool
	Reason  string
}

type magicLinkRateLimiter struct {
	mu sync.Mutex

	window           time.Duration
	maxPerIP         int
	maxPerEmail      int
	resendCooldown   time.Duration
	ipAttempts       map[string][]time.Time
	emailAttempts    map[string][]time.Time
	lastEmailAttempt map[string]time.Time
}

func newMagicLinkRateLimiter(cfg config.Config) *magicLinkRateLimiter {
	return &magicLinkRateLimiter{
		window:           cfg.MagicLinkRateLimitWindow,
		maxPerIP:         cfg.MagicLinkRateLimitMaxPerIP,
		maxPerEmail:      cfg.MagicLinkRateLimitMaxPerEmail,
		resendCooldown:   cfg.MagicLinkResendCooldown,
		ipAttempts:       make(map[string][]time.Time),
		emailAttempts:    make(map[string][]time.Time),
		lastEmailAttempt: make(map[string]time.Time),
	}
}

func (l *magicLinkRateLimiter) Allow(ip, email string, now time.Time) magicLinkDecision {
	if l == nil {
		return magicLinkDecision{Allowed: true}
	}

	ip = strings.TrimSpace(ip)
	email = normalizeEmail(email)

	l.mu.Lock()
	defer l.mu.Unlock()

	l.prune(now)

	if ip != "" && l.maxPerIP > 0 && len(l.ipAttempts[ip]) >= l.maxPerIP {
		return magicLinkDecision{Reason: "ip_window"}
	}
	if email != "" && l.maxPerEmail > 0 && len(l.emailAttempts[email]) >= l.maxPerEmail {
		return magicLinkDecision{Reason: "email_window"}
	}
	if email != "" && l.resendCooldown > 0 {
		if lastAttempt, ok := l.lastEmailAttempt[email]; ok && now.Sub(lastAttempt) < l.resendCooldown {
			return magicLinkDecision{Reason: "email_cooldown"}
		}
	}

	if ip != "" && l.maxPerIP > 0 {
		l.ipAttempts[ip] = append(l.ipAttempts[ip], now)
	}
	if email != "" {
		if l.maxPerEmail > 0 {
			l.emailAttempts[email] = append(l.emailAttempts[email], now)
		}
		if l.resendCooldown > 0 {
			l.lastEmailAttempt[email] = now
		}
	}

	return magicLinkDecision{Allowed: true}
}

func (l *magicLinkRateLimiter) prune(now time.Time) {
	if l.window > 0 {
		cutoff := now.Add(-l.window)
		for key, attempts := range l.ipAttempts {
			l.ipAttempts[key] = keepRecent(attempts, cutoff)
			if len(l.ipAttempts[key]) == 0 {
				delete(l.ipAttempts, key)
			}
		}
		for key, attempts := range l.emailAttempts {
			l.emailAttempts[key] = keepRecent(attempts, cutoff)
			if len(l.emailAttempts[key]) == 0 {
				delete(l.emailAttempts, key)
			}
		}
	}

	if l.resendCooldown > 0 {
		cutoff := now.Add(-l.resendCooldown)
		for key, lastAttempt := range l.lastEmailAttempt {
			if lastAttempt.Before(cutoff) {
				delete(l.lastEmailAttempt, key)
			}
		}
	}
}

func keepRecent(values []time.Time, cutoff time.Time) []time.Time {
	for i, value := range values {
		if !value.Before(cutoff) {
			return values[i:]
		}
	}
	return values[:0]
}

func redactEmail(email string) string {
	email = normalizeEmail(email)
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" {
		return "redacted"
	}
	if len(local) == 1 {
		return local + "***@" + domain
	}
	return local[:1] + "***@" + domain
}

func redactIP(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "unknown"
	}

	if strings.Count(ip, ".") == 3 {
		parts := strings.Split(ip, ".")
		parts[len(parts)-1] = "x"
		return strings.Join(parts, ".")
	}

	if strings.Contains(ip, ":") {
		parts := strings.Split(ip, ":")
		if len(parts) > 2 {
			return strings.Join(parts[:2], ":") + ":*"
		}
	}

	return "redacted"
}
