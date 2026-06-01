package cron

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	store Store
	clock Clock
}

func NewService(store Store, clock Clock) *Service {
	if clock == nil {
		clock = realClock{}
	}
	return &Service{store: store, clock: clock}
}

func (s *Service) CreateJob(ctx context.Context, job CronJob) (string, error) {
	if s.store == nil {
		return "", fmt.Errorf("cron store is required")
	}
	if strings.TrimSpace(job.Prompt) == "" {
		return "", fmt.Errorf("prompt is required")
	}
	job.Cwd = strings.TrimSpace(job.Cwd)
	if job.Cwd == "" {
		job.Cwd = extractCwdFromPrompt(job.Prompt)
	}
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.LastStatus == "" {
		job.LastStatus = "idle"
	}
	job.Active = true
	now := s.clock.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	switch strings.ToLower(strings.TrimSpace(job.ScheduleType)) {
	case "cron":
		if strings.TrimSpace(job.CronExpr) == "" {
			return "", fmt.Errorf("cron_expr is required for cron jobs")
		}
		nextRunAt, err := computeNextRun(job.CronExpr, now)
		if err != nil {
			return "", err
		}
		job.NextRunAt = &nextRunAt
	case "once":
		if job.RunAt == nil {
			return "", fmt.Errorf("run_at is required for once jobs")
		}
		job.NextRunAt = job.RunAt
	default:
		return "", fmt.Errorf("unsupported schedule_type %q", job.ScheduleType)
	}

	if err := s.store.CreateJob(ctx, job); err != nil {
		return "", err
	}
	return job.ID, nil
}

func (s *Service) ListJobs(ctx context.Context, chatID int64) ([]CronJob, error) {
	return s.store.ListJobsByChat(ctx, chatID)
}

func (s *Service) PauseJob(ctx context.Context, jobID string) error {
	fullID, err := s.store.ResolveJobID(ctx, jobID)
	if err != nil {
		return err
	}
	job, err := s.store.GetJob(ctx, fullID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("cron job %s not found", jobID)
	}
	job.Active = false
	job.UpdatedAt = s.clock.Now().UTC()
	return s.store.UpdateJob(ctx, *job)
}

func (s *Service) ResumeJob(ctx context.Context, jobID string) error {
	fullID, err := s.store.ResolveJobID(ctx, jobID)
	if err != nil {
		return err
	}
	job, err := s.store.GetJob(ctx, fullID)
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("cron job %s not found", jobID)
	}
	job.Active = true
	job.UpdatedAt = s.clock.Now().UTC()
	return s.store.UpdateJob(ctx, *job)
}

// ListJobsByOwner returns all cron jobs owned by the given user.
func (s *Service) ListJobsByOwner(ctx context.Context, ownerUserID string) ([]CronJob, error) {
	return s.store.ListJobsByOwner(ctx, ownerUserID)
}

// DeleteJobByOwner deletes a cron job scoped to the owner.
// Returns a user-facing error ("not found") for privacy — does not reveal
// that another user owns the job.
func (s *Service) DeleteJobByOwner(ctx context.Context, ownerUserID, jobID string) error {
	return s.store.DeleteJobByOwnerAndID(ctx, ownerUserID, jobID)
}

// PauseJobByOwner pauses a cron job scoped to the owner.
func (s *Service) PauseJobByOwner(ctx context.Context, ownerUserID, jobID string) error {
	return s.store.PauseJobByOwnerAndID(ctx, ownerUserID, jobID)
}

// ResumeJobByOwner resumes a cron job scoped to the owner.
func (s *Service) ResumeJobByOwner(ctx context.Context, ownerUserID, jobID string) error {
	return s.store.ResumeJobByOwnerAndID(ctx, ownerUserID, jobID)
}

func (s *Service) DeleteJob(ctx context.Context, jobID string) error {
	fullID, err := s.store.ResolveJobID(ctx, jobID)
	if err != nil {
		return err
	}
	return s.store.DeleteJob(ctx, fullID)
}

// AddRecurringJob creates a cron-scheduled job for the given chat/thread.
func (s *Service) AddRecurringJob(ctx context.Context, userID string, chatID int64, threadID int, expr, prompt, cwd string) (string, error) {
	return s.CreateJob(ctx, CronJob{
		ID:             uuid.NewString(),
		OwnerUserID:    userID,
		TargetChatID:   chatID,
		TargetThreadID: threadID,
		Cwd:            cwd,
		ScheduleType:   "cron",
		CronExpr:       expr,
		Prompt:         prompt,
	})
}

// AddOnceJob creates a one-shot job scheduled at the given timestamp for the given chat/thread.
func (s *Service) AddOnceJob(ctx context.Context, userID string, chatID int64, threadID int, timestamp, prompt, cwd string) (string, error) {
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp %q: %w", timestamp, err)
	}
	return s.CreateJob(ctx, CronJob{
		ID:             uuid.NewString(),
		OwnerUserID:    userID,
		TargetChatID:   chatID,
		TargetThreadID: threadID,
		Cwd:            cwd,
		ScheduleType:   "once",
		RunAt:          &t,
		Prompt:         prompt,
	})
}
