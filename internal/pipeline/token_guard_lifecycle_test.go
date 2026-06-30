package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/session"
)

func TestApplyLifecycle_TokenGuardEscalatesToCompact(t *testing.T) {
	s := newLifecycleTestService(t)
	s.tokenGuard = session.NewTokenGuard()
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()
	s.sessions.SetSession(1, 2, 100, "/tmp/test.jsonl")

	inputs := []int{250_000, 300_000, 350_000}
	call := 0
	s.testSessionStats = func(_ context.Context, _ bridge.RequestOptions) (*bridge.SessionStats, error) {
		stats := &bridge.SessionStats{InputTokens: inputs[call]}
		if call < len(inputs)-1 {
			call++
		}
		return stats, nil
	}

	s.testCompactSession = func(_ context.Context, _ int64, _ int, _ int64, _ bridge.RequestOptions) (*bridge.CompactSessionResult, error) {
		return &bridge.CompactSessionResult{Success: true, SessionFile: "/tmp/test.jsonl", TokensBefore: 350_000}, nil
	}

	req := &bridge.Request{Command: "query", Options: bridge.RequestOptions{Continue: true, Resume: "/tmp/test.jsonl"}}

	for i := 0; i < len(inputs)-1; i++ {
		result := s.applyLifecycle(context.Background(), req, 1, 2, 100)
		if result.Decision.Action != session.ActionContinue {
			t.Fatalf("turn %d: expected continue, got %s (%s)", i+1, result.Decision.Action, result.Decision.Reason)
		}
	}

	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)
	if result.Decision.Action != session.ActionCompact {
		t.Fatalf("expected compact on 3rd stall turn, got %s (%s)", result.Decision.Action, result.Decision.Reason)
	}
}

func TestApplyLifecycle_TokenGuardImmediateRotate(t *testing.T) {
	s := newLifecycleTestService(t)
	s.tokenGuard = session.NewTokenGuard()
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()
	sessionFile := writeTestSessionFile(t)
	s.sessions.SetSession(1, 2, 100, sessionFile)

	s.testSessionStats = func(_ context.Context, _ bridge.RequestOptions) (*bridge.SessionStats, error) {
		return &bridge.SessionStats{InputTokens: 550_000}, nil
	}
	s.testRotateSession = func(_ context.Context, _ int64, _ int, _ int64, _ bridge.RequestOptions) (*bridge.RotateSessionResult, error) {
		return &bridge.RotateSessionResult{
			Success:         true,
			OldSessionFile:  sessionFile,
			NewSessionFile:  sessionFile,
		}, nil
	}

	req := &bridge.Request{Command: "query", Options: bridge.RequestOptions{Continue: true, Resume: sessionFile}}
	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

	if result.Decision.Action != session.ActionRotate {
		t.Fatalf("expected rotate at rotate_after, got %s (%s)", result.Decision.Action, result.Decision.Reason)
	}
}

func TestApplyLifecycle_TokenGuardWarnsHighTokens(t *testing.T) {
	s := newLifecycleTestService(t)
	s.tokenGuard = session.NewTokenGuard()
	s.config.SessionLifecycle = config.DefaultSessionLifecycleConfig()
	sessionFile := writeTestSessionFile(t)
	s.sessions.SetSession(1, 2, 100, sessionFile)

	s.testSessionStats = func(_ context.Context, _ bridge.RequestOptions) (*bridge.SessionStats, error) {
		return &bridge.SessionStats{InputTokens: 520_000}, nil
	}
	s.testRotateSession = func(_ context.Context, _ int64, _ int, _ int64, _ bridge.RequestOptions) (*bridge.RotateSessionResult, error) {
		return &bridge.RotateSessionResult{
			Success:        true,
			OldSessionFile: sessionFile,
			NewSessionFile: sessionFile,
		}, nil
	}

	req := &bridge.Request{Command: "query", Options: bridge.RequestOptions{Continue: true, Resume: sessionFile}}
	result := s.applyLifecycle(context.Background(), req, 1, 2, 100)

	if result.Decision.Action != session.ActionRotate {
		t.Fatalf("expected rotate above rotate_after, got %s", result.Decision.Action)
	}
}

func writeTestSessionFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	return path
}