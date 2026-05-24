package planning

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

func TestObserveEvent_Write(t *testing.T) {
	cwd := t.TempDir()
	obs := NewObserver(cwd)
	target := filepath.Join(cwd, "file.go")

	event := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path":    target,
			"content": "package main",
		},
	}

	artifacts := obs.ObserveEvent(event)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Tool != "Write" {
		t.Errorf("Tool = %q, want %q", artifacts[0].Tool, "Write")
	}
	if artifacts[0].Path != target {
		t.Errorf("Path = %q, want %q", artifacts[0].Path, target)
	}
	if !artifacts[0].InsideCWD {
		t.Error("InsideCWD = false, want true")
	}
	if artifacts[0].CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want non-zero")
	}
}

func TestObserveEvent_Edit(t *testing.T) {
	cwd := t.TempDir()
	obs := NewObserver(cwd)
	target := filepath.Join(cwd, "main.go")

	event := bridge.Event{
		Type: "tool_use",
		Name: "Edit",
		Input: map[string]interface{}{
			"path":      target,
			"oldString": "foo",
			"newString": "bar",
		},
	}

	artifacts := obs.ObserveEvent(event)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Tool != "Edit" {
		t.Errorf("Tool = %q, want %q", artifacts[0].Tool, "Edit")
	}
	if artifacts[0].Path != target {
		t.Errorf("Path = %q, want %q", artifacts[0].Path, target)
	}
}

func TestObserveEvent_MultiEdit(t *testing.T) {
	cwd := t.TempDir()
	obs := NewObserver(cwd)
	target1 := filepath.Join(cwd, "a.go")
	target2 := filepath.Join(cwd, "b.go")

	event := bridge.Event{
		Type: "tool_use",
		Name: "MultiEdit",
		Input: map[string]interface{}{
			"edits": []interface{}{
				map[string]interface{}{"path": target1, "oldString": "a", "newString": "b"},
				map[string]interface{}{"path": target2, "oldString": "c", "newString": "d"},
			},
		},
	}

	artifacts := obs.ObserveEvent(event)
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(artifacts))
	}
	if artifacts[0].Path != target1 {
		t.Errorf("artifacts[0].Path = %q, want %q", artifacts[0].Path, target1)
	}
	if artifacts[1].Path != target2 {
		t.Errorf("artifacts[1].Path = %q, want %q", artifacts[1].Path, target2)
	}
	if artifacts[0].Tool != "MultiEdit" {
		t.Errorf("artifacts[0].Tool = %q, want %q", artifacts[0].Tool, "MultiEdit")
	}
	if artifacts[1].Tool != "MultiEdit" {
		t.Errorf("artifacts[1].Tool = %q, want %q", artifacts[1].Tool, "MultiEdit")
	}
}

func TestObserveEvent_RelativePath(t *testing.T) {
	cwd := t.TempDir()
	obs := NewObserver(cwd)

	event := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path": "relative/path/file.go",
		},
	}

	artifacts := obs.ObserveEvent(event)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	expected := filepath.Join(cwd, "relative/path/file.go")
	if artifacts[0].Path != expected {
		t.Errorf("Path = %q, want %q", artifacts[0].Path, expected)
	}
	if !artifacts[0].InsideCWD {
		t.Error("InsideCWD = false for relative path, want true")
	}
}

func TestObserveEvent_OutsideCWD(t *testing.T) {
	cwd := t.TempDir()
	obs := NewObserver(cwd)
	outsidePath := filepath.Join(cwd, "..", "outside.go")

	event := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path": outsidePath,
		},
	}

	artifacts := obs.ObserveEvent(event)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].InsideCWD {
		t.Error("InsideCWD = true for path outside cwd, want false")
	}
}

func TestObserveEvent_MissingInput(t *testing.T) {
	cwd := t.TempDir()
	obs := NewObserver(cwd)

	event := bridge.Event{
		Type:  "tool_use",
		Name:  "Write",
		Input: nil,
	}

	artifacts := obs.ObserveEvent(event)
	if artifacts != nil {
		t.Fatalf("expected nil, got %d artifacts", len(artifacts))
	}
}

func TestObserveEvent_UnknownTool(t *testing.T) {
	cwd := t.TempDir()
	obs := NewObserver(cwd)

	event := bridge.Event{
		Type: "tool_use",
		Name: "Read",
		Input: map[string]interface{}{
			"path": "/tmp/file.go",
		},
	}

	artifacts := obs.ObserveEvent(event)
	if artifacts != nil {
		t.Fatalf("expected nil, got %d artifacts", len(artifacts))
	}
}

func TestObserveEvent_FalsePrefix(t *testing.T) {
	// cwd is "/tmp/<random>/proj", path is "/tmp/<random>/project"
	// filepath.Rel should not be fooled by string prefix matching
	cwd := filepath.Join(t.TempDir(), "proj")
	obs := NewObserver(cwd)

	parent := filepath.Dir(cwd)
	outsidePath := filepath.Join(parent, "project")

	event := bridge.Event{
		Type: "tool_use",
		Name: "Write",
		Input: map[string]interface{}{
			"path": outsidePath,
		},
	}

	artifacts := obs.ObserveEvent(event)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].InsideCWD {
		t.Errorf("InsideCWD = true for prefix trap path %q (cwd=%q), want false",
			outsidePath, cwd)
	}
}

func TestObserveEvent_FileField(t *testing.T) {
	cwd := t.TempDir()
	obs := NewObserver(cwd)
	target := filepath.Join(cwd, "config.yaml")

	event := bridge.Event{
		Type: "tool_use",
		Name: "Edit",
		Input: map[string]interface{}{
			"file_path": target,
			"oldString": "port: 8080",
			"newString": "port: 9090",
		},
	}

	artifacts := obs.ObserveEvent(event)
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}
	if artifacts[0].Path != target {
		t.Errorf("Path = %q, want %q", artifacts[0].Path, target)
	}
}

func TestReconcileArtifacts(t *testing.T) {
	dir := t.TempDir()
	existingFile := filepath.Join(dir, "exists.go")
	missingFile := filepath.Join(dir, "missing.go")
	touch(t, dir, "exists.go")

	now := time.Now()
	artifacts := []Artifact{
		{Path: existingFile, Tool: "Write", InsideCWD: true, CreatedAt: now},
		{Path: missingFile, Tool: "Edit", InsideCWD: true, CreatedAt: now},
	}

	result := ReconcileArtifacts(artifacts)

	if len(result) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(result))
	}
	if !result[0].Confirmed {
		t.Errorf("existing file artifact: Confirmed = false, want true")
	}
	if result[1].Confirmed {
		t.Errorf("missing file artifact: Confirmed = true, want false")
	}
	// Original slice should not be modified
	if artifacts[0].Confirmed {
		t.Error("original artifact was modified, want unmodified")
	}
	if artifacts[1].Confirmed {
		t.Error("original artifact was modified, want unmodified")
	}
}
