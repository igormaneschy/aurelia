package security

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	defaultAuditMaxBytes = 5 * 1024 * 1024
	defaultAuditBackups  = 3
)

// AuditEvent represents a structured security audit entry for a tool call
// evaluation. All sensitive values are redacted before inclusion.
type AuditEvent struct {
	Timestamp time.Time         `json:"timestamp"`
	Decision  ToolDecision      `json:"decision"`
	ToolName  string            `json:"tool_name"`
	Reason    string            `json:"reason"`
	ChatID    int64             `json:"chat_id,omitempty"`
	ThreadID  int               `json:"thread_id,omitempty"`
	UserID    int64             `json:"user_id,omitempty"`
	AgentName string            `json:"agent_name,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	Profile   CapabilityProfile `json:"profile"`
	CWD       string            `json:"cwd,omitempty"`
	Redacted  bool              `json:"redacted"`
}

// auditLogger manages the audit output destination with a mutex for safe
// concurrent writes from multiple bridge requests.
type auditLogger struct {
	mu       sync.Mutex
	w        io.Writer
	filePath string
	maxBytes int64
	backups  int
}

// globalAuditLogger is the package-level audit logger instance.
var globalAuditLogger = &auditLogger{
	w:        os.Stderr,
	filePath: defaultAuditLogPath(),
	maxBytes: defaultAuditMaxBytes,
	backups:  defaultAuditBackups,
}

// SetAuditWriter replaces the stderr-style audit output writer. Useful for tests.
func SetAuditWriter(w io.Writer) {
	globalAuditLogger.mu.Lock()
	defer globalAuditLogger.mu.Unlock()
	if w == nil {
		w = io.Discard
	}
	globalAuditLogger.w = w
}

// SetAuditFile configures the dedicated JSONL audit file. Empty path disables
// file output. Rotation is size-based and keeps backup files as .1, .2, ...
func SetAuditFile(path string, maxBytes int64, backups int) {
	globalAuditLogger.mu.Lock()
	defer globalAuditLogger.mu.Unlock()
	globalAuditLogger.filePath = strings.TrimSpace(path)
	if maxBytes <= 0 {
		maxBytes = defaultAuditMaxBytes
	}
	if backups < 0 {
		backups = 0
	}
	globalAuditLogger.maxBytes = maxBytes
	globalAuditLogger.backups = backups
}

// LogAudit writes a structured audit event as a JSON line to stderr and to the
// dedicated audit file (~/.aurelia/audit.log by default).
//
// LogAudit is safe for concurrent use. If the write fails, the error is
// silently dropped — audit failures must never block execution.
func LogAudit(ev AuditEvent) {
	globalAuditLogger.mu.Lock()
	defer globalAuditLogger.mu.Unlock()

	ev.Redacted = true

	// Redact sensitive fields BEFORE serialization so credentials in
	// reason/cwd/agent_name/request_id are not leaked to audit output.
	ev.Reason = redactAuditSensitive(ev.Reason)
	ev.CWD = redactAuditSensitive(ev.CWD)
	ev.AgentName = redactAuditSensitive(ev.AgentName)
	ev.RequestID = redactAuditSensitive(ev.RequestID)

	data, err := json.Marshal(ev)
	if err != nil {
		line := []byte(`[security] {"error":"marshal_failed","reason":"` + err.Error() + `"}` + "\n")
		if _, err := globalAuditLogger.w.Write(line); err != nil {
			log.Printf("audit: failed to write marshal error line: %v", err)
		}
		if err := globalAuditLogger.writeAuditFile(line); err != nil {
			log.Printf("audit: failed to write audit file (marshal error): %v", err)
		}
		return
	}

	line := []byte("[security] " + string(data) + "\n")
	if _, err := globalAuditLogger.w.Write(line); err != nil {
		log.Printf("audit: failed to write audit event: %v", err)
	}
	if err := globalAuditLogger.writeAuditFile(line); err != nil {
		log.Printf("audit: failed to write audit file: %v", err)
	}
}

func (l *auditLogger) writeAuditFile(line []byte) error {
	if l.filePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.filePath), 0o700); err != nil {
		return err
	}
	if err := l.rotateIfNeeded(int64(len(line))); err != nil {
		return err
	}
	file, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("audit: failed to close audit file: %v", err)
		}
	}()
	_, err = file.Write(line)
	return err
}

func (l *auditLogger) rotateIfNeeded(incomingBytes int64) error {
	if l.maxBytes <= 0 {
		return nil
	}
	info, err := os.Stat(l.filePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size()+incomingBytes <= l.maxBytes {
		return nil
	}
	if l.backups == 0 {
		return os.Truncate(l.filePath, 0)
	}
	for i := l.backups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", l.filePath, i)
		newPath := fmt.Sprintf("%s.%d", l.filePath, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			if err := os.Rename(oldPath, newPath); err != nil {
				log.Printf("audit: failed to rotate audit log %s -> %s: %v", oldPath, newPath, err)
			}
		}
	}
	return os.Rename(l.filePath, l.filePath+".1")
}

// Pre-compiled redaction regexes for credential patterns in audit output.
var (
	apiKeyRE       = regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`)
	pkKeyRE        = regexp.MustCompile(`\bpk-[A-Za-z0-9]{20,}`)
	skAntKeyRE     = regexp.MustCompile(`\bsk-ant-[A-Za-z0-9]{20,}`)
	skProjKeyRE    = regexp.MustCompile(`\bsk-proj-[A-Za-z0-9]{20,}`)
	stripeLiveRE   = regexp.MustCompile(`\bsk_live_[A-Za-z0-9]+`)
	stripeTestRE   = regexp.MustCompile(`\bsk_test_[A-Za-z0-9]+`)
	awsKeyRE       = regexp.MustCompile(`\bAKIA[A-Z0-9]{16}`)
	gcpKeyRE       = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`)
	ghTokenRE      = regexp.MustCompile(`\bgh[puosr]_[A-Za-z0-9]{36}`)
	ghPatRE        = regexp.MustCompile(`\bgithub_pat_[0-9A-Za-z_-]+`)
	jwtRE          = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+`)
	xaiKeyRE       = regexp.MustCompile(`\bxai-[A-Za-z0-9]{20,}`)
	glTokenRE      = regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}`)
	hfTokenRE      = regexp.MustCompile(`\bhf_[A-Za-z0-9]{20,}`)
	npmTokenRE     = regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}`)
	slackTokenRE   = regexp.MustCompile(`\bxox[bpasa]-[A-Za-z0-9-]{20,}`)
	slackAppRE     = regexp.MustCompile(`\bxapp-[A-Za-z0-9-]{20,}`)
	privateKeyRE   = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----`)
	authHeaderRE   = regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|Basic)\s+)\S+`)
	credValueRE    = regexp.MustCompile(`(?i)(password|secret|token|api_key|api-key|apikey|client_secret|access_token|refresh_token)\s*[=:]\s*\S+`)
	jsonCredValueRE = regexp.MustCompile(`"(?:apiKey|api_key|clientSecret|client_secret|accessToken|access_token|refreshToken|refresh_token|token)"\s*:\s*"[^"]{4,}"`)

	// Sensitive path patterns for audit redaction. These match path references
	// that would leak information about secret locations, credential files, or
	// config directories. Each pattern requires a preceding / or ~ or start of
	// string to avoid false positives on unrelated words (e.g. .gitignore).
	// The entire match is replaced, not just the directory name.
	envFileRE    = regexp.MustCompile(`(?:^|[/~])\.env(?:\b|/|$)`)
	sshPathRE    = regexp.MustCompile(`(?:^|[/~])\.ssh(?:/|$)`)
	piPathRE     = regexp.MustCompile(`(?:^|[/~])\.pi(?:/|$)`)
	aureliaCfgRE = regexp.MustCompile(`(?:^|[/~])\.aurelia/config(?:/|$)`)
	gitCfgRE     = regexp.MustCompile(`(?:^|[/~])\.git/config(?:\b|/|$)`)
)

// redactAuditSensitive replaces common credential patterns in a string with
// [REDACTED] markers. Applied to audit event text fields before JSON marshal.
func redactAuditSensitive(s string) string {
	if s == "" {
		return s
	}
	result := s

	// Prefix-pattern API keys and tokens (most selective first)
	for _, re := range []*regexp.Regexp{
		privateKeyRE, // multiline blocks first
		skProjKeyRE, skAntKeyRE, apiKeyRE, pkKeyRE,
		stripeLiveRE, stripeTestRE,
		awsKeyRE, gcpKeyRE,
		ghPatRE, ghTokenRE,
		jwtRE,
		xaiKeyRE, glTokenRE, hfTokenRE, npmTokenRE,
		slackTokenRE, slackAppRE,
	} {
		result = re.ReplaceAllString(result, "[REDACTED]")
	}

	// Authorization headers
	result = authHeaderRE.ReplaceAllString(result, "$1[REDACTED]")

	// Sensitive file/directory paths — replaces the entire path reference
	// (e.g. "/home/user/.ssh/id_rsa" → "[SENSITIVE_PATH_REDACTED]")
	// so that credential locations are not leaked in audit output.
	for _, re := range []*regexp.Regexp{
		sshPathRE, piPathRE, aureliaCfgRE, gitCfgRE, envFileRE,
	} {
		result = re.ReplaceAllString(result, "[SENSITIVE_PATH_REDACTED]")
	}

	// Line-based credential key=value patterns
	lines := strings.Split(result, "\n")
	var filtered []string
	for _, line := range lines {
		if credValueRE.MatchString(line) {
			filtered = append(filtered, "[CREDENTIAL_REDACTED]")
			continue
		}
		if jsonCredValueRE.MatchString(line) {
			filtered = append(filtered, "[CREDENTIAL_REDACTED]")
			continue
		}
		filtered = append(filtered, line)
	}
	return strings.Join(filtered, "\n")
}

func defaultAuditLogPath() string {
	if root := strings.TrimSpace(os.Getenv("AURELIA_HOME")); root != "" {
		return filepath.Join(root, "audit.log")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".aurelia", "audit.log")
}
