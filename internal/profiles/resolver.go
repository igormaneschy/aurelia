package profiles

import (
	"fmt"
	"log"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/igormaneschy/aurelia/internal/agents"
)

// Resolver loads Prompt Profiles from multiple sources, normalizes them,
// and resolves the effective profile for a message turn.
//
// Load order / precedence (highest to lowest):
//  1. User-private profiles (~/.aurelia/users/<id>/profiles/*.md)
//  2. Canonical global profiles (~/.aurelia/profiles/*.md)
//  3. Legacy agents (~/.aurelia/agents/*.md)
//  4. Builtins (general, developer, researcher)
//
// Legacy mode_<name>.md overlays (personas/) merge into the resolved profile
// for developer/researcher when not sourced from user-private profiles/.
//
// Builtins cannot be silently deleted; profiles at higher levels override
// lower levels of the same name.
type Resolver struct {
	agentsDir   string
	profilesDir string
	root        string // ~/.aurelia instance root for user-private paths
	agentsReg   *agents.Registry
	canonical   map[string]*PromptProfile
	builtins    map[string]*PromptProfile
	userPrivate map[int64]map[string]*PromptProfile
	mu          sync.RWMutex
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
// root is the Aurelia instance root (~/.aurelia) used for user-private profiles.
// Returns error if the directories cannot be read (but builtins are still available).
func NewResolver(agentsDir, profilesDir, root string) (*Resolver, error) {
	r := &Resolver{
		agentsDir:   agentsDir,
		profilesDir: profilesDir,
		root:        root,
		builtins:    builtinProfiles(),
		canonical:   make(map[string]*PromptProfile),
		userPrivate: make(map[int64]map[string]*PromptProfile),
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
func NewResolverFromRegistry(reg *agents.Registry, profilesDir, root string) *Resolver {
	r := &Resolver{
		agentsReg:   reg,
		profilesDir: profilesDir,
		root:        root,
		builtins:    builtinProfiles(),
		canonical:   make(map[string]*PromptProfile),
		userPrivate: make(map[int64]map[string]*PromptProfile),
	}
	if profilesDir != "" {
		r.canonical = LoadCanonical(profilesDir)
	}
	return r
}

// Get returns the PromptProfile with the given name (case-insensitive),
// or nil if not found. Does not apply user-private profiles or mode overlays.
// Prefer GetForUser when userID is known.
func (r *Resolver) Get(name string) *PromptProfile {
	return r.GetForUser(0, name)
}

// GetForUser returns the effective profile for a user, applying full precedence
// and legacy mode_<name>.md overlays when applicable.
func (r *Resolver) GetForUser(userID int64, name string) *PromptProfile {
	if r == nil {
		return nil
	}
	userProfiles := r.userProfilesFor(userID)
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.resolveProfile(userID, name, userProfiles)
}

// GetVisible returns the profile only when the caller is allowed to see/use it.
func (r *Resolver) GetVisible(name string, isOwner bool) *PromptProfile {
	return r.GetVisibleForUser(0, name, isOwner)
}

// GetVisibleForUser applies visibility rules for a user-scoped profile lookup.
func (r *Resolver) GetVisibleForUser(userID int64, name string, isOwner bool) *PromptProfile {
	profile := r.GetForUser(userID, name)
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

func (r *Resolver) resolveProfile(userID int64, name string, userProfiles map[string]*PromptProfile) *PromptProfile {
	key := strings.ToLower(name)

	if userProfiles != nil {
		if pp, ok := userProfiles[key]; ok {
			return cloneProfile(pp)
		}
	}

	if pp, ok := r.canonical[key]; ok {
		return mergeModeOverlay(r.root, userID, cloneProfile(pp), false)
	}

	if r.agentsReg != nil {
		if agent := r.agentsReg.Get(name); agent != nil {
			return mergeModeOverlay(r.root, userID, cloneProfile(FromAgent(agent)), false)
		}
	}

	if bp, ok := r.builtins[key]; ok {
		return mergeModeOverlay(r.root, userID, cloneProfile(bp), false)
	}

	return nil
}

func (r *Resolver) userProfilesFor(userID int64) map[string]*PromptProfile {
	if userID == 0 || r.root == "" {
		return nil
	}

	r.mu.RLock()
	if m, ok := r.userPrivate[userID]; ok {
		r.mu.RUnlock()
		return m
	}
	r.mu.RUnlock()

	dir := filepath.Join(r.root, "users", fmt.Sprintf("%d", userID), "profiles")
	loaded := LoadUserPrivate(dir)

	r.mu.Lock()
	defer r.mu.Unlock()
	if m, ok := r.userPrivate[userID]; ok {
		return m
	}
	if r.userPrivate == nil {
		r.userPrivate = make(map[int64]map[string]*PromptProfile)
	}
	r.userPrivate[userID] = loaded
	return loaded
}

// List returns all available Prompt Profiles sorted by name.
// Precedence: user-private > canonical > legacy agents > builtins.
func (r *Resolver) List() []*PromptProfile {
	return r.ListForUser(0)
}

// ListForUser returns all profiles visible in the catalog for a user,
// merging user-private overrides at highest precedence.
func (r *Resolver) ListForUser(userID int64) []*PromptProfile {
	if r == nil {
		return nil
	}
	userProfiles := r.userProfilesFor(userID)
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var result []*PromptProfile

	for name, bp := range r.builtins {
		seen[name] = true
		result = append(result, bp)
	}

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

	if userProfiles != nil {
		for key, up := range userProfiles {
			if seen[key] {
				for i, p := range result {
					if strings.ToLower(p.Name) == key {
						result[i] = up
						break
					}
				}
			} else {
				seen[key] = true
				result = append(result, up)
			}
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// ListVisible returns all profiles visible to the caller, sorted by name.
func (r *Resolver) ListVisible(isOwner bool) []*PromptProfile {
	return r.ListVisibleForUser(0, isOwner)
}

// ListVisibleForUser filters ListForUser by visibility rules.
func (r *Resolver) ListVisibleForUser(userID int64, isOwner bool) []*PromptProfile {
	all := r.ListForUser(userID)
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
func (r *Resolver) ResolveEffective(text string, activeDefault string) (*PromptProfile, string, error) {
	return r.ResolveEffectiveForUser(text, activeDefault, 0, true)
}

// ResolveEffectiveForUser determines the effective profile and enforces
// visibility for non-owner users. userID scopes user-private profiles and
// legacy mode overlays.
func (r *Resolver) ResolveEffectiveForUser(text string, activeDefault string, userID int64, isOwner bool) (*PromptProfile, string, error) {
	if r == nil {
		return builtinProfiles()["general"], text, nil
	}

	if profile, stripped, found := r.parseAtProfileForUser(text, userID); found {
		if profile == nil {
			return nil, "", &ErrProfileNotFound{Name: extractAtName(text)}
		}
		if !ProfileVisible(profile, isOwner) {
			return nil, "", &ErrProfileNotAllowed{Name: extractAtName(text)}
		}
		return profile, stripped, nil
	}

	name := activeDefault
	if name == "" {
		name = "general"
	}
	profile := r.GetForUser(userID, name)
	if profile == nil {
		log.Printf("profiles: active default %q not found, falling back to general", name)
		profile = r.GetForUser(userID, "general")
		if profile == nil {
			profile = r.builtins["general"]
		}
	}
	if !ProfileVisible(profile, isOwner) {
		return nil, "", &ErrProfileNotAllowed{Name: name}
	}
	return profile, text, nil
}

func (r *Resolver) parseAtProfileForUser(text string, userID int64) (*PromptProfile, string, bool) {
	if !strings.HasPrefix(text, "@") {
		return nil, "", false
	}

	rest := text[1:]
	spaceIdx := strings.IndexByte(rest, ' ')
	if spaceIdx == -1 {
		profile := r.GetForUser(userID, rest)
		return profile, text, true
	}

	name := rest[:spaceIdx]
	profile := r.GetForUser(userID, name)
	stripped := strings.TrimSpace(rest[spaceIdx+1:])
	if stripped == "" {
		stripped = text
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