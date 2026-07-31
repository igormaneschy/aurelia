package deps

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v22.14.0", "22.14.0"},
		{"10.9.2", "10.9.2"},
		{"git version 2.43.0.windows.1", "2.43.0"},
		{"gh version 2.74.0 (2025-01-01)", "2.74.0"},
		{"v18.0.0\n", "18.0.0"},
		{"no version here", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseVersion(tt.input)
			if got != tt.want {
				t.Errorf("parseVersion(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"22.14.0", "18.0.0", 1},
		{"18.0.0", "18.0.0", 0},
		{"2.0.0", "2.0.0", 0},
		{"8.0.0", "10.9.2", -1},
		{"1.0.0", "2.0.0", -1},
		{"10.0.0", "9.99.99", 1},
		{"1.2.3", "1.2.4", -1},
		{"1.3.0", "1.2.99", 1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			got := compareVersions(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestCheckAll(t *testing.T) {
	// Integration test: runs against whatever is installed on the system.
	result := CheckAll()

	if len(result.Deps) != 4 {
		t.Fatalf("expected 4 deps, got %d", len(result.Deps))
	}

	// Verify dep names match expected order.
	expectedNames := []string{"Node.js", "npm", "git", "gh"}
	for i, name := range expectedNames {
		if result.Deps[i].Name != name {
			t.Errorf("deps[%d].Name = %q, want %q", i, result.Deps[i].Name, name)
		}
	}

	// If Node.js is found (likely in dev env), version should be parsed.
	for _, d := range result.Deps {
		if d.Found && d.Version != "" {
			if parseVersion(d.Version) == "" {
				t.Errorf("%s found with version %q that doesn't look like semver", d.Name, d.Version)
			}
		}
		t.Logf("%s: found=%v version=%s versionOK=%v", d.Name, d.Found, d.Version, d.VersionOK)
	}
}

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		name string
		dep  DepStatus
		want string
	}{
		{
			"found ok with version",
			DepStatus{Name: "Node.js", Found: true, VersionOK: true, Version: "22.14.0"},
			"[ok]  Node.js v22.14.0",
		},
		{
			"found version too low",
		DepStatus{Name: "Node.js", Found: true, VersionOK: false, Version: "16.0.0", MinVersion: "22.19.0"},
		"[!!]  Node.js v16.0.0 (requires >= 22.19.0)",
		},
		{
			"required missing",
			DepStatus{Name: "Node.js", Required: true, Found: false, InstallURL: "https://nodejs.org/"},
			"[!!]  Node.js — not found. Install: https://nodejs.org/",
		},
		{
			"optional missing",
			DepStatus{Name: "git", Required: false, Found: false},
			"[--]  git — not found (optional)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dep.FormatStatus()
			if got != tt.want {
				t.Errorf("FormatStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}
