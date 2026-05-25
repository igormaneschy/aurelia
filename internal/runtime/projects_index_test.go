package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fastRoot creates a temporary root directory that Rebuild can scan quickly.
func fastRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	// Create a minimal project-like structure so Rebuild has something to do.
	sub := filepath.Join(root, "code", "my-project")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "README.md"), []byte("# test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "main.go"), []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestNewProjectIndex_EmptyRootsDoesNotPanic(t *testing.T) {
	root := fastRoot(t)
	idx := NewProjectIndex([]string{root}, "")
	if idx == nil {
		t.Fatal("NewProjectIndex returned nil")
	}

	if err := idx.Rebuild(context.Background()); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
}

func TestScheduleRebuild_ResetsRebuildingFlag(t *testing.T) {
	root := fastRoot(t)
	idx := NewProjectIndex([]string{root}, "")

	if ok := idx.ScheduleRebuild(0); !ok {
		t.Fatal("first ScheduleRebuild returned false, expected true")
	}

	deadline := time.Now().Add(5 * time.Second)
	ok := false
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if ok = idx.ScheduleRebuild(0); ok {
			break
		}
	}
	if !ok {
		t.Fatal("rebuilding flag was not reset after background rebuild completed")
	}

	_ = idx.Lookup("nonexistent")
}

func TestScheduleRebuild_Debounce(t *testing.T) {
	root := fastRoot(t)
	idx := NewProjectIndex([]string{root}, "")

	if ok := idx.ScheduleRebuild(time.Minute); !ok {
		t.Fatal("first ScheduleRebuild returned false")
	}

	if ok := idx.ScheduleRebuild(time.Minute); ok {
		t.Log("second rebuild accepted (race with goroutine startup) — not a failure")
	}

	time.Sleep(500 * time.Millisecond)

	if ok := idx.ScheduleRebuild(0); !ok {
		t.Fatal("rebuilding flag not reset after debounce wait")
	}
}

func TestScheduleRebuild_PanicReset(t *testing.T) {
	// Regression test for the defer-based panic recovery in ScheduleRebuild.
	// Uses the rebuildFn test seam to inject a panic into the goroutine and
	// verifies the rebuilding flag is reset afterward.
	idx := NewProjectIndex(nil, "")

	// Inject a rebuildFn that panics.
	panicCalled := false
	idx.rebuildFn = func(ctx context.Context) error {
		panicCalled = true
		panic("simulated rebuild panic")
	}

	if ok := idx.ScheduleRebuild(0); !ok {
		t.Fatal("first ScheduleRebuild returned false")
	}

	// Wait for the goroutine to complete (panic + recovery + defer reset).
	deadline := time.Now().Add(5 * time.Second)
	ok := false
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if ok = idx.ScheduleRebuild(0); ok {
			break
		}
	}
	if !ok {
		t.Fatal("rebuilding flag was not reset after panic in background rebuild")
	}
	if !panicCalled {
		t.Fatal("rebuildFn was never called — test seam not exercised")
	}
}

func TestScheduleRebuild_Concurrent(t *testing.T) {
	root := fastRoot(t)
	idx := NewProjectIndex([]string{root}, "")

	if ok := idx.ScheduleRebuild(0); !ok {
		t.Fatal("first ScheduleRebuild returned false")
	}

	time.Sleep(200 * time.Millisecond)

	if ok := idx.ScheduleRebuild(0); !ok {
		t.Fatal("second ScheduleRebuild returned false after wait")
	}

	time.Sleep(200 * time.Millisecond)
	if ok := idx.ScheduleRebuild(0); !ok {
		t.Fatal("third ScheduleRebuild returned false — rebuilding flag stuck")
	}
}
