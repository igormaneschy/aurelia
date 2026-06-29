package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const envKey = "AURELIA_HOME"
const allowedCwdPrefixesEnv = "AURELIA_ALLOWED_CWD_PREFIXES"
const defaultDir = ".aurelia"

// PathResolver resolves and exposes all instance-directory paths.
type PathResolver struct {
	root string
}

// New returns a PathResolver whose root is:
//   - $AURELIA_HOME if set and non-empty
//   - $HOME/.aurelia otherwise
//
// Returns a descriptive error if $AURELIA_HOME is unset and os.UserHomeDir() fails.
func New() (*PathResolver, error) {
	if override := os.Getenv(envKey); override != "" {
		return &PathResolver{root: override}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("runtime: cannot resolve instance root: os.UserHomeDir failed and %s is not set: %w", envKey, err)
	}
	return &PathResolver{root: filepath.Join(home, defaultDir)}, nil
}

// Root returns the instance root directory.
func (r *PathResolver) Root() string { return r.root }

// Config returns the path to the config/ subdirectory.
func (r *PathResolver) Config() string { return filepath.Join(r.root, "config") }

// AppConfig returns the path to the main app config JSON file.
func (r *PathResolver) AppConfig() string { return filepath.Join(r.Config(), "app.json") }

// Data returns the path to the data/ subdirectory.
func (r *PathResolver) Data() string { return filepath.Join(r.root, "data") }

// Memory returns the path to the memory/ subdirectory.
func (r *PathResolver) Memory() string { return filepath.Join(r.root, "memory") }

// MemoryPersonas returns the path to the memory/personas/ subdirectory.
func (r *PathResolver) MemoryPersonas() string { return filepath.Join(r.root, "memory", "personas") }

// Agents returns the path to the agents/ subdirectory.
func (r *PathResolver) Agents() string { return filepath.Join(r.root, "agents") }

// Profiles returns the path to the profiles/ subdirectory (canonical storage).
func (r *PathResolver) Profiles() string { return filepath.Join(r.root, "profiles") }

// DBPath returns the path to a named database file inside the data/ subdirectory.
func (r *PathResolver) DBPath(name string) string { return filepath.Join(r.Data(), name) }

// SanitizeCwd converts an absolute path to a Claude Code-style sanitized key.
// Slashes become dashes, drive prefixes are stripped.
// Example: /home/user/code/my-project → -home-user-code-my-project
func SanitizeCwd(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	return strings.ReplaceAll(filepath.ToSlash(filepath.Clean(cwd)), "/", "-")
}

// resolveRealPath resolves the on-disk casing of each path component by
// walking the filesystem. On case-insensitive filesystems (macOS APFS, Windows NTFS),
// this ensures that two paths with different casing for the same directory produce
// the same canonical path. Symlinks are resolved first via filepath.EvalSymlinks.
//
// If the path doesn't exist or a component can't be read, the original component
// name is preserved (best-effort normalization).
func resolveRealPath(path string) (string, error) {
	// Resolve symlinks first to get the real path.
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		// If path doesn't exist, return the cleaned absolute form.
		abs, aErr := filepath.Abs(path)
		if aErr != nil {
			return "", fmt.Errorf("resolve real path %q: %w", path, aErr)
		}
		return filepath.Clean(abs), nil
	}

	// Walk each component to find the actual on-disk casing.
	abs := filepath.Clean(resolved)
	parts := strings.Split(abs, string(filepath.Separator))

	var result strings.Builder
	start := 0

	// Handle Windows drive letter prefix (e.g., "C:\")
	if len(parts) > 0 && strings.HasSuffix(parts[0], ":") {
		result.WriteString(parts[0])
		result.WriteByte(filepath.Separator)
		start = 1
	} else if strings.HasPrefix(abs, string(filepath.Separator)) {
		// Preserve leading separator for absolute Unix paths.
		result.WriteByte(filepath.Separator)
	}

	for i, part := range parts[start:] {
		if part == "" {
			continue
		}

		parent := result.String()
		if parent == "" {
			parent = string(filepath.Separator)
		}

		entries, err := os.ReadDir(parent)
		if err != nil {
			// Can't read directory, keep original component.
			if i > 0 || result.Len() > 0 {
				result.WriteByte(filepath.Separator)
			}
			result.WriteString(part)
			continue
		}

		found := false
		for _, e := range entries {
			if strings.EqualFold(e.Name(), part) {
				if i > 0 || result.Len() > 1 {
					result.WriteByte(filepath.Separator)
				}
				result.WriteString(e.Name())
				found = true
				break
			}
		}
		if !found {
			if i > 0 || result.Len() > 1 {
				result.WriteByte(filepath.Separator)
			}
			result.WriteString(part)
		}
	}

	return filepath.Clean(result.String()), nil
}

// normalizeProjectCwdInput normalizes a raw user-supplied path before resolution.
// It removes common Telegram/Markdown wrappers and expands ~.
func normalizeProjectCwdInput(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", fmt.Errorf("empty cwd")
	}

	// Strip one balanced pair of wrapping backticks, single quotes, or double quotes.
	for _, quote := range []string{"`", "'", "\""} {
		if len(s) >= 2 && strings.HasPrefix(s, quote) && strings.HasSuffix(s, quote) {
			s = s[len(quote) : len(s)-len(quote)]
			s = strings.TrimSpace(s)
			break
		}
	}
	if s == "" {
		return "", fmt.Errorf("empty cwd after stripping surrounding quotes")
	}

	// Expand ~ and ~/ to home directory. Reject ~otheruser.
	if strings.HasPrefix(s, "~") {
		if len(s) == 1 || s[1] == '/' {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("expand ~: cannot determine home directory: %w", err)
			}
			if len(s) == 1 {
				s = home
			} else {
				s = filepath.Join(home, s[2:])
			}
		} else {
			return "", fmt.Errorf("expand ~user (%q) is not supported; use an absolute path or ~/...", s)
		}
	}

	return s, nil
}

// ResolveProjectCwd validates and canonicalizes a user-provided working-directory path.
// The path must exist, be a directory, and not be a sensitive or disallowed location
// (root, home, ~/.ssh, ~/.config, ~/.aurelia). Unlike earlier versions, it does NOT
// require project markers (.git, go.mod, etc.) — plain workspace directories are valid.
//
// Project bindings use this so equivalent paths such as /repo/app and
// /repo/app/ map to the same persisted cwd and project memory slug.
func ResolveProjectCwd(path string) (string, error) {
	normalized, err := normalizeProjectCwdInput(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(normalized)
	if err != nil {
		return "", fmt.Errorf("resolve absolute cwd %q: %w", path, err)
	}
	clean := filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("resolve cwd symlinks %q: %w", clean, err)
	}
	// Resolve on-disk casing so equivalent paths with different casing
	// (e.g. on macOS APFS case-insensitive) produce the same canonical path.
	resolved, err = resolveRealPath(resolved)
	if err != nil {
		return "", fmt.Errorf("resolve real path %q: %w", resolved, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("stat cwd %q: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd %q is not a directory", resolved)
	}
	if err := rejectSensitiveProjectCwd(resolved); err != nil {
		return "", err
	}
	cleanResolved := filepath.Clean(resolved)
	if err := validateAuthorizedProjectCwdPrefix(cleanResolved); err != nil {
		return "", err
	}
	return cleanResolved, nil
}

func validateAuthorizedProjectCwdPrefix(cwd string) error {
	clean := filepath.Clean(cwd)
	for _, prefix := range authorizedProjectCwdPrefixes() {
		if isPathWithinPrefix(clean, prefix) {
			return nil
		}
	}
	return fmt.Errorf("cwd %q is outside authorized project prefixes; set %s to allow this workspace", clean, allowedCwdPrefixesEnv)
}

func authorizedProjectCwdPrefixes() []string {
	if raw := strings.TrimSpace(os.Getenv(allowedCwdPrefixesEnv)); raw != "" {
		return cleanExistingPrefixes(filepath.SplitList(raw))
	}
	candidates := []string{os.TempDir(), "/Volumes", "/mnt", "/media"}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, home)
	}
	return cleanExistingPrefixes(candidates)
}

func cleanExistingPrefixes(candidates []string) []string {
	prefixes := make([]string, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		clean := filepath.Clean(abs)
		if resolved, err := filepath.EvalSymlinks(clean); err == nil {
			clean = filepath.Clean(resolved)
		}
		if seen[clean] {
			continue
		}
		if info, err := os.Stat(clean); err == nil && info.IsDir() {
			seen[clean] = true
			prefixes = append(prefixes, clean)
		}
	}
	return prefixes
}

func isPathWithinPrefix(path string, prefix string) bool {
	rel, err := filepath.Rel(prefix, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func rejectSensitiveProjectCwd(cwd string) error {
	clean := filepath.Clean(cwd)
	if clean == string(filepath.Separator) {
		return fmt.Errorf("cwd %q is not an allowed project directory", clean)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	home = filepath.Clean(home)
	if clean == home {
		return fmt.Errorf("cwd %q is not an allowed project directory", clean)
	}
	blockedPrefixes := []string{
		filepath.Join(home, ".ssh"),
		filepath.Join(home, ".config"),
		filepath.Join(home, ".aurelia"),
	}
	for _, prefix := range blockedPrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+string(filepath.Separator)) {
			return fmt.Errorf("cwd %q is not an allowed project directory", clean)
		}
	}
	return nil
}

// ProjectSlug returns the filesystem-safe key used for project-scoped state.
func ProjectSlug(cwd string) string { return SanitizeCwd(cwd) }

// ProjectTeamMemoryDir returns the per-project team (shared) memory directory:
// ~/.aurelia/projects/<sanitized-cwd>/team/
func (r *PathResolver) ProjectTeamMemoryDir(cwd string) string {
	return filepath.Join(r.root, "projects", ProjectSlug(cwd), "team")
}

// UserMemoryDir returns the per-user global memory directory.
// ~/.aurelia/users/<userID>/memory/
func (r *PathResolver) UserMemoryDir(userID int64) string {
	return filepath.Join(r.root, "users", fmt.Sprintf("%d", userID), "memory")
}

// TopicMemoryDir returns the topic-scoped memory directory.
// ~/.aurelia/topics/chat_<chatID>/thread_<threadID>/
// Returns empty string when threadID <= 0 (topic memory only exists in forum threads).
func (r *PathResolver) TopicMemoryDir(chatID int64, threadID int) string {
	if threadID <= 0 {
		return ""
	}
	return filepath.Join(r.root, "topics", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID))
}

// ProjectCwdOverlayDir returns the project-scoped CWD overlay memory directory:
// ~/.aurelia/projects/<sanitized-cwd>/cwd_overlay/
//
// Unlike TopicCwdOverlayDir, this path is independent of chat/thread — all
// conversations with the same /cwd share the same cwd_overlay memory.
// This is the canonical path for project-scoped memory.
//
// Returns empty string when cwd is empty.
func (r *PathResolver) ProjectCwdOverlayDir(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return ""
	}
	return filepath.Join(r.root, "projects", ProjectSlug(cwd), "cwd_overlay")
}

// TopicCwdOverlayDir returns the cwd overlay memory directory for a topic.
// ~/.aurelia/topics/chat_<chatID>/thread_<threadID>/cwd_overlay/
// For private chats (threadID == 0), returns ~/.aurelia/topics/chat_<chatID>/cwd_overlay/
//
// Deprecated: Use ProjectCwdOverlayDir(cwd) instead. Topic-scoped cwd_overlay
// fragments project memory across frontends (TUI vs Telegram).
//
// Caller is responsible for verifying that /cwd is actually active for the
// chat/topic before using the returned path. This method only computes the
// path; it does not check project binding state.
//
// Returns empty string when chatID == 0.
func (r *PathResolver) TopicCwdOverlayDir(chatID int64, threadID int) string {
	if chatID == 0 {
		return ""
	}
	if threadID <= 0 {
		return filepath.Join(r.root, "topics", fmt.Sprintf("chat_%d", chatID), "cwd_overlay")
	}
	return filepath.Join(r.root, "topics", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID), "cwd_overlay")
}
