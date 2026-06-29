package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

const (
	composerPromptRunes        = 2 // "> " or "… "
	inputBoxChromeWidth        = 4 // rounded border + horizontal padding
	composerTextareaMinHeight  = 2
	composerTextareaMaxHeight  = 6
)

// composerTextareaWidth is the bubbles textarea wrap width inside the bordered
// input box, accounting for the external prompt and box chrome.
func (m Model) composerTextareaWidth() int {
	inner := m.composerColumnWidth() - inputBoxChromeWidth
	w := inner - composerPromptRunes
	return maxInt(10, w)
}

// composerTextareaLineCount sizes the visible input area to the wrapped content.
func (m Model) composerTextareaLineCount() int {
	text := m.textarea.Value()
	if text == "" {
		return composerTextareaMinHeight
	}
	wrapped := materializeSoftWraps(text, m.composerTextareaWidth())
	lines := strings.Count(wrapped, "\n") + 1
	return clampInt(lines, composerTextareaMinHeight, composerTextareaMaxHeight)
}

func (m *Model) syncTextareaDimensions() {
	m.textarea.SetWidth(m.composerTextareaWidth())
	m.textarea.SetHeight(m.composerTextareaLineCount())
}

func (m Model) composerPlaceholder() string {
	if m.waiting {
		return "Aurelia a pensar…"
	}
	if m.isChatMode() {
		return "Chat mode — sem project. /cwd ou F1 help"
	}
	name := projectName(m.cwdPath)
	if name == "" {
		name = "project"
	}
	return fmt.Sprintf("Mensagem para %s…", name)
}

func (m Model) shouldShowComposerHints() bool {
	return !m.modalOpen() && !m.waiting
}

func (m Model) renderComposerSpacer() string {
	if m.modalOpen() {
		return ""
	}
	w := m.composerColumnWidth()
	rule := m.styles.ComposerSpacerStyle.Render(strings.Repeat("·", maxInt(16, w-8)))
	return lipgloss.NewStyle().Width(w).Render(rule)
}

func (m Model) renderComposerHints() string {
	if !m.shouldShowComposerHints() {
		return ""
	}
	width := m.composerColumnWidth()
	sendHint := m.styles.SidebarMutedStyle.Render("↵ send")

	if strings.TrimSpace(m.textarea.Value()) == "" {
		left := m.styles.SidebarMutedStyle.Render("/help · /cwd · /model")
		right := m.styles.SidebarMutedStyle.Render("F1 · ↵ send")
		gap := width - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 2 {
			return left + "  " + right
		}
		return left + strings.Repeat(" ", gap) + right
	}

	gap := width - lipgloss.Width(sendHint)
	if gap < 0 {
		gap = 0
	}
	return strings.Repeat(" ", gap) + sendHint
}

func (m Model) renderInput() string {
	imageBadges := m.renderPendingImageBadges()
	attachmentBadges := m.renderPendingAttachmentBadges()

	var sections []string
	if spacer := m.renderComposerSpacer(); spacer != "" {
		sections = append(sections, spacer)
	}

	var badgeLines []string
	if imageBadges != "" {
		badgeLines = append(badgeLines, imageBadges)
	}
	if attachmentBadges != "" {
		badgeLines = append(badgeLines, attachmentBadges)
	}
	if toolBar := m.renderToolActivity(); toolBar != "" {
		badgeLines = append(badgeLines, toolBar)
	}
	if pendingBadge := m.renderPendingQueueBadge(); pendingBadge != "" {
		badgeLines = append(badgeLines, pendingBadge)
	}
	if autocomplete := m.renderAutocomplete(); autocomplete != "" {
		badgeLines = append(badgeLines, autocomplete)
	}
	if searchBar := m.renderSearchBar(); searchBar != "" {
		badgeLines = append(badgeLines, searchBar)
	}

	promptText := "> "
	if m.waiting {
		promptText = "… "
	}
	prompt := m.styles.InputPromptStyle.Render(promptText)
	input := renderPromptedTextarea(prompt, promptText, m.textarea.View())

	boxWidth := m.composerColumnWidth()
	style := m.styles.InputBoxStyle
	switch {
	case m.waiting:
		style = m.styles.InputWaitingStyle
	case len(m.pendingQueue) > 0:
		style = m.styles.InputPendingStyle
	}
	content := style.Width(boxWidth).Render(input)

	if hints := m.renderComposerHints(); hints != "" {
		content += "\n" + hints
	}
	if len(badgeLines) > 0 {
		content = strings.Join(badgeLines, "\n") + "\n" + content
	}
	sections = append(sections, content)
	return strings.Join(sections, "\n")
}