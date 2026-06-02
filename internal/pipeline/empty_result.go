package pipeline

import (
	"strings"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

const maxToolSummaryChars = 2000

// emptyResultHadWork returns true when the bridge result event shows evidence
// of processor work (turns, tokens, or cost) despite having no output text.
func emptyResultHadWork(ev bridge.Event) bool {
	return ev.NumTurns > 0 || ev.InputTokens > 0 || ev.OutputTokens > 0 || ev.CostUSD > 0
}

// buildEmptyResultRecoveryMessage returns a user-facing Portuguese recovery
// message for the scenario where the processor did work but returned nothing.
// toolSummary, if non-empty, is included so the user knows what happened.
func buildEmptyResultRecoveryMessage(toolSummary string) string {
	var sb strings.Builder
	sb.WriteString("O processador trabalhou na sua solicitação mas não retornou uma resposta final.")

	if toolSummary != "" {
		sb.WriteString("\n\nFerramentas utilizadas: ")
		if len(toolSummary) > maxToolSummaryChars {
			toolSummary = toolSummary[:maxToolSummaryChars] + "..."
		}
		sb.WriteString(toolSummary)
	}

	sb.WriteString("\n\nUse /status para ver o estado atual ou continue a partir do último checkpoint salvo.")
	return sb.String()
}

// getRunToolSummary reads the in-memory tool summary from the active runLogState
// without consulting the persisted store. Returns empty string if unavailable.
func (s *Service) getRunToolSummary(chatID int64, threadID int, userID int64) string {
	if s.runLog == nil {
		return ""
	}

	key := runLogKey(chatID, threadID, userID)
	s.runLogMu.Lock()
	state, ok := s.runLogStates[key]
	s.runLogMu.Unlock()

	if !ok || state == nil {
		return ""
	}

	state.mu.Lock()
	summary := state.summary.String()
	state.mu.Unlock()
	return summary
}
