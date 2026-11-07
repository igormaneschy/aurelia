package telegram

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeTopicSlug(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
	}{
		{"Finanças", "financas"},
		{"Home Brewing", "home-brewing"},
		{"UPPER_CASE", "upper-case"},
		{"  home--brewing  ", "home-brewing"},
		{"HomeAssistant", "homeassistant"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			got, err := normalizeTopicSlug(tc.raw)
			if err != nil {
				t.Fatalf("normalizeTopicSlug(%q): %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeTopicSlug(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestNormalizeTopicSlugRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "   ", "../evil", "a/b", `a\b`, "a..b", "!!!", "topic/../../etc"} {
		t.Run(raw, func(t *testing.T) {
			if got, err := normalizeTopicSlug(raw); err == nil {
				t.Fatalf("normalizeTopicSlug(%q) = %q, want error", raw, got)
			}
		})
	}
}

func TestProvisionTopicProjectCreatesMarkerAndIsIdempotent(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	dir, project, err := provisionTopicProject(base, "finance", "personal")
	if err != nil {
		t.Fatalf("provisionTopicProject: %v", err)
	}
	if want := filepath.Join(base, "topics", "finance"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
	if project != "aurelia-finance" {
		t.Fatalf("project = %q, want aurelia-finance", project)
	}
	marker := filepath.Join(dir, topicProjectMarkerName)
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `workspace = "personal"`) || !strings.Contains(content, `project = "aurelia-finance"`) {
		t.Fatalf("marker missing scope: %s", content)
	}
	if !strings.Contains(content, "[capture]") || !strings.Contains(content, "~/.ssh/**") {
		t.Fatalf("marker missing capture exclusions: %s", content)
	}

	againDir, againProject, err := provisionTopicProject(base, "finance", "personal")
	if err != nil {
		t.Fatalf("idempotent provisionTopicProject: %v", err)
	}
	if againDir != dir || againProject != project {
		t.Fatalf("second call = (%q,%q), want (%q,%q)", againDir, againProject, dir, project)
	}
}

func TestProvisionTopicProjectRejectsExistingUnmarkedDirectory(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	dir := filepath.Join(base, "topics", "legacy")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := provisionTopicProject(base, "legacy", "personal"); err == nil {
		t.Fatal("expected error for existing unmarked directory")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "existing.txt")); err != nil || string(data) != "keep" {
		t.Fatalf("existing file was modified: %v %q", err, data)
	}
}

func TestProvisionTopicProjectRejectsDifferentProject(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	dir := filepath.Join(base, "topics", "finance")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	marker := `workspace = "personal"
project = "aurelia-other"
`
	if err := os.WriteFile(filepath.Join(dir, topicProjectMarkerName), []byte(marker), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := provisionTopicProject(base, "finance", "personal"); err == nil {
		t.Fatal("expected error for a topic already bound to another project")
	}
}

func TestProvisionTopicProjectRejectsRelativeBase(t *testing.T) {
	t.Parallel()
	if _, _, err := provisionTopicProject("relative/base", "finance", "personal"); err == nil {
		t.Fatal("expected error for relative base")
	}
}
