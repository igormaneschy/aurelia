package telegram

import (
	"fmt"
	"log"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

// InterruptedSessionMaxAge is the startup window for offering cold resume.
const InterruptedSessionMaxAge = time.Minute

func (bc *BotController) NotifyRecentInterruptedSessions(maxAge time.Duration) {
	if bc == nil || bc.bot == nil || bc.sessions == nil {
		return
	}
	for _, info := range bc.sessions.RecentColdSessions(maxAge) {
		text := "⚠️ Fui reiniciado durante uma execução recente.\n\n" +
			"Posso retomar a sessão anterior com segurança em modo frio. " +
			"Envie `continuar` para retomar, ou mande uma nova instrução."
		if _, err := bc.bot.Send(&telebot.Chat{ID: info.ChatID}, text, &telebot.SendOptions{ThreadID: info.ThreadID}); err != nil {
			log.Printf("resume notice: failed to notify chat=%d thread=%d user=%d: %v", info.ChatID, info.ThreadID, info.UserID, err)
		}
	}
}

func isContinueResumeText(text string) bool {
	normalized := strings.TrimSpace(strings.ToLower(text))
	return normalized == "continuar" || normalized == "/continuar"
}

func interruptedResumePrompt() string {
	return "Retome a sessão interrompida pelo restart/deploy do daemon. " +
		"Antes de executar ações com efeito colateral, explique brevemente o estado atual " +
		"e peça confirmação se houver risco de duplicar comandos, commits, edits ou deploys."
}

func interruptedResumeAck(sessionFile string) string {
	return fmt.Sprintf("Retomando a sessão interrompida em modo frio: `%s`", shortSessionFile(sessionFile))
}

func shortSessionFile(path string) string {
	if len(path) <= 48 {
		return path
	}
	return "..." + path[len(path)-45:]
}
