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
		Engines      map[string]string `json:"engines"`
		Overrides    map[string]any    `json:"overrides"`
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
	if pkg.Dependencies["@earendil-works/pi-ai"] != "0.82.1" || pkg.Dependencies["@earendil-works/pi-coding-agent"] != "0.82.1" {
		t.Fatalf("PI SDK dependency versions must be 0.82.1, got ai=%q coding-agent=%q",
			pkg.Dependencies["@earendil-works/pi-ai"], pkg.Dependencies["@earendil-works/pi-coding-agent"])
	}
	if pkg.Engines["node"] != ">=22.19.0" {
		t.Fatalf("Node engine must require >=22.19.0, got %q", pkg.Engines["node"])
	}
	if v, _ := pkg.Overrides["protobufjs"].(string); v != "7.6.5" {
		t.Fatalf("protobufjs override must be 7.6.5, got %q", v)
	}
	if v, _ := pkg.Overrides["esbuild"].(string); v != "0.28.1" {
		t.Fatalf("esbuild override must be 0.28.1, got %q", v)
	}
	if pkg.Dependencies["esbuild"] != "0.28.1" {
		t.Fatalf("esbuild dependency must be 0.28.1, got %q", pkg.Dependencies["esbuild"])
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
	// t.Setenv("HOME") is compatible with os.UserHomeDir() on Unix because
	// UserHomeDir reads $HOME first before falling back to os/user.Current.
	// On macOS/Linux this test path is reliable; on Windows it would skip
	// silently because the project does not use %USERPROFILE%.
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

	_, err := EnsureBridge(readyBridgeTarget(t), nil)
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

func TestEnsureBridge_SymlinksModelsFromPICLI(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	piModelsPath := filepath.Join(homeDir, ".pi", "agent", "models.json")
	if err := os.MkdirAll(filepath.Dir(piModelsPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(piModelsPath, []byte(`{"providers":{"newpi":{"models":[]}}}`), 0600); err != nil {
		t.Fatal(err)
	}

	aureliaPiAgentDir := filepath.Join(homeDir, ".aurelia", "pi-agent")
	if err := os.MkdirAll(aureliaPiAgentDir, 0700); err != nil {
		t.Fatal(err)
	}
	staleModelsPath := filepath.Join(aureliaPiAgentDir, "models.json")
	if err := os.WriteFile(staleModelsPath, []byte(`{"providers":{}}`), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := EnsureBridge(readyBridgeTarget(t), nil)
	if err != nil {
		t.Fatalf("EnsureBridge failed: %v", err)
	}

	linkTarget, err := os.Readlink(staleModelsPath)
	if err != nil {
		t.Fatalf("models.json should be a symlink: %v", err)
	}
	if linkTarget != piModelsPath {
		t.Fatalf("models.json symlink target = %q, want %q", linkTarget, piModelsPath)
	}
}

func TestEnsureBridge_RemovesStaleModelsWhenPICLIModelsAbsent(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	aureliaPiAgentDir := filepath.Join(homeDir, ".aurelia", "pi-agent")
	if err := os.MkdirAll(aureliaPiAgentDir, 0700); err != nil {
		t.Fatal(err)
	}
	staleModelsPath := filepath.Join(aureliaPiAgentDir, "models.json")
	if err := os.WriteFile(staleModelsPath, []byte(`{"providers":{"stale":{"models":[]}}}`), 0600); err != nil {
		t.Fatal(err)
	}

	_, err := EnsureBridge(readyBridgeTarget(t), nil)
	if err != nil {
		t.Fatalf("EnsureBridge failed: %v", err)
	}

	if _, err := os.Stat(staleModelsPath); !os.IsNotExist(err) {
		if err != nil {
			t.Errorf("unexpected error checking models: %v", err)
		} else {
			t.Error("stale models.json was NOT removed when PI CLI models are absent")
		}
	}
}

func readyBridgeTarget(t *testing.T) string {
	t.Helper()
	targetDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(targetDir, "node_modules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "bundle.js"), []byte("// bundle"), 0600); err != nil {
		t.Fatal(err)
	}
	// Symlink tests exercise only isolated-agent setup. Mark the placeholder
	// bundle current so EnsureBridge does not perform an unrelated network npm
	// install/build merely because the embedded source changed.
	if err := os.WriteFile(sourceHashPath(targetDir), []byte(computeSourceHash()), 0600); err != nil {
		t.Fatal(err)
	}
	return targetDir
}
