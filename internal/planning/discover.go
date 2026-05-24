package planning

import (
	"os"
	"path/filepath"
	"time"
)

// Discover scans the given directory and returns a ProjectContext
// without reading full file contents.
func Discover(cwd string) (*ProjectContext, error) {
	if _, err := os.Lstat(cwd); err != nil {
		return nil, err
	}

	ctx := &ProjectContext{DiscoveredAt: time.Now()}
	ctx.HasGit = isDir(cwd, ".git")
	ctx.HasClaudeMD = hasFile(cwd, "CLAUDE.md") || hasFile(cwd, "claude.md") || hasFile(cwd, "Claude.md")
	ctx.HasAgentsMD = hasFile(cwd, "AGENTS.md") || hasFile(cwd, "agents.md") || hasFile(cwd, "Agents.md")
	ctx.HasReadme = hasFile(cwd, "README.md") || hasFile(cwd, "readme.md") || hasFile(cwd, "Readme.md")
	ctx.Layouts = detectLayouts(cwd)
	ctx.Stacks = detectStacks(cwd)
	ctx.NeedsLayoutChoice = len(ctx.Layouts) > 1
	return ctx, nil
}

// detectLayouts checks for common project layout directories.
func detectLayouts(cwd string) []string {
	var layouts []string
	if isDir(cwd, ".specs", "features") {
		layouts = append(layouts, "tlc")
	}
	if isDir(cwd, "docs", "rfc") || isDir(cwd, "rfcs") {
		layouts = append(layouts, "rfc")
	}
	if isDir(cwd, "docs", "adr") || isDir(cwd, "adrs") {
		layouts = append(layouts, "adr")
	}
	if isDir(cwd, "planning") {
		layouts = append(layouts, "planning")
	}
	return layouts
}

// detectStacks checks for common stack indicator files.
func detectStacks(cwd string) []string {
	var stacks []string
	if hasFile(cwd, "go.mod") {
		stacks = append(stacks, "go")
	}
	if hasFile(cwd, "package.json") {
		stacks = append(stacks, "node")
	}
	if hasFile(cwd, "pyproject.toml") {
		stacks = append(stacks, "python")
	}
	if hasFile(cwd, "Cargo.toml") {
		stacks = append(stacks, "rust")
	}
	return stacks
}

// hasFile checks if a regular file exists at cwd/name without following symlinks.
func hasFile(cwd string, name string) bool {
	info, err := os.Lstat(filepath.Join(cwd, name))
	return err == nil && info.Mode().IsRegular()
}

// isDir checks if a directory exists at the given path without following symlinks.
func isDir(cwd string, parts ...string) bool {
	p := filepath.Join(append([]string{cwd}, parts...)...)
	info, err := os.Lstat(p)
	return err == nil && info.IsDir()
}
