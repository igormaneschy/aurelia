// Package deps checks for external runtime dependencies (Node.js, npm, git, gh).
package deps

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DepStatus represents the check result for a single dependency.
type DepStatus struct {
	Name       string // "Node.js", "npm", "git", "gh"
	Command    string // "node", "npm", "git", "gh"
	Required   bool   // true = fatal if missing
	Found      bool   // binary exists in PATH
	Version    string // parsed version string (e.g. "22.14.0")
	MinVersion string // minimum required (e.g. "18.0.0")
	VersionOK  bool   // version >= min (true if no min specified)
	InstallURL string // help URL for installation
}

// CheckResult holds results for all dependencies.
type CheckResult struct {
	Deps  []DepStatus
	AllOK bool // true when all required deps are found and version-ok
}

var depSpecs = []struct {
	Name       string
	Command    string
	Required   bool
	MinVersion string
	InstallURL string
}{
	{"Node.js", "node", true, "22.19.0", "https://nodejs.org/"},
	{"npm", "npm", true, "8.0.0", "https://nodejs.org/"},
	{"git", "git", false, "2.0.0", "https://git-scm.com/"},
	{"gh", "gh", false, "", "https://cli.github.com/"},
}

// CheckAll runs all dependency checks and returns the result.
// Each check runs exec.LookPath + "<cmd> --version" with a 5s timeout.
// Safe to call from any goroutine.
func CheckAll() CheckResult {
	result := CheckResult{AllOK: true}
	for _, spec := range depSpecs {
		status := checkOne(spec.Name, spec.Command, spec.Required, spec.MinVersion, spec.InstallURL)
		result.Deps = append(result.Deps, status)
		if spec.Required && (!status.Found || !status.VersionOK) {
			result.AllOK = false
		}
	}
	return result
}

func checkOne(name, cmd string, required bool, minVersion, installURL string) DepStatus {
	status := DepStatus{
		Name:       name,
		Command:    cmd,
		Required:   required,
		MinVersion: minVersion,
		InstallURL: installURL,
	}

	path, err := exec.LookPath(cmd)
	if err != nil || path == "" {
		return status
	}
	status.Found = true

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, cmd, "--version").Output()
	if err != nil {
		// Binary found but couldn't get version — treat as found with unknown version.
		status.VersionOK = minVersion == ""
		return status
	}

	status.Version = parseVersion(string(out))
	if minVersion == "" || status.Version == "" {
		status.VersionOK = true
	} else {
		status.VersionOK = compareVersions(status.Version, minVersion) >= 0
	}
	return status
}

var semverRe = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

// parseVersion extracts the first semver-like version from command output.
func parseVersion(output string) string {
	m := semverRe.FindString(output)
	return m
}

// compareVersions compares two semver strings (major.minor.patch).
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	ap := parseSemver(a)
	bp := parseSemver(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return -1
		}
		if ap[i] > bp[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(v string) [3]int {
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			break
		}
		result[i] = n
	}
	return result
}

// FormatStatus returns a human-readable line for a single dependency.
func (d DepStatus) FormatStatus() string {
	switch {
	case d.Found && d.VersionOK:
		if d.Version != "" {
			return fmt.Sprintf("[ok]  %s v%s", d.Name, d.Version)
		}
		return fmt.Sprintf("[ok]  %s", d.Name)
	case d.Found && !d.VersionOK:
		return fmt.Sprintf("[!!]  %s v%s (requires >= %s)", d.Name, d.Version, d.MinVersion)
	case !d.Found && d.Required:
		return fmt.Sprintf("[!!]  %s — not found. Install: %s", d.Name, d.InstallURL)
	default: // !Found && !Required
		return fmt.Sprintf("[--]  %s — not found (optional)", d.Name)
	}
}
