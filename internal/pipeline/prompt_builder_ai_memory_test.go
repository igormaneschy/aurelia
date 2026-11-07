package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/memorycontext"
	"github.com/igormaneschy/aurelia/internal/session"
)

type fakeMemoryProvider struct {
	workingSet memorycontext.WorkingSet
	status     memorycontext.Status
	err        error
	calls      int
}

func (f *fakeMemoryProvider) WorkingSet(context.Context, memorycontext.Scope) (memorycontext.WorkingSet, error) {
	f.calls++
	return f.workingSet, f.err
}

func (f *fakeMemoryProvider) Status(context.Context, memorycontext.Scope) (memorycontext.Status, error) {
	return f.status, f.err
}

func TestBuildSystemPromptUsesAIMemoryWorkingSet(t *testing.T) {
	cwd := t.TempDir()
	marker := "workspace = \"personal\"\nproject = \"aurelia\"\n"
	if err := os.WriteFile(filepath.Join(cwd, ".ai-memory.toml"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	store := session.NewStore()
	store.SetCwd(42, 0, cwd)
	provider := &fakeMemoryProvider{workingSet: memorycontext.WorkingSet{
		Content: "Project fact from ai-memory.",
		Chars:   28,
		Sources: []string{"decisions/aurelia.md"},
	}}
	bc := &Service{memoryProvider: provider, sessions: store}

	prompt, err := bc.buildSystemPrompt("oi", nil, 42, 1, 0, 42, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Persistent Memory — ai-memory",
		"personal/aurelia",
		`workspace: "personal"`,
		`project: "aurelia"`,
		"Project fact from ai-memory.",
		"<memory_untrusted>",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, forbidden := range []string{"### Memory Layers", "#### Personal Memory", "| **CWD Overlay**", "## ai-memory scope"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt contains legacy memory marker %q:\n%s", forbidden, prompt)
		}
	}
}

func TestBuildAIMemoryInstructionsDegraded(t *testing.T) {
	cwd := t.TempDir()
	marker := "workspace = \"personal\"\nproject = \"aurelia\"\n"
	if err := os.WriteFile(filepath.Join(cwd, ".ai-memory.toml"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	provider := &fakeMemoryProvider{workingSet: memorycontext.WorkingSet{
		Content:        "last cached context",
		Chars:          19,
		Degraded:       true,
		DegradedReason: "server down",
	}}
	bc := &Service{memoryProvider: provider}
	got := bc.buildAIMemoryInstructions(cwd, 42, 0, 42)
	if !strings.Contains(got, "degraded") || !strings.Contains(got, "last cached context") {
		t.Fatalf("degraded instructions = %q", got)
	}
	if strings.Contains(got, "### Memory Layers") || strings.Contains(got, "| **CWD Overlay**") {
		t.Fatalf("degraded instructions contain legacy file layers: %q", got)
	}
}

func TestBuildAIMemoryInstructionsWithoutProject(t *testing.T) {
	provider := &fakeMemoryProvider{}
	bc := &Service{memoryProvider: provider}
	got := bc.buildAIMemoryInstructions("", 42, 0, 42)
	if !strings.Contains(got, "No ai-memory project is bound") {
		t.Fatalf("no-project instructions = %q", got)
	}
	if provider.calls != 0 {
		t.Fatalf("provider called %d times without a project", provider.calls)
	}
}
