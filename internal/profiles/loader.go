package profiles

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/igormaneschy/aurelia/internal/agents"
)

// profileFrontmatter is the YAML structure parsed from canonical profile
// markdown files (~/.aurelia/profiles/<name>.md).
// Matches Prompt Profiles spec §8.3.
type profileFrontmatter struct {
	Name              string   `yaml:"name"`
	Description       string   `yaml:"description"`
	Kind              string   `yaml:"kind,omitempty"`          // "prompt_profile"
	Harness           string   `yaml:"harness,omitempty"`        // "pi" default
	Model             string   `yaml:"model,omitempty"`
	Cwd               string   `yaml:"cwd,omitempty"`
	CapabilityProfile string   `yaml:"capability_profile,omitempty"`
	AllowedTools      []string `yaml:"allowed_tools,omitempty"`
	DisallowedTools   []string `yaml:"disallowed_tools,omitempty"`
	MaxTurns          int      `yaml:"max_turns,omitempty"`
	ToolBudget        int      `yaml:"tool_budget,omitempty"`
	Public            *bool    `yaml:"public,omitempty"` // nil defaults to true
	Tags              []string `yaml:"tags,omitempty"`
}

// LoadCanonical reads all .md files from dir and returns them as PromptProfiles.
// The canonical profile format uses the Prompt Profiles spec §8.3 frontmatter.
// Files without a valid name are skipped with a warning.
func LoadCanonical(dir string) map[string]*PromptProfile {
	result := make(map[string]*PromptProfile)

	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Debug("profiles: canonical profiles dir not found, skipping", "dir", dir)
		return result
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("profiles: failed to read canonical profile", "file", entry.Name(), "err", err)
			continue
		}

		pp, err := parseCanonicalFile(data, path)
		if err != nil {
			slog.Warn("profiles: skipping canonical profile due to parse error", "file", entry.Name(), "err", err)
			continue
		}
		if pp == nil {
			continue // no valid name
		}

		result[strings.ToLower(pp.Name)] = pp
	}

	return result
}

// parseCanonicalFile splits a markdown file on --- markers, parses YAML
// frontmatter into PromptProfile, and extracts the prompt body.
// Returns nil if the file has no valid frontmatter or the name field is empty.
func parseCanonicalFile(data []byte, source string) (*PromptProfile, error) {
	parts := bytes.SplitN(data, []byte("---"), 3)
	if len(parts) != 3 {
		return nil, nil
	}

	var fm profileFrontmatter
	if err := yaml.Unmarshal(parts[1], &fm); err != nil {
		return nil, fmt.Errorf("parsing yaml frontmatter: %w", err)
	}

	if fm.Name == "" {
		return nil, nil
	}

	// Canonical profiles are always KindGlobal.
	kind := KindGlobal

	// Determine public visibility.
	public := true
	if fm.Public != nil {
		public = *fm.Public
	}

	pp := &PromptProfile{
		Name:              fm.Name,
		Description:       fm.Description,
		Prompt:            string(bytes.TrimSpace(parts[2])),
		Kind:              kind,
		Source:            source,
		Harness:           fm.Harness,
		Model:             fm.Model,
		Cwd:               fm.Cwd,
		CapabilityProfile: fm.CapabilityProfile,
		AllowedTools:      fm.AllowedTools,
		DisallowedTools:   fm.DisallowedTools,
		MaxTurns:          fm.MaxTurns,
		ToolBudget:        fm.ToolBudget,
		Public:            public,
		Tags:              fm.Tags,
	}

	// Default harness to "pi".
	if pp.Harness == "" {
		pp.Harness = "pi"
	}

	return pp, nil
}

// LoadCanonicalWithLegacy loads profiles from a canonical directory and
// a legacy agents directory, merging them with proper precedence.
// Legacy agents override canonical profiles of the same name.
func LoadCanonicalWithLegacy(canonicalDir, agentsDir string) *Resolver {
	r := &Resolver{
		canonical: make(map[string]*PromptProfile),
		builtins:  builtinProfiles(),
	}
	if canonicalDir != "" {
		r.canonical = LoadCanonical(canonicalDir)
	}
	if agentsDir != "" {
		reg, err := agents.Load(agentsDir)
		if err != nil {
			slog.Warn("profiles: failed to load legacy agents", "dir", agentsDir, "err", err)
		} else {
			r.agentsReg = reg
		}
	}
	return r
}
