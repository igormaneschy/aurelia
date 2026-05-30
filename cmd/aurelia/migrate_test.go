package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/cron"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/users"
)

// ─── Helpers ────────────────────────────────────────────────────────────────

func newTestResolver(t *testing.T) *runtime.PathResolver {
	t.Helper()
	t.Setenv("AURELIA_HOME", t.TempDir())
	resolver, err := runtime.New()
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}
	if err := runtime.Bootstrap(resolver); err != nil {
		t.Fatalf("runtime.Bootstrap: %v", err)
	}
	return resolver
}

func writeTestConfig(t *testing.T, resolver *runtime.PathResolver, userIDs []int64) {
	t.Helper()
	raw := map[string]any{
		"telegram_allowed_user_ids": userIDs,
		"default_provider":          "kimi",
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(resolver.AppConfig(), data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func touchFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatalf("touch %s: %v", path, err)
	}
}

// ─── Tests ──────────────────────────────────────────────────────────────────

func TestMigration_DryRunListsAllOps(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{9876})

	root := resolver.Root()

	// Create memory files.
	touchFile(t, filepath.Join(root, "memory", "greeting.md"))
	touchFile(t, filepath.Join(root, "memory", "notes", "todo.md"))
	touchFile(t, filepath.Join(root, "memory", "personas", "USER.md"))
	touchFile(t, filepath.Join(root, "memory", "topics", "golang.md"))
	// Reserved files that should NOT appear in the plan.
	touchFile(t, filepath.Join(root, "memory", "MEMORY.md"))
	touchFile(t, filepath.Join(root, "memory", "OWNER_PLAYBOOK.md"))
	touchFile(t, filepath.Join(root, "memory", "personas", "IDENTITY.md"))
	touchFile(t, filepath.Join(root, "memory", "personas", "SOUL.md"))

	// Create a project dir.
	touchFile(t, filepath.Join(root, "projects", "myapp", "notes.md"))

	plan, err := buildMigrationPlan(root, 9876)
	if err != nil {
		t.Fatalf("buildMigrationPlan: %v", err)
	}

	// Should have: 2 memory files (greeting.md, notes/todo.md),
	// 1 USER.md, 1 topics dir, 1 project dir = 5 ops.
	if len(plan.fileOps) != 5 {
		t.Fatalf("expected 5 ops, got %d", len(plan.fileOps))
	}

	// Check specific operations.
	ops := opMap(plan.fileOps)
	for _, expected := range []string{
		"memory/greeting.md",
		"memory/notes/todo.md",
		"personas/USER.md",
	} {
		dst := filepath.Join(root, "users", "9876", expected)
		if _, ok := ops[dst]; !ok {
			t.Errorf("expected op for %s", dst)
		}
	}

	// Check that reserved files are NOT in the plan.
	for _, reserved := range []string{
		"memory/MEMORY.md",
		"memory/OWNER_PLAYBOOK.md",
		"memory/personas/IDENTITY.md",
		"memory/personas/SOUL.md",
	} {
		src := filepath.Join(root, reserved)
		if _, ok := ops[src]; ok {
			t.Errorf("reserved file %s should not be in plan", reserved)
		}
	}
}

func TestMigration_AfterMigrationProfileExists(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{12345})

	// Create a memory file so migration has something to do.
	touchFile(t, filepath.Join(resolver.Root(), "memory", "test.md"))

	userID := int64(12345)
	if err := runMigration(resolver, userID, false, false, false); err != nil {
		t.Fatalf("runMigration: %v", err)
	}

	profile, err := users.Load(filepath.Join(resolver.Root(), "users", "12345", "profile.json"))
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if profile == nil {
		t.Fatal("profile not found")
	}
	if !profile.IsOwner {
		t.Fatal("expected IsOwner=true")
	}
	if profile.UserID != 12345 {
		t.Fatalf("expected UserID=12345, got %d", profile.UserID)
	}
	if profile.Language != "pt" {
		t.Fatalf("expected Language=pt, got %q", profile.Language)
	}
	if profile.OnboardedAt.IsZero() {
		t.Fatal("expected OnboardedAt to be set")
	}
}

func TestMigration_IdempotentWithMarker(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{12345})

	touchFile(t, filepath.Join(resolver.Root(), "memory", "test.md"))

	// First migration should succeed.
	if err := runMigration(resolver, 12345, false, false, false); err != nil {
		t.Fatalf("first runMigration: %v", err)
	}

	// Second migration without --force should fail with "already migrated".
	err := runMigration(resolver, 12345, false, false, false)
	if err == nil {
		t.Fatal("expected error on second migration")
	}
	if strings.Contains(err.Error(), "already migrated") {
		t.Logf("Got expected error: %v", err)
	} else {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMigration_ConflictsAbortCleanly(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{12345})

	root := resolver.Root()
	touchFile(t, filepath.Join(root, "memory", "conflict.md"))

	// Pre-create the destination to trigger conflict.
	userRoot := filepath.Join(root, "users", "12345")
	touchFile(t, filepath.Join(userRoot, "memory", "conflict.md"))

	err := runMigration(resolver, 12345, false, false, false)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if strings.Contains(err.Error(), "conflict") || strings.Contains(err.Error(), "already exists") {
		t.Logf("Got expected conflict error: %v", err)
	} else {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMigration_CronOwnerIDPopulated(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{12345})

	touchFile(t, filepath.Join(resolver.Root(), "memory", "test.md"))

	// Create cron jobs with empty owner.
	dbPath := resolver.DBPath("cron.db")
	store, err := cron.NewSQLiteCronStore(dbPath)
	if err != nil {
		t.Fatalf("create cron store: %v", err)
	}
	ctx := context.Background()
	for i, owner := range []string{"", "", "0"} {
		if err := store.CreateJob(ctx, cron.CronJob{
			ID:           fmt.Sprintf("job-%d", i),
			OwnerUserID:  owner,
			TargetChatID: 0,
			ScheduleType: "once",
			Prompt:       "test",
			Active:       true,
		}); err != nil {
			t.Fatalf("create job %d: %v", i, err)
		}
	}
	// Create one job that should NOT be updated.
	if err := store.CreateJob(ctx, cron.CronJob{
		ID:           "job-existing",
		OwnerUserID:  "9999",
		TargetChatID: 0,
		ScheduleType: "once",
		Prompt:       "existing",
		Active:       true,
	}); err != nil {
		t.Fatalf("create existing job: %v", err)
	}
	store.Close()

	if err := runMigration(resolver, 12345, false, false, false); err != nil {
		t.Fatalf("runMigration: %v", err)
	}

	// Re-open and verify.
	store2, err := cron.NewSQLiteCronStore(dbPath)
	if err != nil {
		t.Fatalf("re-open cron store: %v", err)
	}
	defer store2.Close()

	for i := 0; i < 3; i++ {
		job, err := store2.GetJob(ctx, fmt.Sprintf("job-%d", i))
		if err != nil {
			t.Fatalf("get job-%d: %v", i, err)
		}
		if job == nil {
			t.Fatalf("job-%d not found", i)
		}
		if job.OwnerUserID != "12345" {
			t.Fatalf("job-%d: expected OwnerUserID=12345, got %q", i, job.OwnerUserID)
		}
	}

	// Existing job should be unchanged.
	existing, err := store2.GetJob(ctx, "job-existing")
	if err != nil {
		t.Fatalf("get job-existing: %v", err)
	}
	if existing.OwnerUserID != "9999" {
		t.Fatalf("job-existing: expected OwnerUserID=9999, got %q", existing.OwnerUserID)
	}
}

func TestMigration_EmptyWhitelistAborts(t *testing.T) {
	resolver := newTestResolver(t)
	// Write config WITHOUT telegram_allowed_user_ids.
	raw := map[string]any{
		"default_provider": "kimi",
	}
	data, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(resolver.AppConfig(), data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	err := runMigration(resolver, 12345, false, false, false)
	if err == nil {
		t.Fatal("expected error for empty whitelist")
	}
	if strings.Contains(err.Error(), "TelegramAllowedUserIDs") {
		t.Logf("Got expected error: %v", err)
	} else {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestMigration_TopicsMovedToGlobal(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{12345})

	root := resolver.Root()
	topicDir := filepath.Join(root, "memory", "topics")
	touchFile(t, filepath.Join(topicDir, "golang.md"))
	touchFile(t, filepath.Join(topicDir, "rust.md"))

	if err := runMigration(resolver, 12345, false, false, false); err != nil {
		t.Fatalf("runMigration: %v", err)
	}

	// topics/ should exist at root.
	globalTopics := filepath.Join(root, "topics")
	if _, err := os.Stat(globalTopics); os.IsNotExist(err) {
		t.Fatal("global topics/ not found")
	}
	if _, err := os.Stat(filepath.Join(globalTopics, "golang.md")); os.IsNotExist(err) {
		t.Fatal("golang.md not in global topics/")
	}
	if _, err := os.Stat(filepath.Join(globalTopics, "rust.md")); os.IsNotExist(err) {
		t.Fatal("rust.md not in global topics/")
	}

	// memory/topics/ should be gone.
	if _, err := os.Stat(topicDir); !os.IsNotExist(err) {
		t.Fatal("memory/topics/ should have been removed")
	}
}

func TestMigration_DefaultOwnerIDPersistedInAppConfig(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{12345})

	touchFile(t, filepath.Join(resolver.Root(), "memory", "test.md"))

	if err := runMigration(resolver, 12345, false, false, false); err != nil {
		t.Fatalf("runMigration: %v", err)
	}

	// Reload config and verify.
	cfg, err := config.Load(resolver)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if cfg.DefaultOwnerUserID != 12345 {
		t.Fatalf("expected DefaultOwnerUserID=12345, got %d", cfg.DefaultOwnerUserID)
	}
}

func TestMigration_RecoversFromInterruptedLock(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{12345})

	root := resolver.Root()
	touchFile(t, filepath.Join(root, "memory", "test.md"))

	// Create a lock file without marker (simulates interruption).
	lock := migratingLock{
		StartedAt:    time.Now().UTC(),
		TargetUserID: 12345,
	}
	data, _ := json.MarshalIndent(lock, "", "  ")
	if err := os.WriteFile(filepath.Join(root, ".multi-user-migrating"), data, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	// --resume should complete successfully.
	if err := runMigration(resolver, 12345, false, true, false); err != nil {
		t.Fatalf("resume migration: %v", err)
	}

	// Marker should exist.
	if _, err := os.Stat(filepath.Join(root, ".multi-user-migrated")); os.IsNotExist(err) {
		t.Fatal("expected .multi-user-migrated after successful resume")
	}
	// Lock should be gone.
	if _, err := os.Stat(filepath.Join(root, ".multi-user-migrating")); !os.IsNotExist(err) {
		t.Fatal("expected .multi-user-migrating to be removed")
	}

	// --- Second scenario: force mode ---
	resolver2 := newTestResolver(t)
	writeTestConfig(t, resolver2, []int64{12345})
	touchFile(t, filepath.Join(resolver2.Root(), "memory", "test.md"))

	// Create lock only.
	lockData, _ := json.MarshalIndent(migratingLock{StartedAt: time.Now().UTC(), TargetUserID: 12345}, "", "  ")
	if err := os.WriteFile(filepath.Join(resolver2.Root(), ".multi-user-migrating"), lockData, 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	// --force should clear lock and complete.
	if err := runMigration(resolver2, 12345, false, false, true); err != nil {
		t.Fatalf("force migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(resolver2.Root(), ".multi-user-migrated")); os.IsNotExist(err) {
		t.Fatal("expected .multi-user-migrated after force migration")
	}
}

func TestMigration_ForceOverwritesExistingDestination(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{12345})

	root := resolver.Root()
	srcPath := filepath.Join(root, "memory", "conflict.md")
	dstPath := filepath.Join(root, "users", "12345", "memory", "conflict.md")

	// Create source file.
	touchFile(t, srcPath)

	// Pre-create destination with different content to trigger conflict.
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o700); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	if err := os.WriteFile(dstPath, []byte("old-content"), 0o600); err != nil {
		t.Fatalf("write dst: %v", err)
	}

	// --force should remove destination and overwrite.
	if err := runMigration(resolver, 12345, false, false, true); err != nil {
		t.Fatalf("force migration: %v", err)
	}

	// Source should be gone (moved).
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Fatal("expected source file to be removed after force migration")
	}

	// Destination should exist with the new content ("content" from touchFile).
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != "content" {
		t.Fatalf("expected dst content %q, got %q", "content", string(data))
	}

	// Marker should exist.
	if _, err := os.Stat(filepath.Join(root, ".multi-user-migrated")); os.IsNotExist(err) {
		t.Fatal("expected .multi-user-migrated after force migration")
	}

	// Lock should be gone.
	if _, err := os.Stat(filepath.Join(root, ".multi-user-migrating")); !os.IsNotExist(err) {
		t.Fatal("expected .multi-user-migrating to be removed")
	}
}

func TestMigration_ForceRemovesMarkerAndRestarts(t *testing.T) {
	resolver := newTestResolver(t)
	writeTestConfig(t, resolver, []int64{12345})

	root := resolver.Root()
	touchFile(t, filepath.Join(root, "memory", "test.md"))

	// First migration completes.
	if err := runMigration(resolver, 12345, false, false, false); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".multi-user-migrated")); os.IsNotExist(err) {
		t.Fatal("expected .multi-user-migrated after first migration")
	}

	// Add another file so we can verify fresh migration runs.
	touchFile(t, filepath.Join(root, "memory", "another.md"))

	// --force should clear marker + lock and re-run.
	if err := runMigration(resolver, 12345, false, false, true); err != nil {
		t.Fatalf("force migration: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".multi-user-migrated")); os.IsNotExist(err) {
		t.Fatal("expected .multi-user-migrated after force migration")
	}
}

// ─── Context Memory Migration Tests (Sprint E) ────────────────────────────

func TestEnsureContextMemoryLayout_DryRun(t *testing.T) {
	resolver := newTestResolver(t)
	root := resolver.Root()

	// Create multi-user marker so the migration is valid.
	multiUserMarker := filepath.Join(root, ".multi-user-migrated")
	if err := os.WriteFile(multiUserMarker, []byte(`{"migrated_at":"2026-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Dry-run should succeed without creating marker.
	if err := runEnsureContextMemoryLayout([]string{"--dry-run", "--user-id", "12345"}); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	ctxMemMarker := filepath.Join(root, contextMemoryMigratedFile)
	if _, err := os.Stat(ctxMemMarker); err == nil {
		t.Fatal("dry-run should not create marker")
	}
}

func TestEnsureContextMemoryLayout_Idempotent(t *testing.T) {
	resolver := newTestResolver(t)
	root := resolver.Root()

	// Create multi-user marker.
	multiUserMarker := filepath.Join(root, ".multi-user-migrated")
	if err := os.WriteFile(multiUserMarker, []byte(`{"migrated_at":"2026-01-01T00:00:00Z"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// First run succeeds.
	if err := runEnsureContextMemoryLayout([]string{"--user-id", "12345"}); err != nil {
		t.Fatalf("first run: %v", err)
	}

	ctxMemMarker := filepath.Join(root, contextMemoryMigratedFile)
	if _, err := os.Stat(ctxMemMarker); os.IsNotExist(err) {
		t.Fatal("expected marker after first run")
	}

	// Second run without --force should abort.
	if err := runEnsureContextMemoryLayout([]string{"--user-id", "12345"}); err == nil {
		t.Fatal("second run should abort (already migrated)")
	}

	// Second run with --force should succeed.
	if err := runEnsureContextMemoryLayout([]string{"--user-id", "12345", "--force"}); err != nil {
		t.Fatalf("second run with --force: %v", err)
	}
}

func TestEnsureContextMemoryLayout_RequiresMultiUserMigration(t *testing.T) {
	_ = newTestResolver(t) // ensure instance root exists

	// No multi-user marker → should fail.
	if err := runEnsureContextMemoryLayout([]string{"--user-id", "12345"}); err == nil {
		t.Fatal("expected error when multi-user migration not done")
	}

	// Dry-run should still work (just warns).
	if err := runEnsureContextMemoryLayout([]string{"--dry-run", "--user-id", "12345"}); err != nil {
		t.Fatalf("dry-run without multi-user marker: %v", err)
	}
}

func TestEnsureContextMemoryLayout_DefaultUserID(t *testing.T) {
	resolver := newTestResolver(t)
	root := resolver.Root()

	// Write app.json with default_owner_user_id directly.
	cfg := config.AppConfig{
		DefaultOwnerUserID:    99999,
		TelegramAllowedUserIDs: []int64{99999},
	}
	cfgPath := resolver.AppConfig()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	// Create multi-user marker.
	multiUserMarker := filepath.Join(root, ".multi-user-migrated")
	if err := os.WriteFile(multiUserMarker, []byte(`{"migrated_at":"2026-01-01T00:00:00Z","target_user_id":99999}`), 0600); err != nil {
		t.Fatal(err)
	}

	// No --user-id provided, should use default_owner_user_id.
	if err := runEnsureContextMemoryLayout([]string{}); err != nil {
		t.Fatalf("migration with default user ID: %v", err)
	}

	ctxMemMarker := filepath.Join(root, contextMemoryMigratedFile)
	markerData, readErr := os.ReadFile(ctxMemMarker)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var marker contextMemoryMigratedMarker
	if err := json.Unmarshal(markerData, &marker); err != nil {
		t.Fatal(err)
	}
	if marker.TargetUserID != 99999 {
		t.Fatalf("expected target_user_id=99999, got %d", marker.TargetUserID)
	}
}

// ─── Utilities ──────────────────────────────────────────────────────────────

func opMap(ops []migrationOp) map[string]string {
	m := make(map[string]string, len(ops))
	for _, op := range ops {
		m[op.dst] = op.src
	}
	return m
}
