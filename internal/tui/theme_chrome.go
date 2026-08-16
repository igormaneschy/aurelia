package tui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// applyTransparency removes opaque backgrounds so the terminal wallpaper shows
// through. Terminals must have background transparency enabled (iTerm, Kitty,
// Ghostty, etc.).
func applyTransparency(s themeStyles) themeStyles {
	s.Surface1Style = s.Surface1Style.UnsetBackground()
	s.ChipStyle = s.ChipStyle.UnsetBackground()
	s.SidebarHoverStyle = s.SidebarHoverStyle.UnsetBackground()
	s.StatusBarStyle = s.StatusBarStyle.UnsetBackground()
	s.AlertChipStyle = s.AlertChipStyle.UnsetBackground()
	s.SearchHighlightStyle = s.SearchHighlightStyle.UnsetBackground()
	return s
}

func stylesForAppearance(theme Theme, transparent bool) themeStyles {
	s := newStylesForTheme(theme)
	if transparent {
		s = applyTransparency(s)
	}
	return s
}

func (m *Model) applyChromeTheme() {
	m.styles = stylesForAppearance(m.theme, m.transparent)
	m.helpModel = newHelpModel(m.styles, m.theme)
	m.sidebarTable.SetStyles(sidebarTableStyles(m.styles))
	if m.streamProgress.active {
		m.streamProgress.bar = newStreamProgressBar(m.styles)
	}
}

func (m *Model) cycleTheme() {
	switch m.theme {
	case ThemeDark:
		m.theme = ThemeLight
	case ThemeLight:
		m.theme = ThemeAuto
	default:
		m.theme = ThemeDark
	}
	m.applyChromeTheme()
	m.glamourRenderer = nil
	m.rendererWidth = 0
	m.syncSidebarRows()
	m.updateViewport()
	m.persistAppearancePrefs()
}

func (m *Model) toggleTransparency() {
	m.transparent = !m.transparent
	m.applyChromeTheme()
	m.syncSidebarRows()
	m.updateViewport()
	m.persistAppearancePrefs()
}

func (m Model) themeStatusLabel() string {
	label := string(m.theme)
	if m.theme == ThemeAuto {
		label = "auto→" + string(ResolveTheme(m.theme))
	}
	if m.transparent {
		label += " · glass"
	}
	return m.styles.HeaderMetaStyle.Render(label)
}

func isThemeToggleKey(msg tea.KeyMsg) bool {
	return key.Matches(msg, defaultKeyMap().ThemeToggle)
}

func isTransparencyToggleKey(msg tea.KeyMsg) bool {
	return key.Matches(msg, defaultKeyMap().TransparencyToggle)
}
