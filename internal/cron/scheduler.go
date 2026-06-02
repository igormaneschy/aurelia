package cron

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	robfigcron "github.com/robfig/cron/v3"
)

// Scheduler polls for due cron jobs and executes them.
type Scheduler struct {
	store   Store
	runtime Runtime
	clock   Clock
	config  SchedulerConfig
	running sync.Map // jobID → struct{} to prevent concurrent execution
}

// NewScheduler creates a cron scheduler.
func NewScheduler(store Store, runtime Runtime, clock Clock, config SchedulerConfig) (*Scheduler, error) {
	if store == nil {
		return nil, fmt.Errorf("cron store is required")
	}
	if runtime == nil {
		return nil, fmt.Errorf("cron runtime is required")
	}
	if clock == nil {
		clock = realClock{}
	}
	if config.PollInterval <= 0 {
		config.PollInterval = time.Minute
	}
	return &Scheduler{
		store:   store,
		runtime: runtime,
		clock:   clock,
		config:  config,
	}, nil
}

// RunDueJobs executes all jobs that are due.
func (s *Scheduler) RunDueJobs(ctx context.Context) (int, error) {
	now := s.clock.Now().UTC()
	jobs, err := s.store.ListDueJobs(ctx, now, 50)
	if err != nil {
		return 0, err
	}

	processed := 0
	for _, job := range jobs {
		if err := s.runSingleJob(ctx, now, job); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

// Start begins the scheduler polling loop.
func (s *Scheduler) Start(ctx context.Context) error {
	ticker := time.NewTicker(s.config.PollInterval)
	defer ticker.Stop()

	for {
		if _, err := s.RunDueJobs(ctx); err != nil {
			slog.Error("cron.scheduler: RunDueJobs failed, continuing", "err", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) runSingleJob(ctx context.Context, now time.Time, job CronJob) error {
	if _, loaded := s.running.LoadOrStore(job.ID, struct{}{}); loaded {
		return nil // already running
	}
	defer s.running.Delete(job.ID)

	startedAt := now
	slog.Info("cron.scheduler: executing job", "job_id", job.ID)

	jobCtx, jobCancel := context.WithTimeout(ctx, 30*time.Minute)
	defer jobCancel()
	result, runErr := s.runtime.ExecuteJob(jobCtx, job)
	finishedAt := s.clock.Now().UTC()

	exec := CronExecution{
		ID:         uuid.NewString(),
		JobID:      job.ID,
		StartedAt:  startedAt,
		FinishedAt: &finishedAt,
	}

	if result != nil {
		exec.OutputSummary = result.Output
		exec.SessionID = result.SessionID
		exec.CostUSD = result.CostUSD
		exec.TokensUsed = result.NumTurns
	}

	if runErr != nil {
		exec.Status = "failed"
		exec.ErrorMessage = runErr.Error()
		job.LastStatus = "failed"
		job.LastError = runErr.Error()
	} else {
		exec.Status = "success"
		job.LastStatus = "success"
		job.LastError = ""
	}

	job.LastRunAt = &finishedAt

	if job.ScheduleType == "once" {
		job.Active = false
		job.NextRunAt = nil
	} else if strings.EqualFold(job.ScheduleType, "cron") {
		nextRunAt, err := computeNextRunInLocation(job.CronExpr, finishedAt, job.Timezone)
		if err != nil {
			return err
		}
		job.NextRunAt = &nextRunAt
	} else {
		slog.Warn("cron.scheduler: unknown schedule type, deactivating", "type", job.ScheduleType, "job_id", job.ID)
		job.Active = false
		job.NextRunAt = nil
	}

	if err := s.store.WithTx(ctx, func(tx *sql.Tx) error {
		if err := s.store.RecordExecutionTx(ctx, tx, exec); err != nil {
			return err
		}
		return s.store.UpdateJobTx(ctx, tx, job)
	}); err != nil {
		return err
	}
	return nil
}

// computeNextRun calculates the next run time for a standard cron expression.
// Uses UTC for backward compatibility.
func computeNextRun(expr string, after time.Time) (time.Time, error) {
	parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
	sched, err := parser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	return sched.Next(after), nil
}

// computeNextRunInLocation calculates the next run time for a cron expression
// interpreted in the given timezone. Empty tzName means UTC.
func computeNextRunInLocation(expr string, after time.Time, tzName string) (time.Time, error) {
	tzName = strings.TrimSpace(tzName)
	loc := time.UTC
	if tzName != "" {
		var err error
		loc, err = time.LoadLocation(tzName)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid timezone %q for cron expr %q: %w", tzName, expr, err)
		}
	}
	parser := robfigcron.NewParser(robfigcron.Minute | robfigcron.Hour | robfigcron.Dom | robfigcron.Month | robfigcron.Dow)
	sched, err := parser.Parse(expr)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression %q: %w", expr, err)
	}
	// Interpret `after` in the job's timezone so the cron parser
	// evaluates the expression relative to user-local wall-clock time.
	// Result is normalized to UTC so ListDueJobs comparisons with now.UTC() are correct.
	return sched.Next(after.In(loc)).UTC(), nil
}
