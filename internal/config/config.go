package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppName          string
	AppBaseURL       string
	ListenAddr       string
	DatabaseURL      string
	MigrationsDir    string
	TemplateDir      string
	StaticDir        string
	CoordinatorEmail string

	CookieSecure      bool
	TrustProxyHeaders bool
	SessionTTL        time.Duration
	MagicLinkTTL      time.Duration

	MagicLinkRateLimitWindow      time.Duration
	MagicLinkRateLimitMaxPerIP    int
	MagicLinkRateLimitMaxPerEmail int
	MagicLinkResendCooldown       time.Duration

	SMTPHost      string
	SMTPPort      int
	SMTPUser      string
	SMTPPass      string
	SMTPFromName  string
	SMTPFromEmail string
}

func Load() (Config, error) {
	cfg := Config{
		AppName:                       "Page Patrol",
		AppBaseURL:                    strings.TrimSpace(getenv("APP_BASE_URL", "http://localhost")),
		ListenAddr:                    strings.TrimSpace(getenv("APP_LISTEN_ADDR", ":8080")),
		DatabaseURL:                   strings.TrimSpace(os.Getenv("DATABASE_URL")),
		MigrationsDir:                 strings.TrimSpace(getenv("MIGRATIONS_DIR", "internal/db/migrations")),
		TemplateDir:                   strings.TrimSpace(getenv("TEMPLATE_DIR", "web/templates")),
		StaticDir:                     strings.TrimSpace(getenv("STATIC_DIR", "web/static")),
		CoordinatorEmail:              normalizeEmail(os.Getenv("COORDINATOR_EMAIL")),
		CookieSecure:                  getenvBool("COOKIE_SECURE", true),
		TrustProxyHeaders:             getenvBool("TRUST_PROXY_HEADERS", false),
		SessionTTL:                    getenvDuration("SESSION_TTL", 30*24*time.Hour),
		MagicLinkTTL:                  getenvDuration("MAGIC_LINK_TTL", 15*time.Minute),
		MagicLinkRateLimitWindow:      getenvDuration("MAGIC_LINK_RATE_LIMIT_WINDOW", 15*time.Minute),
		MagicLinkRateLimitMaxPerIP:    getenvInt("MAGIC_LINK_RATE_LIMIT_MAX_PER_IP", 5),
		MagicLinkRateLimitMaxPerEmail: getenvInt("MAGIC_LINK_RATE_LIMIT_MAX_PER_EMAIL", 3),
		MagicLinkResendCooldown:       getenvDuration("MAGIC_LINK_RESEND_COOLDOWN", time.Minute),
		SMTPHost:                      strings.TrimSpace(os.Getenv("SMTP_HOST")),
		SMTPPort:                      getenvInt("SMTP_PORT", 587),
		SMTPUser:                      strings.TrimSpace(os.Getenv("SMTP_USER")),
		SMTPPass:                      strings.TrimSpace(os.Getenv("SMTP_PASS")),
		SMTPFromName:                  strings.TrimSpace(getenv("SMTP_FROM_NAME", "Page Patrol")),
		SMTPFromEmail:                 normalizeEmail(os.Getenv("SMTP_FROM_EMAIL")),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.CoordinatorEmail == "" {
		return Config{}, errors.New("COORDINATOR_EMAIL is required")
	}
	if cfg.SMTPHost == "" {
		return Config{}, errors.New("SMTP_HOST is required")
	}
	if cfg.SMTPFromEmail == "" {
		return Config{}, errors.New("SMTP_FROM_EMAIL is required")
	}
	if cfg.SMTPPort <= 0 {
		return Config{}, fmt.Errorf("invalid SMTP_PORT: %d", cfg.SMTPPort)
	}

	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func getenvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
