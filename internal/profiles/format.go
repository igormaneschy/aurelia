package profiles

import (
	"fmt"
	"strings"
)

// FormatCatalogLine formats a profile entry for /agents listings.
func FormatCatalogLine(p *PromptProfile, activeDefault string, verbose bool) string {
	if p == nil {
		return ""
	}
	desc := p.Description
	if desc == "" {
		desc = "(sem descrição)"
	}
	marker := ""
	if strings.EqualFold(p.Name, activeDefault) {
		marker = " (● ativo)"
	}
	line := fmt.Sprintf("- **%s**%s: %s", p.Name, marker, desc)
	if !verbose {
		return line
	}
	var hints []string
	if p.Harness != "" && p.Harness != "pi" {
		hints = append(hints, "harness="+p.Harness)
	}
	if p.Model != "" {
		hints = append(hints, "model="+p.Model)
	}
	if p.CapabilityProfile != "" {
		hints = append(hints, "capability="+p.CapabilityProfile)
	}
	if len(p.Tags) > 0 {
		hints = append(hints, "tags="+strings.Join(p.Tags, ","))
	}
	if len(hints) == 0 {
		return line
	}
	return line + " _[" + strings.Join(hints, ", ") + "]_"
}