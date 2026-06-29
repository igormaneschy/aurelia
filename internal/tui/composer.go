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
	if m.modalOpen() {
		return false
	}
	return strings.TrimSpace(m.textarea.Value()) == ""
}

func (m Model) renderComposerHints() string {
	if !m.shouldShowComposerHints() {
		return ""
	}
	left := m.styles.SidebarMutedStyle.Render("/help · /cwd · /model")
	right := m.styles.SidebarMutedStyle.Render("F1 · ↵ send")
	width := m.composerColumnWidth()
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return left + "  " + right
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) renderInput() string {
	imageBadges := m.renderPendingImageBadges()
	attachmentBadges := m.renderPendingAttachmentBadges()

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
	return content
}