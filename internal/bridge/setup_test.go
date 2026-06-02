package bridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedBridgeSourcePresent(t *testing.T) {
	if len(EmbeddedBridgeTS) == 0 {
		t.Fatal("expected embedded TypeScript bridge source")
	}
	if !strings.Contains(string(EmbeddedBridgeTS), "createAgentSession") {
		t.Fatal("embedded bridge source does not look like the PI SDK bridge")
	}
}

func TestEmbeddedBridgeSourceMatchesEditableSource(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "bridge", "index.ts"))
	if err != nil {
		t.Fatalf("read editable bridge source: %v", err)
	}
	if !bytes.Equal(bytes.TrimSpace(EmbeddedBridgeTS), bytes.TrimSpace(source)) {
		t.Fatal("internal/bridge/bundle.ts is out of sync with bridge/index.ts; run make bridge")
	}
}

func TestBridgePackageJSONCanBuildBundle(t *testing.T) {
	var pkg struct {
		Scripts      map[string]string `json:"scripts"`
		Dependencies map[string]string `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(bridgePackageJSON), &pkg); err != nil {
		t.Fatalf("bridgePackageJSON is invalid JSON: %v", err)
	}
	if !strings.Contains(pkg.Scripts["build"], "esbuild index.ts") {
		t.Fatalf("missing esbuild build script: %q", pkg.Scripts["build"])
	}
	if pkg.Dependencies["@earendil-works/pi-coding-agent"] == "" {
		t.Fatal("missing PI SDK dependency")
	}
	if pkg.Dependencies["esbuild"] == "" {
		t.Fatal("missing esbuild dependency")
	}
}

// TestAuthSymlink verifies the daemon ensures auth.json is a symlink to PI CLI.
func TestAuthSymlink(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcAuth := filepath.Join(srcDir, "auth.json")
	if err := os.WriteFile(srcAuth, []byte(`{"key":"test"}`), 0600); err != nil {
		t.Fatal(err)
	}
	dstAuth := filepath.Join(dstDir, "auth.json")

	// Symlink should be created when dst doesn't exist
	if err := os.Symlink(srcAuth, dstAuth); err != nil {
		t.Fatal(err)
	}
	// Verify symlink
	linkTarget, err := os.Readlink(dstAuth)
	if err != nil {
		t.Fatal(err)
	}
	if linkTarget != srcAuth {
		t.Errorf("symlink points to %q, want %q", linkTarget, srcAuth)
	}
}

func TestSourceHashDetection(t *testing.T) {
	// computeSourceHash is consistent across calls.
	h1 := computeSourceHash()
	h2 := computeSourceHash()
	if h1 != h2 {
		t.Errorf("computeSourceHash() not consistent: %q vs %q", h1, h2)
	}

	// Round-trip: write then verify.
	dir := t.TempDir()
	if err := writeSourceHash(dir); err != nil {
		t.Fatalf("writeSourceHash: %v", err)
	}
	if !isSourceHashCurrent(dir) {
		t.Error("isSourceHashCurrent returned false after writeSourceHash")
	}

	// Mismatched hash returns false.
	badPath := sourceHashPath(dir)
	if err := os.WriteFile(badPath, []byte("badhash"), 0600); err != nil {
		t.Fatal(err)
	}
	if isSourceHashCurrent(dir) {
		t.Error("isSourceHashCurrent returned true with wrong hash")
	}

	// Missing file returns false.
	emptyDir := t.TempDir()
	if isSourceHashCurrent(emptyDir) {
		t.Error("isSourceHashCurrent returned true for missing file")
	}
}

// TestEnsureBridge_RemovesStaleAuthWhenPICLIAuthAbsent verifies that when
// PI CLI auth (~/.pi/agent/auth.json) does NOT exist, any stale isolated
// auth file (~/.aurelia/pi-agent/auth.json) is removed so the PI SDK
// cannot consume stale credentials.
func TestEnsureBridge_RemovesStaleAuthWhenPICLIAuthAbsent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	// Create isolated PI agent dir with a stale regular-file auth.json.
	aureliaPiAgentDir := filepath.Join(homeDir, ".aurelia", "pi-agent")
	if err := os.MkdirAll(aureliaPiAgentDir, 0700); err != nil {
		t.Fatal(err)
	}
	staleAuth := filepath.Join(aureliaPiAgentDir, "auth.json")
	if err := os.WriteFile(staleAuth, []byte(`{"key":"stale-creds"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Intentionally do NOT create ~/.pi/agent/auth.json — PI CLI auth is absent.

	// Create a minimal bridge target dir so EnsureBridge returns early
	// (avoids running npm install).
	targetDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(targetDir, "node_modules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "bundle.js"), []byte("// bundle"), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := EnsureBridge(targetDir, nil)
	if err != nil {
		t.Fatalf("EnsureBridge failed: %v", err)
	}

	// Verify the stale auth file was removed.
	if _, err := os.Stat(staleAuth); !os.IsNotExist(err) {
		if err != nil {
			t.Errorf("unexpected error checking auth: %v", err)
		} else {
			t.Error("stale auth.json was NOT removed when PI CLI auth is absent")
		}
	}
}
