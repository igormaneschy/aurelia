package tui

import "github.com/charmbracelet/x/ansi"

// stripANSI removes terminal escape sequences for width/hit-test calculations.
func stripANSI(s string) string {
	return ansi.Strip(s)
}
