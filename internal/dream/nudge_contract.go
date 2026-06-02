package dream

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"unicode"
)

// suspiciousPrefixes signal role/instruction injection attempts.
// Only match colon-suffixed roles and multi-word meta-instruction markers.
// Avoid single bare words like "user" or "never" that appear naturally.
var suspiciousPrefixes = []string{
	"system:", "assistant:", "user:",
	"instruction:", "important:", "warning:",
	"remember:", "attention:",
	"you are now", "you're now", "your task is", "never follow",
}

// memoryUpdate describes one file to update during nudge extraction.
// The model produces these as structured JSON (not via file tools).
type memoryUpdate struct {
	Layer    string   `json:"layer"`    // user_global, topic, cwd_overlay, project_team
	Filename string   `json:"filename"` // basename .md only
	Title    string   `json:"title,omitempty"`
	Facts    []string `json:"facts"`
}

// nudgeExtraction is the top-level contract from the model.
type nudgeExtraction struct {
	Updates []memoryUpdate `json:"updates"`
}

const (
	maxUpdatesPerRun    = 3
	maxFactsPerFile     = 20
	maxFactLength       = 500
	maxFactLengthLabel  = "500"
)

// parseNudgeJSON parses model output into a nudgeExtraction.
// It uses extractJSONObject for tolerant extraction, accepting direct JSON,
// fenced code blocks, and text with embedded JSON.
// Returns nil if parsing fails or the result is empty.
func parseNudgeJSON(raw string) *nudgeExtraction {
	ext, _ := parseNudgeJSONWithError(raw)
	return ext
}

// parseNudgeJSONWithError is like parseNudgeJSON but also returns the
// underlying parse/extraction error for diagnostics.
func parseNudgeJSONWithError(raw string) (*nudgeExtraction, error) {
	jsonStr, err := extractJSONObject(raw)
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}

	var ext nudgeExtraction
	if err := json.Unmarshal([]byte(jsonStr), &ext); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	// Validate, sanitize, and cap
	if len(ext.Updates) > maxUpdatesPerRun {
		log.Printf("[nudge] capping %d updates to %d", len(ext.Updates), maxUpdatesPerRun)
		ext.Updates = ext.Updates[:maxUpdatesPerRun]
	}
	for i := range ext.Updates {
		u := &ext.Updates[i]

		// Sanitize title
		u.Title = sanitizeTitle(u.Title)

		// Sanitize each fact
		var sanitized []string
		for _, f := range u.Facts {
			s := sanitizeFact(f)
			if s != "" {
				sanitized = append(sanitized, s)
			}
		}
		u.Facts = sanitized

		// Cap facts
		if len(u.Facts) > maxFactsPerFile {
			log.Printf("[nudge] capping facts in %s/%s from %d to %d", u.Layer, u.Filename, len(u.Facts), maxFactsPerFile)
			u.Facts = u.Facts[:maxFactsPerFile]
		}

		// Deduplicate facts
		u.Facts = dedupeStrings(u.Facts)
	}

	// Return non-nil even for empty updates: {"updates":[]} is a valid noop
	// signal from the model ("nothing to save"), not an error. The caller
	// distinguishes noop vs invalid by checking ext.Updates length.
	if len(ext.Updates) == 0 {
		return &ext, nil
	}
	return &ext, nil
}

func dedupeStrings(s []string) []string {
	if len(s) < 2 {
		return s
	}
	seen := make(map[string]struct{}, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			result = append(result, v)
		}
	}
	return result
}

// sanitizeFact cleans a fact string for safe storage.
// Collapses control chars/newlines/tabs to spaces, trims, caps length,
// and rejects empty results and obvious injection prefixes.
func sanitizeFact(raw string) string {
	// Collapse control characters, newlines, tabs to single space
	cleaned := collapseControl(raw)
	cleaned = strings.TrimSpace(cleaned)

	// Reject empty
	if cleaned == "" {
		return ""
	}

	// Reject facts that look like injected instructions
	lower := strings.ToLower(cleaned)
	for _, prefix := range suspiciousPrefixes {
		if strings.HasPrefix(lower, prefix) {
			log.Printf("[nudge] rejecting fact with suspicious prefix %q: %.50s", prefix, cleaned)
			return ""
		}
	}

	// Remove markdown code fences within content (turn into inline text)
	cleaned = neutralizeCodeFences(cleaned)

	// Cap length
	if len(cleaned) > maxFactLength {
		cleaned = cleaned[:maxFactLength]
	}

	return cleaned
}

// sanitizeTitle cleans a title string for safe index use.
func sanitizeTitle(raw string) string {
	cleaned := collapseControl(raw)
	cleaned = strings.TrimSpace(cleaned)
	// Reject empty or excessively long titles
	if cleaned == "" || len(cleaned) > 200 {
		return ""
	}
	return cleaned
}

// collapseControl replaces control characters, newlines, and tabs with spaces.
func collapseControl(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteRune(' ')
		} else if unicode.IsControl(r) {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// neutralizeCodeFences replaces markdown code fences with inline backtick notation.
// This prevents stored memory from being rendered as an executable code block.
func neutralizeCodeFences(s string) string {
	if !strings.Contains(s, "```") && !strings.Contains(s, "~~~") {
		return s
	}
	// Replace ``` at line boundaries with inline backtick notation
	s = strings.ReplaceAll(s, "```", "` ` `")
	s = strings.ReplaceAll(s, "~~~", "~ ~ ~")
	return s
}
