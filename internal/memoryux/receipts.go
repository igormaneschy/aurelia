package memoryux

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// ModelOutputDiagnostic builds a safe, sanitized diagnostic string from raw
// model output and an optional parse error. It includes only metadata:
//
//	parse_error=<summary> output_len=<N> fenced=true starts_with_json=true
//
// The result is truncated to maxErrorLen and sanitized (no newlines/control chars).
// It never includes the raw model output, transcripts, prompts, facts, or secrets.
func ModelOutputDiagnostic(raw string, parseErr error) string {
	if raw == "" && parseErr == nil {
		return ""
	}

	var parts []string
	if parseErr != nil {
		parts = append(parts, "parse_error="+briefParseError(parseErr))
	}
	parts = append(parts, fmt.Sprintf("output_len=%d", len(raw)))

	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "{") {
		parts = append(parts, "starts_with_json=true")
	}
	if strings.HasPrefix(trimmed, "```") {
		parts = append(parts, "fenced=true")
	}

	diag := strings.Join(parts, " ")
	if len(diag) > maxErrorLen {
		diag = diag[:maxErrorLen]
	}
	return SanitizeReceiptError(diag)
}

// briefParseError extracts a short, safe summary from a parse error chain.
// It unwraps the leaf error to avoid leaking internal paths or prompts.
func briefParseError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	// Truncate to first ~100 chars to avoid model output fragments in error message
	if len(msg) > 100 {
		msg = msg[:100]
	}
	return SanitizeReceiptError(msg)
}

const (
	receiptFilename = "memory_receipts.jsonl"
	maxErrorLen     = 300
)

// Receipt records a lightweight summary of a nudge or dream run.
// It contains only metadata — never raw transcript, prompts, facts, or secrets.
type Receipt struct {
	Time     time.Time `json:"time"`
	Source   string    `json:"source"`              // "nudge" | "dream"
	ChatID   int64     `json:"chat_id,omitempty"`   // omitted for global dreams
	ThreadID int       `json:"thread_id,omitempty"` // omitted for global dreams
	CWD      string    `json:"cwd,omitempty"`
	Duration string    `json:"duration,omitempty"`
	CostUSD  float64   `json:"cost_usd,omitempty"`
	Turns    int       `json:"turns,omitempty"`
	Applied  int       `json:"applied"`
	Total    int       `json:"total"`
	Status   string    `json:"status"` // "applied" | "noop" | "invalid" | "error"
	Error    string    `json:"error,omitempty"`
}

// AppendReceipt appends a single JSONL receipt to <memoryDir>/memory_receipts.jsonl.
// Creates the file (0600) and parent directory (0700) as needed.
// Logs but does not return I/O errors to the caller — callers should not fail
// nudge or dream consolidation because of a receipt write error.
func AppendReceipt(memoryDir string, r Receipt) error {
	if memoryDir == "" {
		return fmt.Errorf("memoryDir is empty")
	}

	if err := os.MkdirAll(memoryDir, 0700); err != nil {
		return fmt.Errorf("mkdir receipt dir: %w", err)
	}

	path := filepath.Join(memoryDir, receiptFilename)

	line, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("marshal receipt: %w", err)
	}

	// Append newline to the JSON bytes so the full line is written in a single
	// Write syscall. This prevents a concurrent writer from interleaving its
	// content between the JSON and the newline, which would corrupt both lines.
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open receipt file: %w", err)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write receipt: %w", err)
	}

	return nil
}

// LatestReceipt reads the last non-empty JSON line from the receipt file.
// Returns nil, nil if the file does not exist or is empty.
// Skips trailing empty lines and lines that fail JSON parse.
func LatestReceipt(memoryDir string) (*Receipt, error) {
	if memoryDir == "" {
		return nil, fmt.Errorf("memoryDir is empty")
	}

	path := filepath.Join(memoryDir, receiptFilename)

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open receipt file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var lastValid *Receipt
	var skipped int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var r Receipt
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			skipped++
			continue
		}
		lastValid = &r
	}

	if skipped > 0 {
		log.Printf("[memoryux] skipped %d corrupt receipt line(s) in %s", skipped, path)
	}

	return lastValid, scanner.Err()
}

// SanitizeReceiptError prepares an error string for safe storage:
// strips control chars/newlines, truncates to maxErrorLen.
func SanitizeReceiptError(msg string) string {
	if msg == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(msg))
	for _, r := range msg {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteRune(' ')
		} else if unicode.IsControl(r) {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}

	cleaned := strings.TrimSpace(b.String())
	if len(cleaned) > maxErrorLen {
		cleaned = cleaned[:maxErrorLen]
	}
	return cleaned
}
