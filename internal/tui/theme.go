package tui

import (
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/lipgloss/v2"
)

// themeStyles groups every lipgloss.Style used by the TUI view layer so the
// whole UI can be repainted in a different palette by swapping the struct.
type themeStyles struct {
	// Chat messages.
	UserStyle       lipgloss.Style
	AssistantStyle  lipgloss.Style
	ErrorStyle      lipgloss.Style
	MessageDividerStyle lipgloss.Style // subtle divider between consecutive messages

	// Input box.
	InputPromptStyle  lipgloss.Style
	InputBoxStyle     lipgloss.Style
	InputWaitingStyle lipgloss.Style

	// Status bar.
	StatusBarStyle   lipgloss.Style
	StatusReadyStyle lipgloss.Style
	StatusBusyStyle  lipgloss.Style
	StatusErrorStyle lipgloss.Style

	// Sidebar.
	SidebarStyle       lipgloss.Style
	SidebarTitleStyle  lipgloss.Style
	SidebarMutedStyle  lipgloss.Style
	SidebarActiveStyle lipgloss.Style
	SidebarCursorStyle lipgloss.Style

	// Chat header.
	HeaderTitleStyle lipgloss.Style
	HeaderMetaStyle  lipgloss.Style
	HeaderRuleStyle  lipgloss.Style

	// Misc.
	MessageSeparatorStyle lipgloss.Style
	ChatModeStyle         lipgloss.Style
}

// Theme represents the TUI color theme.
type Theme string

const (
	ThemeAuto  Theme = "auto"
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
)

// ParseTheme returns the Theme for the given string, defaulting to auto.
func ParseTheme(s string) Theme {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "light":
		return ThemeLight
	case "dark":
		return ThemeDark
	default:
		return ThemeAuto
	}
}

// ResolveTheme resolves a theme to its effective value (light or dark) based
// on terminal environment detection when ThemeAuto is selected.
func ResolveTheme(t Theme) Theme {
	if t == ThemeAuto {
		if detectLightBackground() {
			return ThemeLight
		}
		return ThemeDark
	}
	return t
}

// detectLightBackground attempts to infer whether the terminal has a light
// background by checking well-known environment variables.
//
// - $TERM_PROGRAM: iTerm2, Warp, and Kitty expose background info via
//   proprietary escape sequences, but few set env vars for it.
// - $COLORFGBG: some terminals set this to "15;0" for light-on-dark and
//   "0;15" for dark-on-light, but it is not universally reliable.
// - macOS Terminal.app default is light background.
//
// Returns true when a light background is detected; defaults to dark.
func detectLightBackground() bool {
	// Check $COLORFGBG — format "<fg>;<bg>" where higher bg values (7-15)
	// suggest a light background.
	if fgbg := os.Getenv("COLORFGBG"); fgbg != "" {
		parts := strings.Split(fgbg, ";")
		if len(parts) >= 2 {
			bg := strings.TrimSpace(parts[len(parts)-1])
			// Common light bg values: 15 (white), 7 (light gray), 231-255
			switch bg {
			case "15", "7", "14", "11":
				return true
			}
		}
	}

	// Terminal.app on macOS defaults to light background and doesn't set
	// COLORFGBG. Apple_Terminal is the bundle identifier exposed via $TERM_PROGRAM.
	if termProg := os.Getenv("TERM_PROGRAM"); termProg == "Apple_Terminal" {
		return true
	}

	// Default: dark (most developer terminals use dark backgrounds).
	return false
}

// GlamourStyle returns the glamour style name for the resolved light/dark theme.
func (t Theme) GlamourStyle() string {
	if ResolveTheme(t) == ThemeLight {
		return "light"
	}
	return "dark"
}

// helpStylesForTheme returns bubbles/help styles aligned with the TUI palette.
func helpStylesForTheme(s themeStyles, theme Theme) help.Styles {
	h := help.DefaultStyles(ResolveTheme(theme) == ThemeDark)
	h.ShortKey = s.UserStyle
	h.FullKey = s.UserStyle
	h.ShortDesc = s.HeaderMetaStyle
	h.FullDesc = s.HeaderMetaStyle
	return h
}

// newStylesForTheme returns the appropriate themeStyles for the given Theme.
func newStylesForTheme(t Theme) themeStyles {
	if ResolveTheme(t) == ThemeLight {
		return newLightStyles()
	}
	return newDarkStyles()
}

// newDarkStyles returns the default dark theme.
func newDarkStyles() themeStyles {
	return themeStyles{
		UserStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")),

		AssistantStyle: lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("205")),

		ErrorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")),

		// MessageDividerStyle: subtle horizontal rule between consecutive messages.
		// Uses a very dim color so it separates without distracting.
		MessageDividerStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("236")),

		InputPromptStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")),

		InputBoxStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1),

		InputWaitingStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("205")).
			Padding(0, 1),

		StatusBarStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")).
			Background(lipgloss.Color("235")).
			Padding(0, 1),

		StatusReadyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("42")),
		StatusBusyStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("205")),
		StatusErrorStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("196")),

		SidebarStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1).
			Width(sidebarWidth),

		SidebarTitleStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")),

		SidebarMutedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")),

		SidebarActiveStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true),

		SidebarCursorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Bold(true),

		HeaderTitleStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("205")),

		HeaderMetaStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("244")),

		HeaderRuleStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")),

		MessageSeparatorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("238")),

		// ChatModeStyle highlights that file system tools are disabled.
		ChatModeStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")). // amber
			Italic(true),
	}
}

// newLightStyles returns a light theme with high-contrast colors suitable for
// terminals with white/light backgrounds.
func newLightStyles() themeStyles {
	return themeStyles{
		UserStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("25")), // blue

		AssistantStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("161")), // magenta-red

		ErrorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("160")), // strong red

		// MessageDividerStyle: subtle horizontal rule between consecutive messages.
		// Uses a very dim color so it separates without distracting.
		MessageDividerStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")),

		InputPromptStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("25")),

		InputBoxStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("244")). // gray
			Padding(0, 1),

		InputWaitingStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("125")). // magenta
			Padding(0, 1),

		StatusBarStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")). // dark gray text
			Background(lipgloss.Color("254")). // near-white bg
			Padding(0, 1),

		StatusReadyStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("28")),  // green
		StatusBusyStyle:  lipgloss.NewStyle().Foreground(lipgloss.Color("125")), // magenta
		StatusErrorStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("160")), // red

		SidebarStyle: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("244")).
			Padding(0, 1).
			Width(sidebarWidth),

		SidebarTitleStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("125")),

		SidebarMutedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")),

		SidebarActiveStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("25")).
			Bold(true),

		SidebarCursorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("130")). // dark yellow / amber
			Bold(true),

		HeaderTitleStyle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("125")),

		HeaderMetaStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")),

		HeaderRuleStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("248")),

		MessageSeparatorStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("248")),

		ChatModeStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("130")). // amber
			Italic(true),
	}
}
