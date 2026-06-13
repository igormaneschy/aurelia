package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestHasPIAuthAt_RejectsRegularFileInIsolatedPath(t *testing.T) {
	// Given ~/.aurelia/pi-agent/auth.json is a regular file (stale copy),
	// hasPIAuthAt must NOT accept it as valid PI auth. Only a symlink to
	// ~/.pi/agent/auth.json should be trusted for the isolated path.

	tmpHome := t.TempDir()

	// Create PI CLI auth.json with a provider key
	piAuthPath := filepath.Join(tmpHome, ".pi", "agent", "auth.json")
	if err := os.MkdirAll(filepath.Dir(piAuthPath), 0700); err != nil {
		t.Fatal(err)
	}
	piAuth := map[string]any{"anthropic": map[string]any{"key": "sk-ant-real"}}
	piAuthData, err := json.Marshal(piAuth)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(piAuthPath, piAuthData, 0600); err != nil {
		t.Fatal(err)
	}

	// Create isolated auth.json as a REGULAR FILE (not a symlink)
	isolatedDir := filepath.Join(tmpHome, ".aurelia", "pi-agent")
	if err := os.MkdirAll(isolatedDir, 0700); err != nil {
		t.Fatal(err)
	}
	isolatedAuthPath := filepath.Join(isolatedDir, "auth.json")
	// Write stale credentials as a regular file
	staleAuth := map[string]any{"anthropic": map[string]any{"key": "sk-ant-stale"}}
	staleData, err := json.Marshal(staleAuth)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(isolatedAuthPath, staleData, 0600); err != nil {
		t.Fatal(err)
	}

	// The isolated path is a regular file, not a symlink.
	_, linkErr := os.Readlink(isolatedAuthPath)
	if linkErr == nil {
		t.Fatal("expected regular file, but os.Readlink succeeded")
	}

	// hasPIAuthAt should fall through to the PI CLI path and return true.
	if !hasPIAuthAt(tmpHome, "anthropic") {
		t.Fatal("hasPIAuthAt(anthropic) should return true from PI CLI path")
	}

	// Now remove the PI CLI auth and verify the isolated regular file
	// is NOT trusted on its own.
	if err := os.Remove(piAuthPath); err != nil {
		t.Fatal(err)
	}
	if hasPIAuthAt(tmpHome, "anthropic") {
		t.Fatal("hasPIAuthAt(anthropic) should return false when only a regular-file isolated auth exists")
	}
}

func TestHasPIAuthAt_AcceptsValidSymlink(t *testing.T) {
	// Given ~/.aurelia/pi-agent/auth.json is a symlink to ~/.pi/agent/auth.json,
	// hasPIAuthAt should trust it via the symlink.

	tmpHome := t.TempDir()

	// Create PI CLI auth.json
	piAuthPath := filepath.Join(tmpHome, ".pi", "agent", "auth.json")
	if err := os.MkdirAll(filepath.Dir(piAuthPath), 0700); err != nil {
		t.Fatal(err)
	}
	piAuth := map[string]any{"anthropic": map[string]any{"key": "sk-ant-real"}}
	piAuthData, err := json.Marshal(piAuth)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(piAuthPath, piAuthData, 0600); err != nil {
		t.Fatal(err)
	}

	// Create isolated dir and symlink
	isolatedDir := filepath.Join(tmpHome, ".aurelia", "pi-agent")
	if err := os.MkdirAll(isolatedDir, 0700); err != nil {
		t.Fatal(err)
	}
	isolatedAuthPath := filepath.Join(isolatedDir, "auth.json")
	if err := os.Symlink(piAuthPath, isolatedAuthPath); err != nil {
		t.Fatal(err)
	}

	// Verify symlink target matches PI CLI path
	linkTarget, err := os.Readlink(isolatedAuthPath)
	if err != nil {
		t.Fatal(err)
	}
	if linkTarget != piAuthPath {
		t.Fatalf("symlink target = %q, want %q", linkTarget, piAuthPath)
	}

	// hasPIAuthAt should return true via the symlink
	if !hasPIAuthAt(tmpHome, "anthropic") {
		t.Fatal("hasPIAuthAt(anthropic) should return true via valid symlink")
	}
}

func TestHasPIAuthAt_MissingProvider_ReturnsFalse(t *testing.T) {
	tmpHome := t.TempDir()

	piAuthPath := filepath.Join(tmpHome, ".pi", "agent", "auth.json")
	if err := os.MkdirAll(filepath.Dir(piAuthPath), 0700); err != nil {
		t.Fatal(err)
	}
	// Write auth without the requested provider
	piAuth := map[string]any{"openai": map[string]any{"key": "sk-openai"}}
	piAuthData, err := json.Marshal(piAuth)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(piAuthPath, piAuthData, 0600); err != nil {
		t.Fatal(err)
	}

	if hasPIAuthAt(tmpHome, "anthropic") {
		t.Fatal("hasPIAuthAt(anthropic) should return false when anthropic not in auth")
	}
}

func TestHasPIAuthAt_NoAuthFiles_ReturnsFalse(t *testing.T) {
	tmpHome := t.TempDir()
	if hasPIAuthAt(tmpHome, "anthropic") {
		t.Fatal("hasPIAuthAt(anthropic) should return false when no auth files exist")
	}
}

func TestGoSafe_RecoversFromPanic(t *testing.T) {
	// goSafe must recover panics and log them instead of crashing.
	// We test by launching a goroutine that panics and verifying
	// the test doesn't itself panic.

	done := make(chan struct{})
	goSafe("test-panicker", func() {
		defer close(done)
		panic("test intentional panic")
	})

	// Wait for panic recovery; should not crash the test process.
	<-done
	// If we reach here, goSafe recovered the panic successfully.
}

func TestGoSafe_NonPanic_RunsNormally(t *testing.T) {
	done := make(chan struct{})
	var ran bool
	goSafe("test-normal", func() {
		ran = true
		close(done)
	})

	<-done
	if !ran {
		t.Fatal("goSafe fn did not run")
	}
}
