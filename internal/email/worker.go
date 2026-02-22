package email

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"page-patrol/internal/models"
	"page-patrol/internal/repository"
)

type Worker struct {
	repo        *repository.Repository
	sender      *SMTPSender
	logger      *log.Logger
	pollEvery   time.Duration
	maxAttempts int
}

func NewWorker(repo *repository.Repository, sender *SMTPSender, logger *log.Logger) *Worker {
	return &Worker{
		repo:        repo,
		sender:      sender,
		logger:      logger,
		pollEvery:   3 * time.Second,
		maxAttempts: 6,
	}
}

func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.pollEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Printf("worker stopping: %v", ctx.Err())
			return nil
		case <-ticker.C:
			if err := w.processOnce(ctx); err != nil {
				w.logger.Printf("process jobs: %v", err)
			}
		}
	}
}

func (w *Worker) processOnce(ctx context.Context) error {
	jobs, err := w.repo.FetchDueEmailJobs(ctx, 25)
	if err != nil {
		return fmt.Errorf("fetch due email jobs: %w", err)
	}

	for _, job := range jobs {
		if err := w.processJob(ctx, job); err != nil {
			w.logger.Printf("job %d failed: %v", job.ID, err)
		}
	}
	return nil
}

func (w *Worker) processJob(ctx context.Context, job models.EmailJob) error {
	var payload models.OutboundEmail
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		attempt := job.AttemptCount + 1
		return w.repo.MarkEmailJobFailed(ctx, job.ID, attempt, nil)
	}

	if payload.Subject == "" || payload.Body == "" {
		attempt := job.AttemptCount + 1
		return w.repo.MarkEmailJobFailed(ctx, job.ID, attempt, nil)
	}

	if err := w.sender.Send(job.RecipientEmail, payload.Subject, payload.Body); err != nil {
		attempt := job.AttemptCount + 1
		var nextAttempt *time.Time
		if attempt < w.maxAttempts {
			next := time.Now().Add(backoff(attempt))
			nextAttempt = &next
		}
		if markErr := w.repo.MarkEmailJobFailed(ctx, job.ID, attempt, nextAttempt); markErr != nil {
			return fmt.Errorf("send err: %v; mark failed err: %w", err, markErr)
		}
		return err
	}

	if err := w.repo.MarkEmailJobSent(ctx, job.ID); err != nil {
		return fmt.Errorf("mark sent: %w", err)
	}
	return nil
}

func backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		attempt = 6
	}
	minutes := 1 << (attempt - 1)
	return time.Duration(minutes) * time.Minute
}
