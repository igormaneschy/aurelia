package security

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLogAudit_WritesDedicatedFile(t *testing.T) {
	var stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "audit.log")
	SetAuditWriter(&stderr)
	SetAuditFile(path, 1024, 1)
	defer SetAuditFile("", 0, 0)
	defer SetAuditWriter(os.Stderr)

	LogAudit(AuditEvent{Decision: DecisionBlock, ToolName: "Bash", Reason: "blocked", Profile: ProfileExecuteSafe})

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(audit.log) error = %v", err)
	}
	if !strings.Contains(string(content), `"tool_name":"Bash"`) {
		t.Fatalf("audit file missing tool event: %s", content)
	}
	if !strings.Contains(stderr.String(), "[security]") {
		t.Fatalf("stderr writer missing security prefix: %s", stderr.String())
	}
}

// ── Audit redaction regression tests ─────────────────────────────────────

func TestRedactAuditSensitive_APIKey(t *testing.T) {
	t.Parallel()
	input := "token sk-proj-abc123def4567890abcdef in reason"
	got := redactAuditSensitive(input)
	if strings.Contains(got, "sk-proj-abc123def4567890abcdef") {
		t.Errorf("API key not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got: %s", got)
	}
}

func TestRedactAuditSensitive_BearerToken(t *testing.T) {
	t.Parallel()
	input := "Authorization: Bearer ghp_abc123def456ghi789jkl012mno345pqr678stu901"
	got := redactAuditSensitive(input)
	if strings.Contains(got, "ghp_abc123def456") {
		t.Errorf("Bearer token not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got: %s", got)
	}
}

func TestRedactAuditSensitive_JWT(t *testing.T) {
	t.Parallel()
	input := "access_token=eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dKcGZrKQgQ"
	got := redactAuditSensitive(input)
	if strings.Contains(got, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("JWT not redacted: %s", got)
	}
	if !strings.Contains(got, "[CREDENTIAL_REDACTED]") {
		t.Errorf("expected [CREDENTIAL_REDACTED] marker, got: %s", got)
	}
}

func TestRedactAuditSensitive_PrivateKeyBlock(t *testing.T) {
	t.Parallel()
	input := "key: -----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA0gI5j5KfVw==\n-----END RSA PRIVATE KEY-----"
	got := redactAuditSensitive(input)
	if strings.Contains(got, "MIIEpAIBAAKCAQEA0gI5j5KfVw==") {
		t.Errorf("private key body not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got: %s", got)
	}
}

func TestRedactAuditSensitive_PathWithSecrets(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{name: "AWS key in path", input: "access AKIAIOSFODNN7EXAMPLE via"},
		{name: "GitHub PAT in reason", input: "token github_pat_abc123def456ghi789jkl012mno345pqr678stu901vwx234"},
		{name: "xAI key", input: "xai-abc123def456ghi789jkl012mno345"},
		{name: "GitLab token", input: "glpat-abc123def456ghi789jkl012mno345"},
		{name: "HF token", input: "hf_abc123def456ghi789jkl012mno345"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactAuditSensitive(tc.input)
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("expected [REDACTED] marker for %q, got: %s", tc.input, got)
			}
		})
	}
}

func TestRedactAuditSensitive_FalsePositives(t *testing.T) {
	t.Parallel()
	cases := []string{
		"Please enter your password to continue.",
		"the secret was not found",
		"We need authorization from the admin",
		"the token is valid",
		// Intentionally removed: "Located at /home/user/.ssh/id_rsa"
		// — now redacted as sensitive path per hardening requirement.
	}
	for _, input := range cases {
		label := input
		if len(label) > 20 {
			label = label[:20]
		}
		t.Run(label, func(t *testing.T) {
			got := redactAuditSensitive(input)
			if got != input {
				t.Errorf("false positive: input=%q got=%q", input, got)
			}
		})
	}
}

// ── Sensitive path redaction tests ───────────────────────────────────────

func TestRedactAuditSensitive_SSHPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{name: "absolute .ssh path", input: "/home/user/.ssh/id_ed25519"},
		{name: "tilde .ssh path", input: "~/.ssh/id_rsa"},
		{name: "relative .ssh path", input: ".ssh/known_hosts"},
		{name: ".ssh in cwd", input: "cwd=/home/user/.ssh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactAuditSensitive(tc.input)
			if strings.Contains(got, ".ssh") {
				t.Errorf(".ssh not redacted in %q: got %q", tc.input, got)
			}
			if !strings.Contains(got, "[SENSITIVE_PATH_REDACTED]") {
				t.Errorf("expected [SENSITIVE_PATH_REDACTED] marker for %q, got: %q", tc.input, got)
			}
		})
	}
}

func TestRedactAuditSensitive_PiPath(t *testing.T) {
	t.Parallel()
	cases := []string{
		"/home/user/.pi/agent/auth.json",
		"~/.pi/agent/auth.json",
		"/home/user/.pi/config",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			got := redactAuditSensitive(input)
			if strings.Contains(got, ".pi/") {
				t.Errorf(".pi path not redacted in %q: got %q", input, got)
			}
			if !strings.Contains(got, "[SENSITIVE_PATH_REDACTED]") {
				t.Errorf("expected [SENSITIVE_PATH_REDACTED] for %q, got: %q", input, got)
			}
		})
	}
}

func TestRedactAuditSensitive_AureliaConfigPath(t *testing.T) {
	t.Parallel()
	cases := []string{
		"/home/user/.aurelia/config/app.json",
		"~/.aurelia/config/settings.json",
		"/home/user/.aurelia/config/credentials.json",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			got := redactAuditSensitive(input)
			if strings.Contains(got, ".aurelia/config") {
				t.Errorf(".aurelia/config path not redacted in %q: got %q", input, got)
			}
			if !strings.Contains(got, "[SENSITIVE_PATH_REDACTED]") {
				t.Errorf("expected [SENSITIVE_PATH_REDACTED] for %q, got: %q", input, got)
			}
		})
	}
}

func TestRedactAuditSensitive_GitConfigPath(t *testing.T) {
	t.Parallel()
	cases := []string{
		"/home/user/project/.git/config",
		"~/.git/config",
		".git/config",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			got := redactAuditSensitive(input)
			if strings.Contains(got, "/.git/config") {
				t.Errorf(".git/config path not redacted in %q: got %q", input, got)
			}
			if !strings.Contains(got, "[SENSITIVE_PATH_REDACTED]") {
				t.Errorf("expected [SENSITIVE_PATH_REDACTED] for %q, got: %q", input, got)
			}
		})
	}
}

func TestRedactAuditSensitive_EnvFilePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input string
	}{
		{name: ".env file", input: "/home/user/project/.env"},
		{name: ".env.production", input: "~/.env.production"},
		{name: ".env at start", input: ".env.local"},
		{name: ".env in cwd", input: "cwd=/home/user/project/.env"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactAuditSensitive(tc.input)
			if strings.Contains(got, ".env") {
				t.Errorf(".env not redacted in %q: got %q", tc.input, got)
			}
			if !strings.Contains(got, "[SENSITIVE_PATH_REDACTED]") {
				t.Errorf("expected [SENSITIVE_PATH_REDACTED] for %q, got: %q", tc.input, got)
			}
		})
	}
}

func TestRedactAuditSensitive_EnvPathFalsePositive(t *testing.T) {
	t.Parallel()
	// .envrc should NOT be redacted (it's not an env file path).
	input := "source .envrc"
	got := redactAuditSensitive(input)
	if got != input {
		t.Errorf("false positive: input=%q got=%q", input, got)
	}
}

func TestRedactAuditSensitive_GitignoreFalsePositive(t *testing.T) {
	t.Parallel()
	// .gitignore should NOT be redacted (it's not .git/config).
	input := "check .gitignore"
	got := redactAuditSensitive(input)
	if got != input {
		t.Errorf("false positive: input=%q got=%q", input, got)
	}
}

func TestLogAudit_RedactsSensitivePathInReason(t *testing.T) {
	var buf bytes.Buffer
	SetAuditWriter(&buf)
	defer SetAuditWriter(os.Stderr)

	LogAudit(AuditEvent{
		Decision: DecisionBlock,
		ToolName: "Read",
		Reason:   "access to sensitive path blocked: /home/user/.aurelia/config/app.json",
		Profile:  ProfileReadOnly,
		CWD:      "/home/user/project",
	})

	output := buf.String()
	if strings.Contains(output, ".aurelia/config") {
		t.Errorf("sensitive path leaked in audit output: %s", output)
	}
	if !strings.Contains(output, "[SENSITIVE_PATH_REDACTED]") {
		t.Errorf("expected [SENSITIVE_PATH_REDACTED] in audit output: %s", output)
	}
}

func TestLogAudit_RedactsSensitivePathInCWD(t *testing.T) {
	var buf bytes.Buffer
	SetAuditWriter(&buf)
	defer SetAuditWriter(os.Stderr)

	LogAudit(AuditEvent{
		Decision: DecisionAllow,
		ToolName: "Bash",
		Reason:   "executed in project",
		Profile:  ProfileExecuteSafe,
		CWD:      "/home/user/.aurelia/config",
	})

	output := buf.String()
	if strings.Contains(output, ".aurelia/config") {
		t.Errorf("sensitive path leaked in CWD field: %s", output)
	}
	if !strings.Contains(output, "[SENSITIVE_PATH_REDACTED]") {
		t.Errorf("expected [SENSITIVE_PATH_REDACTED] in audit output: %s", output)
	}
}

func TestLogAudit_RedactsReasonBeforeWrite(t *testing.T) {
	var buf bytes.Buffer
	SetAuditWriter(&buf)
	defer SetAuditWriter(os.Stderr)

	// Reason contains an API key — must not appear raw in output.
	LogAudit(AuditEvent{
		Decision: DecisionBlock,
		ToolName: "Bash",
		Reason:   "cat sk-proj-abc123def4567890abcdef blocked",
		Profile:  ProfileExecuteSafe,
		CWD:      "/home/user/project",
	})

	output := buf.String()
	if strings.Contains(output, "sk-proj-abc123def4567890abcdef") {
		t.Errorf("API key leaked in audit output: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in audit output: %s", output)
	}
}

func TestLogAudit_RedactsBearerTokenInReason(t *testing.T) {
	var buf bytes.Buffer
	SetAuditWriter(&buf)
	defer SetAuditWriter(os.Stderr)

	LogAudit(AuditEvent{
		Decision:  DecisionBlock,
		ToolName:  "Bash",
		Reason:    `Authorization: Bearer ghp_abc123def456ghi789jkl012mno345pqr678stu901`,
		Profile:   ProfileExecuteSafe,
		CWD:       "/home/user/project",
		AgentName: "helper",
	})

	output := buf.String()
	if strings.Contains(output, "ghp_abc123def456") {
		t.Errorf("Bearer token leaked in audit output: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in audit output: %s", output)
	}
}

func TestLogAudit_RedactsJWAndPrivateKey(t *testing.T) {
	var buf bytes.Buffer
	SetAuditWriter(&buf)
	defer SetAuditWriter(os.Stderr)

	reason := `JWT: eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dKcGZrKQgQ`
	if !regexp.MustCompile(`eyJ[A-Za-z0-9]`).MatchString(reason) {
		t.Fatal("test invariant failed: JWT pattern must match input")
	}

	LogAudit(AuditEvent{
		Decision: DecisionBlock,
		ToolName: "Bash",
		Reason:   reason,
		Profile:  ProfileExecuteSafe,
		CWD:      "/home/user/project",
	})

	output := buf.String()
	if strings.Contains(output, "eyJhbGciOiJIUzI1NiJ9") {
		t.Errorf("JWT leaked in audit output: %s", output)
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in audit output: %s", output)
	}
}

func TestLogAudit_RotatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	SetAuditWriter(&bytes.Buffer{})
	SetAuditFile(path, 80, 2)
	defer SetAuditFile("", 0, 0)
	defer SetAuditWriter(os.Stderr)

	for i := 0; i < 3; i++ {
		LogAudit(AuditEvent{Decision: DecisionAllow, ToolName: "Read", Reason: strings.Repeat("x", 60), Profile: ProfileReadOnly})
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("expected rotated audit backup: %v", err)
	}
}
