package profiles

import (
	"os"
	"strings"

	"github.com/igormaneschy/aurelia/internal/users"
)

const modeOverlayHeader = "## User Mode Overlay\n\n"

// mergeModeOverlay appends a legacy personas/mode_<name>.md overlay onto a
// resolved profile when the profile name maps to a builtin mode (developer,
// researcher). Skipped when the profile already came from user-private
// ~/.aurelia/users/<id>/profiles/.
func mergeModeOverlay(root string, userID int64, profile *PromptProfile, fromUserPrivate bool) *PromptProfile {
	if profile == nil || fromUserPrivate || userID == 0 || root == "" {
		return profile
	}
	mode, err := users.NormalizeMode(profile.Name)
	if err != nil || mode == "" {
		return profile
	}
	userRes := users.NewResolver(root)
	path := userRes.UserModePath(userID, mode)
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return profile
		}
		return profile
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return profile
	}
	merged := cloneProfile(profile)
	base := strings.TrimSpace(merged.Prompt)
	if base == "" {
		merged.Prompt = modeOverlayHeader + content
	} else {
		merged.Prompt = base + "\n\n" + modeOverlayHeader + content
	}
	return merged
}

func cloneProfile(p *PromptProfile) *PromptProfile {
	if p == nil {
		return nil
	}
	cp := *p
	if len(p.AllowedTools) > 0 {
		cp.AllowedTools = append([]string(nil), p.AllowedTools...)
	}
	if len(p.DisallowedTools) > 0 {
		cp.DisallowedTools = append([]string(nil), p.DisallowedTools...)
	}
	if len(p.Tags) > 0 {
		cp.Tags = append([]string(nil), p.Tags...)
	}
	return &cp
}