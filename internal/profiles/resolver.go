package profiles

import (
	"fmt"
	"log"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/igormaneschy/aurelia/internal/agents"
)

// Resolver loads Prompt Profiles from multiple sources, normalizes them,
// and resolves the effective profile for a message turn.
//
// Load order / precedence (highest to lowest):
//  1. User-private profiles (~/.aurelia/users/<id>/profiles/*.md) — future
//  2. Canonical global profiles (~/.aurelia/profiles/*.md) — Phase 2
//  3. Legacy agents (~/.aurelia/agents/*.md)
//  4. Builtins (general, developer, researcher)
//
// Builtins cannot be silently deleted; profiles at higher levels override
// lower levels of the same name.
type Resolver struct {
	agentsDir string
	agentsReg *agents.Registry
	canonical map[string]*PromptProfile // canonical ~/.aurelia/profiles/
	builtins  map[string]*PromptProfile
	mu        sync.RWMutex
}

// ErrProfileNotAllowed reports an existing profile that is not visible to the
// caller. The name is safe to echo because the caller supplied it explicitly.
type ErrProfileNotAllowed struct {
	Name string
}

func (e *ErrProfileNotAllowed) Error() string {
	return fmt.Sprintf("profile %q is not available to this user", e.Name)
}

// NewResolver creates a Resolver that loads legacy agents from agentsDir,
// canonical profiles from profilesDir, and always includes builtins.
// Returns error if the directories cannot be read (but builtins are still available).
func NewResolver(agentsDir, profilesDir string) (*Resolver, error) {
	r := &Resolver{
		agentsDir: agentsDir,
		builtins:  builtinProfiles(),
		canonical: make(map[string]*PromptProfile),
	}

	if profilesDir != "" {
		r.canonical = LoadCanonical(profilesDir)
	}

	if agentsDir != "" {
		reg, err := agents.Load(agentsDir)
		if err != nil {
			slog.Warn("profiles: failed to load legacy agents, builtins only", "dir", agentsDir, "err", err)
		} else {
			r.agentsReg = reg
		}
	}

	return r, nil
}

// NewResolverFromRegistry creates a Resolver from an already-loaded
// agents.Registry and canonical profiles directory.
// Used when the registry was loaded externally (e.g., in the app entrypoint).
func NewResolverFromRegistry(reg *agents.Registry, profilesDir string) *Resolver {
	r := &Resolver{
		agentsReg: reg,
		builtins:  builtinProfiles(),
		canonical: make(map[string]*PromptProfile),
	}
	if profilesDir != "" {
		r.canonical = LoadCanonical(profilesDir)
	}
	return r
}

// Get returns the PromptProfile with the given name (case-insensitive),
// or nil if not found. Precedence: canonical profiles override legacy agents.
func (r *Resolver) Get(name string) *PromptProfile {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getLocked(name)
}

// GetVisible returns the profile only when the caller is allowed to see/use it.
func (r *Resolver) GetVisible(name string, isOwner bool) *PromptProfile {
	profile := r.Get(name)
	if !ProfileVisible(profile, isOwner) {
		return nil
	}
	return profile
}

// ProfileVisible reports whether a profile can be listed, explained, or invoked.
func ProfileVisible(profile *PromptProfile, isOwner bool) bool {
	if profile == nil {
		return false
	}
	return isOwner || profile.Public
}

func (r *Resolver) getLocked(name string) *PromptProfile {
	key := strings.ToLower(name)

	// Canonical global profiles have higher precedence than legacy agents.
	if pp, ok := r.canonical[key]; ok {
		return pp
	}

	// Legacy agents.
	if r.agentsReg != nil {
		if agent := r.agentsReg.Get(name); agent != nil {
			return FromAgent(agent)
		}
	}

	// Builtins as fallback.
	return r.builtins[key]
}

// List returns all available Prompt Profiles sorted by name.
// Precedence: canonical > legacy agents > builtins.
// Canonical and legacy profiles override builtins when they share the same name.
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

	// Override with legacy agents.
	if r.agentsReg != nil {
		for _, agent := range r.agentsReg.Agents() {
			key := strings.ToLower(agent.Name)
			if seen[key] {
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

	// Override with canonical profiles (highest precedence for global).
	for key, cp := range r.canonical {
		if seen[key] {
			for i, p := range result {
				if strings.ToLower(p.Name) == key {
					result[i] = cp
					break
				}
			}
		} else {
			seen[key] = true
			result = append(result, cp)
		}
	}

	// Sort by name.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// ListVisible returns all profiles visible to the caller, sorted by name.
func (r *Resolver) ListVisible(isOwner bool) []*PromptProfile {
	all := r.List()
	if isOwner {
		return all
	}
	var visible []*PromptProfile
	for _, p := range all {
		if ProfileVisible(p, false) {
			visible = append(visible, p)
		}
	}
	return visible
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
	return r.ResolveEffectiveForUser(text, activeDefault, true)
}

// ResolveEffectiveForUser determines the effective profile and enforces
// visibility for non-owner users.
func (r *Resolver) ResolveEffectiveForUser(text string, activeDefault string, isOwner bool) (*PromptProfile, string, error) {
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
		if !ProfileVisible(profile, isOwner) {
			return nil, "", &ErrProfileNotAllowed{Name: extractAtName(text)}
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
	if !ProfileVisible(profile, isOwner) {
		return nil, "", &ErrProfileNotAllowed{Name: name}
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
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx == -1 {
		// "@name" with no text after — treat as profile invocation without content.
		// Return the profile but keep text as-is (stripping would empty it).
		profile := r.Get(rest)
		return profile, text, true
	}

	name := rest[:spaceIdx]
	profile := r.Get(name)
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
