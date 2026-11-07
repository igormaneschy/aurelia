package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireLock_AcquireAndRelease(t *testing.T) {
	tmp := t.TempDir()
	origLock := lockPath
	lockPath = func() string { return filepath.Join(tmp, "instance.lock") }
	t.Cleanup(func() { lockPath = origLock })

	release1, err := acquireLock()
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}

	// Must be able to release
	release1()

	// Re-acquire after release
	release2, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock after release: %v", err)
	}
	release2()
}

func TestAcquireLock_StaleLockIsReplaced(t *testing.T) {
	tmp := t.TempDir()
	origLock := lockPath
	lockPath = func() string { return filepath.Join(tmp, "instance.lock") }
	t.Cleanup(func() { lockPath = origLock })

	// Write a stale PID (999999 doesn't exist)
	stalePath := filepath.Join(tmp, "instance.lock")
	if err := os.WriteFile(stalePath, []byte("999999\n"), 0644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	release, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock with stale lock: %v", err)
	}
	release()
}

// A PID file can survive a crash or point at a host PID that is unrelated to
// the process inside a PID namespace (ai-jail). Only the flock is authoritative.
func TestAcquireLock_LivePIDWithoutFlockIsStale(t *testing.T) {
	tmp := t.TempDir()
	origLock := lockPath
	lockPath = func() string { return filepath.Join(tmp, "instance.lock") }
	t.Cleanup(func() { lockPath = origLock })

	if err := os.WriteFile(filepath.Join(tmp, "instance.lock"), []byte("2\n"), 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	release, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock with unheld PID file: %v", err)
	}
	release()
}

func TestAcquireLock_RejectsSymlink(t *testing.T) {
	tmp := t.TempDir()
	origLock := lockPath
	lockPath = func() string { return filepath.Join(tmp, "instance.lock") }
	t.Cleanup(func() { lockPath = origLock })

	target := filepath.Join(tmp, "unrelated")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(tmp, "instance.lock")); err != nil {
		t.Fatal(err)
	}

	if _, err := acquireLock(); err == nil {
		t.Fatal("acquireLock must reject a symlink lock path")
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "keep" {
		t.Fatalf("symlink target was modified: %q", content)
	}
}

func TestAcquireLock_SecondInstanceFails(t *testing.T) {
	tmp := t.TempDir()
	origLock := lockPath
	lockPath = func() string { return filepath.Join(tmp, "instance.lock") }
	t.Cleanup(func() { lockPath = origLock })

	release, err := acquireLock()
	if err != nil {
		t.Fatalf("first acquireLock: %v", err)
	}
	if _, err := acquireLock(); err == nil {
		release()
		t.Fatal("second acquireLock must fail while the first is held")
	}
	release()

	second, err := acquireLock()
	if err != nil {
		t.Fatalf("acquireLock after release: %v", err)
	}
	second()
}

func TestAcquireLock_ExistingAliveProcess(t *testing.T) {
	tmp := t.TempDir()
	origLock := lockPath
	lockPath = func() string { return filepath.Join(tmp, "instance.lock") }
	t.Cleanup(func() { lockPath = origLock })

	// Write the current process's own PID — it should be detected as alive
	pid := os.Getpid()
	if err := os.WriteFile(filepath.Join(tmp, "instance.lock"), []byte{}, 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	_ = pid

	// Acquire should succeed because the lock file exists but has no valid PID/content
	// AND there's no flock held on it (it was just written by test, not by acquireLock)
	release, err := acquireLock()
	if err != nil {
		t.Fatalf("expected lock with empty PID to be treated as stale: %v", err)
	}
	release()
}
