package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/igormaneschy/aurelia/internal/cron"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

func runCronCLI(args []string) error {
	if len(args) == 0 {
		return printCronUsage()
	}

	resolver, err := runtime.New()
	if err != nil {
		return fmt.Errorf("resolve instance root: %w", err)
	}
	if err := runtime.Bootstrap(resolver); err != nil {
		return fmt.Errorf("bootstrap instance directory: %w", err)
	}

	store, err := cron.NewSQLiteCronStore(resolver.DBPath("cron.db"))
	if err != nil {
		return fmt.Errorf("open cron store: %w", err)
	}
	defer func() { _ = store.Close() }()

	svc := cron.NewService(store, nil)
	ctx := context.Background()

	switch args[0] {
	case "add":
		// aurelia cron add "<cron-expr>" "<prompt>" [--chat-id <id>] [--owner-user-id <id>]
		if len(args) < 3 {
			return printCronUsage()
		}
		expr := args[1]
		prompt := args[2]
		chatID := parseChatIDFlag(args[3:])
		ownerUserID := parseOwnerUserIDFlag(args[3:])
		jobID, err := svc.AddRecurringJob(ctx, ownerUserID, chatID, 0, expr, prompt)
		if err != nil {
			return err
		}
		fmt.Printf("Cron job created: %s\n", jobID)
		return nil

	case "once":
		// aurelia cron once "<timestamp>" "<prompt>" [--chat-id <id>] [--owner-user-id <id>]
		if len(args) < 3 {
			return printCronUsage()
		}
		timestamp := args[1]
		prompt := args[2]
		chatID := parseChatIDFlag(args[3:])
		ownerUserID := parseOwnerUserIDFlag(args[3:])
		jobID, err := svc.AddOnceJob(ctx, ownerUserID, chatID, 0, timestamp, prompt)
		if err != nil {
			return err
		}
		fmt.Printf("One-time job created: %s\n", jobID)
		return nil

	case "list":
		chatID := parseChatIDFlag(args[1:])
		ownerUserID := parseOwnerUserIDFlag(args[1:])
		var jobs []cron.CronJob
		if ownerUserID != "" {
			jobs, err = svc.ListJobsByOwner(ctx, ownerUserID)
		} else {
			jobs, err = svc.ListJobs(ctx, chatID)
		}
		if err != nil {
			return err
		}
		if len(jobs) == 0 {
			fmt.Println("No cron jobs found.")
			return nil
		}
		for _, j := range jobs {
			schedule := j.CronExpr
			if j.ScheduleType == "once" && j.RunAt != nil {
				schedule = j.RunAt.Format("2006-01-02 15:04:05")
			}
			fmt.Printf("  %s  [%s]  active=%t  schedule=%s  prompt=%s\n", shortID(j.ID), j.ScheduleType, j.Active, schedule, j.Prompt)
		}
		return nil

	case "del", "delete":
		if len(args) < 2 {
			return printCronUsage()
		}
		ownerUserID := parseOwnerUserIDFlag(args[2:])
		if ownerUserID != "" {
			return svc.DeleteJobByOwner(ctx, ownerUserID, args[1])
		}
		return svc.DeleteJob(ctx, args[1])

	case "pause":
		if len(args) < 2 {
			return printCronUsage()
		}
		ownerUserID := parseOwnerUserIDFlag(args[2:])
		if ownerUserID != "" {
			return svc.PauseJobByOwner(ctx, ownerUserID, args[1])
		}
		return svc.PauseJob(ctx, args[1])

	case "resume":
		if len(args) < 2 {
			return printCronUsage()
		}
		ownerUserID := parseOwnerUserIDFlag(args[2:])
		if ownerUserID != "" {
			return svc.ResumeJobByOwner(ctx, ownerUserID, args[1])
		}
		return svc.ResumeJob(ctx, args[1])

	default:
		return printCronUsage()
	}
}

func parseChatIDFlag(args []string) int64 {
	for i, a := range args {
		if a == "--chat-id" && i+1 < len(args) {
			v, _ := strconv.ParseInt(args[i+1], 10, 64)
			return v
		}
	}
	return 0
}

func parseOwnerUserIDFlag(args []string) string {
	for i, a := range args {
		if a == "--owner-user-id" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func printCronUsage() error {
	usage := []string{
		"Usage:",
		"  aurelia cron add \"<cron-expr>\" \"<prompt>\" [--chat-id <id>] [--owner-user-id <id>]",
		"  aurelia cron once \"<timestamp>\" \"<prompt>\" [--chat-id <id>] [--owner-user-id <id>]",
		"  aurelia cron list [--chat-id <id>] [--owner-user-id <id>]",
		"  aurelia cron del <job-id> [--owner-user-id <id>]",
		"  aurelia cron pause <job-id> [--owner-user-id <id>]",
		"  aurelia cron resume <job-id> [--owner-user-id <id>]",
	}
	fmt.Println(strings.Join(usage, "\n"))
	return nil
}
