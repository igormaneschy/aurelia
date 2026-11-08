package security

// PolicyMode controls how strictly policy rules are enforced.
type PolicyMode string

const (
	// PolicyWarn evaluates all tool calls but only blocks extreme cases.
	// Violations are logged and allowed. Use during Phase 1 rollout.
	PolicyWarn PolicyMode = "warn"

	// PolicyBlock blocks all violations (sensitive paths, destructive commands,
	// exfiltration, outside-cwd writes). Normal build/test/lint remains allowed.
	PolicyBlock PolicyMode = "block"
)

// BashPolicy defines granular rules for bash command evaluation.
type BashPolicy struct {
	AllowBuild     bool `json:"allow_build"`      // go build, npm run, etc.
	AllowGitSafe   bool `json:"allow_git_safe"`   // git status, diff, log
	AllowTest      bool `json:"allow_test"`       // go test, npm test
	Destructive    bool `json:"destructive"`      // rm -rf, sudo, chmod
	AllowEnvAccess bool `json:"allow_env_access"` // env, printenv, $VAR
}

// SecurityConfig holds the complete security policy configuration.
type SecurityConfig struct {
	Mode                   PolicyMode        `json:"mode"`                // "warn" | "block"
	DefaultProfile         CapabilityProfile `json:"default_profile"`     // default for coding contexts
	AllowPrivilegedAgents  bool              `json:"allow_privileged"`    // allow privileged profile
	SensitivePathPatterns  []string          `json:"sensitive_paths"`     // glob patterns for sensitive paths
	AllowedOutsideCWDPaths []string          `json:"allowed_outside_cwd"` // exceptions to cwd boundary
	BashRules              BashPolicy        `json:"bash_rules"`
}

// ToolDecision represents the outcome of a policy evaluation.
type ToolDecision string

const (
	DecisionAllow            ToolDecision = "allow"
	DecisionBlock            ToolDecision = "block"
	DecisionRewrite          ToolDecision = "rewrite"
	DecisionApprovalRequired ToolDecision = "approval_required"
)

// PolicyDecision is the result of evaluating a tool call against the policy.
type PolicyDecision struct {
	Decision ToolDecision   `json:"decision"`
	Reason   string         `json:"reason"`
	Redacted map[string]any `json:"redacted,omitempty"`
}

// DefaultConfig returns safe defaults for Phase 2 Block mode.
func DefaultConfig() SecurityConfig {
	return SecurityConfig{
		Mode:           PolicyBlock,
		DefaultProfile: ProfileExecuteSafe,
		SensitivePathPatterns: []string{
			".env",
			".env.*",
			"*.pem",
			"*.key",
			"id_rsa",
			"id_ed25519",
			"config.json",
			"credentials.json",
			"*.credentials",
			"service-account*.json",
			".ssh/*",
			".pi/*",
			".aurelia/config/*",
			".notebooklm/*",
			".git/config",
		},
		AllowedOutsideCWDPaths: nil,
		BashRules: BashPolicy{
			AllowBuild:     true,
			AllowGitSafe:   true,
			AllowTest:      true,
			Destructive:    false,
			AllowEnvAccess: false,
		},
	}
}
