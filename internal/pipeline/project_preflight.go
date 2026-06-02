package pipeline

import (
	"fmt"
	"log"
	"strings"

	"github.com/igormaneschy/aurelia/internal/agents"
)

// preflightMessageNoProjects is the local response sent when user asks to
// read/analyze a codebase but no CWD is bound and no known project paths exist.
const preflightMessageNoProjects = "Este chat/tópico ainda não tem projeto fixado.\n\nUse /cwd <caminho> para fixar um projeto."

// checkProjectPreflight handles deterministic local feedback when the user
// asks to read/analyze a codebase or project but no working directory (CWD)
// is bound for this chat. Returns true when the request was handled locally
// and the bridge should NOT be called.
func (s *Service) checkProjectPreflight(input pipelineInput, agent *agents.Agent, userText string) bool {
	if !looksLikeCodebaseRead(userText) {
		return false
	}
	if s.effectiveCwdForContext(agent, input.chatID, input.threadID, input.userID, input.isPrivateChat) != "" {
		return false
	}

	msg := buildProjectPreflightMessage(s.listKnownProjectPaths(input.userID))
	if _, err := s.output.SendText(input.chatID, input.threadID, msg); err != nil {
		log.Printf("preflight: send guidance for chat=%d thread=%d: %v", input.chatID, input.threadID, err)
	}
	s.output.ConfirmMessage(input.chatID, input.messageID)
	return true
}

// buildProjectPreflightMessage returns a local guidance message telling the
// user to set a /cwd when they ask to read a codebase without a bound project.
// knownPaths lists the user's project bindings from other chats as suggestions.
func buildProjectPreflightMessage(knownPaths []string) string {
	if len(knownPaths) == 0 {
		return preflightMessageNoProjects
	}

	var b strings.Builder
	b.WriteString("Este chat/tópico ainda não tem projeto fixado.")
	b.WriteString("\n\nProjetos que você usou em outros chats (sugestões — não são o CWD ativo):")
	for _, p := range knownPaths {
		fmt.Fprintf(&b, "\n/cwd %s", p)
	}
	b.WriteString("\n\nUse /cwd <caminho> para fixar um projeto.")
	return b.String()
}
