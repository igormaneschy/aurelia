package security

import "strings"

// Extension-registered tools are registered at runtime by PI extensions
// (the ai-memory lifecycle extension bridges the wiki tools; pi-mcp-adapter
// registers `mcpScript`). The PI SDK treats RequestOptions.tools as a **closed
// allowlist** and filters every extension-registered definition through it
// (pi-coding-agent: AgentSession._refreshToolRegistry → isAllowedTool), so a
// tool that is not named here is dropped from the registry entirely — the
// model never sees it and the AGENTS.md routing rules point at nothing.
//
// Those names cannot be derived from the built-in tool set: they depend on
// what each extension registers at load time. Each capability profile
// therefore names the extension tools it grants, keeping the wiki surface
// behind the same profile boundary as file and shell access.
//
// Read tools stay available wherever the profile can read the project;
// page/handoff writes follow the write-capable profiles; destructive and
// bulk operations (delete, forget-sweep, lint, consolidate, auto-improve,
// self-routing install) remain privileged-only.
var (
	memoryReadTools = []string{
		"memory_query",
		"memory_read_page",
		"memory_recent",
		"memory_status",
		"memory_briefing",
		"memory_explore",
		"memory_read_session_observations",
	}
	memoryWriteTools = []string{
		"memory_write_page",
		"memory_feedback",
		"memory_handoff_begin",
		"memory_handoff_accept",
		"memory_handoff_cancel",
	}
	memoryAdminTools = []string{
		"memory_delete_page",
		"memory_forget_sweep",
		"memory_lint",
		"memory_consolidate",
		"memory_auto_improve",
		"memory_install_self_routing",
	}
	// mcpScript batches several MCP calls into one tool invocation. It is a
	// trusted, agent-authored scripting surface (documented as "not an
	// isolation boundary"), so it follows the profiles that already grant Bash.
	mcpScriptTools = []string{"mcpScript"}
)

// ProfileExtensionTools returns the extension-registered tool names granted by
// a capability profile. The result is a fresh slice: callers may append to it.
func ProfileExtensionTools(p CapabilityProfile) []string {
	switch p {
	case ProfileObserve:
		return nil
	case ProfileReadOnly:
		return joinToolNames(memoryReadTools)
	case ProfileEditProject, ProfileExecuteSafe:
		return joinToolNames(memoryReadTools, memoryWriteTools, mcpScriptTools)
	case ProfilePrivileged:
		return joinToolNames(memoryReadTools, memoryWriteTools, memoryAdminTools, mcpScriptTools)
	default:
		return nil
	}
}

// GrantExtensionTools appends the profile's extension tools to a resolved
// built-in allowlist, skipping names the agent explicitly disallowed.
//
// A nil `tools` (the observe profile grants nothing) stays nil: granting an
// extension tool there would silently widen a profile that is defined as
// having no tools at all.
func GrantExtensionTools(p CapabilityProfile, tools []string, agentDisallowed []string) []string {
	if tools == nil {
		return nil
	}
	denied := make(map[string]bool, len(agentDisallowed))
	for _, name := range agentDisallowed {
		denied[name] = true
	}
	granted := make(map[string]bool, len(tools))
	for _, name := range tools {
		granted[name] = true
	}
	for _, name := range ProfileExtensionTools(p) {
		if denied[name] || granted[name] {
			continue
		}
		tools = append(tools, name)
		granted[name] = true
	}
	return tools
}

func joinToolNames(groups ...[]string) []string {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	out := make([]string, 0, total)
	for _, group := range groups {
		out = append(out, group...)
	}
	return out
}

// extensionToolLabels maps a case-folded tool name to its canonical label.
// Built once: the groups above are package-level and never mutated.
var extensionToolLabels = func() map[string]string {
	labels := make(map[string]string)
	for _, group := range [][]string{memoryReadTools, memoryWriteTools, memoryAdminTools, mcpScriptTools} {
		for _, tool := range group {
			labels[strings.ToLower(tool)] = tool
		}
	}
	return labels
}()

// ExtensionToolLabel returns the canonical label for a known extension tool.
// The lookup is case-insensitive; the returned name keeps its canonical casing
// (mcpScript). Callers that must reduce untrusted SDK names to a bounded label
// set use this instead of degrading to a generic placeholder.
func ExtensionToolLabel(name string) (string, bool) {
	label, ok := extensionToolLabels[strings.ToLower(strings.TrimSpace(name))]
	return label, ok
}
