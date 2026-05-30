package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/cron"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/users"
)

// ─── Migration state files ──────────────────────────────────────────────────

type migratingLock struct {
	StartedAt    time.Time `json:"started_at"`
	TargetUserID int64     `json:"target_user_id"`
}

type migratedMarker struct {
	MigratedAt            time.Time `json:"migrated_at"`
	TargetUserID          int64     `json:"target_user_id"`
	ItemsMoved            int       `json:"items_moved"`
	CronUpdated           int       `json:"cron_updated"`
	DefaultOwnerUserIDSet bool      `json:"default_owner_user_id_set"`
	SchemaVersion         int       `json:"schema_version"`
}

// ─── Operation types ────────────────────────────────────────────────────────

type migrationOp struct {
	src  string
	dst  string
	kind string // "move_file" or "move_dir"
}

type migrationPlan struct {
	targetUserID int64
	root         string
	fileOps      []migrationOp
}

type migrateStats struct {
	itemsMoved  int
	cronUpdated int
}

// ─── CLI entrypoint ─────────────────────────────────────────────────────────

func runMigrateMultiUser(args []string) error {
	var (
		userID int64
		dryRun bool
		resume bool
		force  bool
	)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--user-id":
			if i+1 >= len(args) {
				return fmt.Errorf("--user-id requires a value")
			}
			i++
			v, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid --user-id %q: %w", args[i], err)
			}
			if v <= 0 {
				return fmt.Errorf("--user-id must be a positive integer, got %d", v)
			}
			userID = v
		case "--dry-run":
			dryRun = true
		case "--resume":
			resume = true
		case "--force":
			force = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	if resume && force {
		return fmt.Errorf("--resume and --force are mutually exclusive")
	}

	resolver, err := runtime.New()
	if err != nil {
		return fmt.Errorf("resolve instance root: %w", err)
	}
	if err := runtime.Bootstrap(resolver); err != nil {
		return fmt.Errorf("bootstrap instance: %w", err)
	}

	cfg, err := config.Load(resolver)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Determine target user ID.
	if userID == 0 {
		userID = cfg.DefaultOwnerUserIDOrFallback()
	}
	if userID == 0 {
		return fmt.Errorf("no target user ID: provide --user-id, set default_owner_user_id in config, or configure telegram_allowed_user_ids")
	}

	return runMigration(resolver, userID, dryRun, resume, force)
}

// ─── Core migration logic ───────────────────────────────────────────────────

func runMigration(resolver *runtime.PathResolver, userID int64, dryRun, resume, force bool) error {
	root := resolver.Root()
	migratingPath := filepath.Join(root, ".multi-user-migrating")
	migratedPath := filepath.Join(root, ".multi-user-migrated")

	slog.Info("migration starting",
		"user_id", userID,
		"dry_run", dryRun,
		"resume", resume,
		"force", force,
	)

	// Pre-flight 1: whitelist must be set.
	cfg, err := config.Load(resolver)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if len(cfg.TelegramAllowedUserIDs) == 0 {
		return fmt.Errorf("configure TelegramAllowedUserIDs first")
	}

	// Pre-flight 2: marker check.
	if _, err := os.Stat(migratedPath); err == nil {
		if !force {
			return fmt.Errorf("already migrated (run with --force to re-run)")
		}
	}

	// Pre-flight 3: lock check.
	if _, err := os.Stat(migratingPath); err == nil {
		if !resume && !force {
			return fmt.Errorf("migration in progress; use --resume to continue, or --force to discard progress and restart")
		}
	}

	// Build migration plan.
	plan, err := buildMigrationPlan(root, userID)
	if err != nil {
		return fmt.Errorf("build migration plan: %w", err)
	}

	if dryRun {
		printMigrationPlan(plan)
		return nil
	}

	// Conflict detection (skip in resume/force mode).
	if !resume && !force {
		conflicts := detectConflicts(plan)
		if len(conflicts) > 0 {
			fmt.Fprintf(os.Stderr, "Migration conflicts detected:\n")
			for _, c := range conflicts {
				fmt.Fprintf(os.Stderr, "  %s → %s (already exists)\n", c.src, c.dst)
			}
			return fmt.Errorf("aborting due to %d conflict(s); remove destination files or use --resume or --force", len(conflicts))
		}
	}

	// Remove existing lock/marker now that we've confirmed migration can proceed.
	_ = os.Remove(migratingPath)
	_ = os.Remove(migratedPath)

	// Create lock.
	if err := writeMigrationLock(migratingPath, userID); err != nil {
		return fmt.Errorf("create migration lock: %w", err)
	}

	// Execute.
	stats, err := executeMigration(plan, resume, force)
	if err != nil {
		return fmt.Errorf("migration failed (lock preserved at %s): %w", migratingPath, err)
	}

	// Create profile.
	userRoot := filepath.Join(root, "users", fmt.Sprintf("%d", userID))
	profile := &users.Profile{
		UserID:      userID,
		Language:    "pt",
		IsOwner:     true,
		OnboardedAt: time.Now().UTC(),
	}
	if err := users.Save(filepath.Join(userRoot, "profile.json"), profile); err != nil {
		return fmt.Errorf("create profile: %w", err)
	}
	stats.itemsMoved++

	// Update app.json.
	if err := config.SetDefaultOwnerUserID(resolver, userID); err != nil {
		return fmt.Errorf("update default_owner_user_id: %w", err)
	}
	stats.itemsMoved++

	// Update cron jobs.
	cronUpdated, err := migrateCronOwnerID(resolver.DBPath("cron.db"), userID)
	if err != nil {
		return fmt.Errorf("update cron owner_user_id: %w", err)
	}
	stats.cronUpdated = cronUpdated

	// Write marker.
	if err := writeMigrationMarker(migratedPath, userID, stats); err != nil {
		return fmt.Errorf("write migration marker: %w", err)
	}

	// Remove lock.
	_ = os.Remove(migratingPath)

	slog.Info("migration complete",
		"user_id", userID,
		"items_moved", stats.itemsMoved,
		"cron_updated", stats.cronUpdated,
	)
	return nil
}

// ─── Plan building ──────────────────────────────────────────────────────────

func buildMigrationPlan(root string, userID int64) (*migrationPlan, error) {
	plan := &migrationPlan{
		targetUserID: userID,
		root:         root,
	}
	userRoot := filepath.Join(root, "users", fmt.Sprintf("%d", userID))
	memoryDir := filepath.Join(root, "memory")

	// Reserved files that stay in memory/.
	reserved := map[string]bool{
		filepath.Join(memoryDir, "personas", "IDENTITY.md"): true,
		filepath.Join(memoryDir, "personas", "SOUL.md"):     true,
		filepath.Join(memoryDir, "OWNER_PLAYBOOK.md"):       true,
		filepath.Join(memoryDir, "MEMORY.md"):               true,
	}

	// 1. Walk memory/ and move non-reserved, non-persona, non-topics files.
	if err := filepath.WalkDir(memoryDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip personas/ and topics/ subdirectories (handled separately).
			rel, _ := filepath.Rel(memoryDir, path)
			if rel == "personas" || rel == "topics" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip symlinks — they could point outside the memory directory.
		if fi, statErr := os.Lstat(path); statErr == nil && fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if reserved[path] {
			return nil
		}
		rel, err := filepath.Rel(memoryDir, path)
		if err != nil {
			return err
		}
		plan.fileOps = append(plan.fileOps, migrationOp{
			src:  path,
			dst:  filepath.Join(userRoot, "memory", rel),
			kind: "move_file",
		})
		return nil
	}); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("walk memory dir: %w", err)
	}

	// 2. USER.md.
	userMdSrc := filepath.Join(memoryDir, "personas", "USER.md")
	if _, err := os.Stat(userMdSrc); err == nil {
		plan.fileOps = append(plan.fileOps, migrationOp{
			src:  userMdSrc,
			dst:  filepath.Join(userRoot, "personas", "USER.md"),
			kind: "move_file",
		})
	}

	// 3. Topics: memory/topics/ → root/topics/ (global).
	topicsSrc := filepath.Join(memoryDir, "topics")
	if _, err := os.Stat(topicsSrc); err == nil {
		plan.fileOps = append(plan.fileOps, migrationOp{
			src:  topicsSrc,
			dst:  filepath.Join(root, "topics"),
			kind: "move_dir",
		})
	}

	// 4. Projects: root/projects/*/ → userRoot/projects/*/.
	projectsSrc := filepath.Join(root, "projects")
	entries, err := os.ReadDir(projectsSrc)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read projects dir: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		plan.fileOps = append(plan.fileOps, migrationOp{
			src:  filepath.Join(projectsSrc, name),
			dst:  filepath.Join(userRoot, "projects", name),
			kind: "move_dir",
		})
	}

	return plan, nil
}

// ─── Conflict detection ─────────────────────────────────────────────────────

type conflict struct {
	src string
	dst string
}

func detectConflicts(plan *migrationPlan) []conflict {
	var conflicts []conflict
	for _, op := range plan.fileOps {
		if _, err := os.Stat(op.dst); err == nil {
			// Destination exists and src also exists (if src doesn't exist,
			// it's a no-op situation, not a conflict).
			if _, err := os.Stat(op.src); err == nil {
				conflicts = append(conflicts, conflict{src: op.src, dst: op.dst})
			}
		}
	}
	return conflicts
}

// ─── Execution ──────────────────────────────────────────────────────────────

func executeMigration(plan *migrationPlan, resume, force bool) (*migrateStats, error) {
	stats := &migrateStats{}

	for _, op := range plan.fileOps {
		srcExists := fileExists(op.src)
		dstExists := fileExists(op.dst)

		if !srcExists && !dstExists {
			continue // nothing to do
		}

		if resume && dstExists && srcExists {
			// Verify match: if same size, delete src.
			if sameSize(op.src, op.dst) {
				if err := os.RemoveAll(op.src); err != nil {
					return nil, fmt.Errorf("remove already-migrated source %s: %w", op.src, err)
				}
				stats.itemsMoved++
				continue
			}
			// Mismatch.
			return nil, fmt.Errorf("conflict on resume: %s and %s differ", op.src, op.dst)
		}

		if resume && dstExists && !srcExists {
			// Already migrated in a prior run.
			stats.itemsMoved++
			continue
		}

		if dstExists && !resume && !force {
			return nil, fmt.Errorf("destination already exists: %s", op.dst)
		}

		// Force mode: remove destination before overwriting.
		if force && dstExists {
			if !srcExists {
				// Destination exists but source doesn't — nothing to do.
				stats.itemsMoved++
				continue
			}
			if err := os.RemoveAll(op.dst); err != nil {
				return nil, fmt.Errorf("remove destination for force move: %w", err)
			}
		}

		// Perform the move.
		if op.kind == "move_dir" {
			if err := moveDir(op.src, op.dst); err != nil {
				return nil, fmt.Errorf("move dir %s → %s: %w", op.src, op.dst, err)
			}
		} else {
			if err := moveFile(op.src, op.dst); err != nil {
				return nil, fmt.Errorf("move file %s → %s: %w", op.src, op.dst, err)
			}
		}
		stats.itemsMoved++
	}

	return stats, nil
}

// ─── File operations ────────────────────────────────────────────────────────

func moveFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	tmpPath := dst + ".tmp"
	if err := copyFile(src, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("copy: %w", err)
	}

	// Verify size.
	srcInfo, err := os.Stat(src)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("stat source: %w", err)
	}
	dstInfo, err := os.Stat(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("stat temp: %w", err)
	}
	if srcInfo.Size() != dstInfo.Size() {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("size mismatch: src %d ≠ tmp %d", srcInfo.Size(), dstInfo.Size())
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	return os.Remove(src)
}

func moveDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	// Use a unique temp name to avoid collisions with previous attempts.
	tmpPath := dst + "." + strconv.FormatInt(time.Now().UnixNano(), 36) + ".tmp"

	// Recursively copy.
	if err := copyDir(src, tmpPath); err != nil {
		_ = os.RemoveAll(tmpPath)
		return fmt.Errorf("copy dir: %w", err)
	}

	// Verify: compare file sizes for every file in both trees.
	if err := verifyDirMatch(src, tmpPath); err != nil {
		_ = os.RemoveAll(tmpPath)
		return fmt.Errorf("dir verification: %w", err)
	}

	if err := os.Rename(tmpPath, dst); err != nil {
		_ = os.RemoveAll(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	return os.RemoveAll(src)
}

// verifyDirMatch walks both directories and compares every file by relative
// path and size. Returns an error if any file is missing or has a different size.
func verifyDirMatch(a, b string) error {
	return filepath.WalkDir(a, func(pa string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(a, pa)
		if err != nil {
			return err
		}
		pb := filepath.Join(b, rel)
		ai, err := os.Stat(pa)
		if err != nil {
			return fmt.Errorf("stat %s: %w", rel, err)
		}
		bi, err := os.Stat(pb)
		if err != nil {
			return fmt.Errorf("missing %s in destination: %w", rel, err)
		}
		if ai.Size() != bi.Size() {
			return fmt.Errorf("size mismatch for %s: src %d ≠ dst %d", rel, ai.Size(), bi.Size())
		}
		return nil
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	// Reject symlink destinations to prevent arbitrary file overwrite.
	if fi, statErr := os.Lstat(dst); statErr == nil && fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("destination %q is a symlink, refusing to follow", dst)
	}
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(dstPath, 0o700)
		}
		return copyFile(path, dstPath)
	})
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sameSize(a, b string) bool {
	ai, errA := os.Stat(a)
	bi, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	if ai.IsDir() != bi.IsDir() {
		return false
	}
	if ai.IsDir() {
		n, err := countFiles(a)
		if err != nil {
			return false
		}
		m, err := countFiles(b)
		if err != nil {
			return false
		}
		return n == m
	}
	return ai.Size() == bi.Size()
}

func countFiles(dir string) (int, error) {
	var count int
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}

// ─── Marker and lock ────────────────────────────────────────────────────────

func writeMigrationLock(path string, userID int64) error {
	data, err := json.MarshalIndent(migratingLock{
		StartedAt:    time.Now().UTC(),
		TargetUserID: userID,
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func writeMigrationMarker(path string, userID int64, stats *migrateStats) error {
	data, err := json.MarshalIndent(migratedMarker{
		MigratedAt:            time.Now().UTC(),
		TargetUserID:          userID,
		ItemsMoved:            stats.itemsMoved,
		CronUpdated:           stats.cronUpdated,
		DefaultOwnerUserIDSet: true,
		SchemaVersion:         1,
	}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

// ─── Cron migration ─────────────────────────────────────────────────────────

func migrateCronOwnerID(dbPath string, userID int64) (int, error) {
	store, err := cron.NewSQLiteCronStore(dbPath)
	if err != nil {
		return 0, fmt.Errorf("open cron store: %w", err)
	}
	defer func() { _ = store.Close() }()

	userIDStr := strconv.FormatInt(userID, 10)
	var updated int
	err = store.WithTx(context.Background(), func(tx *sql.Tx) error {
		res, err := tx.Exec(`
			UPDATE cron_jobs
			SET owner_user_id = ?
			WHERE owner_user_id IS NULL OR owner_user_id = '' OR owner_user_id = '0'
		`, userIDStr)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		updated = int(n)
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("update cron owner_user_id: %w", err)
	}
	return updated, nil
}

// ─── Dry-run output ─────────────────────────────────────────────────────────

func printMigrationPlan(plan *migrationPlan) {
	fmt.Printf("Migration plan for user ID %d in %s:\n\n", plan.targetUserID, plan.root)
	for _, op := range plan.fileOps {
		fmt.Printf("  [%s] %s → %s\n", op.kind, relHome(op.src, plan.root), relHome(op.dst, plan.root))
	}
	fmt.Println()
	fmt.Println("  [profile] create profile.json")
	fmt.Println("  [config]  set default_owner_user_id")
	fmt.Println("  [cron]    UPDATE cron_jobs SET owner_user_id")
	fmt.Printf("\nTotal operations: %d\n", len(plan.fileOps)+3)
}

func relHome(path, root string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return "~/.aurelia/" + rel
}

// ─── Context Memory Migration (Sprint E) ───────────────────────────────────

const contextMemoryMigratedFile = ".context-memory-migrated"

type contextMemoryMigratedMarker struct {
	MigratedAt         time.Time `json:"migrated_at"`
	TargetUserID       int64     `json:"target_user_id"`
	MultiUserMigrated  bool      `json:"multi_user_migrated"`
	SchemaVersion      int       `json:"schema_version"`
}

// runEnsureContextMemoryLayout validates that the deployment is ready for the
// context-scoped memory layout (Sprint E) and writes the migration marker.
// It depends on the multi-user migration having been run first.
func runEnsureContextMemoryLayout(args []string) error {
	var (
		userID int64
		dryRun bool
		force  bool
	)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--user-id":
			if i+1 >= len(args) {
				return fmt.Errorf("--user-id requires a value")
			}
			i++
			v, err := strconv.ParseInt(args[i], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid --user-id %q: %w", args[i], err)
			}
			if v <= 0 {
				return fmt.Errorf("--user-id must be a positive integer, got %d", v)
			}
			userID = v
		case "--dry-run":
			dryRun = true
		case "--force":
			force = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	resolver, err := runtime.New()
	if err != nil {
		return fmt.Errorf("resolve instance root: %w", err)
	}

	root := resolver.Root()
	multiUserMarker := filepath.Join(root, ".multi-user-migrated")
	ctxMemMarker := filepath.Join(root, contextMemoryMigratedFile)

	// Check idempotency: if marker already exists, abort (unless --force).
	if _, err := os.Stat(ctxMemMarker); err == nil {
		if !force {
			return fmt.Errorf("context-memory migration already applied (%s exists); use --force to re-run", ctxMemMarker)
		}
		slog.Info("context-memory marker exists, re-running due to --force")
	}

	// Depends on multi-user migration.
	multiUserDone := false
	if _, err := os.Stat(multiUserMarker); err == nil {
		multiUserDone = true
	}

	// Determine target user ID.
	if userID == 0 {
		cfg, cfgErr := config.Load(resolver)
		if cfgErr != nil {
			return fmt.Errorf("load config: %w", cfgErr)
		}
		userID = cfg.DefaultOwnerUserIDOrFallback()
	}
	if userID == 0 {
		return fmt.Errorf("no target user ID: provide --user-id or set default_owner_user_id in config")
	}

	if dryRun {
		fmt.Printf("Context-memory migration plan (dry-run):\n")
		fmt.Printf("  Instance root:    %s\n", root)
		fmt.Printf("  Target user ID:   %d\n", userID)
		fmt.Printf("  Multi-user done:  %v\n", multiUserDone)
		fmt.Printf("  Marker to create: %s\n", ctxMemMarker)
		if !multiUserDone {
			fmt.Printf("\n⚠️  Multi-user migration NOT detected. Run 'aurelia migrate-multi-user --user-id %d' first.\n", userID)
		}
		return nil
	}

	if !multiUserDone {
		return fmt.Errorf("multi-user migration not detected: run 'aurelia migrate-multi-user --user-id %d' first", userID)
	}

	// Write marker.
	marker := contextMemoryMigratedMarker{
		MigratedAt:        time.Now().UTC(),
		TargetUserID:      userID,
		MultiUserMigrated: true,
		SchemaVersion:     1,
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal marker: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(ctxMemMarker, data, 0o600); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}

	slog.Info("context-memory migration complete",
		"user_id", userID,
		"marker", ctxMemMarker,
	)
	fmt.Printf("✅ Context-scoped memory layout activated for user %d.\n", userID)
	fmt.Printf("   Marker: %s\n", ctxMemMarker)
	return nil
}

// ─── Boot check ─────────────────────────────────────────────────────────────

// checkMigrationLock returns an error if a .multi-user-migrating lock exists
// without a corresponding .multi-user-migrated marker, indicating an interrupted
// migration that needs --resume or --force.
func checkMigrationLock(resolver *runtime.PathResolver) error {
	root := resolver.Root()
	migratingPath := filepath.Join(root, ".multi-user-migrating")
	migratedPath := filepath.Join(root, ".multi-user-migrated")

	if _, err := os.Stat(migratingPath); err == nil {
		if _, err := os.Stat(migratedPath); os.IsNotExist(err) {
			return fmt.Errorf("migration lock found at %s without completion marker; run 'aurelia migrate-multi-user --resume' or --force", migratingPath)
		}
	}
	return nil
}
