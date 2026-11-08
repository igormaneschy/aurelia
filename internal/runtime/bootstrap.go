package runtime

import (
	"fmt"
	"os"
	"path/filepath"
)

// chatModeMarker isolates desktop/chat-mode sessions from project memories.
// It disables ai-memory handoff/recovery delivery for the neutral chat scope.
const chatModeMarker = `workspace = "personal"
project = "aurelia-chat"
deliver_on_session_start = false

[recovery]
deliver_on_session_start = false
`

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
	return ensureChatModeMarker(r)
}

// ensureChatModeMarker creates the neutral chat-mode ai-memory marker when the
// instance root has no marker yet. Existing markers are never overwritten.
func ensureChatModeMarker(r *PathResolver) error {
	path := filepath.Join(r.Root(), ".ai-memory.toml")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("runtime: stat chat-mode marker %q: %w", path, err)
	}
	if err := os.WriteFile(path, []byte(chatModeMarker), 0600); err != nil {
		return fmt.Errorf("runtime: create chat-mode marker %q: %w", path, err)
	}
	return nil
}
