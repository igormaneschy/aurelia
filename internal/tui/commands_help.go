package tui

import "strings"

func isBareHelpCommand(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), "/help")
}

func isBareStatusCommand(text string) bool {
	return strings.EqualFold(strings.TrimSpace(text), "/status")
}
