package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

type fakeMigrationWriter struct {
	writes    []migrationPage
	existing  map[string]string
	failWrite bool
}

func (f *fakeMigrationWriter) ResolveTarget(context.Context, migrationPage) error {
	return nil
}

func (f *fakeMigrationWriter) ReadPage(_ context.Context, page migrationPage) (string, bool, error) {
	if page.Scope == "profile" {
		value, ok := f.existing["profile|"+page.Path]
		return value, ok, nil
	}
	value, ok := f.existing[page.projectRef()+"|"+page.Path]
	return value, ok, nil
}

func (f *fakeMigrationWriter) WritePage(_ context.Context, page migrationPage) error {
	if f.failWrite {
		return os.ErrInvalid
	}
	f.writes = append(f.writes, page)
	if f.existing == nil {
		f.existing = make(map[string]string)
	}
	key := page.projectRef() + "|" + page.Path
	if page.Scope == "profile" {
		key = "profile|" + page.Path
	}
	f.existing[key] = page.Body
	return nil
}

func TestBuildMemoryMigrationPlanFromCwdOverlay(t *testing.T) {
	resolver, bindings, cwd := migrationTestEnv(t)
	overlay := filepath.Join(resolver.Root(), "projects", runtime.ProjectSlug(cwd), "cwd_overlay")
	if err := os.MkdirAll(overlay, 0700); err != nil {
		t.Fatal(err)
	}
	writeMigrationFile(t, filepath.Join(overlay, "MEMORY.md"), "Project index fact")
	writeMigrationFile(t, filepath.Join(overlay, "current_task.md"), "Current task fact")
	writeMigrationFile(t, filepath.Join(overlay, "decision-auth.md"), "Use the adapter")

	plan, err := buildMemoryMigrationPlan(context.Background(), resolver, bindings, 42)
	if err != nil {
		t.Fatalf("buildMemoryMigrationPlan: %v", err)
	}
	var targets []string
	for _, page := range plan.Pages {
		targets = append(targets, page.Path)
		if page.Project != "aurelia" || page.Workspace != "personal" {
			t.Fatalf("page scope = %s/%s, want personal/aurelia", page.Workspace, page.Project)
		}
	}
	for _, want := range []string{
		"imports/legacy/cwd_overlay/memory.md",
		"sessions/legacy-current-task-",
		"decisions/imported-decision-auth.md",
	} {
		if !hasPrefixIn(targets, want) {
			t.Fatalf("targets = %v, missing %s", targets, want)
		}
	}
	for _, page := range plan.Pages {
		if page.Path == "decisions/imported-decision-auth.md" && !page.Pinned {
			t.Fatal("decision page must be pinned")
		}
	}
}

func TestApplyMigrationPlanWritesAndVerifies(t *testing.T) {
	plan := memoryMigrationPlan{Pages: []migrationPage{
		{Source: "projects/x/cwd_overlay/MEMORY.md", Workspace: "personal", Project: "aurelia", Path: "imports/legacy/cwd_overlay/memory.md", Body: "# Memory\n\nfact", Kind: "fact", Tier: "semantic", Tags: []string{"legacy-import"}},
		{Source: "projects/x/cwd_overlay/decision.md", Workspace: "personal", Project: "aurelia", Path: "decisions/imported-decision.md", Body: "# Decision\n\nuse it", Kind: "decision", Tier: "semantic", Pinned: true},
	}}
	writer := &fakeMigrationWriter{}
	report, err := applyMigrationPlan(context.Background(), writer, plan, true)
	if err != nil {
		t.Fatalf("applyMigrationPlan: %v", err)
	}
	if report.Written != 2 || report.Verified != 2 || len(writer.writes) != 2 {
		t.Fatalf("report = %+v writes=%d", report, len(writer.writes))
	}
}

func TestCheckMigrationTargetsReportsExistingPage(t *testing.T) {
	plan := memoryMigrationPlan{Pages: []migrationPage{
		{Source: "a", Workspace: "personal", Project: "aurelia", Path: "imports/a.md"},
		{Source: "b", Workspace: "personal", Project: "aurelia", Path: "imports/b.md"},
	}}
	writer := &fakeMigrationWriter{existing: map[string]string{"personal/aurelia|imports/a.md": "already here"}}
	report, err := checkMigrationTargets(context.Background(), writer, plan)
	if err != nil {
		t.Fatalf("checkMigrationTargets: %v", err)
	}
	if report.Checked != 2 || report.Existing != 1 || report.Unknown != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestBuildMemoryMigrationPlanFromContinuity(t *testing.T) {
	resolver, bindings, cwd := migrationTestEnv(t)
	dbPath := resolver.DBPath("continuity.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE conversation_state (
		chat_id INTEGER NOT NULL,
		thread_id INTEGER NOT NULL,
		user_id INTEGER NOT NULL DEFAULT 0,
		cwd TEXT DEFAULT '',
		active_goal TEXT DEFAULT '',
		last_user_intent TEXT DEFAULT '',
		last_assistant_summary TEXT DEFAULT '',
		last_checkpoint TEXT DEFAULT '',
		last_run_id TEXT DEFAULT '',
		last_run_status TEXT DEFAULT '',
		last_tools TEXT DEFAULT '',
		session_id TEXT DEFAULT '',
		session_cold INTEGER NOT NULL DEFAULT 0,
		reset_reason TEXT DEFAULT '',
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (chat_id, thread_id, user_id)
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO conversation_state
		(chat_id, thread_id, user_id, cwd, active_goal, last_user_intent,
		 last_assistant_summary, last_checkpoint, session_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		42, 0, 42, cwd, "finish migration", "continue", "work in progress", "phase 1", "session-42", time.Unix(100, 0).Unix()); err != nil {
		t.Fatal(err)
	}

	plan, err := buildMemoryMigrationPlan(context.Background(), resolver, bindings, 42)
	if err != nil {
		t.Fatalf("buildMemoryMigrationPlan: %v", err)
	}
	var found bool
	for _, page := range plan.Pages {
		if page.Path == "sessions/legacy-continuity-42-0-u42.md" {
			found = true
			if page.Tier != "episodic" || page.SessionID != "session-42" {
				t.Fatalf("continuity page = %+v", page)
			}
		}
	}
	if !found {
		t.Fatalf("continuity page missing: %+v", plan.Pages)
	}
}

func TestBuildMemoryMigrationPlanUserGlobalProfile(t *testing.T) {
	resolver, bindings, _ := migrationTestEnv(t)
	dir := filepath.Join(resolver.Root(), "users", "42", "memory")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	writeMigrationFile(t, filepath.Join(dir, "USER.md"), "User preference")
	plan, err := buildMemoryMigrationPlan(context.Background(), resolver, bindings, 42)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, page := range plan.Pages {
		if page.Scope == "profile" && page.Path == "imports/legacy/user-global/user.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("profile page missing: %+v", plan.Pages)
	}
}

func migrationTestEnv(t *testing.T) (*runtime.PathResolver, *projectbinding.SQLiteStore, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("AURELIA_HOME", root)
	resolver, err := runtime.New()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(resolver.Data(), 0700); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "workspace", "aurelia")
	if err := os.MkdirAll(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cwd, ".ai-memory.toml"), []byte("workspace = \"personal\"\nproject = \"aurelia\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bindings, err := projectbinding.NewSQLiteStore(resolver.DBPath("project_bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bindings.Close() })
	now := time.Now()
	if err := bindings.Set(context.Background(), projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 42, ThreadID: 0},
		CWD:         cwd,
		ProjectSlug: runtime.ProjectSlug(cwd),
		Source:      projectbinding.BindingManual,
		CreatedBy:   42,
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}
	return resolver, bindings, cwd
}

func writeMigrationFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func hasPrefixIn(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
