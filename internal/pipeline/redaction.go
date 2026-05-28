package pipeline

import (
	"regexp"
	"strings"
)

// summarizeToolResult produces a truncated, redacted summary of a tool result.
func summarizeToolResult(content string) string {
	if content == "" {
		return ""
	}
	// Redact common secret patterns (API keys, tokens, etc.)
	redacted := redactSecrets(content)
	// Take first 1KB
	truncated := strings.TrimSpace(redacted)
	if len(truncated) > 1024 {
		truncated = truncated[:1024]
	}
	return truncated
}

// Pre-compiled redaction regexes to avoid re-parsing on every call.
var (
	prefixREs    []*regexp.Regexp
	prefixLabels []string
	privateKeyRE *regexp.Regexp
	authRE       *regexp.Regexp
	lineRE       *regexp.Regexp
	jsonSecretRE *regexp.Regexp
)

func init() {
	type pattern struct {
		repl  string
		label string
	}
	prefixPatterns := []pattern{
		// API keys
		{`\bsk-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bpk-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk-ant-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk-proj-[A-Za-z0-9]{20,}`, "[API_KEY_REDACTED]"},
		{`\bsk_live_[A-Za-z0-9]+`, "[STRIPE_KEY_REDACTED]"},
		{`\bsk_test_[A-Za-z0-9]+`, "[STRIPE_KEY_REDACTED]"},
		// Cloud provider keys
		{`\bAKIA[A-Z0-9]{16}`, "[AWS_KEY_REDACTED]"},
		{`\bAIza[0-9A-Za-z_-]{35}`, "[GCP_KEY_REDACTED]"},
		// GitHub tokens
		{`\bghp_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bgho_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghu_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghs_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bghr_[A-Za-z0-9]{36}`, "[GH_TOKEN_REDACTED]"},
		{`\bgithub_pat_[0-9A-Za-z_-]+`, "[GH_PAT_REDACTED]"},
		// Other tokens
		{`\bglpat-[A-Za-z0-9_-]{20,}`, "[GL_TOKEN_REDACTED]"},
		{`\bhf_[A-Za-z0-9]{20,}`, "[HF_TOKEN_REDACTED]"},
		{`\bnpm_[A-Za-z0-9]{36}`, "[NPM_TOKEN_REDACTED]"},
		{`\bxox[bpasa]-[A-Za-z0-9-]{20,}`, "[SLACK_TOKEN_REDACTED]"},
		{`\bxapp-[A-Za-z0-9-]{20,}`, "[SLACK_TOKEN_REDACTED]"},
		// Base64-encoded JSON (JWT-like)
		{`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+`, "[JWT_REDACTED]"},
		// AI provider keys
		{`\bxai-[A-Za-z0-9]{20,}`, "[XAI_KEY_REDACTED]"},
	}

	prefixREs = make([]*regexp.Regexp, len(prefixPatterns))
	prefixLabels = make([]string, len(prefixPatterns))
	for i, p := range prefixPatterns {
		prefixREs[i] = regexp.MustCompile(p.repl)
		prefixLabels[i] = p.label
	}

	privateKeyRE = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----`)
	authRE = regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|Basic)\s+)\S+`)
	lineRE = regexp.MustCompile(`(password|secret|api_key|api-key|api\.key|apikey|clientsecret|client_secret|access_token|refresh_token|token)\s*[=:]\s*\S+`)
	jsonSecretRE = regexp.MustCompile(`"(?:apiKey|api_key|api-key|api\.key|clientSecret|client_secret|client-secret|client\.secret|accessToken|access_token|access-token|access\.token|refreshToken|refresh_token|refresh-token|refresh\.token|token)"\s*:\s*"[^"]{4,}"`)
}

// RedactSecrets is the public wrapper for redactSecrets, shared with other
// packages (e.g. dream nudge prompt) that need credential redaction.
func RedactSecrets(s string) string { return redactSecrets(s) }

// redactSecrets replaces common credential patterns with [REDACTED].
// All regexes are pre-compiled at init for performance.
func redactSecrets(s string) string {
	result := s
	for i, re := range prefixREs {
		result = re.ReplaceAllString(result, prefixLabels[i])
	}

	// Multi-line block redaction (must run before line splitting)
	result = privateKeyRE.ReplaceAllString(result, "[PRIVATE_KEY_BLOCK_REDACTED]")

	// Header-based auth: Authorization: Bearer xxx, Authorization: Basic xxx
	result = authRE.ReplaceAllString(result, "$1[REDACTED]")

	// Line-based redaction for structured data with known keys
	lines := strings.Split(result, "\n")
	var filtered []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		// Match key=value, key:value, key = value, and key: value patterns
		// Patterns cover: password, secret, api_key, api-key, api.key, apikey,
		// clientsecret, access_token, refresh_token, and generic token.
		if lineRE.MatchString(lower) {
			filtered = append(filtered, "[CREDENTIAL_REDACTED]")
			continue
		}
		// JSON-style embedded secrets: "apiKey":"xxx", "clientSecret":"xxx"
		if jsonSecretRE.MatchString(line) {
			filtered = append(filtered, "[CREDENTIAL_REDACTED]")
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}
