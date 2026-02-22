package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"page-patrol/internal/models"
	"page-patrol/internal/trends"
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func displayNameFromEmail(email string) string {
	local := email
	if parts := strings.Split(email, "@"); len(parts) > 0 {
		local = parts[0]
	}
	local = strings.ReplaceAll(local, ".", " ")
	local = strings.ReplaceAll(local, "_", " ")
	local = strings.TrimSpace(local)
	if local == "" {
		return email
	}
	return strings.Title(local)
}

func (r *Repository) EnsureCoordinatorInvite(ctx context.Context, coordinatorEmail string) error {
	email := normalizeEmail(coordinatorEmail)
	if email == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO invites (email, display_name, active)
		VALUES ($1, $2, TRUE)
		ON CONFLICT (email) DO UPDATE SET
			active = TRUE,
			updated_at = NOW()
	`, email, "Coordinator")
	if err != nil {
		return fmt.Errorf("ensure coordinator invite: %w", err)
	}
	return nil
}

func (r *Repository) IsEmailAllowed(ctx context.Context, email, coordinatorEmail string) (bool, error) {
	normalized := normalizeEmail(email)
	if normalized == "" {
		return false, nil
	}
	if normalized == normalizeEmail(coordinatorEmail) {
		return true, nil
	}

	var allowed bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM invites
			WHERE email = $1 AND active = TRUE
		)
	`, normalized).Scan(&allowed)
	if err != nil {
		return false, fmt.Errorf("is email allowed: %w", err)
	}
	return allowed, nil
}

func (r *Repository) CreateMagicLinkToken(ctx context.Context, email, tokenHash string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO magic_link_tokens (email, token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, normalizeEmail(email), tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("create magic link token: %w", err)
	}
	return nil
}

func (r *Repository) ConsumeMagicLinkToken(ctx context.Context, tokenHash string) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin consume token tx: %w", err)
	}
	defer tx.Rollback()

	var id int64
	var email string
	err = tx.QueryRowContext(ctx, `
		SELECT id, email
		FROM magic_link_tokens
		WHERE token_hash = $1
		  AND used_at IS NULL
		  AND expires_at > NOW()
		FOR UPDATE
	`, tokenHash).Scan(&id, &email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", sql.ErrNoRows
		}
		return "", fmt.Errorf("find token: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE magic_link_tokens SET used_at = NOW() WHERE id = $1`, id); err != nil {
		return "", fmt.Errorf("consume token: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit consume token: %w", err)
	}
	return normalizeEmail(email), nil
}

func (r *Repository) UpsertUserForEmail(ctx context.Context, email, coordinatorEmail string) (models.User, error) {
	normalized := normalizeEmail(email)
	role := models.RoleMember
	if normalized == normalizeEmail(coordinatorEmail) {
		role = models.RoleCoordinator
	}
	displayName := displayNameFromEmail(normalized)

	var user models.User
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO users (email, display_name, role, active)
		VALUES ($1, $2, $3, TRUE)
		ON CONFLICT (email) DO UPDATE SET
			active = TRUE,
			role = CASE WHEN EXCLUDED.role = 'coordinator' THEN 'coordinator' ELSE users.role END
		RETURNING id, email, COALESCE(display_name, ''), role, active, created_at
	`, normalized, displayName, role).Scan(
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&user.Role,
		&user.Active,
		&user.CreatedAt,
	)
	if err != nil {
		return models.User{}, fmt.Errorf("upsert user: %w", err)
	}
	return user, nil
}

func (r *Repository) CreateSession(ctx context.Context, userID int64, sessionHash string, expiresAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO sessions (user_id, session_token_hash, expires_at)
		VALUES ($1, $2, $3)
	`, userID, sessionHash, expiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

func (r *Repository) GetUserBySessionToken(ctx context.Context, sessionHash string) (models.User, error) {
	var user models.User
	err := r.db.QueryRowContext(ctx, `
		SELECT u.id, u.email, COALESCE(u.display_name, ''), u.role, u.active, u.created_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.session_token_hash = $1
		  AND s.expires_at > NOW()
		  AND u.active = TRUE
		LIMIT 1
	`, sessionHash).Scan(
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&user.Role,
		&user.Active,
		&user.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.User{}, sql.ErrNoRows
		}
		return models.User{}, fmt.Errorf("get user by session: %w", err)
	}
	return user, nil
}

func (r *Repository) DeleteSessionByHash(ctx context.Context, sessionHash string) error {
	_, err := r.db.ExecContext(ctx, `DELETE FROM sessions WHERE session_token_hash = $1`, sessionHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (r *Repository) RevokeSessionsByEmail(ctx context.Context, email string) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE user_id IN (
			SELECT id FROM users WHERE email = $1
		)
	`, normalizeEmail(email))
	if err != nil {
		return fmt.Errorf("revoke sessions by email: %w", err)
	}
	return nil
}

func (r *Repository) GetActiveTarget(ctx context.Context) (*models.ReadingTarget, error) {
	var target models.ReadingTarget
	var notes sql.NullString
	err := r.db.QueryRowContext(ctx, `
		SELECT id, book_title, progress_mode, progress_start, progress_end, due_date, notes, status, created_by, created_at
		FROM reading_targets
		WHERE status = 'active'
		ORDER BY id DESC
		LIMIT 1
	`).Scan(
		&target.ID,
		&target.BookTitle,
		&target.ProgressMode,
		&target.ProgressStart,
		&target.ProgressEnd,
		&target.DueDate,
		&notes,
		&target.Status,
		&target.CreatedBy,
		&target.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get active target: %w", err)
	}
	target.Notes = notes.String
	return &target, nil
}

func (r *Repository) PublishTarget(ctx context.Context, input models.CreateTargetInput, createdBy int64) (models.ReadingTarget, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.ReadingTarget{}, fmt.Errorf("begin publish target tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE reading_targets SET status = 'archived' WHERE status = 'active'`); err != nil {
		return models.ReadingTarget{}, fmt.Errorf("archive active target: %w", err)
	}

	var target models.ReadingTarget
	var notes sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO reading_targets (
			book_title,
			progress_mode,
			progress_start,
			progress_end,
			due_date,
			notes,
			status,
			created_by
		) VALUES ($1, $2, $3, $4, $5, $6, 'active', $7)
		RETURNING id, book_title, progress_mode, progress_start, progress_end, due_date, notes, status, created_by, created_at
	`, input.BookTitle, input.ProgressMode, input.ProgressStart, input.ProgressEnd, input.DueDate, strings.TrimSpace(input.Notes), createdBy).Scan(
		&target.ID,
		&target.BookTitle,
		&target.ProgressMode,
		&target.ProgressStart,
		&target.ProgressEnd,
		&target.DueDate,
		&notes,
		&target.Status,
		&target.CreatedBy,
		&target.CreatedAt,
	)
	if err != nil {
		return models.ReadingTarget{}, fmt.Errorf("insert target: %w", err)
	}
	target.Notes = notes.String

	if err := tx.Commit(); err != nil {
		return models.ReadingTarget{}, fmt.Errorf("commit publish target: %w", err)
	}
	return target, nil
}

func (r *Repository) ToggleReadStatus(ctx context.Context, targetID, userID int64) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin toggle read tx: %w", err)
	}
	defer tx.Rollback()

	var current bool
	err = tx.QueryRowContext(ctx, `
		SELECT is_read
		FROM read_statuses
		WHERE target_id = $1 AND user_id = $2
		FOR UPDATE
	`, targetID, userID).Scan(&current)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO read_statuses (target_id, user_id, is_read, updated_at)
				VALUES ($1, $2, TRUE, NOW())
			`, targetID, userID); err != nil {
				return false, fmt.Errorf("insert read status: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return false, fmt.Errorf("commit read status insert: %w", err)
			}
			return true, nil
		}
		return false, fmt.Errorf("select read status: %w", err)
	}

	newState := !current
	if _, err := tx.ExecContext(ctx, `
		UPDATE read_statuses
		SET is_read = $1, updated_at = NOW()
		WHERE target_id = $2 AND user_id = $3
	`, newState, targetID, userID); err != nil {
		return false, fmt.Errorf("update read status: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit read status update: %w", err)
	}
	return newState, nil
}

func (r *Repository) ListMemberStatuses(ctx context.Context, targetID int64) ([]models.MemberStatus, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			u.id,
			u.email,
			COALESCE(u.display_name, ''),
			COALESCE(rs.is_read, FALSE)
		FROM users u
		LEFT JOIN read_statuses rs ON rs.user_id = u.id AND rs.target_id = $1
		WHERE u.active = TRUE
		ORDER BY LOWER(COALESCE(NULLIF(u.display_name, ''), u.email))
	`, targetID)
	if err != nil {
		return nil, fmt.Errorf("list member statuses: %w", err)
	}
	defer rows.Close()

	statuses := make([]models.MemberStatus, 0)
	for rows.Next() {
		var status models.MemberStatus
		if err := rows.Scan(&status.UserID, &status.Email, &status.DisplayName, &status.IsRead); err != nil {
			return nil, fmt.Errorf("scan member status: %w", err)
		}
		statuses = append(statuses, status)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate member statuses: %w", err)
	}
	return statuses, nil
}

func (r *Repository) CreateActivityEvent(ctx context.Context, targetID *int64, actorUserID int64, eventType, payloadJSON string) (models.ActivityEvent, error) {
	var targetArg any
	if targetID == nil {
		targetArg = nil
	} else {
		targetArg = *targetID
	}

	var event models.ActivityEvent
	var targetIDOut sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO activity_events (target_id, actor_user_id, event_type, payload_json)
		VALUES ($1, $2, $3, $4::jsonb)
		RETURNING id, target_id, actor_user_id, event_type, payload_json, created_at
	`, targetArg, actorUserID, eventType, payloadJSON).Scan(
		&event.ID,
		&targetIDOut,
		&event.ActorUserID,
		&event.EventType,
		&event.PayloadJSON,
		&event.CreatedAt,
	)
	if err != nil {
		return models.ActivityEvent{}, fmt.Errorf("create activity event: %w", err)
	}
	if targetIDOut.Valid {
		targetID := targetIDOut.Int64
		event.TargetID = &targetID
	}
	return event, nil
}

func (r *Repository) ListActivityEvents(ctx context.Context, limit int) ([]models.ActivityEvent, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			ae.id,
			ae.target_id,
			ae.actor_user_id,
			u.email,
			COALESCE(u.display_name, ''),
			COALESCE(rt.book_title, ''),
			ae.event_type,
			ae.payload_json,
			ae.created_at
		FROM activity_events ae
		JOIN users u ON u.id = ae.actor_user_id
		LEFT JOIN reading_targets rt ON rt.id = ae.target_id
		ORDER BY ae.id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list activity events: %w", err)
	}
	defer rows.Close()

	items := make([]models.ActivityEvent, 0)
	for rows.Next() {
		var event models.ActivityEvent
		var targetID sql.NullInt64
		if err := rows.Scan(
			&event.ID,
			&targetID,
			&event.ActorUserID,
			&event.ActorEmail,
			&event.ActorName,
			&event.TargetTitle,
			&event.EventType,
			&event.PayloadJSON,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan activity event: %w", err)
		}
		if targetID.Valid {
			id := targetID.Int64
			event.TargetID = &id
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity events: %w", err)
	}
	return items, nil
}

func (r *Repository) ListActivityEventsAfter(ctx context.Context, afterID int64, limit int) ([]models.ActivityEvent, error) {
	if limit <= 0 {
		limit = 30
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			ae.id,
			ae.target_id,
			ae.actor_user_id,
			u.email,
			COALESCE(u.display_name, ''),
			COALESCE(rt.book_title, ''),
			ae.event_type,
			ae.payload_json,
			ae.created_at
		FROM activity_events ae
		JOIN users u ON u.id = ae.actor_user_id
		LEFT JOIN reading_targets rt ON rt.id = ae.target_id
		WHERE ae.id > $1
		ORDER BY ae.id ASC
		LIMIT $2
	`, afterID, limit)
	if err != nil {
		return nil, fmt.Errorf("list activity events after: %w", err)
	}
	defer rows.Close()

	items := make([]models.ActivityEvent, 0)
	for rows.Next() {
		var event models.ActivityEvent
		var targetID sql.NullInt64
		if err := rows.Scan(
			&event.ID,
			&targetID,
			&event.ActorUserID,
			&event.ActorEmail,
			&event.ActorName,
			&event.TargetTitle,
			&event.EventType,
			&event.PayloadJSON,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan activity event after: %w", err)
		}
		if targetID.Valid {
			id := targetID.Int64
			event.TargetID = &id
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity events after: %w", err)
	}
	return items, nil
}

func (r *Repository) ListActiveMemberEmailsExcept(ctx context.Context, excludedUserID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT email
		FROM users
		WHERE active = TRUE AND id <> $1
		ORDER BY email
	`, excludedUserID)
	if err != nil {
		return nil, fmt.Errorf("list active emails: %w", err)
	}
	defer rows.Close()

	emails := make([]string, 0)
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, fmt.Errorf("scan active email: %w", err)
		}
		emails = append(emails, email)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active emails: %w", err)
	}
	return emails, nil
}

func (r *Repository) InsertEmailJobs(ctx context.Context, jobs []models.EmailJob) error {
	if len(jobs) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin insert email jobs tx: %w", err)
	}
	defer tx.Rollback()

	for _, job := range jobs {
		recipient := normalizeEmail(job.RecipientEmail)
		if recipient == "" {
			continue
		}
		payload := strings.TrimSpace(job.PayloadJSON)
		if payload == "" {
			payload = `{}`
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO email_jobs (job_type, recipient_email, payload_json, status, attempt_count, next_attempt_at)
			VALUES ($1, $2, $3::jsonb, 'queued', 0, NOW())
		`, job.JobType, recipient, payload); err != nil {
			return fmt.Errorf("insert email job: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit email jobs: %w", err)
	}
	return nil
}

func (r *Repository) FetchDueEmailJobs(ctx context.Context, limit int) ([]models.EmailJob, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, job_type, recipient_email, payload_json, status, attempt_count, next_attempt_at, created_at, sent_at
		FROM email_jobs
		WHERE status IN ('queued', 'failed')
		  AND attempt_count < 6
		  AND (next_attempt_at IS NULL OR next_attempt_at <= NOW())
		ORDER BY created_at ASC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("fetch due email jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]models.EmailJob, 0)
	for rows.Next() {
		var job models.EmailJob
		var nextAttempt sql.NullTime
		var sentAt sql.NullTime
		if err := rows.Scan(
			&job.ID,
			&job.JobType,
			&job.RecipientEmail,
			&job.PayloadJSON,
			&job.Status,
			&job.AttemptCount,
			&nextAttempt,
			&job.CreatedAt,
			&sentAt,
		); err != nil {
			return nil, fmt.Errorf("scan due email job: %w", err)
		}
		if nextAttempt.Valid {
			t := nextAttempt.Time
			job.NextAttemptAt = &t
		}
		if sentAt.Valid {
			t := sentAt.Time
			job.SentAt = &t
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due email jobs: %w", err)
	}
	return jobs, nil
}

func (r *Repository) MarkEmailJobSent(ctx context.Context, id int64) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE email_jobs
		SET status = 'sent', sent_at = NOW(), next_attempt_at = NULL
		WHERE id = $1
	`, id)
	if err != nil {
		return fmt.Errorf("mark email job sent: %w", err)
	}
	return nil
}

func (r *Repository) MarkEmailJobFailed(ctx context.Context, id int64, attemptCount int, nextAttempt *time.Time) error {
	if nextAttempt == nil {
		_, err := r.db.ExecContext(ctx, `
			UPDATE email_jobs
			SET status = 'failed', attempt_count = $2, next_attempt_at = NULL
			WHERE id = $1
		`, id, attemptCount)
		if err != nil {
			return fmt.Errorf("mark email job failed permanent: %w", err)
		}
		return nil
	}

	_, err := r.db.ExecContext(ctx, `
		UPDATE email_jobs
		SET status = 'failed', attempt_count = $2, next_attempt_at = $3
		WHERE id = $1
	`, id, attemptCount, *nextAttempt)
	if err != nil {
		return fmt.Errorf("mark email job failed retry: %w", err)
	}
	return nil
}

func (r *Repository) ListInvites(ctx context.Context) ([]models.Invite, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, email, COALESCE(display_name, ''), active, created_at, updated_at
		FROM invites
		ORDER BY LOWER(email)
	`)
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	defer rows.Close()

	invites := make([]models.Invite, 0)
	for rows.Next() {
		var invite models.Invite
		if err := rows.Scan(
			&invite.ID,
			&invite.Email,
			&invite.DisplayName,
			&invite.Active,
			&invite.CreatedAt,
			&invite.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan invite: %w", err)
		}
		invites = append(invites, invite)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invites: %w", err)
	}
	return invites, nil
}

func (r *Repository) AddOrReactivateInvite(ctx context.Context, email, displayName string, invitedBy int64) error {
	normalized := normalizeEmail(email)
	if normalized == "" {
		return fmt.Errorf("email is required")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin add invite tx: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO invites (email, display_name, active, invited_by)
		VALUES ($1, $2, TRUE, $3)
		ON CONFLICT (email) DO UPDATE SET
			active = TRUE,
			display_name = CASE WHEN EXCLUDED.display_name = '' THEN invites.display_name ELSE EXCLUDED.display_name END,
			updated_at = NOW()
	`, normalized, strings.TrimSpace(displayName), invitedBy); err != nil {
		return fmt.Errorf("upsert invite: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE users
		SET active = TRUE,
			display_name = CASE WHEN $2 = '' THEN users.display_name ELSE $2 END
		WHERE email = $1
	`, normalized, strings.TrimSpace(displayName)); err != nil {
		return fmt.Errorf("reactivate user on invite: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit add invite: %w", err)
	}
	return nil
}

func (r *Repository) DeactivateInvite(ctx context.Context, id int64) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin deactivate invite tx: %w", err)
	}
	defer tx.Rollback()

	var email string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM invites WHERE id = $1 FOR UPDATE`, id).Scan(&email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", sql.ErrNoRows
		}
		return "", fmt.Errorf("find invite: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE invites SET active = FALSE, updated_at = NOW() WHERE id = $1`, id); err != nil {
		return "", fmt.Errorf("deactivate invite: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET active = FALSE WHERE email = $1`, email); err != nil {
		return "", fmt.Errorf("deactivate user: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM sessions
		WHERE user_id IN (SELECT id FROM users WHERE email = $1)
	`, email); err != nil {
		return "", fmt.Errorf("revoke sessions: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit deactivate invite: %w", err)
	}
	return email, nil
}

func (r *Repository) ReactivateInvite(ctx context.Context, id int64) (string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("begin reactivate invite tx: %w", err)
	}
	defer tx.Rollback()

	var email string
	if err := tx.QueryRowContext(ctx, `SELECT email FROM invites WHERE id = $1 FOR UPDATE`, id).Scan(&email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", sql.ErrNoRows
		}
		return "", fmt.Errorf("find invite: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `UPDATE invites SET active = TRUE, updated_at = NOW() WHERE id = $1`, id); err != nil {
		return "", fmt.Errorf("reactivate invite: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET active = TRUE WHERE email = $1`, email); err != nil {
		return "", fmt.Errorf("reactivate user: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit reactivate invite: %w", err)
	}
	return email, nil
}

func (r *Repository) GetWeeklyCompletionRates(ctx context.Context, limit int) ([]models.WeeklyTrend, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT
			t.id,
			t.book_title,
			t.due_date,
			COALESCE(SUM(CASE WHEN rs.is_read THEN 1 ELSE 0 END), 0) AS readers,
			COUNT(u.id) AS total_members
		FROM reading_targets t
		LEFT JOIN users u ON u.active = TRUE
		LEFT JOIN read_statuses rs ON rs.target_id = t.id AND rs.user_id = u.id
		WHERE t.status = 'archived'
		GROUP BY t.id, t.book_title, t.due_date
		ORDER BY t.due_date DESC, t.id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query weekly completion rates: %w", err)
	}
	defer rows.Close()

	trendsOut := make([]models.WeeklyTrend, 0)
	for rows.Next() {
		var trend models.WeeklyTrend
		if err := rows.Scan(&trend.TargetID, &trend.BookTitle, &trend.DueDate, &trend.Readers, &trend.TotalMembers); err != nil {
			return nil, fmt.Errorf("scan weekly trend: %w", err)
		}
		if trend.TotalMembers > 0 {
			trend.CompletionRate = float64(trend.Readers) * 100 / float64(trend.TotalMembers)
		}
		trendsOut = append(trendsOut, trend)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate weekly trends: %w", err)
	}
	return trendsOut, nil
}

func (r *Repository) GetMemberStreaks(ctx context.Context, targetWindow int) ([]models.MemberStreak, error) {
	if targetWindow <= 0 {
		targetWindow = 26
	}

	type activeUser struct {
		ID          int64
		Email       string
		DisplayName string
	}
	userRows, err := r.db.QueryContext(ctx, `
		SELECT id, email, COALESCE(display_name, '')
		FROM users
		WHERE active = TRUE
		ORDER BY LOWER(email)
	`)
	if err != nil {
		return nil, fmt.Errorf("list active users for streaks: %w", err)
	}
	defer userRows.Close()

	users := make([]activeUser, 0)
	for userRows.Next() {
		var u activeUser
		if err := userRows.Scan(&u.ID, &u.Email, &u.DisplayName); err != nil {
			return nil, fmt.Errorf("scan active user for streaks: %w", err)
		}
		users = append(users, u)
	}
	if err := userRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active users for streaks: %w", err)
	}

	if len(users) == 0 {
		return []models.MemberStreak{}, nil
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT
			u.id,
			t.rn,
			COALESCE(rs.is_read, FALSE)
		FROM users u
		JOIN (
			SELECT id, ROW_NUMBER() OVER (ORDER BY due_date DESC, id DESC) AS rn
			FROM reading_targets
			WHERE status = 'archived'
			ORDER BY due_date DESC, id DESC
			LIMIT $1
		) t ON TRUE
		LEFT JOIN read_statuses rs ON rs.user_id = u.id AND rs.target_id = t.id
		WHERE u.active = TRUE
		ORDER BY u.id, t.rn
	`, targetWindow)
	if err != nil {
		return nil, fmt.Errorf("query streak matrix: %w", err)
	}
	defer rows.Close()

	matrix := map[int64][]bool{}
	for rows.Next() {
		var userID int64
		var rank int
		var isRead bool
		if err := rows.Scan(&userID, &rank, &isRead); err != nil {
			return nil, fmt.Errorf("scan streak matrix: %w", err)
		}
		matrix[userID] = append(matrix[userID], isRead)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate streak matrix: %w", err)
	}

	result := make([]models.MemberStreak, 0, len(users))
	for _, user := range users {
		streakValues := matrix[user.ID]
		result = append(result, models.MemberStreak{
			UserID:      user.ID,
			Email:       user.Email,
			DisplayName: user.DisplayName,
			Streak:      trends.ComputeStreak(streakValues),
		})
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Streak == result[j].Streak {
			return strings.ToLower(result[i].Email) < strings.ToLower(result[j].Email)
		}
		return result[i].Streak > result[j].Streak
	})

	return result, nil
}
