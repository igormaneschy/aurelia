package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNew_UsesAURELIA_HOME(t *testing.T) {
	want := t.TempDir()
	t.Setenv("AURELIA_HOME", want)

	r, err := New()
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}
	if r.Root() != want {
		t.Errorf("Root() = %q, want %q", r.Root(), want)
	}
}

func TestNew_DefaultsToUserHome(t *testing.T) {
	t.Setenv("AURELIA_HOME", "") // ensure env var is not set

	r, err := New()
	if err != nil {
		t.Fatalf("New() unexpected error: %v", err)
	}

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".aurelia")
	if r.Root() != want {
		t.Errorf("Root() = %q, want %q", r.Root(), want)
	}
}

func TestPathResolver_Root(t *testing.T) {
	base := t.TempDir()
	r := &PathResolver{root: base}

	if r.Root() != base {
		t.Errorf("Root() = %q, want %q", r.Root(), base)
	}
}

func TestPathResolver_Accessors(t *testing.T) {
	base := "/tmp/testinstance"
	r := &PathResolver{root: base}

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"Config", r.Config(), filepath.Join(base, "config")},
		{"Data", r.Data(), filepath.Join(base, "data")},
		{"Memory", r.Memory(), filepath.Join(base, "memory")},
		{"MemoryPersonas", r.MemoryPersonas(), filepath.Join(base, "memory", "personas")},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
}

func TestProjectHelpers(t *testing.T) {
	base := t.TempDir()

	if got := ProjectRoot(base); got != filepath.Join(base, ".aurelia") {
		t.Fatalf("ProjectRoot() = %q", got)
	}
	if got := ProjectSkills(base); got != filepath.Join(base, ".aurelia", "skills") {
		t.Fatalf("ProjectSkills() = %q", got)
	}
}

func TestNormalizeProjectCwdInput_BacktickWrapped(t *testing.T) {
	input := "`/Volumes/Dados/Workspaces/AutoTradersOMQS`"
	got, err := normalizeProjectCwdInput(input)
	if err != nil {
		t.Fatalf("normalizeProjectCwdInput(%q) unexpected error: %v", input, err)
	}
	want := "/Volumes/Dados/Workspaces/AutoTradersOMQS"
	if got != want {
		t.Errorf("normalizeProjectCwdInput(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeProjectCwdInput_SingleQuoteWrapped(t *testing.T) {
	input := "'/Volumes/Dados/Workspaces/AutoTradersOMQS'"
	got, err := normalizeProjectCwdInput(input)
	if err != nil {
		t.Fatalf("normalizeProjectCwdInput(%q) unexpected error: %v", input, err)
	}
	want := "/Volumes/Dados/Workspaces/AutoTradersOMQS"
	if got != want {
		t.Errorf("normalizeProjectCwdInput(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeProjectCwdInput_DoubleQuoteWrapped(t *testing.T) {
	input := "\"/Volumes/Dados/Workspaces/AutoTradersOMQS\""
	got, err := normalizeProjectCwdInput(input)
	if err != nil {
		t.Fatalf("normalizeProjectCwdInput(%q) unexpected error: %v", input, err)
	}
	want := "/Volumes/Dados/Workspaces/AutoTradersOMQS"
	if got != want {
		t.Errorf("normalizeProjectCwdInput(%q) = %q, want %q", input, got, want)
	}
}

func TestNormalizeProjectCwdInput_EmptyRejected(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty string", ""},
		{"whitespace only", "   "},
		{"backticks around whitespace", "`   `"},
		{"quotes around whitespace", `'   '`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := normalizeProjectCwdInput(c.input)
			if err == nil {
				t.Errorf("normalizeProjectCwdInput(%q) expected error", c.input)
			}
		})
	}
}

func TestNormalizeProjectCwdInput_WrappedEmptyRejected(t *testing.T) {
	_, err := normalizeProjectCwdInput("``")
	if err == nil {
		t.Fatal("expected error for wrapped empty input")
	}
}

func TestNormalizeProjectCwdInput_TildeExpansion(t *testing.T) {
	// Use a temp home directory to avoid polluting real home
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// os.UserHomeDir may respect $HOME on darwin; skip if it doesn't match
	home, err := os.UserHomeDir()
	if err != nil || home != tmpHome {
		t.Skip("cannot override HOME reliably on this platform")
	}

	t.Run("tilde alone", func(t *testing.T) {
		got, err := normalizeProjectCwdInput("~")
		if err != nil {
			t.Fatalf("normalizeProjectCwdInput(~) unexpected error: %v", err)
		}
		if got != tmpHome {
			t.Errorf("normalizeProjectCwdInput(~) = %q, want %q", got, tmpHome)
		}
	})

	t.Run("tilde with subpath", func(t *testing.T) {
		got, err := normalizeProjectCwdInput("~/myproject")
		if err != nil {
			t.Fatalf("normalizeProjectCwdInput(~/myproject) unexpected error: %v", err)
		}
		want := filepath.Join(tmpHome, "myproject")
		if got != want {
			t.Errorf("normalizeProjectCwdInput(~/myproject) = %q, want %q", got, want)
		}
	})

	t.Run("tilde with nested subpath", func(t *testing.T) {
		got, err := normalizeProjectCwdInput("~/code/go/app")
		if err != nil {
			t.Fatalf("normalizeProjectCwdInput(~/code/go/app) unexpected error: %v", err)
		}
		want := filepath.Join(tmpHome, "code/go/app")
		if got != want {
			t.Errorf("normalizeProjectCwdInput(~/code/go/app) = %q, want %q", got, want)
		}
	})
}

func TestNormalizeProjectCwdInput_TildeOtherUserRejected(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"with subpath", "~otheruser/project"},
		{"alone", "~otheruser"},
		{"nested", "~someone/foo/bar"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := normalizeProjectCwdInput(c.input)
			if err == nil {
				t.Errorf("normalizeProjectCwdInput(%q) expected error", c.input)
			}
		})
	}
}

func TestNormalizeProjectCwdInput_QuoteThenTilde(t *testing.T) {
	// Wrapping backticks stripped, then ~ expanded
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	home, err := os.UserHomeDir()
	if err != nil || home != tmpHome {
		t.Skip("cannot override HOME reliably on this platform")
	}

	got, err := normalizeProjectCwdInput("`~/project`")
	if err != nil {
		t.Fatalf("normalizeProjectCwdInput(`~/project`) unexpected error: %v", err)
	}
	want := filepath.Join(tmpHome, "project")
	if got != want {
		t.Errorf("normalizeProjectCwdInput(`~/project`) = %q, want %q", got, want)
	}
}

func TestSanitizeCwd(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"/home/user/code", "-home-user-code"},
		{"/home/user/code/", "-home-user-code"},
		{"/home/user/code", "-home-user-code"},
		{"/", "-"},
		{"", ""},
	}
	for _, c := range cases {
		if got := SanitizeCwd(c.input); got != c.want {
			t.Errorf("SanitizeCwd(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestResolveProjectCwd_CleansEquivalentPaths(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveProjectCwd(filepath.Join(dir, "."))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("ResolveProjectCwd() = %q, want %q", got, want)
	}
}

func TestResolveProjectCwd_BacktickWrapped(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n"), 0600); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ResolveProjectCwd("`" + dir + "`")
	if err != nil {
		t.Fatalf("ResolveProjectCwd(backtick-wrapped) unexpected error: %v", err)
	}
	if got != filepath.Clean(want) {
		t.Errorf("ResolveProjectCwd(backtick-wrapped) = %q, want %q", got, want)
	}
}

func TestResolveProjectCwd_RejectsEmptyAndWhitespace(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"whitespace", "   "},
		{"backtick only", "``"},
		{"quoted whitespace", "`   `"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ResolveProjectCwd(c.input)
			if err == nil {
				t.Errorf("ResolveProjectCwd(%q) expected error", c.input)
			}
		})
	}
}

func TestResolveProjectCwd_AllowsPlainDirectory(t *testing.T) {
	dir := t.TempDir()
	got, err := ResolveProjectCwd(dir)
	if err != nil {
		t.Fatalf("ResolveProjectCwd(%q) unexpected error: %v", dir, err)
	}
	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Errorf("ResolveProjectCwd(%q) = %q, want %q", dir, got, want)
	}
}

func TestResolveProjectCwd_RejectsNonExistentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	_, err := ResolveProjectCwd(dir)
	if err == nil {
		t.Fatal("expected non-existent directory to be rejected")
	}
}

func TestResolveProjectCwd_RejectsHomeDirectory(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("home directory unavailable")
	}
	if _, err := ResolveProjectCwd(home); err == nil {
		t.Fatal("expected home directory to be rejected as project cwd")
	}
}

func TestProjectMemoryDirs(t *testing.T) {
	r := &PathResolver{root: "/tmp/aurelia"}
	cwd := "/home/user/myproject"

	gotPrivate := r.ProjectMemoryDir(cwd)
	wantPrivate := filepath.Join("/tmp/aurelia", "projects", "-home-user-myproject", "memory")
	if gotPrivate != wantPrivate {
		t.Errorf("ProjectMemoryDir() = %q, want %q", gotPrivate, wantPrivate)
	}

	gotTeam := r.ProjectTeamMemoryDir(cwd)
	wantTeam := filepath.Join("/tmp/aurelia", "projects", "-home-user-myproject", "team")
	if gotTeam != wantTeam {
		t.Errorf("ProjectTeamMemoryDir() = %q, want %q", gotTeam, wantTeam)
	}

	gotConversation := r.ConversationProjectMemoryDir(cwd, 42, 99)
	wantConversation := filepath.Join(wantPrivate, "conversations", "chat_42", "thread_99")
	if gotConversation != wantConversation {
		t.Errorf("ConversationProjectMemoryDir() = %q, want %q", gotConversation, wantConversation)
	}
}

func TestResolveProjectCwd_RequiresAuthorizedPrefix(t *testing.T) {
	root := t.TempDir()
	safe := filepath.Join(root, "safe")
	project := filepath.Join(safe, "project")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(allowedCwdPrefixesEnv, safe)

	if _, err := ResolveProjectCwd(project); err != nil {
		t.Fatalf("ResolveProjectCwd(%q) unexpected error: %v", project, err)
	}
	if _, err := ResolveProjectCwd(outside); err == nil {
		t.Fatalf("ResolveProjectCwd(%q) expected authorized-prefix error", outside)
	}
}

func TestResolveProjectCwd_CleansTraversalBeforePrefixCheck(t *testing.T) {
	root := t.TempDir()
	safe := filepath.Join(root, "safe")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(filepath.Join(safe, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(allowedCwdPrefixesEnv, safe)

	traversal := filepath.Join(safe, "child", "..", "..", "outside")
	if _, err := ResolveProjectCwd(traversal); err == nil {
		t.Fatalf("ResolveProjectCwd(%q) expected traversal outside prefix to be rejected", traversal)
	}
}
