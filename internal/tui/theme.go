package tui

import "github.com/charmbracelet/lipgloss"

// themeStyles groups every lipgloss.Style used by the TUI view layer so the
// whole UI can be repainted in a different palette by swapping the struct.
//
// The default (dark) palette preserves the look the TUI had before themes
// existed. Light styles are placeholders for T5.2.1 and will be filled in
// when theme auto-detection lands. Keeping both constructors side-by-side
// makes the palette surface obvious and the eventual switch is a one-liner.
type themeStyles struct {
	// Chat messages.
	UserStyle      lipgloss.Style
	AssistantStyle lipgloss.Style
	ErrorStyle     lipgloss.Style

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

// newDarkStyles returns the default dark theme. The palette is identical to
// the global styles that lived in view.go before T5.1.0.
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

// newLightStyles returns a light theme placeholder. The actual palette will be
// tuned in T5.2.1 once theme auto-detection lands; for now it reuses the dark
// styles so the code path is exercisable without a visible regression.
func newLightStyles() themeStyles {
	return newDarkStyles()
}
