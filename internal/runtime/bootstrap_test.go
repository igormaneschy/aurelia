package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestBootstrap_CreatesChatModeMarker(t *testing.T) {
	root := t.TempDir()
	r := &PathResolver{root: root}
	if err := Bootstrap(r); err != nil {
		t.Fatalf("Bootstrap() error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".ai-memory.toml"))
	if err != nil {
		t.Fatalf("chat-mode marker: %v", err)
	}
	for _, want := range []string{`project = "aurelia-chat"`, "deliver_on_session_start = false", "[recovery]"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("marker missing %q: %s", want, data)
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
