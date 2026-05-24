package planning

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	ctx, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover(%q) returned error: %v", dir, err)
	}
	if ctx.HasGit {
		t.Error("HasGit = true, want false")
	}
	if ctx.HasClaudeMD {
		t.Error("HasClaudeMD = true, want false")
	}
	if ctx.HasAgentsMD {
		t.Error("HasAgentsMD = true, want false")
	}
	if ctx.HasReadme {
		t.Error("HasReadme = true, want false")
	}
	if len(ctx.Layouts) != 0 {
		t.Errorf("Layouts = %v, want empty", ctx.Layouts)
	}
	if ctx.NeedsLayoutChoice {
		t.Error("NeedsLayoutChoice = true, want false")
	}
	if len(ctx.Stacks) != 0 {
		t.Errorf("Stacks = %v, want empty", ctx.Stacks)
	}
	if ctx.DiscoveredAt.IsZero() {
		t.Error("DiscoveredAt is zero, want non-zero")
	}
}

func TestDiscover_TLC(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, dir, ".specs", "features")

	ctx, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Layouts) != 1 || ctx.Layouts[0] != "tlc" {
		t.Fatalf("Layouts = %v, want [tlc]", ctx.Layouts)
	}
	if ctx.NeedsLayoutChoice {
		t.Error("NeedsLayoutChoice = true with single layout, want false")
	}
	if ctx.DiscoveredAt.IsZero() {
		t.Error("DiscoveredAt is zero")
	}
}

func TestDiscover_RFC(t *testing.T) {
	t.Run("docs/rfc", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, dir, "docs", "rfc")

		ctx, err := Discover(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(ctx.Layouts) != 1 || ctx.Layouts[0] != "rfc" {
			t.Fatalf("Layouts = %v, want [rfc]", ctx.Layouts)
		}
	})

	t.Run("rfcs", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, dir, "rfcs")

		ctx, err := Discover(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(ctx.Layouts) != 1 || ctx.Layouts[0] != "rfc" {
			t.Fatalf("Layouts = %v, want [rfc]", ctx.Layouts)
		}
	})
}

func TestDiscover_ADR(t *testing.T) {
	t.Run("docs/adr", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, dir, "docs", "adr")

		ctx, err := Discover(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(ctx.Layouts) != 1 || ctx.Layouts[0] != "adr" {
			t.Fatalf("Layouts = %v, want [adr]", ctx.Layouts)
		}
	})

	t.Run("adrs", func(t *testing.T) {
		dir := t.TempDir()
		mkdir(t, dir, "adrs")

		ctx, err := Discover(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(ctx.Layouts) != 1 || ctx.Layouts[0] != "adr" {
			t.Fatalf("Layouts = %v, want [adr]", ctx.Layouts)
		}
	})
}

func TestDiscover_MultipleLayouts(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, dir, ".specs", "features")
	mkdir(t, dir, "docs", "rfc")

	ctx, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Layouts) != 2 {
		t.Fatalf("Layouts = %v, want length 2", ctx.Layouts)
	}
	if !ctx.NeedsLayoutChoice {
		t.Error("NeedsLayoutChoice = false with 2 layouts, want true")
	}
}

func TestDiscover_GoStack(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "go.mod")

	ctx, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Stacks) != 1 || ctx.Stacks[0] != "go" {
		t.Fatalf("Stacks = %v, want [go]", ctx.Stacks)
	}
}

func TestDiscover_NodeStack(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "package.json")

	ctx, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ctx.Stacks) != 1 || ctx.Stacks[0] != "node" {
		t.Fatalf("Stacks = %v, want [node]", ctx.Stacks)
	}
}

func TestDiscover_FullProject(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, dir, ".git")
	touch(t, dir, "CLAUDE.md")
	touch(t, dir, "README.md")
	touch(t, dir, "go.mod")
	mkdir(t, dir, ".specs", "features")

	ctx, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !ctx.HasGit {
		t.Error("HasGit = false, want true")
	}
	if !ctx.HasClaudeMD {
		t.Error("HasClaudeMD = false, want true")
	}
	if ctx.HasAgentsMD {
		t.Error("HasAgentsMD = true, want false")
	}
	if !ctx.HasReadme {
		t.Error("HasReadme = false, want true")
	}
	if len(ctx.Layouts) != 1 || ctx.Layouts[0] != "tlc" {
		t.Errorf("Layouts = %v, want [tlc]", ctx.Layouts)
	}
	if ctx.NeedsLayoutChoice {
		t.Error("NeedsLayoutChoice = true with single layout, want false")
	}
	if len(ctx.Stacks) != 1 || ctx.Stacks[0] != "go" {
		t.Errorf("Stacks = %v, want [go]", ctx.Stacks)
	}
}

func TestDiscover_NonExistent(t *testing.T) {
	_, err := Discover("/this/path/definitely/does/not/exist/aurelia-test")
	if err == nil {
		t.Fatal("Discover on non-existent path returned nil error, want error")
	}
}

func TestDiscover_Deterministic(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, dir, ".git")
	touch(t, dir, "go.mod")
	touch(t, dir, "CLAUDE.md")
	mkdir(t, dir, ".specs", "features")
	mkdir(t, dir, "docs", "rfc")

	ctx1, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx2, err := Discover(dir)
	if err != nil {
		t.Fatal(err)
	}

	if ctx1.HasGit != ctx2.HasGit {
		t.Errorf("HasGit changed between calls: %v vs %v", ctx1.HasGit, ctx2.HasGit)
	}
	if len(ctx1.Layouts) != len(ctx2.Layouts) {
		t.Errorf("Layouts length changed: %d vs %d", len(ctx1.Layouts), len(ctx2.Layouts))
	}
	if ctx1.NeedsLayoutChoice != ctx2.NeedsLayoutChoice {
		t.Errorf("NeedsLayoutChoice changed: %v vs %v", ctx1.NeedsLayoutChoice, ctx2.NeedsLayoutChoice)
	}
	if len(ctx1.Stacks) != len(ctx2.Stacks) {
		t.Errorf("Stacks length changed: %d vs %d", len(ctx1.Stacks), len(ctx2.Stacks))
	}
}

// mkdir creates a directory tree rooted at dir with the given path components.
func mkdir(t testing.TB, dir string, parts ...string) {
	t.Helper()
	p := filepath.Join(append([]string{dir}, parts...)...)
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatalf("mkdir(%q): %v", p, err)
	}
}

// touch creates an empty file at dir/name.
func touch(t testing.TB, dir string, name string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte{}, 0644); err != nil {
		t.Fatalf("touch(%q): %v", p, err)
	}
}

