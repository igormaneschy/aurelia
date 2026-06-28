package bridge

import (
	"github.com/igormaneschy/aurelia/internal/security"
)

// BuildSecurityContext constructs a SecurityContext with SensitivePaths and
// AllowedOutsideCWD forwarded from config, resolves the capability profile
// against agent tool constraints, and applies the privileged→execute_safe
// downgrade when AllowPrivilegedAgents is false — re-resolving tools via
// security.ResolveProfile so DisallowedTools are not lost.
//
// secCfg may be nil; DefaultConfig() is used as fallback.
func BuildSecurityContext(
	capProfile security.CapabilityProfile,
	allowedTools []string,
	disallowedTools []string,
	hasCWD bool,
	secCfg *security.SecurityConfig,
	cwd string,
	chatID int64,
	threadID int,
	userID int64,
	agentName string,
	requestID string,
) (security.CapabilityProfile, []string, *SecurityContext) {
	if secCfg == nil {
		def := security.DefaultConfig()
		secCfg = &def
	}

	// Resolve profile against agent-level tool constraints.
	profile, tools := security.ResolveProfile(capProfile, allowedTools, disallowedTools, hasCWD)

	sec := &SecurityContext{
		Enabled:           true,
		Profile:           string(profile),
		Mode:              string(secCfg.Mode),
		Cwd:               cwd,
		SensitivePaths:    secCfg.SensitivePathPatterns,
		AllowedOutsideCWD: secCfg.AllowedOutsideCWDPaths,
		ChatID:            chatID,
		ThreadID:          threadID,
		UserID:            userID,
		AgentName:         agentName,
		RequestID:         requestID,
	}

	// Downgrade privileged → execute_safe when not explicitly allowed.
	// Re-resolves tools so DisallowedTools are not lost (Finding #1).
	if profile == security.ProfilePrivileged && !secCfg.AllowPrivilegedAgents {
		downgraded := security.ProfileExecuteSafe
		_, tools = security.ResolveProfile(downgraded, allowedTools, disallowedTools, hasCWD)
		profile = downgraded
		sec.Profile = string(profile)
	}

	return profile, tools, sec
}
