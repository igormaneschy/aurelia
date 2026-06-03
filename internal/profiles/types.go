package profiles

import "github.com/igormaneschy/aurelia/internal/agents"

// ProfileKind classifies the origin of a PromptProfile.
type ProfileKind string

const (
	KindBuiltin     ProfileKind = "builtin"
	KindLegacyAgent ProfileKind = "legacy_agent"
	KindGlobal      ProfileKind = "global"
	KindUser        ProfileKind = "user"
)

// PromptProfile is a preset of context/instructions that Aurelia injects
// into the request before sending to the harness.
// Canonical type per Prompt Profiles spec §8.1.
type PromptProfile struct {
	Name        string
	Description string
	Prompt      string

	// Product classification
	Kind   ProfileKind
	Source string // file path or "builtin"

	// Optional execution hints passed to harness adapter when supported.
	Harness           string
	Model             string
	Cwd               string
	CapabilityProfile string
	AllowedTools      []string
	DisallowedTools   []string
	MaxTurns          int
	ToolBudget        int

	// Safe display controls
	Public bool
	Tags   []string
}

// FromAgent converts a legacy agents.Agent to a PromptProfile.
// Maps legacy fields to the canonical Prompt Profile data model
// per Prompt Profiles spec §8.3 compatibility mapping.
func FromAgent(a *agents.Agent) *PromptProfile {
	if a == nil {
		return nil
	}
	return &PromptProfile{
		Name:              a.Name,
		Description:       a.Description,
		Prompt:            a.Prompt,
		Kind:              KindLegacyAgent,
		Model:             a.Model,
		Cwd:               a.Cwd,
		CapabilityProfile: a.CapabilityProfile,
		AllowedTools:      a.AllowedTools,
		DisallowedTools:   a.DisallowedTools,
		MaxTurns:          a.MaxTurns,
		ToolBudget:        a.ToolBudget,
		Public:            true, // legacy agents default to public name+description
	}
}

// IsReadOnly reports whether the profile has no write-capable tools.
// Mirrors agents.Agent.IsReadOnly for compatibility.
func (p *PromptProfile) IsReadOnly() bool {
	if p.CapabilityProfile != "" {
		switch p.CapabilityProfile {
		case "observe", "read_only":
			return true
		case "edit_project", "execute_safe", "privileged":
			return false
		default:
			// Unknown profile — fall through to tool-based detection.
		}
	}
	var effective []string
	if len(p.AllowedTools) > 0 {
		effective = make([]string, len(p.AllowedTools))
		copy(effective, p.AllowedTools)
	} else {
		effective = []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "LS", "List", "WebSearch", "WebSearchPremium", "WebFetch"}
	}
	denied := make(map[string]bool, len(p.DisallowedTools))
	for _, t := range p.DisallowedTools {
		denied[t] = true
	}
	for _, t := range effective {
		if denied[t] {
			continue
		}
		if t == "Write" || t == "Edit" || t == "Bash" {
			return false
		}
	}
	return true
}
