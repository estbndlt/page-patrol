package web

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"page-patrol/internal/config"
	"page-patrol/internal/models"
	"page-patrol/internal/repository"
	"page-patrol/internal/security"
)

const (
	sessionCookieName = "pp_session"
	csrfCookieName    = "pp_csrf"
	permissionsPolicy = "accelerometer=(), autoplay=(), camera=(), display-capture=(), geolocation=(), gyroscope=(), hid=(), microphone=(), payment=(), usb=()"
)

type Server struct {
	cfg              config.Config
	repo             *repository.Repository
	logger           *log.Logger
	templates        *template.Template
	magicLinkLimiter *magicLinkRateLimiter
}

type loginPageData struct {
	AppName    string
	CSRFToken  string
	Sent       bool
	Error      string
	LoginEmail string
}

type homePageData struct {
	AppName   string
	User      models.User
	Target    *models.ReadingTarget
	ReadPanel readPanelData
	Feed      []eventView
	CSRFToken string
	NowYear   int
	IsCoord   bool
}

type readPanelData struct {
	HasTarget   bool
	TargetID    int64
	TargetTitle string
	IsRead      bool
	ReadCount   int
	MemberCount int
	Members     []memberView
	CSRFToken   string
}

type memberView struct {
	Name   string
	Email  string
	IsRead bool
}

type eventView struct {
	ID      int64
	Message string
	When    string
}

type coordinatorPageData struct {
	AppName       string
	User          models.User
	Target        *models.ReadingTarget
	WeeklyTrends  []models.WeeklyTrend
	MemberStreaks []models.MemberStreak
	CSRFToken     string
	NowYear       int
}

type membersPageData struct {
	AppName   string
	User      models.User
	Invites   []models.Invite
	CSRFToken string
	NowYear   int
}

func NewServer(cfg config.Config, repo *repository.Repository, logger *log.Logger) (*Server, error) {
	tpl, err := template.New("root").Funcs(template.FuncMap{
		"formatDate": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format("2006-01-02")
		},
		"formatPct": func(v float64) string {
			return fmt.Sprintf("%.1f", v)
		},
		"displayName": func(name, email string) string {
			if strings.TrimSpace(name) != "" {
				return name
			}
			return email
		},
	}).ParseGlob(filepath.Join(cfg.TemplateDir, "*.html"))
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	return &Server{
		cfg:              cfg,
		repo:             repo,
		logger:           logger,
		templates:        tpl,
		magicLinkLimiter: newMagicLinkRateLimiter(cfg),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(s.cfg.StaticDir))))
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /auth/request-link", s.handleRequestMagicLink)
	mux.HandleFunc("GET /auth/verify", s.handleVerifyMagicLink)
	mux.HandleFunc("POST /auth/logout", s.handleLogout)

	mux.HandleFunc("GET /", s.requireAuth(s.handleHome))
	mux.HandleFunc("POST /status/toggle", s.requireAuth(s.handleToggleStatus))
	mux.HandleFunc("GET /feed/events", s.requireAuth(s.handleFeedEvents))
	mux.HandleFunc("GET /feed/fragment", s.requireAuth(s.handleFeedFragment))

	mux.HandleFunc("GET /coordinator", s.requireCoordinator(s.handleCoordinator))
	mux.HandleFunc("POST /coordinator/targets", s.requireCoordinator(s.handlePublishTarget))
	mux.HandleFunc("GET /coordinator/members", s.requireCoordinator(s.handleMembersPage))
	mux.HandleFunc("POST /coordinator/members", s.requireCoordinator(s.handleAddMember))
	mux.HandleFunc("POST /coordinator/members/{id}/deactivate", s.requireCoordinator(s.handleDeactivateMember))
	mux.HandleFunc("POST /coordinator/members/{id}/reactivate", s.requireCoordinator(s.handleReactivateMember))
	mux.HandleFunc("GET /coordinator/trends", s.requireCoordinator(s.handleTrendsAPI))

	return s.logRequests(s.securityMiddleware(mux))
}

func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	baseURL, err := url.Parse(strings.TrimSpace(s.cfg.AppBaseURL))
	enforceHTTPS := err == nil && strings.EqualFold(baseURL.Scheme, "https")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if enforceHTTPS && !requestIsSecure(r, s.cfg.TrustProxyHeaders) {
			http.Redirect(w, r, redirectURL(baseURL, r), http.StatusPermanentRedirect)
			return
		}

		w.Header().Set("Content-Security-Policy", s.contentSecurityPolicy())
		w.Header().Set("Permissions-Policy", permissionsPolicy)
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if enforceHTTPS {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	csrf := s.ensureCSRFToken(w, r)
	data := loginPageData{
		AppName:   s.cfg.AppName,
		CSRFToken: csrf,
		Sent:      r.URL.Query().Get("sent") == "1",
		Error:     strings.TrimSpace(r.URL.Query().Get("error")),
	}
	s.render(w, "login", data)
}

func (s *Server) handleRequestMagicLink(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.validateCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}

	email := normalizeEmail(r.FormValue("email"))
	clientIP := s.clientIP(r)
	if decision := s.magicLinkLimiter.Allow(clientIP, email, time.Now()); !decision.Allowed {
		s.logger.Printf(
			"throttled magic link request reason=%s ip=%s email=%s",
			decision.Reason,
			redactIP(clientIP),
			redactEmail(email),
		)
		http.Redirect(w, r, "/login?sent=1", http.StatusSeeOther)
		return
	}
	if email != "" {
		allowed, err := s.repo.IsEmailAllowed(r.Context(), email, s.cfg.CoordinatorEmail)
		if err != nil {
			s.logger.Printf("allow-list lookup error: %v", err)
		}
		if allowed {
			if err := s.queueMagicLinkEmail(r.Context(), email); err != nil {
				s.logger.Printf("queue magic link failed for %s: %v", email, err)
			}
		}
	}

	http.Redirect(w, r, "/login?sent=1", http.StatusSeeOther)
}

func (s *Server) queueMagicLinkEmail(ctx context.Context, email string) error {
	rawToken, err := security.RandomToken(32)
	if err != nil {
		return fmt.Errorf("generate magic token: %w", err)
	}
	hashed := security.HashToken(rawToken)
	expiresAt := time.Now().Add(s.cfg.MagicLinkTTL)
	if err := s.repo.CreateMagicLinkToken(ctx, email, hashed, expiresAt); err != nil {
		return fmt.Errorf("store magic token: %w", err)
	}

	baseURL := strings.TrimRight(s.cfg.AppBaseURL, "/")
	verifyURL := fmt.Sprintf("%s/auth/verify?token=%s", baseURL, url.QueryEscape(rawToken))
	payload := models.OutboundEmail{
		Subject: "Your Page Patrol sign-in link",
		Body: fmt.Sprintf(
			"Use this secure sign-in link for Page Patrol:\n\n%s\n\nThis link expires in %s and can only be used once.",
			verifyURL,
			s.cfg.MagicLinkTTL.String(),
		),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode magic link email payload: %w", err)
	}

	job := models.EmailJob{
		JobType:        "magic_link",
		RecipientEmail: email,
		PayloadJSON:    string(encoded),
	}
	if err := s.repo.InsertEmailJobs(ctx, []models.EmailJob{job}); err != nil {
		return fmt.Errorf("insert magic link email job: %w", err)
	}
	return nil
}

func (s *Server) handleVerifyMagicLink(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		http.Redirect(w, r, "/login?error=missing+token", http.StatusSeeOther)
		return
	}

	email, err := s.repo.ConsumeMagicLinkToken(r.Context(), security.HashToken(token))
	if err != nil {
		http.Redirect(w, r, "/login?error=invalid+or+expired+token", http.StatusSeeOther)
		return
	}

	allowed, err := s.repo.IsEmailAllowed(r.Context(), email, s.cfg.CoordinatorEmail)
	if err != nil {
		s.logger.Printf("allow-list verify check error: %v", err)
		http.Redirect(w, r, "/login?error=login+failed", http.StatusSeeOther)
		return
	}
	if !allowed {
		http.Redirect(w, r, "/login?error=access+not+allowed", http.StatusSeeOther)
		return
	}

	user, err := s.repo.UpsertUserForEmail(r.Context(), email, s.cfg.CoordinatorEmail)
	if err != nil {
		s.logger.Printf("upsert user failed: %v", err)
		http.Redirect(w, r, "/login?error=login+failed", http.StatusSeeOther)
		return
	}

	rawSession, err := security.RandomToken(32)
	if err != nil {
		s.logger.Printf("generate session token failed: %v", err)
		http.Redirect(w, r, "/login?error=login+failed", http.StatusSeeOther)
		return
	}
	if err := s.repo.CreateSession(r.Context(), user.ID, security.HashToken(rawSession), time.Now().Add(s.cfg.SessionTTL)); err != nil {
		s.logger.Printf("create session failed: %v", err)
		http.Redirect(w, r, "/login?error=login+failed", http.StatusSeeOther)
		return
	}

	s.setSessionCookie(w, rawSession)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.validateCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}

	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		hash := security.HashToken(cookie.Value)
		if err := s.repo.DeleteSessionByHash(r.Context(), hash); err != nil {
			s.logger.Printf("delete session failed: %v", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request, user models.User) {
	csrf := s.ensureCSRFToken(w, r)
	target, err := s.repo.GetActiveTarget(r.Context())
	if err != nil {
		http.Error(w, "failed to load target", http.StatusInternalServerError)
		return
	}

	panel, err := s.buildReadPanelData(r.Context(), user, target, csrf)
	if err != nil {
		http.Error(w, "failed to load statuses", http.StatusInternalServerError)
		return
	}

	events, err := s.repo.ListActivityEvents(r.Context(), 40)
	if err != nil {
		http.Error(w, "failed to load feed", http.StatusInternalServerError)
		return
	}

	data := homePageData{
		AppName:   s.cfg.AppName,
		User:      user,
		Target:    target,
		ReadPanel: panel,
		Feed:      s.toEventViews(events),
		CSRFToken: csrf,
		NowYear:   time.Now().Year(),
		IsCoord:   user.Role == models.RoleCoordinator,
	}
	s.render(w, "home", data)
}

func (s *Server) buildReadPanelData(ctx context.Context, user models.User, target *models.ReadingTarget, csrf string) (readPanelData, error) {
	panel := readPanelData{CSRFToken: csrf}
	if target == nil {
		return panel, nil
	}

	statuses, err := s.repo.ListMemberStatuses(ctx, target.ID)
	if err != nil {
		return readPanelData{}, err
	}

	panel.HasTarget = true
	panel.TargetID = target.ID
	panel.TargetTitle = target.BookTitle
	panel.MemberCount = len(statuses)
	panel.Members = make([]memberView, 0, len(statuses))

	for _, status := range statuses {
		name := status.DisplayName
		if strings.TrimSpace(name) == "" {
			name = status.Email
		}
		panel.Members = append(panel.Members, memberView{
			Name:   name,
			Email:  status.Email,
			IsRead: status.IsRead,
		})
		if status.IsRead {
			panel.ReadCount++
		}
		if status.UserID == user.ID {
			panel.IsRead = status.IsRead
		}
	}
	return panel, nil
}

func (s *Server) handleToggleStatus(w http.ResponseWriter, r *http.Request, user models.User) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.validateCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}

	target, err := s.repo.GetActiveTarget(r.Context())
	if err != nil {
		http.Error(w, "failed to load target", http.StatusInternalServerError)
		return
	}
	if target == nil {
		http.Error(w, "no active target", http.StatusBadRequest)
		return
	}

	isRead, err := s.repo.ToggleReadStatus(r.Context(), target.ID, user.ID)
	if err != nil {
		http.Error(w, "failed to update status", http.StatusInternalServerError)
		return
	}

	eventType := models.EventReadUnmarked
	stateLabel := "unread"
	if isRead {
		eventType = models.EventReadMarked
		stateLabel = "read"
	}

	payloadMap := map[string]string{
		"actor_email":  user.Email,
		"actor_name":   user.Name(),
		"target_title": target.BookTitle,
		"state":        stateLabel,
		"due_date":     target.DueDate.Format("2006-01-02"),
	}
	payloadJSON, _ := json.Marshal(payloadMap)
	targetID := target.ID
	if _, err := s.repo.CreateActivityEvent(r.Context(), &targetID, user.ID, eventType, string(payloadJSON)); err != nil {
		s.logger.Printf("create read-status activity event failed: %v", err)
	}

	if err := s.queueStatusChangeNotifications(r.Context(), user, target, isRead); err != nil {
		s.logger.Printf("queue status-change notifications failed: %v", err)
	}

	if isHXRequest(r) {
		csrf := s.ensureCSRFToken(w, r)
		panel, err := s.buildReadPanelData(r.Context(), user, target, csrf)
		if err != nil {
			http.Error(w, "failed to refresh panel", http.StatusInternalServerError)
			return
		}
		s.render(w, "read_panel", panel)
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) queueStatusChangeNotifications(ctx context.Context, actor models.User, target *models.ReadingTarget, isRead bool) error {
	recipients, err := s.repo.ListActiveMemberEmailsExcept(ctx, actor.ID)
	if err != nil {
		return err
	}
	if len(recipients) == 0 {
		return nil
	}

	stateText := "marked unread"
	if isRead {
		stateText = "clicked I read!"
	}
	subject := fmt.Sprintf("%s updated reading status", actor.Name())
	body := fmt.Sprintf(
		"%s %s in Page Patrol.\n\nTarget: %s (%s %d-%d)\nDue: %s",
		actor.Name(),
		stateText,
		target.BookTitle,
		target.ProgressMode,
		target.ProgressStart,
		target.ProgressEnd,
		target.DueDate.Format("2006-01-02"),
	)
	payload, err := json.Marshal(models.OutboundEmail{Subject: subject, Body: body})
	if err != nil {
		return err
	}

	jobs := make([]models.EmailJob, 0, len(recipients))
	for _, recipient := range recipients {
		jobs = append(jobs, models.EmailJob{
			JobType:        "status_change",
			RecipientEmail: recipient,
			PayloadJSON:    string(payload),
		})
	}
	return s.repo.InsertEmailJobs(ctx, jobs)
}

func (s *Server) handleFeedFragment(w http.ResponseWriter, r *http.Request, _ models.User) {
	events, err := s.repo.ListActivityEvents(r.Context(), 40)
	if err != nil {
		http.Error(w, "failed to load feed", http.StatusInternalServerError)
		return
	}
	s.render(w, "feed_items", s.toEventViews(events))
}

func (s *Server) handleFeedEvents(w http.ResponseWriter, r *http.Request, _ models.User) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	var afterID int64
	if q := strings.TrimSpace(r.URL.Query().Get("since")); q != "" {
		afterID, _ = strconv.ParseInt(q, 10, 64)
	}
	if headerID := strings.TrimSpace(r.Header.Get("Last-Event-ID")); headerID != "" {
		if parsed, err := strconv.ParseInt(headerID, 10, 64); err == nil {
			afterID = parsed
		}
	}

	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		events, err := s.repo.ListActivityEventsAfter(r.Context(), afterID, 30)
		if err != nil {
			s.logger.Printf("feed stream query failed: %v", err)
			return
		}

		for _, event := range events {
			payload := map[string]string{
				"message":    s.eventMessage(event),
				"created_at": event.CreatedAt.Format(time.RFC3339),
			}
			encoded, _ := json.Marshal(payload)
			fmt.Fprintf(w, "id: %d\n", event.ID)
			fmt.Fprintf(w, "event: activity\n")
			fmt.Fprintf(w, "data: %s\n\n", string(encoded))
			afterID = event.ID
		}

		fmt.Fprint(w, ": ping\n\n")
		flusher.Flush()

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) handleCoordinator(w http.ResponseWriter, r *http.Request, user models.User) {
	csrf := s.ensureCSRFToken(w, r)
	target, err := s.repo.GetActiveTarget(r.Context())
	if err != nil {
		http.Error(w, "failed to load target", http.StatusInternalServerError)
		return
	}

	weekly, err := s.repo.GetWeeklyCompletionRates(r.Context(), 12)
	if err != nil {
		http.Error(w, "failed to load weekly trends", http.StatusInternalServerError)
		return
	}
	streaks, err := s.repo.GetMemberStreaks(r.Context(), 26)
	if err != nil {
		http.Error(w, "failed to load streaks", http.StatusInternalServerError)
		return
	}

	s.render(w, "coordinator", coordinatorPageData{
		AppName:       s.cfg.AppName,
		User:          user,
		Target:        target,
		WeeklyTrends:  weekly,
		MemberStreaks: streaks,
		CSRFToken:     csrf,
		NowYear:       time.Now().Year(),
	})
}

func (s *Server) handlePublishTarget(w http.ResponseWriter, r *http.Request, user models.User) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.validateCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}

	bookTitle := strings.TrimSpace(r.FormValue("book_title"))
	progressMode := strings.TrimSpace(r.FormValue("progress_mode"))
	if progressMode != "chapters" && progressMode != "pages" {
		http.Error(w, "progress_mode must be chapters or pages", http.StatusBadRequest)
		return
	}
	progressStart, err := strconv.Atoi(strings.TrimSpace(r.FormValue("progress_start")))
	if err != nil || progressStart < 0 {
		http.Error(w, "invalid progress_start", http.StatusBadRequest)
		return
	}
	progressEnd, err := strconv.Atoi(strings.TrimSpace(r.FormValue("progress_end")))
	if err != nil || progressEnd < progressStart {
		http.Error(w, "invalid progress_end", http.StatusBadRequest)
		return
	}
	dueDate, err := time.Parse("2006-01-02", strings.TrimSpace(r.FormValue("due_date")))
	if err != nil {
		http.Error(w, "invalid due_date", http.StatusBadRequest)
		return
	}
	if bookTitle == "" {
		http.Error(w, "book_title is required", http.StatusBadRequest)
		return
	}

	target, err := s.repo.PublishTarget(r.Context(), models.CreateTargetInput{
		BookTitle:     bookTitle,
		ProgressMode:  progressMode,
		ProgressStart: progressStart,
		ProgressEnd:   progressEnd,
		DueDate:       dueDate,
		Notes:         strings.TrimSpace(r.FormValue("notes")),
	}, user.ID)
	if err != nil {
		http.Error(w, "failed to publish target", http.StatusInternalServerError)
		return
	}

	payload, _ := json.Marshal(map[string]string{
		"actor_email":  user.Email,
		"actor_name":   user.Name(),
		"target_title": target.BookTitle,
		"due_date":     target.DueDate.Format("2006-01-02"),
	})
	targetID := target.ID
	if _, err := s.repo.CreateActivityEvent(r.Context(), &targetID, user.ID, models.EventTargetSet, string(payload)); err != nil {
		s.logger.Printf("create target-published event failed: %v", err)
	}

	http.Redirect(w, r, "/coordinator", http.StatusSeeOther)
}

func (s *Server) handleMembersPage(w http.ResponseWriter, r *http.Request, user models.User) {
	csrf := s.ensureCSRFToken(w, r)
	invites, err := s.repo.ListInvites(r.Context())
	if err != nil {
		http.Error(w, "failed to load members", http.StatusInternalServerError)
		return
	}
	s.render(w, "members", membersPageData{
		AppName:   s.cfg.AppName,
		User:      user,
		Invites:   invites,
		CSRFToken: csrf,
		NowYear:   time.Now().Year(),
	})
}

func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request, user models.User) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.validateCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}

	email := normalizeEmail(r.FormValue("email"))
	displayName := strings.TrimSpace(r.FormValue("display_name"))
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}

	if err := s.repo.AddOrReactivateInvite(r.Context(), email, displayName, user.ID); err != nil {
		http.Error(w, "failed to add member", http.StatusInternalServerError)
		return
	}

	payload, _ := json.Marshal(map[string]string{
		"actor_email":  user.Email,
		"actor_name":   user.Name(),
		"member_email": email,
	})
	if _, err := s.repo.CreateActivityEvent(r.Context(), nil, user.ID, models.EventMemberAdded, string(payload)); err != nil {
		s.logger.Printf("create member-added event failed: %v", err)
	}

	http.Redirect(w, r, "/coordinator/members", http.StatusSeeOther)
}

func (s *Server) handleDeactivateMember(w http.ResponseWriter, r *http.Request, user models.User) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.validateCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid member id", http.StatusBadRequest)
		return
	}

	email, err := s.repo.DeactivateInvite(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "member not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to deactivate member", http.StatusInternalServerError)
		return
	}

	payload, _ := json.Marshal(map[string]string{
		"actor_email":  user.Email,
		"actor_name":   user.Name(),
		"member_email": email,
	})
	if _, err := s.repo.CreateActivityEvent(r.Context(), nil, user.ID, models.EventMemberRemoved, string(payload)); err != nil {
		s.logger.Printf("create member-removed event failed: %v", err)
	}

	http.Redirect(w, r, "/coordinator/members", http.StatusSeeOther)
}

func (s *Server) handleReactivateMember(w http.ResponseWriter, r *http.Request, user models.User) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	if !s.validateCSRF(r) {
		http.Error(w, "invalid csrf token", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid member id", http.StatusBadRequest)
		return
	}

	email, err := s.repo.ReactivateInvite(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "member not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to reactivate member", http.StatusInternalServerError)
		return
	}

	payload, _ := json.Marshal(map[string]string{
		"actor_email":  user.Email,
		"actor_name":   user.Name(),
		"member_email": email,
	})
	if _, err := s.repo.CreateActivityEvent(r.Context(), nil, user.ID, models.EventMemberAdded, string(payload)); err != nil {
		s.logger.Printf("create member-reactivated event failed: %v", err)
	}

	http.Redirect(w, r, "/coordinator/members", http.StatusSeeOther)
}

func (s *Server) handleTrendsAPI(w http.ResponseWriter, r *http.Request, _ models.User) {
	weekly, err := s.repo.GetWeeklyCompletionRates(r.Context(), 20)
	if err != nil {
		http.Error(w, "failed to load weekly trends", http.StatusInternalServerError)
		return
	}
	streaks, err := s.repo.GetMemberStreaks(r.Context(), 52)
	if err != nil {
		http.Error(w, "failed to load streaks", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"weekly":  weekly,
		"streaks": streaks,
	})
}

func (s *Server) requireAuth(next func(http.ResponseWriter, *http.Request, models.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.authenticatedUser(r)
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r, user)
	}
}

func (s *Server) requireCoordinator(next func(http.ResponseWriter, *http.Request, models.User)) http.HandlerFunc {
	return s.requireAuth(func(w http.ResponseWriter, r *http.Request, user models.User) {
		if user.Role != models.RoleCoordinator {
			http.Error(w, "coordinator access required", http.StatusForbidden)
			return
		}
		next(w, r, user)
	})
}

func (s *Server) authenticatedUser(r *http.Request) (models.User, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return models.User{}, false
	}
	user, err := s.repo.GetUserBySessionToken(r.Context(), security.HashToken(cookie.Value))
	if err != nil {
		return models.User{}, false
	}
	return user, true
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(s.cfg.SessionTTL),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) ensureCSRFToken(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(csrfCookieName); err == nil && strings.TrimSpace(cookie.Value) != "" {
		return cookie.Value
	}
	token, err := security.RandomToken(32)
	if err != nil {
		token = fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	return token
}

func (s *Server) validateCSRF(r *http.Request) bool {
	cookie, err := r.Cookie(csrfCookieName)
	if err != nil {
		return false
	}
	token := strings.TrimSpace(r.FormValue("csrf_token"))
	if token == "" {
		token = strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
	}
	if token == "" || cookie.Value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(cookie.Value)) == 1
}

func (s *Server) render(w http.ResponseWriter, templateName string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, templateName, data); err != nil {
		s.logger.Printf("render template %s failed: %v", templateName, err)
		http.Error(w, "template rendering error", http.StatusInternalServerError)
	}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Server) toEventViews(events []models.ActivityEvent) []eventView {
	views := make([]eventView, 0, len(events))
	for _, event := range events {
		views = append(views, eventView{
			ID:      event.ID,
			Message: s.eventMessage(event),
			When:    event.CreatedAt.Local().Format("2006-01-02 15:04"),
		})
	}
	return views
}

func (s *Server) eventMessage(event models.ActivityEvent) string {
	actor := event.ActorEmail
	if strings.TrimSpace(event.ActorName) != "" {
		actor = event.ActorName
	}
	target := event.TargetTitle

	payload := map[string]string{}
	if strings.TrimSpace(event.PayloadJSON) != "" {
		_ = json.Unmarshal([]byte(event.PayloadJSON), &payload)
		if payloadTarget := strings.TrimSpace(payload["target_title"]); payloadTarget != "" {
			target = payloadTarget
		}
		if payloadActor := strings.TrimSpace(payload["actor_name"]); payloadActor != "" {
			actor = payloadActor
		}
		if actor == "" {
			actor = strings.TrimSpace(payload["actor_email"])
		}
	}

	switch event.EventType {
	case models.EventReadMarked:
		if target == "" {
			return fmt.Sprintf("%s clicked I read!", actor)
		}
		return fmt.Sprintf("%s clicked I read! for %s", actor, target)
	case models.EventReadUnmarked:
		if target == "" {
			return fmt.Sprintf("%s marked unread", actor)
		}
		return fmt.Sprintf("%s marked unread for %s", actor, target)
	case models.EventTargetSet:
		due := strings.TrimSpace(payload["due_date"])
		if due != "" {
			return fmt.Sprintf("%s published a new target: %s (due %s)", actor, target, due)
		}
		return fmt.Sprintf("%s published a new target: %s", actor, target)
	case models.EventMemberAdded:
		member := strings.TrimSpace(payload["member_email"])
		if member == "" {
			member = "a member"
		}
		return fmt.Sprintf("%s added %s to the allow list", actor, member)
	case models.EventMemberRemoved:
		member := strings.TrimSpace(payload["member_email"])
		if member == "" {
			member = "a member"
		}
		return fmt.Sprintf("%s removed %s from the allow list", actor, member)
	default:
		if actor == "" {
			return "Activity updated"
		}
		return fmt.Sprintf("%s updated activity", actor)
	}
}

func isHXRequest(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("HX-Request")), "true")
}

func requestIsSecure(r *http.Request, trustProxyHeaders bool) bool {
	if r.TLS != nil {
		return true
	}
	if !trustProxyHeaders {
		return false
	}

	if proto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto")); strings.EqualFold(proto, "https") {
		return true
	}

	if strings.Contains(strings.ToLower(strings.ReplaceAll(r.Header.Get("CF-Visitor"), " ", "")), "\"scheme\":\"https\"") {
		return true
	}

	if strings.EqualFold(forwardedDirectiveValue(r.Header.Get("Forwarded"), "proto"), "https") {
		return true
	}

	return false
}

func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxyHeaders {
		if ip := firstHeaderValue(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
		if ip := firstHeaderValue(r.Header.Get("X-Forwarded-For")); ip != "" {
			return ip
		}
		if ip := forwardedDirectiveValue(r.Header.Get("Forwarded"), "for"); ip != "" {
			return strings.Trim(ip, "\"[]")
		}
	}

	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func redirectURL(baseURL *url.URL, r *http.Request) string {
	target := &url.URL{
		Scheme:   "https",
		Host:     r.Host,
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
	}
	if baseURL != nil && baseURL.Host != "" {
		target.Host = baseURL.Host
	}
	return target.String()
}

func firstHeaderValue(value string) string {
	first, _, _ := strings.Cut(value, ",")
	return strings.TrimSpace(first)
}

func (s *Server) contentSecurityPolicy() string {
	scriptSrc := "'self'"
	if s.cfg.CSPAllowUnsafeInline {
		scriptSrc += " 'unsafe-inline'"
	}
	return "default-src 'self'; script-src " + scriptSrc + "; style-src 'self'; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
}

func forwardedDirectiveValue(value, directive string) string {
	for _, element := range strings.Split(value, ",") {
		for _, part := range strings.Split(element, ";") {
			key, parsedValue, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok && strings.EqualFold(key, directive) {
				return strings.TrimSpace(parsedValue)
			}
		}
	}
	return ""
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}
