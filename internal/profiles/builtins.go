package profiles

// builtinProfiles returns the three built-in Prompt Profiles that are always
// available even when no files exist on disk. Per Prompt Profiles spec §11.
func builtinProfiles() map[string]*PromptProfile {
	return map[string]*PromptProfile{
		"general": {
			Name:              "general",
			Description:       "Conversa geral, equilibrada e útil.",
			Prompt: `You are in general mode. Be conversational, concise, and helpful.
Ask clarifying questions only when truly necessary.
Adapt your tone and depth to the user's request.`,
			Kind:              KindBuiltin,
			Source:            "builtin",
			Public:            true,
			CapabilityProfile: "execute_safe",
			Tags:              []string{"general", "conversation"},
		},
		"developer": {
			Name:              "developer",
			Description:       "Engenharia de software e produto — prioriza arquitetura, riscos e validação.",
			Prompt: `You are in developer mode. Contextualize requests for software/product engineering:
- Prioritize architecture, risks, validation and maintainability.
- Preserve scope discipline — don't expand the request without asking.
- Prefer concrete file/test references when code context exists.
- Do not execute outside harness capabilities.`,
			Kind:              KindBuiltin,
			Source:            "builtin",
			Public:            true,
			CapabilityProfile: "execute_safe",
			Tags:              []string{"developer", "engineering", "code"},
		},
		"researcher": {
			Name:              "researcher",
			Description:       "Pesquisa, comparação e síntese — distingue evidência de inferência.",
			Prompt: `You are in researcher mode. Explore, compare, and synthesize:
- Distinguish evidence, inference and uncertainty.
- Compare alternatives when relevant.
- Cite sources when web/tool access exists.
- Summarize trade-offs and provide a recommendation.`,
			Kind:              KindBuiltin,
			Source:            "builtin",
			Public:            true,
			CapabilityProfile: "execute_safe",
			Tags:              []string{"researcher", "research", "analysis"},
		},
	}
}
