package tui

import (
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// keyMap defines Aurelia TUI shortcuts for bubbles/help and key.Matches handlers.
type keyMap struct {
	Submit       key.Binding
	Cancel       key.Binding
	Newline      key.Binding
	Sidebar      key.Binding
	SidebarFocus key.Binding
	Help         key.Binding
	HelpClose    key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	MouseToggle  key.Binding
	ProjectPanel key.Binding
	Clear        key.Binding
	Quit         key.Binding
	CopyTranscript key.Binding
	CopyResponse key.Binding
	ClearPending key.Binding
	PasteImage   key.Binding
	HistoryUp    key.Binding
	HistoryDown  key.Binding
	HistoryNext  key.Binding
	HistoryPrev  key.Binding
	HistorySearch key.Binding
	NewSession   key.Binding
	Tab          key.Binding

	CmdHelp         key.Binding
	CmdStatus       key.Binding
	CmdModel        key.Binding
	CmdModelName    key.Binding
	CmdModelAuto    key.Binding
	CmdModelRefresh key.Binding
	CmdCwd          key.Binding
	CmdCwdPath      key.Binding
	CmdCwdClear     key.Binding
	CmdImg          key.Binding
	CmdAttach       key.Binding
}

// uiContext identifies which TUI mode is active for contextual help.
type uiContext int

const (
	uiContextChat uiContext = iota
	uiContextSidebar
	uiContextSearch
	uiContextForm
)

func (m Model) uiContext() uiContext {
	switch {
	case m.formOpen:
		return uiContextForm
	case m.historySearch.active:
		return uiContextSearch
	case m.sidebarFocused:
		return uiContextSidebar
	default:
		return uiContextChat
	}
}

// fullKeyMap returns the active keymap for the current model state.
func (m Model) fullKeyMap() keyMap {
	return defaultKeyMap()
}

func defaultKeyMap() keyMap {
	return keyMap{
		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("Enter", "Send message"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("Esc", "Cancel current response"),
		),
		Newline: key.NewBinding(
			key.WithKeys("alt+enter", "ctrl+j"),
			key.WithHelp("Alt+Enter / Ctrl+J", "Insert newline"),
		),
		Sidebar: key.NewBinding(
			key.WithKeys("tab", "ctrl+i"),
			key.WithHelp("Tab", "Complete command or cycle sidebar"),
		),
		SidebarFocus: key.NewBinding(
			key.WithKeys("f2"),
			key.WithHelp("F2", "Focus sidebar sessions"),
		),
		HistorySearch: key.NewBinding(
			key.WithKeys("ctrl+s"),
			key.WithHelp("Ctrl+S", "Search chat history"),
		),
		NewSession: key.NewBinding(
			key.WithKeys("ctrl+n"),
			key.WithHelp("Ctrl+N", "New session (form)"),
		),
		Help: key.NewBinding(
			key.WithKeys("f1"),
			key.WithHelp("F1", "Toggle help"),
		),
		HelpClose: key.NewBinding(
			key.WithKeys("f1", "esc", "enter"),
			key.WithHelp("F1 / Esc / Enter", "Close this help"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup"),
			key.WithHelp("PgUp", "Scroll chat up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown"),
			key.WithHelp("PgDn", "Scroll chat down"),
		),
		MouseToggle: key.NewBinding(
			key.WithKeys("ctrl+o"),
			key.WithHelp("Ctrl+O", "Toggle mouse on/off (sidebar click + scroll)"),
		),
		ProjectPanel: key.NewBinding(
			key.WithKeys("ctrl+p"),
			key.WithHelp("Ctrl+P", "Toggle project state panel"),
		),
		Clear: key.NewBinding(
			key.WithKeys("ctrl+l"),
			key.WithHelp("Ctrl+L", "Clear chat screen"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("Ctrl+C", "Quit"),
		),
		CopyTranscript: key.NewBinding(
			key.WithKeys("ctrl+y"),
			key.WithHelp("Ctrl+Y", "Copy transcript to clipboard"),
		),
		CopyResponse: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("Ctrl+R", "Copy last response to clipboard"),
		),
		ClearPending: key.NewBinding(
			key.WithKeys("ctrl+x"),
			key.WithHelp("Ctrl+X", "Clear pending images/docs"),
		),
		PasteImage: key.NewBinding(
			key.WithKeys("ctrl+v"),
			key.WithHelp("Ctrl+V", "Paste image from clipboard"),
		),
		HistoryUp: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "Navigate input history"),
		),
		HistoryDown: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "Navigate input history"),
		),
		HistoryNext: key.NewBinding(
			key.WithKeys("ctrl+f"),
			key.WithHelp("Ctrl+F", "Next history page or scroll down"),
		),
		HistoryPrev: key.NewBinding(
			key.WithKeys("ctrl+b"),
			key.WithHelp("Ctrl+B", "Previous history page or scroll up"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("Tab", "Complete command or cycle sidebar"),
		),

		CmdHelp:         helpOnlyBinding("/help", "Show this help"),
		CmdStatus:       helpOnlyBinding("/status", "Daemon, model, cwd, session status"),
		CmdModel:        helpOnlyBinding("/model", "List available models"),
		CmdModelName:    helpOnlyBinding("/model <name>", "Switch model"),
		CmdModelAuto:    helpOnlyBinding("/model auto", "Use automatic model selection"),
		CmdModelRefresh: helpOnlyBinding("/model refresh", "Refresh model list"),
		CmdCwd:          helpOnlyBinding("/cwd", "Show current project binding"),
		CmdCwdPath:      helpOnlyBinding("/cwd <path>", "Set project working directory"),
		CmdCwdClear:     helpOnlyBinding("/cwd clear", "Remove project binding"),
		CmdImg:          helpOnlyBinding("/img <path>", "Attach image (png, jpg, gif, webp)"),
		CmdAttach:       helpOnlyBinding("/attach <path>", "Attach document (md, docx, pdf, etc.)"),
	}
}

// ShortHelp returns bindings shown in the compact help strip.
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Submit,
		k.Help,
		k.SidebarFocus,
		k.HistorySearch,
		k.ProjectPanel,
		k.Quit,
	}
}

// FullHelp returns grouped bindings for the full help overlay.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{
			k.Cancel,
			k.Submit,
			k.Newline,
			k.MouseToggle,
			k.ProjectPanel,
			k.SidebarFocus,
			k.HistorySearch,
			k.Clear,
			k.CopyTranscript,
			k.CopyResponse,
			k.ClearPending,
			k.PasteImage,
			k.Quit,
			k.HelpClose,
			k.HistoryUp,
			k.HistoryDown,
			k.HistoryNext,
			k.HistoryPrev,
			k.Tab,
			k.PageUp,
			k.PageDown,
		},
		{
			k.CmdHelp,
			k.CmdStatus,
			k.CmdModel,
			k.CmdModelName,
			k.CmdModelAuto,
			k.CmdModelRefresh,
			k.CmdCwd,
			k.CmdCwdPath,
			k.CmdCwdClear,
			k.CmdImg,
			k.CmdAttach,
		},
	}
}

func newHelpModel(styles themeStyles, theme Theme) help.Model {
	h := help.New()
	h.Styles = helpStylesForTheme(styles, theme)
	return h
}

// helpVisible reports whether the full help overlay is open.
func (m Model) helpVisible() bool {
	return m.helpModel.ShowAll
}

// isHelpToggleKey reports F1 (dedicated help shortcut).
func isHelpToggleKey(msg tea.KeyMsg) bool {
	return key.Matches(msg, defaultKeyMap().Help)
}

// helpOnlyBinding creates a display-only binding for slash commands in help.
func helpOnlyBinding(keyLabel, desc string) key.Binding {
	return key.NewBinding(
		key.WithKeys("cmd:"+keyLabel),
		key.WithHelp(keyLabel, desc),
	)
}

func (m Model) helpPanelTitle() string {
	switch m.uiContext() {
	case uiContextSidebar:
		return "Sidebar Shortcuts"
	case uiContextSearch:
		return "Search Shortcuts"
	case uiContextForm:
		return "Form Shortcuts"
	default:
		return "Keyboard Shortcuts"
	}
}

// helpPanelGroups returns contextual help sections for the overlay.
func (m Model) helpPanelGroups() [][]key.Binding {
	km := m.fullKeyMap()
	switch m.uiContext() {
	case uiContextSidebar:
		return [][]key.Binding{sidebarContextBindings(), chatContextBindings(km)}
	case uiContextSearch:
		return [][]key.Binding{searchContextBindings(), chatContextBindings(km)}
	case uiContextForm:
		return [][]key.Binding{formContextBindings()}
	default:
		return km.FullHelp()
	}
}

func sidebarContextBindings() []key.Binding {
	return []key.Binding{
		helpOnlyBinding("↑↓ / j k", "Navigate sessions"),
		helpOnlyBinding("enter", "Open session"),
		helpOnlyBinding("n", "Quick new session"),
		helpOnlyBinding("r", "Rename session"),
		helpOnlyBinding("d", "Delete session"),
		helpOnlyBinding("esc / tab", "Exit sidebar focus"),
		helpOnlyBinding("click row", "Open session (mouse)"),
		helpOnlyBinding("+ New session", "Click hint to open form"),
	}
}

func searchContextBindings() []key.Binding {
	return []key.Binding{
		helpOnlyBinding("type", "Filter matches"),
		helpOnlyBinding("enter / ctrl+s", "Next match"),
		helpOnlyBinding("backspace", "Delete character"),
		helpOnlyBinding("esc", "Close search"),
	}
}

func formContextBindings() []key.Binding {
	return []key.Binding{
		helpOnlyBinding("↑↓", "Navigate fields"),
		helpOnlyBinding("enter", "Confirm / submit"),
		helpOnlyBinding("esc", "Cancel form"),
	}
}

func chatContextBindings(km keyMap) []key.Binding {
	return []key.Binding{
		km.Cancel,
		km.Submit,
		km.Newline,
		km.MouseToggle,
		km.SidebarFocus,
		km.HistorySearch,
		km.NewSession,
		km.HistoryNext,
		km.HistoryPrev,
		km.ProjectPanel,
		km.Clear,
		km.Quit,
		km.HelpClose,
	}
}