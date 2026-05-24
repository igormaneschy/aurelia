package planning

import (
	"context"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/session"
)

// TestTypes_Compile verifies all types can be instantiated.
func TestTypes_Compile(t *testing.T) {
	// Status constants
	_ = StatusActive
	_ = StatusAwaitingExec
	_ = StatusCompleted
	_ = StatusCancelled

	// Phase constants
	_ = PhaseSpecify
	_ = PhaseDesign
	_ = PhaseTasks
	_ = PhaseReview

	// Artifact
	a := Artifact{
		Path:      "/tmp/plan.md",
		Phase:     PhaseSpecify,
		Tool:      "Write",
		InsideCWD: true,
		Confirmed: false,
		CreatedAt: time.Now(),
	}
	if a.Path != "/tmp/plan.md" {
		t.Fatalf("Artifact.Path = %q, want %q", a.Path, "/tmp/plan.md")
	}
	if a.Phase != PhaseSpecify {
		t.Fatalf("Artifact.Phase = %q, want %q", a.Phase, PhaseSpecify)
	}

	// ProjectContext
	pc := ProjectContext{
		HasGit:            true,
		HasClaudeMD:       false,
		HasAgentsMD:       true,
		HasReadme:         false,
		Layouts:           []string{"tlc"},
		NeedsLayoutChoice: true,
		Stacks:            []string{"go"},
		DiscoveredAt:      time.Now(),
	}
	if !pc.HasGit {
		t.Fatal("ProjectContext.HasGit = false, want true")
	}
	if len(pc.Layouts) != 1 || pc.Layouts[0] != "tlc" {
		t.Fatalf("ProjectContext.Layouts = %v, want [tlc]", pc.Layouts)
	}

	// State
	s := State{
		Key:     session.SessionKey{ChatID: 1, ThreadID: 2, UserID: 3},
		Version: 0,
		Status:  StatusActive,
		Phase:   PhaseSpecify,
		CWD:     "/tmp",
		ProjectCtx: &ProjectContext{
			HasGit: true,
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if s.Key.ChatID != 1 {
		t.Fatalf("State.Key.ChatID = %d, want 1", s.Key.ChatID)
	}
	if s.Status != StatusActive {
		t.Fatalf("State.Status = %q, want %q", s.Status, StatusActive)
	}
	if s.Phase != PhaseSpecify {
		t.Fatalf("State.Phase = %q, want %q", s.Phase, PhaseSpecify)
	}
	if s.ProjectCtx == nil || !s.ProjectCtx.HasGit {
		t.Fatal("State.ProjectCtx.HasGit = false, want true")
	}

	// Store interface (compile check: can we assign a nil of the right type?)
	var store Store
	if store != nil {
		t.Fatal("Store interface should be nil")
	}

	// OfferStore interface
	var ostore OfferStore
	if ostore != nil {
		t.Fatal("OfferStore interface should be nil")
	}

	// Ensure context is used in interface signatures
	_ = func(Store) {
		_ = context.Background()
	}
}

// TestState_OptimisticLocking verifies Version field exists and defaults to 0.
func TestState_OptimisticLocking(t *testing.T) {
	s := State{}
	if s.Version != 0 {
		t.Fatalf("State{}.Version = %d, want 0", s.Version)
	}

	key := session.SessionKey{ChatID: 1, ThreadID: 2, UserID: 3}
	state := State{Key: key, Status: StatusActive, Phase: PhaseDesign}
	if state.Version != 0 {
		t.Fatalf("State.Version = %d, want 0", state.Version)
	}
	state.Version = 5
	if state.Version != 5 {
		t.Fatalf("State.Version after set = %d, want 5", state.Version)
	}
}

// TestArtifact_InsideCWD verifies InsideCWD bool field.
func TestArtifact_InsideCWD(t *testing.T) {
	a := Artifact{}
	if a.InsideCWD {
		t.Fatal("Artifact{}.InsideCWD = true, want false (default)")
	}

	inside := Artifact{InsideCWD: true}
	if !inside.InsideCWD {
		t.Fatal("Artifact{InsideCWD: true}.InsideCWD = false, want true")
	}

	outside := Artifact{InsideCWD: false}
	if outside.InsideCWD {
		t.Fatal("Artifact{InsideCWD: false}.InsideCWD = true, want false")
	}
}

// TestStatus_Constants verifies all status constants are distinct.
func TestStatus_Constants(t *testing.T) {
	statuses := []Status{StatusActive, StatusAwaitingExec, StatusCompleted, StatusCancelled}
	seen := make(map[Status]bool)
	for _, s := range statuses {
		if seen[s] {
			t.Fatalf("duplicate status constant: %q", s)
		}
		seen[s] = true
	}
	if len(statuses) != 4 {
		t.Fatalf("expected 4 status constants, got %d", len(statuses))
	}
}

// TestPhase_Constants verifies all phase constants are distinct.
func TestPhase_Constants(t *testing.T) {
	phases := []Phase{PhaseSpecify, PhaseDesign, PhaseTasks, PhaseReview}
	seen := make(map[Phase]bool)
	for _, p := range phases {
		if seen[p] {
			t.Fatalf("duplicate phase constant: %q", p)
		}
		seen[p] = true
	}
	if len(phases) != 4 {
		t.Fatalf("expected 4 phase constants, got %d", len(phases))
	}
}
