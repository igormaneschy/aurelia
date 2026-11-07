package memorycontext

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var scopeValuePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// Scope maps one Aurelia conversation to the ai-memory project and session
// identity. Project is derived from the nearest .ai-memory.toml marker.
type Scope struct {
	Workspace string
	Project   string
	CWD       string
	UserID    int64
	ChatID    int64
	ThreadID  int
	SessionID string
}

// HasProject reports whether a durable ai-memory project is resolved.
func (s Scope) HasProject() bool {
	return s.Workspace != "" && s.Project != ""
}

// ProjectRef returns workspace/project or an empty string.
func (s Scope) ProjectRef() string {
	if !s.HasProject() {
		return ""
	}
	return s.Workspace + "/" + s.Project
}

// Key returns a cache key that includes the conversation identity.
func (s Scope) Key() string {
	return fmt.Sprintf("%s|%s|%d|%d|%d", s.ProjectRef(), s.CWD, s.UserID, s.ChatID, s.ThreadID)
}

// ConversationTag returns the per-conversation tag used when a project is
// shared by multiple conversations. It is empty for the implicit DM (0/0).
func (s Scope) ConversationTag() string {
	if s.ChatID == 0 && s.ThreadID == 0 {
		return ""
	}
	return fmt.Sprintf("chat:%d/thread:%d", s.ChatID, s.ThreadID)
}

// ResolveScope resolves the project marker and fills the conversation fields.
// A missing marker is not an error: the scope remains without a project so the
// caller can return an empty working set instead of guessing another project.
func ResolveScope(cwd string, userID, chatID int64, threadID int, sessionID string) (Scope, error) {
	scope := Scope{
		UserID:    userID,
		ChatID:    chatID,
		ThreadID:  threadID,
		SessionID: sessionID,
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return scope, nil
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return scope, fmt.Errorf("memory scope: resolve cwd %q: %w", cwd, err)
	}
	scope.CWD = filepath.Clean(abs)
	marker, ok := findMarker(scope.CWD)
	if !ok {
		// Mirror ai-memory's default project resolution: the git root name,
		// or the cwd basename when there is no repository metadata.
		if project := fallbackProjectName(scope.CWD); project != "" {
			scope.Workspace = "default"
			scope.Project = project
		}
		return scope, nil
	}
	workspace, project, err := readMarker(marker)
	if err != nil {
		return scope, fmt.Errorf("memory scope: %w", err)
	}
	scope.Workspace = workspace
	scope.Project = project
	return scope, nil
}

func fallbackProjectName(cwd string) string {
	current := cwd
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return validProjectName(filepath.Base(current))
		}
		parent := filepath.Dir(current)
		if parent == current {
			return validProjectName(filepath.Base(cwd))
		}
		current = parent
	}
}

func validProjectName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || !scopeValuePattern.MatchString(name) || name == "/" {
		return ""
	}
	return name
}

func findMarker(start string) (string, bool) {
	current := start
	for {
		candidate := filepath.Join(current, ".ai-memory.toml")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func readMarker(path string) (string, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("read marker %s: %w", path, err)
	}
	workspace := ""
	project := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "workspace":
			workspace = value
		case "project":
			project = value
		}
	}
	for name, value := range map[string]string{"workspace": workspace, "project": project} {
		if value != "" && !scopeValuePattern.MatchString(value) {
			return "", "", fmt.Errorf("invalid %s %q in %s", name, value, path)
		}
	}
	if (workspace == "") != (project == "") {
		return "", "", fmt.Errorf("marker %s must define both workspace and project", path)
	}
	return workspace, project, nil
}
