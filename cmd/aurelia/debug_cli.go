package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

// debugCommand handles "aurelia debug <subcommand>" for operational observability.
// Subcommands: last, run, errors, metrics.
func debugCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: aurelia debug <last|run|errors|metrics|prune> [options]")
	}

	sub := args[0]
	subArgs := args[1:]

	// Resolve instance path and open runlog store.
	resolver, err := runtime.New()
	if err != nil {
		return fmt.Errorf("resolve instance root: %w", err)
	}
	store, err := runlog.NewSQLiteStore(resolver.DBPath("runlog.db"))
	if err != nil {
		return fmt.Errorf("open runlog: %w", err)
	}
	defer func() { _ = store.Close() }()

	ctx := context.Background()
	useJSON := false

	switch sub {
	case "last":
		useJSON = hasJSONFlag(subArgs)
		return debugLast(ctx, store, useJSON)
	case "run":
		if len(subArgs) == 0 || strings.HasPrefix(subArgs[0], "-") {
			return fmt.Errorf("usage: aurelia debug run <run_id> [--json]")
		}
		runID := subArgs[0]
		rest := subArgs[1:]
		useJSON = hasJSONFlag(rest)
		return debugRun(ctx, store, runID, useJSON)
	case "errors":
		limit := 10
		fs := flag.NewFlagSet("errors", flag.ContinueOnError)
		fs.IntVar(&limit, "limit", 10, "max results")
		fs.BoolVar(&useJSON, "json", false, "JSON output")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		return debugErrors(ctx, store, limit, useJSON)
	case "metrics":
		days := 1
		fs := flag.NewFlagSet("metrics", flag.ContinueOnError)
		fs.IntVar(&days, "days", 1, "lookback window in days")
		fs.BoolVar(&useJSON, "json", false, "JSON output")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		return debugMetrics(ctx, store, days, useJSON)
	case "prune":
		days := 0
		dryRun := false
		fs := flag.NewFlagSet("prune", flag.ContinueOnError)
		fs.IntVar(&days, "days", 0, "delete runs older than N days (default: from config or 30)")
		fs.BoolVar(&dryRun, "dry-run", false, "report counts without deleting")
		fs.BoolVar(&useJSON, "json", false, "JSON output")
		if err := fs.Parse(subArgs); err != nil {
			return err
		}
		return debugPrune(ctx, resolver, store, days, dryRun, useJSON)
	default:
		return fmt.Errorf("unknown debug subcommand: %s (use: last, run, errors, metrics, prune)", sub)
	}
}

func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--json" || a == "-json" {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// debug last
// ---------------------------------------------------------------------------

func debugLast(ctx context.Context, store *runlog.SQLiteStore, jsonOut bool) error {
	runs, err := store.ListRuns(ctx, 0, 1)
	if err != nil {
		return fmt.Errorf("list runs: %w", err)
	}
	if len(runs) == 0 {
		fmt.Println("Nenhuma execução encontrada.")
		return nil
	}
	r := runs[0]

	events, _ := store.ListEvents(ctx, r.RunID)

	if jsonOut {
		return printJSON(struct {
			Run    runlog.RunRecord  `json:"run"`
			Events []runlog.RunEvent `json:"events"`
		}{Run: r, Events: events})
	}

	printRunSummary(&r, events)
	return nil
}

// ---------------------------------------------------------------------------
// debug run <id>
// ---------------------------------------------------------------------------

func debugRun(ctx context.Context, store *runlog.SQLiteStore, runID string, jsonOut bool) error {
	r, err := store.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("get run: %w", err)
	}
	if r == nil {
		fmt.Printf("Execução %q não encontrada.\n", runID)
		return nil
	}

	events, _ := store.ListEvents(ctx, runID)

	if jsonOut {
		return printJSON(struct {
			Run    runlog.RunRecord  `json:"run"`
			Events []runlog.RunEvent `json:"events"`
		}{Run: *r, Events: events})
	}

	printRunDetail(r, events)
	return nil
}

// ---------------------------------------------------------------------------
// debug errors
// ---------------------------------------------------------------------------

func debugErrors(ctx context.Context, store *runlog.SQLiteStore, limit int, jsonOut bool) error {
	runs, err := store.ListRuns(ctx, 0, limit)
	if err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	// Filter to only failed/timed-out runs.
	var failed []runlog.RunRecord
	for _, r := range runs {
		if r.Status == runlog.RunFailed || r.Status == runlog.RunTimedOut || r.Status == runlog.RunCanceled {
			failed = append(failed, r)
		}
	}

	if len(failed) == 0 {
		fmt.Println("Nenhuma execução com erro encontrada.")
		return nil
	}

	if jsonOut {
		return printJSON(failed)
	}

	fmt.Printf("❌ Últimas %d execuções com erro:\n\n", len(failed))
	for _, r := range failed {
		dur := time.Duration(r.DurationMs) * time.Millisecond
		errMsg := r.Error
		if r.TimeoutOrigin != "" {
			errMsg = r.TimeoutOrigin
		}
		if errMsg == "" {
			errMsg = "(sem detalhe)"
		}
		fmt.Printf("  run=%s  status=%s  dur=%s  chat=%d  error=%s\n",
			shortID(r.RunID), r.Status, dur.Round(time.Second), r.ChatID, errMsg)
	}
	return nil
}

// ---------------------------------------------------------------------------
// debug metrics
// ---------------------------------------------------------------------------

func debugPrune(ctx context.Context, resolver *runtime.PathResolver, store *runlog.SQLiteStore, days int, dryRun, jsonOut bool) error {
	if days <= 0 {
		cfg, err := config.Load(resolver)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		days = cfg.RunlogRetentionDays()
	}
	if days <= 0 {
		return fmt.Errorf("runlog pruning is disabled (observability.retention_days=0); pass --days N to override")
	}

	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	result, err := store.Prune(ctx, runlog.PruneOptions{
		OlderThan: cutoff,
		DryRun:    dryRun,
	})
	if err != nil {
		return fmt.Errorf("prune: %w", err)
	}

	if jsonOut {
		return printJSON(struct {
			Days          int                `json:"days"`
			Cutoff        time.Time          `json:"cutoff"`
			DryRun        bool               `json:"dry_run"`
			RunsDeleted   int64              `json:"runs_deleted"`
			EventsDeleted int64              `json:"events_deleted"`
		}{
			Days:          days,
			Cutoff:        cutoff,
			DryRun:        dryRun,
			RunsDeleted:   result.RunsDeleted,
			EventsDeleted: result.EventsDeleted,
		})
	}

	action := "Removidos"
	if dryRun {
		action = "Seriam removidos"
	}
	fmt.Printf("🧹 Runlog prune (%d dias, cutoff=%s)\n", days, cutoff.Format(time.RFC3339))
	fmt.Printf("  %s: %d runs, %d events\n", action, result.RunsDeleted, result.EventsDeleted)
	return nil
}

func debugMetrics(ctx context.Context, store *runlog.SQLiteStore, days int, jsonOut bool) error {
	now := time.Now()
	since := now.Add(-time.Duration(days) * 24 * time.Hour)

	metrics, err := store.Metrics(ctx, runlog.MetricsFilter{
		Since: since,
		Until: now,
	})
	if err != nil {
		return fmt.Errorf("metrics: %w", err)
	}

	if jsonOut {
		return printJSON(metrics)
	}

	printMetricsTable(metrics, days)
	return nil
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

func printRunSummary(r *runlog.RunRecord, events []runlog.RunEvent) {
	fmt.Printf("🔎 Última execução\n")
	fmt.Printf("run: %s · status: %s", shortID(r.RunID), r.Status)

	if r.DurationMs > 0 {
		dur := time.Duration(r.DurationMs) * time.Millisecond
		fmt.Printf(" · %s", dur.Round(time.Second))
	}
	fmt.Println()

	if r.UserID > 0 || r.ChatID > 0 {
		fmt.Printf("user: %d · chat: %d · thread: %d\n", r.UserID, r.ChatID, r.ThreadID)
	}
	if r.Provider != "" || r.Model != "" {
		fmt.Printf("model: %s/%s\n", r.Provider, r.Model)
	}
	if r.CWD != "" {
		fmt.Printf("cwd: %s\n", r.CWD)
	}
	if r.CostUSD > 0 || r.InputTokens > 0 {
		fmt.Printf("cost: $%.4f · tokens: %d in / %d out", r.CostUSD, r.InputTokens, r.OutputTokens)
		if r.UsedFallback {
			fmt.Printf(" ⚠️ fallback")
		}
		fmt.Println()
	}
	if r.ErrorClass != "" {
		fmt.Printf("error_class: %s\n", r.ErrorClass)
	}
	if r.TimeoutOrigin != "" {
		fmt.Printf("timeout: %s\n", r.TimeoutOrigin)
	}
	if r.EntryPoint != "" {
		fmt.Printf("entrypoint: %s\n", r.EntryPoint)
	}
	if r.AgentName != "" {
		fmt.Printf("agent: %s\n", r.AgentName)
	}

	if len(events) > 0 {
		fmt.Println("\nTimeline:")
		for _, ev := range events {
			t := time.Unix(ev.Timestamp, 0)
			var marker string
			switch ev.Level {
			case "error":
				marker = "❌"
			case "warn":
				marker = "⚠️"
			default:
				marker = "  "
			}
			fmt.Printf("  %s %s %s", t.Format("15:04:05"), marker, ev.Phase)
			if ev.Message != "" {
				fmt.Printf(" %s", ev.Message)
			}
			fmt.Println()
		}
	}
}

func printRunDetail(r *runlog.RunRecord, events []runlog.RunEvent) {
	fmt.Printf("📋 Execução %s\n\n", r.RunID)
	fmt.Printf("  Status:       %s\n", r.Status)
	fmt.Printf("  Chat/Thread:  %d / %d\n", r.ChatID, r.ThreadID)
	fmt.Printf("  User:         %d\n", r.UserID)
	if r.EntryPoint != "" {
		fmt.Printf("  Entrypoint:   %s\n", r.EntryPoint)
	}
	if r.AgentName != "" {
		fmt.Printf("  Agent:        %s\n", r.AgentName)
	}
	fmt.Printf("  Provider:     %s\n", r.Provider)
	fmt.Printf("  Model:        %s\n", r.Model)
	if r.CapabilityProfile != "" {
		fmt.Printf("  Profile:      %s\n", r.CapabilityProfile)
	}
	fmt.Printf("  CWD:          %s\n", r.CWD)
	if r.SessionFile != "" {
		fmt.Printf("  Session:      %s\n", r.SessionFile)
	}
	if r.DurationMs > 0 {
		dur := time.Duration(r.DurationMs) * time.Millisecond
		fmt.Printf("  Duração:      %s\n", dur.Round(time.Millisecond))
	}
	if r.InputTokens > 0 || r.OutputTokens > 0 {
		fmt.Printf("  Tokens:       %d in / %d out\n", r.InputTokens, r.OutputTokens)
	}
	if r.CostUSD > 0 {
		fmt.Printf("  Custo:        $%.4f\n", r.CostUSD)
	}
	if r.Error != "" {
		fmt.Printf("  Erro:         %s\n", r.Error)
	}
	if r.ErrorClass != "" {
		fmt.Printf("  Classe erro:  %s\n", r.ErrorClass)
	}
	if r.TimeoutOrigin != "" {
		fmt.Printf("  Timeout:      %s\n", r.TimeoutOrigin)
	}
	if r.UsedFallback {
		fmt.Printf("  Fallback:     sim\n")
	}
	if r.ToolCount > 0 {
		fmt.Printf("  Tools:        %d\n", r.ToolCount)
	}
	if r.ParentRunID != "" {
		fmt.Printf("  Parent:       %s\n", r.ParentRunID)
	}
	if r.FirstFeedbackMs > 0 || r.MaxSilenceMs > 0 || r.StallCount > 0 || r.SteerCount > 0 {
		fmt.Printf("  First feedback: %s\n", time.Duration(r.FirstFeedbackMs)*time.Millisecond)
		fmt.Printf("  Max silêncio:   %s\n", time.Duration(r.MaxSilenceMs)*time.Millisecond)
		fmt.Printf("  Stalls:         %d\n", r.StallCount)
		fmt.Printf("  Steers:         %d\n", r.SteerCount)
	}

	if len(events) > 0 {
		fmt.Println("\nTimeline:")
		for _, ev := range events {
			t := time.Unix(ev.Timestamp, 0)
			var marker string
			switch ev.Level {
			case "error":
				marker = "❌"
			case "warn":
				marker = "⚠️"
			default:
				marker = "  "
			}
			fmt.Printf("  %s %s %s", t.Format("15:04:05"), marker, ev.Phase)
			if ev.Message != "" {
				fmt.Printf(" %s", ev.Message)
			}
			fmt.Println()
		}
	}
}

func printMetricsTable(m *runlog.MetricsResult, days int) {
	window := fmt.Sprintf("últimas %d horas", days*24)
	if days == 1 {
		window = "últimas 24 horas"
	}

	fmt.Printf("📊 Métricas (%s)\n\n", window)
	fmt.Printf("  Execuções:     %d\n", m.RunsTotal)
	fmt.Printf("  ✅ Completas:  %d (%.1f%%)\n", m.RunsCompleted, m.SuccessRate)
	fmt.Printf("  ❌ Falhas:     %d\n", m.RunsFailed)
	fmt.Printf("  ⏰ Timeout:    %d\n", m.RunsTimedOut)
	fmt.Printf("  🛑 Canceladas: %d\n", m.RunsCanceled)
	fmt.Printf("  ⏳ Em andamento: %d\n", m.RunsRunning)
	if m.FallbackCount > 0 {
		fmt.Printf("  ⚠️  Fallbacks:  %d\n", m.FallbackCount)
	}

	if m.TokensInputTotal > 0 || m.TokensOutputTotal > 0 {
		fmt.Printf("\n  Tokens in:    %d\n", m.TokensInputTotal)
		fmt.Printf("  Tokens out:   %d\n", m.TokensOutputTotal)
	}
	if m.CostUSDTotal > 0 {
		fmt.Printf("  Custo total:  $%.4f\n", m.CostUSDTotal)
	}
	if m.DurationP50Ms > 0 {
		fmt.Printf("\n  Duração p50:  %s\n", time.Duration(m.DurationP50Ms)*time.Millisecond)
		fmt.Printf("  Duração p95:  %s\n", time.Duration(m.DurationP95Ms)*time.Millisecond)
	}

	// Long-session aggregates.
	if m.StallsTotal > 0 || m.SteersTotal > 0 || m.AvgFirstFeedbackMs > 0 || m.AvgMaxSilenceMs > 0 {
		fmt.Printf("\n  Stalls:       %d\n", m.StallsTotal)
		fmt.Printf("  Steers:       %d\n", m.SteersTotal)
		if m.AvgFirstFeedbackMs > 0 {
			fmt.Printf("  First feedback (méd): %s\n", time.Duration(m.AvgFirstFeedbackMs)*time.Millisecond)
		}
		if m.AvgMaxSilenceMs > 0 {
			fmt.Printf("  Max silêncio (méd):   %s\n", time.Duration(m.AvgMaxSilenceMs)*time.Millisecond)
		}
	}

	// Breakdowns
	printBreakdown("Provider", m.ProviderBreakdown)
	printBreakdown("Modelo", m.ModelBreakdown)
	printBreakdown("Entrypoint", m.EntrypointBreakdown)
}

func printBreakdown(label string, items []runlog.BreakdownItem) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("\n  Por %s:\n", label)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, item := range items {
		_, _ = fmt.Fprintf(w, "    %s\t%d (%.1f%%)\n", item.Key, item.Count, item.Pct)
	}
	_ = w.Flush()
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("json marshal: %w", err)
	}
	fmt.Println(string(data))
	return nil
}
