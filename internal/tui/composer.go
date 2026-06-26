package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

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
	width := inputBoxContentWidth(m.width)
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

	boxWidth := inputBoxContentWidth(m.width)
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