package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/memorycontext"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

type migrationPage struct {
	Source    string
	Workspace string
	Project   string
	Scope     string
	Path      string
	Body      string
	Kind      string
	Tier      string
	Tags      []string
	Pinned    bool
	SessionID string
}

func (p migrationPage) projectRef() string {
	if p.Scope == "profile" {
		return "profile"
	}
	if p.Workspace == "" || p.Project == "" {
		return "unmapped"
	}
	return p.Workspace + "/" + p.Project
}

type memoryMigrationPlan struct {
	Pages    []migrationPage
	Warnings []string
}

func (p memoryMigrationPlan) conflicts() int {
	seen := make(map[string]int)
	for _, page := range p.Pages {
		seen[page.projectRef()+"|"+page.Path]++
	}
	conflicts := 0
	for _, count := range seen {
		if count > 1 {
			conflicts++
		}
	}
	return conflicts
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type migrationWriter interface {
	ResolveTarget(ctx context.Context, page migrationPage) error
	ReadPage(ctx context.Context, page migrationPage) (string, bool, error)
	WritePage(ctx context.Context, page migrationPage) error
}

type aiMemoryWriter struct {
	provider *memorycontext.AIMemoryProvider
}

func (w *aiMemoryWriter) ResolveTarget(ctx context.Context, page migrationPage) error {
	if page.Scope == "profile" {
		return nil
	}
	_, err := w.provider.CallTool(ctx, "memory_resolve_write_target", map[string]any{
		"workspace": page.Workspace,
		"project":   page.Project,
	})
	if err != nil {
		return fmt.Errorf("resolve target %s: %w", page.projectRef(), err)
	}
	return nil
}

func (w *aiMemoryWriter) ReadPage(ctx context.Context, page migrationPage) (string, bool, error) {
	args := map[string]any{"path": page.Path, "max_chars": 200}
	if page.Scope == "profile" {
		args["scope"] = "profile"
	} else {
		args["workspace"] = page.Workspace
		args["project"] = page.Project
	}
	text, err := w.provider.CallTool(ctx, "memory_read_page", args)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			return "", false, nil
		}
		return "", false, err
	}
	return text, true, nil
}

func (w *aiMemoryWriter) WritePage(ctx context.Context, page migrationPage) error {
	args := map[string]any{
		"path":         page.Path,
		"body":         page.Body,
		"kind":         page.Kind,
		"tier":         page.Tier,
		"tags":         page.Tags,
		"pinned":       page.Pinned,
		"source_files": []map[string]string{{"path": page.Source, "commit": shortHash(page.Source)}},
	}
	if page.SessionID != "" && uuidPattern.MatchString(page.SessionID) {
		args["session_id"] = page.SessionID
	}
	if page.Scope == "profile" {
		args["scope"] = "profile"
	} else {
		args["workspace"] = page.Workspace
		args["project"] = page.Project
	}
	if _, err := w.provider.CallTool(ctx, "memory_write_page", args); err != nil {
		return fmt.Errorf("write %s: %w", page.Path, err)
	}
	return nil
}

func runMigrateMemory(args []string) error {
	apply := false
	dryRun := true
	checkTarget := false
	verify := false
	for _, arg := range args {
		switch arg {
		case "--apply":
			apply = true
			dryRun = false
		case "--dry-run":
			dryRun = true
		case "--check-target":
			checkTarget = true
		case "--verify":
			verify = true
		case "--help", "-h":
			fmt.Println("usage: aurelia migrate-memory [--dry-run] [--check-target] [--apply] [--verify]")
			return nil
		default:
			return fmt.Errorf("unknown migrate-memory flag %q", arg)
		}
	}
	if apply && checkTarget {
		return fmt.Errorf("--check-target is a dry-run option")
	}

	resolver, err := runtime.New()
	if err != nil {
		return fmt.Errorf("resolve instance root: %w", err)
	}
	personaID, err := loadMigrationPersonaID(resolver)
	if err != nil {
		return err
	}
	bindings, err := projectbinding.NewSQLiteStore(resolver.DBPath("project_bindings.db"))
	if err != nil {
		return fmt.Errorf("open project bindings: %w", err)
	}
	defer func() { _ = bindings.Close() }()

	plan, err := buildMemoryMigrationPlan(context.Background(), resolver, bindings, personaID)
	if err != nil {
		return err
	}
	printMemoryMigrationPlan(plan, dryRun)

	if dryRun && !checkTarget {
		printMigrationRollbackNote()
		return nil
	}

	provider := memorycontext.NewAIMemoryProvider(memorycontext.AIMemoryOptions{})
	writer := &aiMemoryWriter{provider: provider}
	if dryRun && checkTarget {
		report, err := checkMigrationTargets(context.Background(), writer, plan)
		if err != nil {
			return err
		}
		fmt.Printf("targets checked: %d; existing: %d; unknown: %d\n", report.Checked, report.Existing, report.Unknown)
		printMigrationRollbackNote()
		return nil
	}

	report, err := applyMigrationPlan(context.Background(), writer, plan, verify)
	if err != nil {
		return err
	}
	fmt.Printf("imported: %d; verified: %d; skipped: %d\n", report.Written, report.Verified, report.Skipped)
	for _, warning := range report.Warnings {
		fmt.Printf("warning: %s\n", warning)
	}
	printMigrationRollbackNote()
	return nil
}

func loadMigrationPersonaID(resolver *runtime.PathResolver) (int64, error) {
	data, err := os.ReadFile(resolver.AppConfig())
	if err != nil {
		return 0, fmt.Errorf("read app config for persona id: %w", err)
	}
	var cfg struct {
		DefaultPersonaUserID int64   `json:"default_persona_user_id"`
		DefaultOwnerUserID   int64   `json:"default_owner_user_id"`
		AllowedUserIDs       []int64 `json:"telegram_allowed_user_ids"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return 0, fmt.Errorf("decode app config for persona id: %w", err)
	}
	if cfg.DefaultPersonaUserID != 0 {
		return cfg.DefaultPersonaUserID, nil
	}
	if cfg.DefaultOwnerUserID != 0 {
		return cfg.DefaultOwnerUserID, nil
	}
	if len(cfg.AllowedUserIDs) > 0 {
		return cfg.AllowedUserIDs[0], nil
	}
	return 0, fmt.Errorf("no persona/owner user id configured")
}

func buildMemoryMigrationPlan(ctx context.Context, resolver *runtime.PathResolver, bindings projectbinding.Store, personaID int64) (memoryMigrationPlan, error) {
	plan := memoryMigrationPlan{}
	bindingList, err := bindings.ListByUser(ctx, personaID, 100)
	if err != nil {
		return plan, fmt.Errorf("list persona bindings: %w", err)
	}
	bindingBySlug := make(map[string]projectbinding.ProjectBinding, len(bindingList))
	for _, binding := range bindingList {
		bindingBySlug[binding.ProjectSlug] = binding
		bindingBySlug[runtime.ProjectSlug(binding.CWD)] = binding
	}

	scanCwdOverlays(resolver, bindingBySlug, personaID, &plan)
	scanUserGlobal(resolver, personaID, &plan)
	scanTopicMemory(ctx, resolver, bindings, personaID, &plan)
	if err := scanContinuity(ctx, resolver, bindings, personaID, &plan); err != nil {
		return plan, err
	}
	scanMemoryuxReceipts(resolver, &plan)

	sort.SliceStable(plan.Pages, func(i, j int) bool {
		if plan.Pages[i].projectRef() == plan.Pages[j].projectRef() {
			return plan.Pages[i].Path < plan.Pages[j].Path
		}
		return plan.Pages[i].projectRef() < plan.Pages[j].projectRef()
	})
	return plan, nil
}

func scanCwdOverlays(resolver *runtime.PathResolver, bindingBySlug map[string]projectbinding.ProjectBinding, personaID int64, plan *memoryMigrationPlan) {
	projectsDir := filepath.Join(resolver.Root(), "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("read projects dir: %v", err))
		}
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		slug := entry.Name()
		overlayDir := filepath.Join(projectsDir, slug, "cwd_overlay")
		if info, err := os.Stat(overlayDir); err != nil || !info.IsDir() {
			continue
		}
		binding, ok := bindingBySlug[slug]
		if !ok {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("cwd_overlay %s has no persona binding", slug))
			continue
		}
		scope, err := memorycontext.ResolveScope(binding.CWD, personaID, binding.Key.ChatID, binding.Key.ThreadID, "")
		if err != nil || !scope.HasProject() {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("cwd_overlay %s has no ai-memory project", slug))
			continue
		}
		files, err := os.ReadDir(overlayDir)
		if err != nil {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("read cwd_overlay %s: %v", slug, err))
			continue
		}
		for _, file := range files {
			if file.IsDir() || strings.ToLower(filepath.Ext(file.Name())) != ".md" {
				continue
			}
			path := filepath.Join(overlayDir, file.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("read %s: %v", file.Name(), err))
				continue
			}
			target, kind, tier, pinned, sessionID := cwdOverlayTarget(scope, file.Name())
			plan.Pages = append(plan.Pages, migrationPage{
				Source:    relativeToRoot(resolver.Root(), path),
				Workspace: scope.Workspace,
				Project:   scope.Project,
				Path:      target,
				Body:      ensureH1(titleFromFilename(file.Name()), string(data)),
				Kind:      kind,
				Tier:      tier,
				Tags:      []string{"legacy-import", "cwd-overlay"},
				Pinned:    pinned,
				SessionID: sessionID,
			})
		}
	}
}

func cwdOverlayTarget(scope memorycontext.Scope, filename string) (target, kind, tier string, pinned bool, sessionID string) {
	base := strings.ToLower(strings.TrimSuffix(filename, filepath.Ext(filename)))
	switch base {
	case "memory":
		return "imports/legacy/cwd_overlay/memory.md", "fact", "semantic", false, ""
	case "current_task", "current-task":
		return "sessions/legacy-current-task-" + shortHash(scope.ProjectRef()) + ".md", "fact", "episodic", false, ""
	}
	safe := sanitizePageSegment(base)
	if strings.Contains(base, "decision") || strings.Contains(base, "adr") {
		return "decisions/imported-" + safe + ".md", "decision", "semantic", true, ""
	}
	if strings.Contains(base, "rule") {
		return "_rules/imported-" + safe + ".md", "rule", "semantic", true, ""
	}
	return "imports/legacy/cwd_overlay/" + safe + ".md", "fact", "semantic", false, ""
}

func scanUserGlobal(resolver *runtime.PathResolver, personaID int64, plan *memoryMigrationPlan) {
	usersDir := filepath.Join(resolver.Root(), "users")
	entries, err := os.ReadDir(usersDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		userID, err := strconv.ParseInt(entry.Name(), 10, 64)
		if err != nil || userID != personaID {
			continue
		}
		dir := filepath.Join(usersDir, entry.Name(), "memory")
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, file := range files {
			if file.IsDir() || strings.ToLower(filepath.Ext(file.Name())) != ".md" {
				continue
			}
			path := filepath.Join(dir, file.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("read user-global %s: %v", file.Name(), err))
				continue
			}
			plan.Pages = append(plan.Pages, migrationPage{
				Source: relativeToRoot(resolver.Root(), path),
				Scope:  "profile",
				Path:   "imports/legacy/user-global/" + sanitizePageSegment(strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))) + ".md",
				Body:   ensureH1(titleFromFilename(file.Name()), string(data)),
				Kind:   "fact",
				Tier:   "semantic",
				Tags:   []string{"legacy-import", "user-global"},
			})
		}
	}
}

func scanTopicMemory(ctx context.Context, resolver *runtime.PathResolver, bindings projectbinding.Store, personaID int64, plan *memoryMigrationPlan) {
	topicsDir := filepath.Join(resolver.Root(), "topics")
	entries, err := os.ReadDir(topicsDir)
	if err != nil {
		return
	}
	for _, chatEntry := range entries {
		if !chatEntry.IsDir() || !strings.HasPrefix(chatEntry.Name(), "chat_") {
			continue
		}
		chatID, err := strconv.ParseInt(strings.TrimPrefix(chatEntry.Name(), "chat_"), 10, 64)
		if err != nil {
			continue
		}
		threads, err := os.ReadDir(filepath.Join(topicsDir, chatEntry.Name()))
		if err != nil {
			continue
		}
		for _, threadEntry := range threads {
			if !threadEntry.IsDir() || !strings.HasPrefix(threadEntry.Name(), "thread_") {
				continue
			}
			threadID, err := strconv.Atoi(strings.TrimPrefix(threadEntry.Name(), "thread_"))
			if err != nil {
				continue
			}
			resolved, err := bindings.Resolve(ctx, projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID})
			if err != nil || resolved == nil || resolved.Binding == nil {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("topic %d/%d has no binding", chatID, threadID))
				continue
			}
			scope, err := memorycontext.ResolveScope(resolved.Binding.CWD, personaID, chatID, threadID, "")
			if err != nil || !scope.HasProject() {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("topic %d/%d has no ai-memory project", chatID, threadID))
				continue
			}
			dir := filepath.Join(topicsDir, chatEntry.Name(), threadEntry.Name())
			files, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, file := range files {
				if file.IsDir() || strings.ToLower(filepath.Ext(file.Name())) != ".md" {
					continue
				}
				path := filepath.Join(dir, file.Name())
				data, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				plan.Pages = append(plan.Pages, migrationPage{
					Source:    relativeToRoot(resolver.Root(), path),
					Workspace: scope.Workspace,
					Project:   scope.Project,
					Path:      "imports/legacy/topic/" + strconv.FormatInt(chatID, 10) + "/" + strconv.Itoa(threadID) + "/" + sanitizePageSegment(strings.TrimSuffix(file.Name(), filepath.Ext(file.Name()))) + ".md",
					Body:      ensureH1(titleFromFilename(file.Name()), string(data)),
					Kind:      "fact",
					Tier:      "semantic",
					Tags:      []string{"legacy-import", fmt.Sprintf("chat:%d/thread:%d", chatID, threadID)},
				})
			}
		}
	}
}

func scanContinuity(ctx context.Context, resolver *runtime.PathResolver, bindings projectbinding.Store, personaID int64, plan *memoryMigrationPlan) error {
	dbPath := resolver.DBPath("continuity.db")
	if _, err := os.Stat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat continuity db: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open continuity db: %w", err)
	}
	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, `SELECT chat_id, thread_id, user_id, cwd, active_goal, last_user_intent, last_assistant_summary, last_checkpoint, last_run_id, last_run_status, last_tools, session_id, session_cold, reset_reason, updated_at FROM conversation_state`)
	if err != nil {
		return fmt.Errorf("query continuity states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			chatID, threadID, userID, sessionCold, updatedAt int64
			cwd, activeGoal, lastIntent, lastSummary         string
			lastCheckpoint, lastRunID, lastRunStatus         string
			lastTools, sessionID, resetReason                string
		)
		if err := rows.Scan(&chatID, &threadID, &userID, &cwd, &activeGoal, &lastIntent, &lastSummary, &lastCheckpoint, &lastRunID, &lastRunStatus, &lastTools, &sessionID, &sessionCold, &resetReason, &updatedAt); err != nil {
			return fmt.Errorf("scan continuity state: %w", err)
		}
		resolved, _ := bindings.Resolve(ctx, projectbinding.ConversationKey{ChatID: chatID, ThreadID: int(threadID)})
		scopeCWD := ""
		if resolved != nil && resolved.Binding != nil {
			scopeCWD = resolved.Binding.CWD
		}
		if scopeCWD == "" {
			scopeCWD = cwd
		}
		if scopeCWD != "" && resolved != nil && resolved.Binding == nil {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("continuity %d/%d has no binding; using stored cwd", chatID, threadID))
		}
		scope, scopeErr := memorycontext.ResolveScope(scopeCWD, personaID, chatID, int(threadID), "")
		if scopeErr != nil || !scope.HasProject() {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("continuity %d/%d has no ai-memory project", chatID, threadID))
			continue
		}
		body := fmt.Sprintf("# Legacy continuity %d/%d\n\nUpdated: %d\nCWD: %s\nActive goal: %s\nLast user intent: %s\nLast assistant summary: %s\nLast checkpoint: %s\nLast run: %s (%s)\nLast tools: %s\nSession: %s\nCold: %d\nReset reason: %s\n",
			chatID, threadID, updatedAt, cwd, continuity.EscapeUntrusted(activeGoal), continuity.EscapeUntrusted(lastIntent), continuity.EscapeUntrusted(lastSummary), continuity.EscapeUntrusted(lastCheckpoint), lastRunID, lastRunStatus, continuity.EscapeUntrusted(lastTools), sessionID, sessionCold, continuity.EscapeUntrusted(resetReason))
		plan.Pages = append(plan.Pages, migrationPage{
			Source:    "data/continuity.db",
			Workspace: scope.Workspace,
			Project:   scope.Project,
			Path:      fmt.Sprintf("sessions/legacy-continuity-%d-%d-u%d.md", chatID, threadID, userID),
			Body:      body,
			Kind:      "fact",
			Tier:      "episodic",
			Tags:      []string{"legacy-import", "continuity", fmt.Sprintf("chat:%d/thread:%d", chatID, threadID)},
			SessionID: sessionID,
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate continuity states: %w", err)
	}
	return scanProjectWorkState(ctx, db, bindings, personaID, plan)
}

func scanProjectWorkState(ctx context.Context, db *sql.DB, bindings projectbinding.Store, personaID int64, plan *memoryMigrationPlan) error {
	rows, err := db.QueryContext(ctx, `SELECT user_id, project_slug, cwd, active_goal, last_user_intent, last_assistant_summary, last_checkpoint, last_run_id, last_run_status, last_tools, last_entrypoint, last_chat_id, updated_at FROM project_work_state`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil
		}
		return fmt.Errorf("query project work states: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			userID, lastChatID, updatedAt int64
			slug, cwd, activeGoal         string
			lastIntent, lastSummary       string
			lastCheckpoint, lastRunID     string
			lastRunStatus, lastTools      string
			lastEntrypoint                string
		)
		if err := rows.Scan(&userID, &slug, &cwd, &activeGoal, &lastIntent, &lastSummary, &lastCheckpoint, &lastRunID, &lastRunStatus, &lastTools, &lastEntrypoint, &lastChatID, &updatedAt); err != nil {
			return fmt.Errorf("scan project work state: %w", err)
		}
		scopeCWD := cwd
		resolved, err := bindings.Resolve(ctx, projectbinding.ConversationKey{ChatID: lastChatID, ThreadID: 0})
		if err == nil && resolved != nil && resolved.Binding != nil {
			scopeCWD = resolved.Binding.CWD
		}
		scope, scopeErr := memorycontext.ResolveScope(scopeCWD, userID, lastChatID, 0, "")
		if scopeErr != nil || !scope.HasProject() {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("project work %s has no ai-memory project", slug))
			continue
		}
		body := fmt.Sprintf("# Legacy project work\n\nProject: %s\nUpdated: %d\nActive goal: %s\nLast user intent: %s\nLast assistant summary: %s\nLast checkpoint: %s\nLast run: %s (%s)\nLast tools: %s\nLast entrypoint: %s\n",
			cwd, updatedAt, continuity.EscapeUntrusted(activeGoal), continuity.EscapeUntrusted(lastIntent), continuity.EscapeUntrusted(lastSummary), continuity.EscapeUntrusted(lastCheckpoint), lastRunID, lastRunStatus, continuity.EscapeUntrusted(lastTools), continuity.EscapeUntrusted(lastEntrypoint))
		plan.Pages = append(plan.Pages, migrationPage{
			Source:    "data/continuity.db",
			Workspace: scope.Workspace,
			Project:   scope.Project,
			Path:      fmt.Sprintf("sessions/legacy-project-work-%d-%s.md", userID, shortHash(slug)),
			Body:      body,
			Kind:      "fact",
			Tier:      "episodic",
			Tags:      []string{"legacy-import", "project-work"},
		})
	}
	return rows.Err()
}

func scanMemoryuxReceipts(resolver *runtime.PathResolver, plan *memoryMigrationPlan) {
	path := filepath.Join(resolver.Memory(), "memory_receipts.jsonl")
	if _, err := os.Stat(path); err == nil {
		plan.Warnings = append(plan.Warnings, "memoryux receipts exist; import them manually as session feedback (not copied by this command)")
	}
}

func checkMigrationTargets(ctx context.Context, writer migrationWriter, plan memoryMigrationPlan) (migrationTargetReport, error) {
	report := migrationTargetReport{}
	seen := make(map[string]struct{})
	for _, page := range plan.Pages {
		key := page.projectRef() + "|" + page.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		report.Checked++
		_, exists, err := writer.ReadPage(ctx, page)
		if err != nil {
			report.Unknown++
			report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %v", page.Path, err))
			continue
		}
		if exists {
			report.Existing++
		}
	}
	return report, nil
}

type migrationTargetReport struct {
	Checked  int
	Existing int
	Unknown  int
	Warnings []string
}

type migrationApplyReport struct {
	Written  int
	Verified int
	Skipped  int
	Warnings []string
}

func applyMigrationPlan(ctx context.Context, writer migrationWriter, plan memoryMigrationPlan, verify bool) (migrationApplyReport, error) {
	report := migrationApplyReport{}
	resolved := make(map[string]struct{})
	for _, page := range plan.Pages {
		key := page.projectRef() + "|" + page.Path
		if _, ok := resolved[page.projectRef()]; !ok {
			if err := writer.ResolveTarget(ctx, page); err != nil {
				// The write itself may create an on-demand project; keep going
				// and let the write be the authority.
				report.Warnings = append(report.Warnings, err.Error())
			}
			resolved[page.projectRef()] = struct{}{}
		}
		if err := writer.WritePage(ctx, page); err != nil {
			report.Skipped++
			report.Warnings = append(report.Warnings, err.Error())
			continue
		}
		report.Written++
		if verify {
			text, exists, err := writer.ReadPage(ctx, page)
			if err != nil || !exists || strings.TrimSpace(text) == "" {
				report.Skipped++
				report.Warnings = append(report.Warnings, fmt.Sprintf("verify %s: exists=%v err=%v", key, exists, err))
				continue
			}
			report.Verified++
		}
	}
	return report, nil
}

func printMemoryMigrationPlan(plan memoryMigrationPlan, dryRun bool) {
	mode := "apply"
	if dryRun {
		mode = "dry-run"
	}
	fmt.Printf("Memory consolidation migration (%s)\n", mode)
	fmt.Printf("pages: %d; warnings: %d; duplicate targets: %d\n", len(plan.Pages), len(plan.Warnings), plan.conflicts())
	for _, page := range plan.Pages {
		fmt.Printf("%s -> %s %s kind=%s tier=%s pinned=%t tags=%s\n", page.Source, page.projectRef(), page.Path, page.Kind, page.Tier, page.Pinned, strings.Join(page.Tags, ","))
	}
	for _, warning := range plan.Warnings {
		fmt.Printf("warning: %s\n", warning)
	}
}

func printMigrationRollbackNote() {
	fmt.Println("rollback: keep the legacy originals untouched; set memory_provider=legacy (or unset AURELIA_MEMORY_PROVIDER) before removing the legacy fallback")
}

func relativeToRoot(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

func titleFromFilename(filename string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	base = strings.NewReplacer("_", " ", "-", " ").Replace(base)
	base = strings.TrimSpace(base)
	if base == "" {
		return "Imported legacy memory"
	}
	return strings.ToUpper(base[:1]) + base[1:]
}

func ensureH1(title, body string) string {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "# ") || strings.HasPrefix(trimmed, "#\t") {
		return trimmed
	}
	return "# " + title + "\n\n" + trimmed
}

func sanitizePageSegment(value string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-', r == '_', r == ' ', r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	result := strings.Trim(b.String(), "-")
	if result == "" {
		return "imported"
	}
	if len(result) > 80 {
		result = strings.TrimRight(result[:80], "-")
	}
	return result
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}
