package telegram

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/planning"
	"github.com/igormaneschy/aurelia/internal/session"
)

func (bc *BotController) handlePlan(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())

	senderID := safeSenderID(c.Sender())
	if bc.userGate != nil && bc.userGate.Check(senderID) != UserGateOK {
		return SendContextText(c, "Você precisa completar o onboarding primeiro.")
	}

	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	key := session.SessionKey{ChatID: chatID, ThreadID: threadID, UserID: senderID}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	existing, err := bc.planningStore.Get(ctx, key)
	if err != nil {
		return SendContextText(c, fmt.Sprintf("Erro ao verificar estado: %v", err))
	}
	if existing != nil {
		return SendContextText(c, fmt.Sprintf("Já existe um plano ativo (fase: %s). Use /plan status para ver.", existing.Phase))
	}

	cwd := bc.currentCwd(chatID, threadID)
	if cwd == "" {
		return SendContextText(c, "Use /cwd <path> para fixar um projeto antes de criar um plano.")
	}

	projectCtx, err := planning.Discover(cwd)
	if err != nil {
		log.Printf("plan: discovery error: %v", err)
		projectCtx = nil
	}

	state := &planning.State{
		Key:        key,
		Version:    0,
		Status:     planning.StatusActive,
		Phase:      planning.PhaseSpecify,
		CWD:        cwd,
		ProjectCtx: projectCtx,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := bc.planningStore.Save(ctx, state); err != nil {
		return SendContextText(c, fmt.Sprintf("Erro ao criar plano: %v", err))
	}

	msg := fmt.Sprintf("Modo Plano ativado!\nFase: %s\nCWD: %s", state.Phase, cwd)
	if projectCtx != nil && len(projectCtx.Layouts) > 0 {
		msg += fmt.Sprintf("\nLayouts detectados: %v", projectCtx.Layouts)
	}
	return SendContextText(c, msg)
}

func (bc *BotController) handlePlanStatus(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())

	senderID := safeSenderID(c.Sender())
	if bc.userGate != nil && bc.userGate.Check(senderID) != UserGateOK {
		return SendContextText(c, "Você precisa completar o onboarding primeiro.")
	}

	key := session.SessionKey{ChatID: c.Chat().ID, ThreadID: c.Message().ThreadID, UserID: senderID}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	state, err := bc.planningStore.Get(ctx, key)
	if err != nil {
		return SendContextText(c, fmt.Sprintf("Erro ao buscar plano: %v", err))
	}
	if state == nil {
		return SendContextText(c, "Nenhum plano ativo. Use /plan para criar um.")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**Plano**\nFase: %s\nStatus: %s\nCWD: `%s`\n", state.Phase, state.Status, state.CWD)

	if state.ProjectCtx != nil {
		if len(state.ProjectCtx.Layouts) > 0 {
			fmt.Fprintf(&b, "Layouts: %v\n", state.ProjectCtx.Layouts)
		}
		if len(state.ProjectCtx.Stacks) > 0 {
			fmt.Fprintf(&b, "Stacks: %v\n", state.ProjectCtx.Stacks)
		}
	}

	if len(state.Materialized) > 0 {
		fmt.Fprintf(&b, "Artefatos: %d\n", len(state.Materialized))
		for _, a := range state.Materialized {
			b.WriteString(fmt.Sprintf("  • `%s` (%s, %s)\n", a.Path, a.Tool, a.Phase))
		}
	}

	age := time.Since(state.CreatedAt).Round(time.Second)
	fmt.Fprintf(&b, "Criado: %s atrás\n", age)

	if state.LastHandoffError != "" {
		fmt.Fprintf(&b, "Último erro: %s\n", state.LastHandoffError)
	}

	return SendContextText(c, b.String())
}

func (bc *BotController) handlePlanList(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())

	senderID := safeSenderID(c.Sender())
	if bc.userGate != nil && bc.userGate.Check(senderID) != UserGateOK {
		return SendContextText(c, "Você precisa completar o onboarding primeiro.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	states, err := bc.planningStore.ListByUser(ctx, senderID)
	if err != nil {
		return SendContextText(c, fmt.Sprintf("Erro ao listar planos: %v", err))
	}

	if len(states) == 0 {
		return SendContextText(c, "Nenhum plano encontrado.")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "**Planos** (%d)\n", len(states))
	for _, s := range states {
		chatLabel := fmt.Sprintf("%d", s.Key.ChatID)
		if s.Key.ThreadID > 0 {
			chatLabel = fmt.Sprintf("%d/t%d", s.Key.ChatID, s.Key.ThreadID)
		}
		age := time.Since(s.CreatedAt).Round(time.Second)
		fmt.Fprintf(&b, "• Chat %s · %s · %s (%s atrás)\n", chatLabel, s.Phase, s.Status, age)
	}

	return SendContextText(c, b.String())
}

func (bc *BotController) handlePlanCancel(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())

	senderID := safeSenderID(c.Sender())
	if bc.userGate != nil && bc.userGate.Check(senderID) != UserGateOK {
		return SendContextText(c, "Você precisa completar o onboarding primeiro.")
	}

	key := session.SessionKey{ChatID: c.Chat().ID, ThreadID: c.Message().ThreadID, UserID: senderID}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	state, err := bc.planningStore.Get(ctx, key)
	if err != nil {
		return SendContextText(c, fmt.Sprintf("Erro ao buscar plano: %v", err))
	}
	if state == nil {
		return SendContextText(c, "Nenhum plano ativo para cancelar.")
	}

	artifacts := state.Materialized

	if err := bc.planningStore.Delete(ctx, key); err != nil {
		return SendContextText(c, fmt.Sprintf("Erro ao cancelar plano: %v", err))
	}

	msg := "Plano cancelado."
	if len(artifacts) > 0 {
		var paths []string
		for _, a := range artifacts {
			paths = append(paths, a.Path)
		}
		msg += fmt.Sprintf("\nArtefatos preservados (%d):\n  %s", len(artifacts), strings.Join(paths, "\n  "))
	}
	return SendContextText(c, msg)
}

func (bc *BotController) handlePlanReset(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())

	senderID := safeSenderID(c.Sender())
	if bc.userGate != nil && bc.userGate.Check(senderID) != UserGateOK {
		return SendContextText(c, "Você precisa completar o onboarding primeiro.")
	}

	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	key := session.SessionKey{ChatID: chatID, ThreadID: threadID, UserID: senderID}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Delete existing state if present
	existing, _ := bc.planningStore.Get(ctx, key)
	if existing != nil {
		if err := bc.planningStore.Delete(ctx, key); err != nil {
			return SendContextText(c, fmt.Sprintf("Erro ao remover plano anterior: %v", err))
		}
	}

	cwd := bc.currentCwd(chatID, threadID)
	if cwd == "" {
		return SendContextText(c, "Use /cwd <path> para fixar um projeto antes de criar um plano.")
	}

	projectCtx, err := planning.Discover(cwd)
	if err != nil {
		log.Printf("plan: discovery error on reset: %v", err)
		projectCtx = nil
	}

	state := &planning.State{
		Key:        key,
		Version:    0,
		Status:     planning.StatusActive,
		Phase:      planning.PhaseSpecify,
		CWD:        cwd,
		ProjectCtx: projectCtx,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := bc.planningStore.Save(ctx, state); err != nil {
		return SendContextText(c, fmt.Sprintf("Erro ao criar novo plano: %v", err))
	}

	msg := "Plano resetado!\nNovo modo Plano ativado."
	msg += fmt.Sprintf("\nFase: %s\nCWD: %s", state.Phase, cwd)
	if projectCtx != nil && len(projectCtx.Layouts) > 0 {
		msg += fmt.Sprintf("\nLayouts detectados: %v", projectCtx.Layouts)
	}
	return SendContextText(c, msg)
}

func (bc *BotController) handleExecute(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())

	senderID := safeSenderID(c.Sender())
	if bc.userGate != nil && bc.userGate.Check(senderID) != UserGateOK {
		return SendContextText(c, "Você precisa completar o onboarding primeiro.")
	}

	key := session.SessionKey{ChatID: c.Chat().ID, ThreadID: c.Message().ThreadID, UserID: senderID}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	state, err := bc.planningStore.Get(ctx, key)
	if err != nil {
		return SendContextText(c, fmt.Sprintf("Erro ao buscar plano: %v", err))
	}
	if state == nil {
		return SendContextText(c, "Nenhum plano ativo. Use /plan para criar um.")
	}

	state.Status = planning.StatusAwaitingExec
	state.UpdatedAt = time.Now()

	if err := bc.planningStore.Save(ctx, state); err != nil {
		return SendContextText(c, fmt.Sprintf("Erro ao marcar plano para execução: %v", err))
	}

	return SendContextText(c, "Plano marcado para execução! O pipeline irá processá-lo quando possível.")
}
