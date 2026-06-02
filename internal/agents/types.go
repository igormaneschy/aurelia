package agents

// Agent represents a loaded agent definition from a markdown file.
type Agent struct {
	Name              string         `yaml:"name"`
	Description       string         `yaml:"description"`
	Model             string         `yaml:"model,omitempty"`
	Schedule          string         `yaml:"schedule,omitempty"`
	Cwd               string         `yaml:"cwd,omitempty"`
	MCPServers        map[string]any `yaml:"mcp_servers,omitempty"`
	CapabilityProfile string         `yaml:"capability_profile,omitempty"`
	AllowedTools        []string       `yaml:"allowed_tools,omitempty"`
	DisallowedTools     []string       `yaml:"disallowed_tools,omitempty"`
	MaxTurns            int            `yaml:"max_turns,omitempty"`
	ToolBudget          int            `yaml:"tool_budget,omitempty"` // overrides global tool call thresholds
	Prompt              string         `yaml:"-"` // body after frontmatter
}

// IsReadOnly reports whether the agent has no write-capable tools.
// It considers CapabilityProfile first, then AllowedTools and DisallowedTools.
func (a *Agent) IsReadOnly() bool {
	// If CapabilityProfile is set, use it as the source of truth.
	if a.CapabilityProfile != "" {
		switch a.CapabilityProfile {
		case "observe", "read_only":
			return true
		case "edit_project", "execute_safe", "privileged":
			return false
		default:
			// Unknown profile — fall through to tool-based detection.
		}
	}

	// Determine the effective set of tools.
	var effective []string
	if len(a.AllowedTools) > 0 {
		effective = make([]string, len(a.AllowedTools))
		copy(effective, a.AllowedTools)
	} else {
		// No allowlist means all built-ins are available before denylist.
		effective = []string{"Read", "Write", "Edit", "Bash", "Grep", "Glob", "LS", "List", "WebSearch", "WebSearchPremium", "WebFetch"}
	}

	// Apply denylist.
	denied := make(map[string]bool, len(a.DisallowedTools))
	for _, t := range a.DisallowedTools {
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
