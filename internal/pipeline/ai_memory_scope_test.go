package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/session"
)

func TestDeriveProjectNameFromCwd_GitRoot(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "myproject")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "cmd", "server")
	if err := os.MkdirAll(sub, 0700); err != nil {
		t.Fatal(err)
	}
	if got := deriveProjectNameFromCwd(sub); got != "myproject" {
		t.Fatalf("deriveProjectNameFromCwd(%q) = %q, want %q", sub, got, "myproject")
	}
	if got := deriveProjectNameFromCwd(repo); got != "myproject" {
		t.Fatalf("deriveProjectNameFromCwd(%q) = %q, want %q", repo, got, "myproject")
	}
}

func TestDeriveProjectNameFromCwd_GitFileWorktree(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "wt")
	if err := os.MkdirAll(repo, 0700); err != nil {
		t.Fatal(err)
	}
	// git worktrees use a .git FILE, not a directory
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/wt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := deriveProjectNameFromCwd(repo); got != "wt" {
		t.Fatalf("deriveProjectNameFromCwd(%q) = %q, want %q", repo, got, "wt")
	}
}

func TestDeriveProjectNameFromCwd_NoGitFallsBackToBasename(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "wiki-project") // no .git anywhere above
	if err := os.MkdirAll(proj, 0700); err != nil {
		t.Fatal(err)
	}
	if got := deriveProjectNameFromCwd(proj); got != "wiki-project" {
		t.Fatalf("deriveProjectNameFromCwd(%q) = %q, want %q", proj, got, "wiki-project")
	}
}

func TestDeriveProjectNameFromCwd_Empty(t *testing.T) {
	if got := deriveProjectNameFromCwd(""); got != "" {
		t.Fatalf("deriveProjectNameFromCwd(\"\") = %q, want \"\"", got)
	}
}

func TestBuildAiMemoryScopeSection_WithCwd(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "AutoTradersOMQS-GO")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	store := session.NewStore()
	store.SetCwd(123, 2, repo)
	bc := &Service{sessions: store}

	section := bc.buildAiMemoryScopeSection(nil, 123, 2, 0, false)
	if section == "" {
		t.Fatal("expected non-empty scope section when cwd is set")
	}
	if !strings.Contains(section, "AutoTradersOMQS-GO") {
		t.Fatalf("section should mention project name, got: %s", section)
	}
	if !strings.Contains(section, `project: "AutoTradersOMQS-GO"`) {
		t.Fatalf("section should include explicit project arg, got: %s", section)
	}
	if !strings.Contains(section, "global: true") {
		t.Fatalf("section should mention global fallback, got: %s", section)
	}
}

func TestBuildAiMemoryScopeSection_NoCwd(t *testing.T) {
	bc := &Service{sessions: session.NewStore()}
	if got := bc.buildAiMemoryScopeSection(nil, 123, 2, 0, false); got != "" {
		t.Fatalf("expected empty section without cwd, got: %q", got)
	}
}

func TestBuildSystemPrompt_IncludesAiMemoryScope(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo-x")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	store := session.NewStore()
	store.SetCwd(7, 0, repo)
	bc := &Service{sessions: store}

	prompt, err := bc.buildSystemPrompt("oi", nil, 7, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "## ai-memory MCP scope") {
		t.Fatalf("system prompt should contain ai-memory MCP scope section")
	}
	if !strings.Contains(prompt, `project: "repo-x"`) {
		t.Fatalf("system prompt should inject explicit project, got: %s", prompt)
	}
}

func TestBuildSystemPrompt_NoScopeInChatMode(t *testing.T) {
	bc := &Service{sessions: session.NewStore()}
	prompt, err := bc.buildSystemPrompt("oi", nil, 7, 1, 0, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "## ai-memory MCP scope") {
		t.Fatalf("chat mode (no cwd) should NOT include ai-memory scope section")
	}
}
