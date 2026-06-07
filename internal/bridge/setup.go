package bridge

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const bridgePackageJSON = `{
  "name": "aurelia-bridge",
  "version": "1.0.0",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "esbuild index.ts --bundle --platform=node --target=node18 --outfile=bundle.js --format=esm --banner:js=\"import { createRequire as __piCreateRequire } from 'module';const require = __piCreateRequire(import.meta.url);\""
  },
  "dependencies": {
    "@earendil-works/pi-coding-agent": "0.74.0",
    "esbuild": "^0.28.0"
  }
}
`

// EnsureBridge checks if the bridge is set up at targetDir. If not,
// creates it with package.json, runs npm install, and builds bundle.js
// from TypeScript source. Returns the directory path.
// If bundleJS is non-nil, it is written as bundle.js (legacy embedded path).
func EnsureBridge(targetDir string, bundleJS []byte) (string, error) {
	bundlePath := filepath.Join(targetDir, "bundle.js")
	nodeModules := filepath.Join(targetDir, "node_modules")

	needsNpmInstall := false
	if _, err := os.Stat(nodeModules); os.IsNotExist(err) {
		needsNpmInstall = true
	}

	// Check if bundle.js exists and matches embedded (if provided).
	bundleExists := false
	if info, err := os.Stat(bundlePath); err == nil && info.Size() > 0 {
		if len(bundleJS) > 0 && info.Size() == int64(len(bundleJS)) {
			existing, readErr := os.ReadFile(bundlePath)
			bundleExists = readErr == nil && string(existing) == string(bundleJS)
		} else {
			bundleExists = true // already on disk, no embedded to compare
		}
	}

	// Ensure isolated PI agent directory exists and symlink auth.json
	// from PI CLI so credentials are always in sync. The isolated directory
	// prevents session/settings collisions between the daemon and interactive
	// PI usage, but auth.json must be shared — stale credentials cause silent
	// failures (the daemon uses a valid-looking old key while PI CLI uses a
	// different, working key).
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("cannot determine home directory for PI agent dir", "error", err)
	} else if home != "" {
		aureliaPiAgentDir := filepath.Join(home, ".aurelia", "pi-agent")
		if err := os.MkdirAll(aureliaPiAgentDir, 0700); err != nil {
			slog.Warn("failed to create isolated PI agent dir", "error", err)
		} else {
			piCliAuthPath := filepath.Join(home, ".pi", "agent", "auth.json")
			aureliaAuthPath := filepath.Join(aureliaPiAgentDir, "auth.json")
			if _, statErr := os.Stat(piCliAuthPath); statErr == nil {
				// Check if existing auth is already a valid symlink.
				linkTarget, linkErr := os.Readlink(aureliaAuthPath)
				if linkErr != nil || linkTarget != piCliAuthPath {
					// Remove stale regular file or broken symlink.
					if err := os.Remove(aureliaAuthPath); err != nil && !os.IsNotExist(err) {
						slog.Warn("failed to remove stale auth.json", "error", err)
					}
					if err := os.Symlink(piCliAuthPath, aureliaAuthPath); err != nil {
						slog.Warn("failed to symlink auth.json from PI CLI", "error", err)
					} else {
						slog.Info("Linked auth.json from PI CLI")
					}
				} else {
					// Symlink target path matches — verify target file actually exists.
					if _, err := os.Stat(piCliAuthPath); err != nil {
						slog.Error("bridge: auth symlink target does not exist", "target", piCliAuthPath, "error", err)
						_ = os.Remove(aureliaAuthPath)
						if linkErr := os.Symlink(piCliAuthPath, aureliaAuthPath); linkErr != nil {
							slog.Warn("failed to recreate auth symlink after broken target", "error", linkErr)
						} else {
							slog.Info("Recreated auth.json symlink after broken target")
						}
					}
				}
			} else {
				// PI CLI auth is absent — remove any stale isolated auth file
				// so the PI SDK does not consume stale credentials.
				if err := os.Remove(aureliaAuthPath); err != nil && !os.IsNotExist(err) {
					slog.Warn("failed to remove stale isolated auth.json (PI CLI auth absent)", "error", err)
				} else if err == nil {
					slog.Info("Removed stale isolated auth.json — PI CLI auth is absent")
				}
			}

			// Share only the PI CLI extensions that Aurelia needs
			// (pi-mcp-adapter and pi-web-access) via individual
			// package symlinks. We intentionally exclude
			// pi-hermes-memory because its SQLite database can
			// cause startup hangs when locked by another process.
			aureliaNpmModules := filepath.Join(aureliaPiAgentDir, "npm", "node_modules")
			piCliNpmModules := filepath.Join(home, ".pi", "agent", "npm", "node_modules")
			for _, pkg := range []string{"pi-mcp-adapter", "pi-web-access"} {
				piPkg := filepath.Join(piCliNpmModules, pkg)
				aureliaPkg := filepath.Join(aureliaNpmModules, pkg)
				if _, statErr := os.Stat(piPkg); statErr == nil {
					if err := os.MkdirAll(aureliaNpmModules, 0700); err != nil {
						slog.Warn("failed to create npm/node_modules dir", "error", err)
						continue
					}
					linkTarget, linkErr := os.Readlink(aureliaPkg)
					if linkErr != nil || linkTarget != piPkg {
						_ = os.Remove(aureliaPkg)
						if err := os.Symlink(piPkg, aureliaPkg); err != nil {
							slog.Warn("failed to symlink "+pkg+" from PI CLI", "error", err)
						} else {
							slog.Info("Linked " + pkg + " from PI CLI")
						}
					}
				}
			}

			// Share MCP configuration files so Aurelia can discover and
			// connect to the same MCP servers as PI CLI. The adapter needs
			// mcp.json (server list), mcp-cache.json (cached manifests),
			// and mcp-npx-cache.json (npx resolution cache).
			for _, name := range []string{"mcp.json", "mcp-cache.json", "mcp-npx-cache.json"} {
				piCliPath := filepath.Join(home, ".pi", "agent", name)
				aureliaLink := filepath.Join(aureliaPiAgentDir, name)
				if _, statErr := os.Stat(piCliPath); statErr == nil {
					linkTarget, linkErr := os.Readlink(aureliaLink)
					if linkErr != nil || linkTarget != piCliPath {
						_ = os.Remove(aureliaLink)
						if err := os.Symlink(piCliPath, aureliaLink); err != nil {
							slog.Warn("failed to symlink "+name+" from PI CLI", "error", err)
						} else {
							slog.Info("Linked " + name + " from PI CLI")
						}
					}
				}
			}

			// Share AGENTS.md so ai-memory routing rules (memory_query,
			// memory_write_page, etc.) are available to the model.
			piAgentsPath := filepath.Join(home, ".pi", "agent", "AGENTS.md")
			aureliaAgentsLink := filepath.Join(aureliaPiAgentDir, "AGENTS.md")
			if _, statErr := os.Stat(piAgentsPath); statErr == nil {
				linkTarget, linkErr := os.Readlink(aureliaAgentsLink)
				if linkErr != nil || linkTarget != piAgentsPath {
					_ = os.Remove(aureliaAgentsLink)
					if err := os.Symlink(piAgentsPath, aureliaAgentsLink); err != nil {
						slog.Warn("failed to symlink AGENTS.md from PI CLI", "error", err)
					} else {
						slog.Info("Linked AGENTS.md from PI CLI")
					}
				}
			}
		}
	}

	// Detect stale bundle: if bundle exists but TypeScript source changed,
	// force rebuild. This handles the case where EmbeddedBundleJS is empty
	// (no pre-built JS to compare) but the embedded TS source was updated.
	if bundleExists && len(bundleJS) == 0 && len(EmbeddedBridgeTS) > 0 {
		if !isSourceHashCurrent(targetDir) {
			slog.Info("Bridge bundle is stale (TypeScript source changed), rebuilding...")
			bundleExists = false
		}
	}

	if bundleExists && !needsNpmInstall {
		return targetDir, nil
	}

	if needsNpmInstall {
		slog.Info("Setting up Bridge for first time...")
	} else if !bundleExists {
		slog.Info("Building Bridge bundle...")
	}

	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return "", fmt.Errorf("create bridge dir: %w", err)
	}

	buildingFromSource := !bundleExists && len(bundleJS) == 0
	if buildingFromSource {
		if err := writeBridgeSource(targetDir); err != nil {
			return "", err
		}
		needsNpmInstall = true
	}

	// Write package.json and npm install first (needed for both embedded and TS build paths).
	if needsNpmInstall {
		pkgPath := filepath.Join(targetDir, "package.json")
		if err := os.WriteFile(pkgPath, []byte(bridgePackageJSON), 0600); err != nil {
			return "", fmt.Errorf("write package.json: %w", err)
		}

		slog.Info("Installing PI SDK bridge dependencies (npm install)...")
		installCtx, installCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer installCancel()
		cmd := exec.CommandContext(installCtx, "npm", "install", "--production", "--no-optional")
		// SysProcAttr.Setpgid is Unix-only (Linux/macOS). This project targets
		// Unix daemons exclusively; Windows support would require a different
		// strategy for process group cleanup.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Dir = targetDir
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		// Kill the entire process group on context cancellation so npm's children
		// (e.g. esbuild) are also terminated rather than becoming orphans.
		// cmd.Process may be nil if the context is canceled before the process starts.
		cmd.Cancel = func() error {
			if cmd.Process != nil {
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
			return nil
		}
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("npm install failed: %w", err)
		}
	}

	// Write or build bundle.js
	if !bundleExists {
		if len(bundleJS) > 0 {
			// Embedded bundle provided — write it directly.
			tmpPath := bundlePath + ".tmp"
			if err := os.WriteFile(tmpPath, bundleJS, 0600); err != nil {
				_ = os.Remove(tmpPath)
				return "", fmt.Errorf("write bundle.js.tmp: %w", err)
			}
			if err := os.Rename(tmpPath, bundlePath); err != nil {
				_ = os.Remove(tmpPath)
				return "", fmt.Errorf("rename bundle.js.tmp → bundle.js: %w", err)
			}
			if err := writeSourceHash(targetDir); err != nil {
				slog.Warn("failed to write bridge source hash", "error", err)
			}
		} else {
			// No embedded bundle — build from TypeScript source.
			slog.Info("Building Bridge from TypeScript source (esbuild)...")
			buildCtx, buildCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer buildCancel()
			cmd := exec.CommandContext(buildCtx, "npm", "run", "build")
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Dir = targetDir
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			// Kill the entire process group on context cancellation.
			// cmd.Process may be nil if the context is canceled before the process starts.
			cmd.Cancel = func() error {
				if cmd.Process != nil {
					_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				}
				return nil
			}
			if err := cmd.Run(); err != nil {
				return "", fmt.Errorf("npm run build failed: %w", err)
			}
			if err := writeSourceHash(targetDir); err != nil {
				slog.Warn("failed to write bridge source hash", "error", err)
			}
		}
	}

	slog.Info("Bridge setup complete.")
	return targetDir, nil
}

func writeBridgeSource(targetDir string) error {
	if len(EmbeddedBridgeTS) == 0 {
		return fmt.Errorf("bridge source is not embedded")
	}
	indexPath := filepath.Join(targetDir, "index.ts")
	if err := os.WriteFile(indexPath, EmbeddedBridgeTS, 0600); err != nil {
		return fmt.Errorf("write index.ts: %w", err)
	}
	return nil
}

// sourceHashPath returns the path to the bridge source hash file.
func sourceHashPath(targetDir string) string {
	return filepath.Join(targetDir, ".source-hash")
}

// computeSourceHash returns the SHA-256 hash of the embedded TypeScript source.
func computeSourceHash() string {
	return fmt.Sprintf("%x", sha256.Sum256(EmbeddedBridgeTS))
}

// isSourceHashCurrent returns true if the stored hash matches the current source hash.
func isSourceHashCurrent(targetDir string) bool {
	data, err := os.ReadFile(sourceHashPath(targetDir))
	if err != nil {
		return false
	}
	return string(data) == computeSourceHash()
}

// writeSourceHash writes the current source hash to the target directory.
func writeSourceHash(targetDir string) error {
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return err
	}
	return os.WriteFile(sourceHashPath(targetDir), []byte(computeSourceHash()), 0600)
}
