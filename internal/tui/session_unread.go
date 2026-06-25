package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) ensureSessionUnreadMaps() {
	if m.sessionUnread == nil {
		m.sessionUnread = make(map[int64]int)
	}
	if m.sessionSeenCount == nil {
		m.sessionSeenCount = make(map[int64]int)
	}
}

func (m *Model) markSessionSeen(chatID int64, count int) {
	m.ensureSessionUnreadMaps()
	if count < 0 {
		count = 0
	}
	m.sessionSeenCount[chatID] = count
	m.sessionUnread[chatID] = 0
}

func (m *Model) updateSessionUnread(chatID int64, messageCount int) tea.Cmd {
	m.ensureSessionUnreadMaps()
	if messageCount < 0 {
		messageCount = 0
	}
	seen := m.sessionSeenCount[chatID]
	if chatID == m.activeSession && len(m.messages) > seen {
		seen = len(m.messages)
	}
	prev := m.sessionUnread[chatID]
	if messageCount > seen {
		m.sessionUnread[chatID] = messageCount - seen
	} else {
		m.sessionUnread[chatID] = 0
	}
	if m.sessionUnread[chatID] > prev && m.sessionUnread[chatID] > 0 {
		return m.animations.pulseNewMessages()
	}
	return nil
}

func (m *Model) applySessionsUnread(sessions []tuiSessionInfo) tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range sessions {
		if cmd := m.updateSessionUnread(s.ChatID, s.MessageCount); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m Model) sessionUnreadBadge(chatID int64) string {
	if chatID == m.activeSession {
		return ""
	}
	n := m.sessionUnread[chatID]
	if n <= 0 {
		return ""
	}
	if n > 99 {
		return "[99+]"
	}
	return "[" + strconv.Itoa(n) + "]"
}