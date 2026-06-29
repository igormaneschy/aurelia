package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBootstrap_CreatesAllDirectories(t *testing.T) {
	root := t.TempDir()
	r := &PathResolver{root: root}

	if err := Bootstrap(r); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	expected := []string{
		filepath.Join(root, "config"),
		filepath.Join(root, "data"),
		filepath.Join(root, "memory"),
		filepath.Join(root, "memory", "personas"),
		filepath.Join(root, "agents"),
	}
	for _, dir := range expected {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("directory not created: %s (err=%v)", dir, err)
		}
	}
}

func TestBootstrap_Idempotent(t *testing.T) {
	root := t.TempDir()
	r := &PathResolver{root: root}

	// Place a sentinel file inside a directory Bootstrap will visit
	agentsDir := filepath.Join(root, "agents")
	if err := os.MkdirAll(agentsDir, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(agentsDir, "my-agent.md")
	if err := os.WriteFile(sentinel, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	// Run Bootstrap twice
	if err := Bootstrap(r); err != nil {
		t.Fatalf("first Bootstrap() error: %v", err)
	}
	if err := Bootstrap(r); err != nil {
		t.Fatalf("second Bootstrap() error: %v", err)
	}

	// Sentinel file must survive
	data, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("sentinel file removed by Bootstrap: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("sentinel file content changed: got %q", data)
	}
}

func TestBootstrapProject_CreatesLocalSkillsDirectory(t *testing.T) {
	projectRoot := t.TempDir()

	if err := BootstrapProject(projectRoot); err != nil {
		t.Fatalf("BootstrapProject() error: %v", err)
	}

	expected := []string{
		filepath.Join(projectRoot, ".aurelia"),
	}
	for _, dir := range expected {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Errorf("directory not created: %s (err=%v)", dir, err)
		}
	}
}

func TestBootstrapProjectMemory_IsNoOpSinceV031(t *testing.T) {
	root := t.TempDir()
	r := &PathResolver{root: root}
	cwd := "/home/user/myproject"

	// BootstrapProjectMemory is a no-op since v0.31.0 (project_team removed).
	if err := BootstrapProjectMemory(r, cwd); err != nil {
		t.Fatalf("BootstrapProjectMemory() error: %v", err)
	}

	// Verify team dir is NOT created (project_team removed).
	teamDir := r.ProjectTeamMemoryDir(cwd)
	if _, err := os.Stat(teamDir); err == nil {
		t.Errorf("team directory should NOT be created after v0.31.0, but exists: %s", teamDir)
	}

	// Idempotent: running again should not fail
	if err := BootstrapProjectMemory(r, cwd); err != nil {
		t.Fatalf("second BootstrapProjectMemory() error: %v", err)
	}
}

func TestBootstrapProjectMemory_EmptyCwd(t *testing.T) {
	root := t.TempDir()
	r := &PathResolver{root: root}

	if err := BootstrapProjectMemory(r, ""); err != nil {
		t.Fatalf("BootstrapProjectMemory('') should be no-op, got: %v", err)
	}
	if err := BootstrapProjectMemory(r, "  "); err != nil {
		t.Fatalf("BootstrapProjectMemory('  ') should be no-op, got: %v", err)
	}
}

func TestBootstrapConversationProjectMemory_CreatesCwdOverlayIndex(t *testing.T) {
	root := t.TempDir()
	r := &PathResolver{root: root}
	cwd := "/home/user/myproject"

	if err := BootstrapConversationProjectMemory(r, cwd, 42, 99); err != nil {
		t.Fatalf("BootstrapConversationProjectMemory() error: %v", err)
	}

	// cwd_overlay is project-scoped: projects/<slug>/cwd_overlay/
	dir := r.ProjectCwdOverlayDir(cwd)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("directory not created: %s err=%v", dir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "MEMORY.md")); err != nil {
		t.Fatalf("MEMORY.md not created in %s: %v", dir, err)
	}

	// Verify team dir is NOT created (project_team removed).
	teamDir := r.ProjectTeamMemoryDir(cwd)
	if _, err := os.Stat(teamDir); err == nil {
		t.Errorf("team directory should NOT be created after v0.31.0, but exists: %s", teamDir)
	}
}

func TestBootstrap_PermissionsUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are no-op on Windows (ACL-based)")
	}

	root := t.TempDir()
	r := &PathResolver{root: root}

	if err := Bootstrap(r); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}

	dirs := []string{
		filepath.Join(root, "config"),
		filepath.Join(root, "data"),
		filepath.Join(root, "memory"),
	}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("cannot stat %s: %v", dir, err)
			continue
		}
		mode := info.Mode().Perm()
		if mode != 0700 {
			t.Errorf("%s: mode = %o, want %o", dir, mode, 0700)
		}
	}
}
