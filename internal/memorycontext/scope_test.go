package memorycontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveScopeFindsMarkerFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	marker := `workspace = "personal"
project = "aurelia"

[capture]
ignore_paths = []
`
	if err := os.WriteFile(filepath.Join(root, ".ai-memory.toml"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "topics", "finance")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	scope, err := ResolveScope(nested, 42, -1001234567890, 7, "session-1")
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if scope.Workspace != "personal" || scope.Project != "aurelia" {
		t.Fatalf("scope = %+v, want personal/aurelia", scope)
	}
	if scope.CWD != nested {
		t.Fatalf("CWD = %q, want %q", scope.CWD, nested)
	}
	if !scope.HasProject() || scope.ProjectRef() != "personal/aurelia" {
		t.Fatalf("HasProject/ProjectRef = %v/%q", scope.HasProject(), scope.ProjectRef())
	}
	if tag := scope.ConversationTag(); tag != "chat:-1001234567890/thread:7" {
		t.Fatalf("ConversationTag = %q", tag)
	}
}

func TestResolveScopeWithoutMarkerUsesDefaultProject(t *testing.T) {
	dir := t.TempDir()
	scope, err := ResolveScope(dir, 0, 0, 0, "")
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if !scope.HasProject() || scope.Workspace != "default" || scope.Project != filepath.Base(dir) {
		t.Fatalf("scope = %+v, want default/%s", scope, filepath.Base(dir))
	}
	if scope.CWD != dir {
		t.Fatalf("CWD = %q, want %q", scope.CWD, dir)
	}
	if tag := scope.ConversationTag(); tag != "" {
		t.Fatalf("implicit DM tag = %q, want empty", tag)
	}
}

func TestResolveScopeNoCwdDoesNotGuessProject(t *testing.T) {
	scope, err := ResolveScope("", 0, 0, 0, "")
	if err != nil {
		t.Fatalf("ResolveScope: %v", err)
	}
	if scope.HasProject() {
		t.Fatalf("empty cwd unexpectedly resolved project: %+v", scope)
	}
}

func TestReadMarkerRejectsInvalidOrPartialMarker(t *testing.T) {
	tests := map[string]string{
		"partial":   "workspace = \"personal\"\n",
		"invalid":   "workspace = \"personal\"\nproject = \"aurelia/other\"\n",
		"bad-space": "workspace = \"personal\"\nproject = \"aurelia project\"\n",
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".ai-memory.toml")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := readMarker(path); err == nil {
				t.Fatal("expected marker error")
			}
		})
	}
}

func TestReadMarkerIgnoresCaptureSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ai-memory.toml")
	body := strings.Join([]string{
		"# comment",
		`workspace = "personal"`,
		`project = "aurelia-group"`,
		"[capture]",
		`ignore_paths = ["~/.ssh/**"]`,
	}, "\n")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	workspace, project, err := readMarker(path)
	if err != nil {
		t.Fatalf("readMarker: %v", err)
	}
	if workspace != "personal" || project != "aurelia-group" {
		t.Fatalf("marker = %q/%q", workspace, project)
	}
}

func TestProjectRefForFindsMarker(t *testing.T) {
	root := t.TempDir()
	marker := `workspace = "personal"
project = "aurelia-homebrewing"
`
	if err := os.WriteFile(filepath.Join(root, ".ai-memory.toml"), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "topics", "homebrewing")
	if err := os.MkdirAll(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if got := ProjectRefFor(nested); got != "personal/aurelia-homebrewing" {
		t.Fatalf("ProjectRefFor = %q, want personal/aurelia-homebrewing", got)
	}
}

func TestProjectRefForEmptyCwd(t *testing.T) {
	if got := ProjectRefFor(""); got != "" {
		t.Fatalf("ProjectRefFor(empty) = %q, want empty", got)
	}
}
