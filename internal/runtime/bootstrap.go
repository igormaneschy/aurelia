package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// requiredDirs lists all directories Bootstrap must ensure exist.
// Order does not matter — MkdirAll creates parent paths as needed.
var requiredDirs = []func(*PathResolver) string{
	(*PathResolver).Config,
	(*PathResolver).Data,
	(*PathResolver).Memory,
	(*PathResolver).MemoryPersonas,
	(*PathResolver).Agents,
	(*PathResolver).Profiles,
}

// Bootstrap creates the full instance directory tree with 0700 permissions.
// It is safe to call multiple times — existing directories and files are not modified.
// On Windows, the 0700 permission argument is accepted but has no effect (ACL-based permissions).
func Bootstrap(r *PathResolver) error {
	for _, dir := range requiredDirs {
		if err := os.MkdirAll(dir(r), 0700); err != nil {
			return fmt.Errorf("runtime: bootstrap failed to create %q: %w", dir(r), err)
		}
	}
	// Ensure the global memory index exists so loadMemoryDir can discover memory files.
	globalIndex := filepath.Join(r.Memory(), "MEMORY.md")
	if _, err := os.Stat(globalIndex); os.IsNotExist(err) {
		if err := os.WriteFile(globalIndex, nil, 0600); err != nil {
			return fmt.Errorf("runtime: create global memory index: %w", err)
		}
	}
	return nil
}

// BootstrapProjectMemory is a no-op since project team memory was removed (v0.31.0+).
// Kept for API compatibility; returns nil.
func BootstrapProjectMemory(r *PathResolver, cwd string) error {
	return nil
}

// BootstrapConversationProjectMemory ensures project-scoped memory directories
// exist for the bound project: cwd overlay only.
// Project team memory removed in v0.31.0 — redundant with cwd_overlay.
func BootstrapConversationProjectMemory(r *PathResolver, cwd string, chatID int64, threadID int) error {
	if strings.TrimSpace(cwd) == "" {
		return nil
	}

	dirs := []string{
		r.ProjectCwdOverlayDir(cwd),
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("runtime: bootstrap conversation project memory %q: %w", dir, err)
		}
		indexPath := filepath.Join(dir, "MEMORY.md")
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			if err := os.WriteFile(indexPath, nil, 0600); err != nil {
				return fmt.Errorf("runtime: create memory index %q: %w", indexPath, err)
			}
		}
	}
	return nil
}
