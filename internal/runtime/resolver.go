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

// ProjectMemoryDir returns the per-project private memory directory:
// ~/.aurelia/projects/<sanitized-cwd>/memory/
func (r *PathResolver) ProjectMemoryDir(cwd string) string {
	return filepath.Join(r.root, "projects", ProjectSlug(cwd), "memory")
}

// ConversationProjectMemoryDir returns project-private memory for one
// conversation. This prevents notes from one Telegram group/topic leaking into
// another conversation that happens to bind the same repository.
func (r *PathResolver) ConversationProjectMemoryDir(cwd string, chatID int64, threadID int) string {
	return filepath.Join(r.ProjectMemoryDir(cwd), "conversations", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID))
}

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
// Returns empty string when threadID <= 0 (topic memory only exists in threads).
func (r *PathResolver) TopicMemoryDir(chatID int64, threadID int) string {
	if threadID <= 0 {
		return ""
	}
	return filepath.Join(r.root, "topics", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID))
}

// TopicCwdOverlayDir returns the cwd overlay memory directory for a topic.
// ~/.aurelia/topics/chat_<chatID>/thread_<threadID>/cwd_overlay/
// Only valid when /cwd is declared for the topic. Returns empty string when
// threadID <= 0.
func (r *PathResolver) TopicCwdOverlayDir(chatID int64, threadID int) string {
	if threadID <= 0 {
		return ""
	}
	return filepath.Join(r.root, "topics", fmt.Sprintf("chat_%d", chatID), fmt.Sprintf("thread_%d", threadID), "cwd_overlay")
}
