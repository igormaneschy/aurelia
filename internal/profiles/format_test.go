package profiles

import (
	"strings"
	"testing"
)

func TestFormatCatalogLine_ActiveMarker(t *testing.T) {
	p := &PromptProfile{Name: "developer", Description: "dev desc"}
	got := FormatCatalogLine(p, "developer", false)
	if got != "- **developer** (● ativo): dev desc" {
		t.Fatalf("FormatCatalogLine() = %q", got)
	}
}

func TestFormatCatalogLine_VerboseHints(t *testing.T) {
	p := &PromptProfile{
		Name:              "coder",
		Description:       "code",
		Model:             "claude-sonnet",
		CapabilityProfile: "execute_safe",
		Tags:              []string{"code"},
	}
	got := FormatCatalogLine(p, "general", true)
	for _, want := range []string{"model=claude-sonnet", "capability=execute_safe", "tags=code"} {
		if !strings.Contains(got, want) {
			t.Fatalf("FormatCatalogLine(verbose) = %q, want substring %q", got, want)
		}
	}
}