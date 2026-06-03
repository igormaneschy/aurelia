package profiles

import (
	"fmt"
	"log"
	"log/slog"
	"strings"
	"sync"

	"github.com/igormaneschy/aurelia/internal/agents"
)

// Resolver loads Prompt Profiles from multiple sources, normalizes them,
// and resolves the effective profile for a message turn.
//
// Load order / precedence (highest to lowest):
//  1. User-private profiles (~/.aurelia/users/<id>/profiles/*.md) — future P2
//  2. Canonical global profiles (~/.aurelia/profiles/*.md) — future P2
//  3. Legacy agents (~/.aurelia/agents/*.md)
//  4. Builtins (general, developer, researcher)
//
// Builtins cannot be silently deleted; legacy agents with the same name as
// a builtin override the builtin's prompt/description but keep the builtin kind.
type Resolver struct {
	agentsDir string
	agentsReg *agents.Registry
	builtins  map[string]*PromptProfile
	mu        sync.RWMutex
}

// NewResolver creates a Resolver that loads legacy agents from agentsDir
// and always includes builtins. Returns error if the agents directory
// cannot be read (but builtins are still available).
func NewResolver(agentsDir string) (*Resolver, error) {
	r := &Resolver{
		agentsDir: agentsDir,
		builtins:  builtinProfiles(),
	}

	if agentsDir != "" {
		reg, err := agents.Load(agentsDir)
		if err != nil {
			slog.Warn("profiles: failed to load legacy agents, builtins only", "dir", agentsDir, "err", err)
			// Don't fail — builtins are still available.
		} else {
			r.agentsReg = reg
		}
	}

	return r, nil
}

// NewResolverFromRegistry creates a Resolver from an already-loaded
// agents.Registry. Used when the registry was loaded externally
// (e.g., in the app entrypoint).
func NewResolverFromRegistry(reg *agents.Registry) *Resolver {
	return &Resolver{
		agentsReg: reg,
		builtins:  builtinProfiles(),
	}
}

// Get returns the PromptProfile with the given name (case-insensitive),
// or nil if not found. Precedence: legacy agents override builtins.
func (r *Resolver) Get(name string) *PromptProfile {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getLocked(name)
}

func (r *Resolver) getLocked(name string) *PromptProfile {
	key := strings.ToLower(name)

	// Legacy agents have higher precedence than builtins.
	if r.agentsReg != nil {
		if agent := r.agentsReg.Get(name); agent != nil {
			return FromAgent(agent)
		}
	}

	// Builtins as fallback.
	return r.builtins[key]
}

// List returns all available Prompt Profiles sorted by name.
// Builtins are always included; legacy agents override builtins
// when they share the same name.
func (r *Resolver) List() []*PromptProfile {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var result []*PromptProfile

	// Start with builtins as base.
	for name, bp := range r.builtins {
		seen[name] = true
		result = append(result, bp)
	}

	// Override with legacy agents when they exist.
	if r.agentsReg != nil {
		for _, agent := range r.agentsReg.Agents() {
			key := strings.ToLower(agent.Name)
			if seen[key] {
				// Replace builtin entry with legacy version.
				for i, p := range result {
					if strings.ToLower(p.Name) == key {
						result[i] = FromAgent(agent)
						break
					}
				}
			} else {
				seen[key] = true
				result = append(result, FromAgent(agent))
			}
		}
	}

	// Sort by name.
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i].Name > result[j].Name {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

// ActiveDefault returns the effective default profile name for display purposes.
// Returns "general" when activeProfile is empty.
func ActiveDefault(activeProfile string) string {
	if activeProfile == "" {
		return "general"
	}
	return activeProfile
}

// ResolveEffective determines which Prompt Profile should be used for a message.
// Resolution: @name prefix > active default > "general".
//
// When text starts with "@name ", the named profile is used as a one-shot
// override and the "@name " prefix is stripped from the returned text.
// When the named profile doesn't exist, returns (nil, "", ErrProfileNotFound).
//
// When text does not start with "@name ", the activeDefault profile is used.
// Falls back to "general" when activeDefault is empty.
func (r *Resolver) ResolveEffective(text string, activeDefault string) (*PromptProfile, string, error) {
	if r == nil {
		// No resolver — return general builtin.
		return builtinProfiles()["general"], text, nil
	}

	// Check for explicit @name prefix.
	if profile, stripped, found := r.parseAtProfile(text); found {
		if profile == nil {
			// @name was parsed but profile not found.
			return nil, "", &ErrProfileNotFound{Name: extractAtName(text)}
		}
		return profile, stripped, nil
	}

	// No @name prefix — use active default.
	name := activeDefault
	if name == "" {
		name = "general"
	}
	profile := r.Get(name)
	if profile == nil {
		// Active default not found — fall back to general builtin.
		log.Printf("profiles: active default %q not found, falling back to general", name)
		profile = r.builtins["general"]
	}
	return profile, text, nil
}

// parseAtProfile checks if text starts with "@name " and returns the
// matching profile with the "@name " prefix stripped.
// Returns (nil, "", false) when the text does not start with "@name ".
// Returns (nil, "", true) when @name was parsed but profile not found
// (caller should return an error to the user).
func (r *Resolver) parseAtProfile(text string) (*PromptProfile, string, bool) {
	if !strings.HasPrefix(text, "@") {
		return nil, "", false
	}

	// Extract name after @, before the first space.
	rest := text[1:]
	name := rest
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx == -1 {
		// "@name" with no text after — treat as profile invocation without content.
		// Return the profile but keep text as-is (stripping would empty it).
		profile := r.getLocked(rest)
		return profile, text, true
	}

	name = rest[:spaceIdx]
	profile := r.getLocked(name)
	stripped := strings.TrimSpace(rest[spaceIdx+1:])
	if stripped == "" {
		stripped = text // keep original if nothing after @name
	}
	return profile, stripped, true
}

// extractAtName extracts the @name token from text that is known to start with @.
func extractAtName(text string) string {
	if !strings.HasPrefix(text, "@") {
		return ""
	}
	rest := text[1:]
	if idx := strings.IndexByte(rest, ' '); idx != -1 {
		return rest[:idx]
	}
	return rest
}

// ErrProfileNotFound is returned when a message starts with @name but
// no profile with that name exists.
type ErrProfileNotFound struct {
	Name string
}

func (e *ErrProfileNotFound) Error() string {
	return fmt.Sprintf("perfil @%s não encontrado", e.Name)
}
