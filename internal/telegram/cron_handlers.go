package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/cron"
)

type CronCommandService interface {
	AddRecurringJob(ctx context.Context, userID string, chatID int64, threadID int, expr, prompt, cwd, tzName string) (string, error)
	AddOnceJob(ctx context.Context, userID string, chatID int64, threadID int, timestamp, prompt, cwd string) (string, error)
	ListJobs(ctx context.Context, chatID int64) ([]cron.CronJob, error)
	ListJobsByOwner(ctx context.Context, ownerUserID string) ([]cron.CronJob, error)
	PauseJob(ctx context.Context, jobID string) error
	PauseJobByOwner(ctx context.Context, ownerUserID, jobID string) error
	ResumeJob(ctx context.Context, jobID string) error
	ResumeJobByOwner(ctx context.Context, ownerUserID, jobID string) error
	DeleteJob(ctx context.Context, jobID string) error
	DeleteJobByOwner(ctx context.Context, ownerUserID, jobID string) error
}

type CronCommandHandler struct {
	service CronCommandService
}

func NewCronCommandHandler(service CronCommandService) *CronCommandHandler {
	return &CronCommandHandler{service: service}
}

func (h *CronCommandHandler) HandleText(ctx context.Context, userID string, chatID int64, threadID int, text, tzName string) (string, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/cron") {
		return cronUsage(), nil
	}

	rest := strings.TrimSpace(strings.TrimPrefix(text, "/cron"))
	if rest == "" {
		return cronUsage(), nil
	}

	switch {
	case rest == "list":
		jobs, err := h.service.ListJobsByOwner(ctx, userID)
		if err != nil {
			return "", err
		}
		return formatCronJobs(jobs), nil
	case strings.HasPrefix(rest, "add "):
		args := splitCronArgs(strings.TrimSpace(strings.TrimPrefix(rest, "add")))
		if len(args) < 2 {
			return cronUsage(), nil
		}
		jobID, err := h.service.AddRecurringJob(ctx, userID, chatID, threadID, args[0], args[1], parseFlagValue(args[2:], "--cwd"), tzName)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Job cron criado com sucesso: `%s`", jobID), nil
	case strings.HasPrefix(rest, "once "):
		args := splitCronArgs(strings.TrimSpace(strings.TrimPrefix(rest, "once")))
		if len(args) < 2 {
			return cronUsage(), nil
		}
		jobID, err := h.service.AddOnceJob(ctx, userID, chatID, threadID, args[0], args[1], parseFlagValue(args[2:], "--cwd"))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Job pontual criado com sucesso: `%s`", jobID), nil
	case strings.HasPrefix(rest, "pause "):
		jobID := strings.TrimSpace(strings.TrimPrefix(rest, "pause"))
		if jobID == "" {
			return cronUsage(), nil
		}
		if err := h.service.PauseJobByOwner(ctx, userID, jobID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Job `%s` pausado.", jobID), nil
	case strings.HasPrefix(rest, "resume "):
		jobID := strings.TrimSpace(strings.TrimPrefix(rest, "resume"))
		if jobID == "" {
			return cronUsage(), nil
		}
		if err := h.service.ResumeJobByOwner(ctx, userID, jobID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Job `%s` retomado.", jobID), nil
	case strings.HasPrefix(rest, "del "):
		jobID := strings.TrimSpace(strings.TrimPrefix(rest, "del"))
		if jobID == "" {
			return cronUsage(), nil
		}
		if err := h.service.DeleteJobByOwner(ctx, userID, jobID); err != nil {
			return "", err
		}
		return fmt.Sprintf("Job `%s` removido.", jobID), nil
	default:
		return cronUsage(), nil
	}
}

func cronUsage() string {
	return "Uso: /cron list | /cron add \"<expr>\" \"<prompt>\" [--cwd <path>] | /cron once \"<timestamp>\" \"<prompt>\" [--cwd <path>] | /cron pause <id> | /cron resume <id> | /cron del <id>"
}

func splitCronArgs(input string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		args = append(args, current.String())
		current.Reset()
	}

	for _, r := range input {
		switch r {
		case '"':
			flush()
			inQuotes = !inQuotes
		case ' ', '\t':
			if inQuotes {
				current.WriteRune(r)
			} else {
				flush()
			}
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return args
}

func parseFlagValue(args []string, name string) string {
	for i, arg := range args {
		if arg == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func formatCronJobs(jobs []cron.CronJob) string {
	if len(jobs) == 0 {
		return "Nenhum job cron cadastrado neste chat."
	}

	var lines []string
	for _, job := range jobs {
		schedule := job.CronExpr
		if job.ScheduleType == "once" && job.RunAt != nil {
			schedule = job.RunAt.Format(time.RFC3339)
		}
		cwd := ""
		if job.Cwd != "" {
			cwd = fmt.Sprintf(" cwd=%s", job.Cwd)
		}
		lines = append(lines, fmt.Sprintf("- `%s` [%s] active=%t status=%s schedule=%s%s prompt=%s", job.ID, job.ScheduleType, job.Active, job.LastStatus, schedule, cwd, job.Prompt))
	}
	return strings.Join(lines, "\n")
}
