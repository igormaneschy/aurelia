package telegram

import (
	"context"
	"log"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	pipelinepkg "github.com/igormaneschy/aurelia/internal/pipeline"
	"github.com/igormaneschy/aurelia/internal/transport"
	"github.com/igormaneschy/aurelia/internal/users"
)

const typingIndicatorInterval = 4 * time.Second

func (bc *BotController) processInput(c telebot.Context, text string) error {
	return bc.processInputWithImages(c, text, nil)
}

func (bc *BotController) processInputWithImages(c telebot.Context, text string, images []bridge.ImageAttachment) error {
	senderID := safeSenderID(c.Sender())
	if state, ok := bc.popPendingBootstrap(senderID); ok {
		defer bc.confirmMessage(c.Message())
		switch state.Step {
		case bootstrapStepAssistant:
			return bc.completeBootstrapAssistant(c, state, text)
		case bootstrapStepTimezone:
			return bc.completeBootstrapTimezone(c, state, text)
		default:
			return bc.completeBootstrapProfile(c, state, text)
		}
	}

	// UserGate: intercept users without profiles
	if bc.userGate != nil {
		switch bc.userGate.Check(senderID) {
		case UserGateNeedsOnboarding:
			greeting, err := bc.userGate.Begin(senderID, c.Chat().ID, c.Message().ThreadID, text)
			if err != nil {
				return err
			}
			defer bc.confirmMessage(c.Message())
			return SendContextText(c, greeting)

		case UserGateOnboarding:
			reply, done, err := bc.userGate.Step(senderID, text)
			if err != nil {
				return err
			}
			defer bc.confirmMessage(c.Message())
			if err := SendContextText(c, reply); err != nil {
				return err
			}
			if done {
				// Onboarding complete — re-process the user's original first message
				firstMsg := bc.userGate.FirstMsg(senderID)
				if err := bc.userGate.Complete(senderID); err != nil {
					log.Printf("user_gate: complete error for user %d: %v", senderID, err)
				}
				return bc.processInputWithImages(c, firstMsg, images)
			}
			return nil
		}
	}

	chatID := c.Chat().ID
	threadID := c.Message().ThreadID

	if isContinueResumeText(text) && bc.sessions != nil {
		if sessionFile, active := bc.sessions.GetSessionWithState(chatID, threadID, senderID); sessionFile != "" && !active {
			// Clear any pending plan before resume processing
			bc.ensurePipeline().ClearPendingPlan(chatID, threadID, senderID)
			if err := SendContextText(c, interruptedResumeAck(sessionFile)); err != nil {
				return err
			}
			return bc.runPipeline(chatID, threadID, c.Message().ID, interruptedResumePrompt(), images, senderID, c.Chat().Type == telebot.ChatPrivate)
		}
	}

	if cmd := MatchCommand(text); cmd != nil {
		return bc.handleCommand(c, cmd)
	}

	// Non-command message — clear any pending plan before normal processing
	bc.ensurePipeline().ClearPendingPlan(chatID, threadID, senderID)

	return bc.runPipeline(chatID, threadID, c.Message().ID, text, images, senderID, c.Chat().Type == telebot.ChatPrivate)
}

func (bc *BotController) runPipeline(chatID int64, threadID int, messageID int, text string, images []bridge.ImageAttachment, userID int64, isPrivateChat bool) error {
	return bc.ensurePipeline().Process(chatID, threadID, messageID, text, images, userID, isPrivateChat)
}

func (bc *BotController) processBridgeEventsAsync(chat *telebot.Chat, ch <-chan bridge.Event, progress *progressReporter, userText string, messageID int) bridgeOutcome {
	return bc.processBridgeEventsAsyncWithThread(chat, ch, progress, userText, messageID, 0, 0)
}

func (bc *BotController) processBridgeEventsAsyncWithThread(chat *telebot.Chat, ch <-chan bridge.Event, progress *progressReporter, userText string, messageID int, threadID int, userID ...int64) bridgeOutcome {
	uid := int64(0)
	if len(userID) > 0 {
		uid = userID[0]
	}
	return bridgeOutcome(bc.ensurePipeline().ProcessBridgeEvents(chat.ID, threadID, messageID, ch, progress, userText, nil, uid, chat.Type == telebot.ChatPrivate, nil, nil))
}

func (bc *BotController) invalidateMemoryDirs(chatID int64, threadID int, userID int64, cwd string) {
	bc.ensurePipeline().InvalidateMemoryDirs(chatID, threadID, userID, cwd)
}

func (bc *BotController) invalidateMemoryOverlay(cwd string) {
	bc.ensurePipeline().InvalidateMemoryOverlay(cwd)
}

func (bc *BotController) invalidateMemorySession(chatID int64, threadID int, userID int64) {
	bc.ensurePipeline().InvalidateMemorySession(chatID, threadID, userID)
}

func (bc *BotController) ensurePipeline() *pipelinepkg.Service {
	if bc.pipeline != nil {
		return bc.pipeline
	}
	var (
		userStore    *users.Store
		userResolver *users.Resolver
	)
	if bc.resolver != nil {
		userResolver = users.NewResolver(bc.resolver.Root())
		userStore = users.NewStore(userResolver)
	}
	bc.pipeline = pipelinepkg.NewService(pipelinepkg.Config{
		AppConfig:    bc.config,
		Bridge:       bc.bridge,
		Agents:       bc.agents,
		Profiles:     bc.profiles,
		Persona:      bc.persona,
		Sessions:     bc.sessions,
		Resolver:     bc.resolver,
		MemoryDir:    bc.memoryDir,
		ExePath:      bc.exePath,
		BotCwd:       bc.botCwd,
		Output:       telegramPipelineOutput{bc: bc, tg: NewTelegramTransport(bc.bot)},
		Orchestrator: bc.orchestrator,
		Dreamer:      bc.dreamer,
		ProjectIndex: bc.projectIndex,
		Bindings:     bc.bindings,
		RunLog:       bc.runLog,
		Continuity:   bc.continuity,
		UsersStore:   userStore,
		UserResolver: userResolver,
		NudgeBuffer:  bc.NudgeBuffer(),
		MemoryCache:  bc.MemoryCache(),
		TokenGuard:   bc.TokenGuard(),
	})
	bc.nudgeBuffer = bc.pipeline.NudgeBuffer()
	return bc.pipeline
}

// telegramPipelineOutput is a thin adapter that implements pipeline.Output for
// Telegram. Generic send/delete/react operations delegate to TelegramTransport;
// only Telegram-specific behavior (progress reporter, plan execution) lives here.
type telegramPipelineOutput struct {
	bc *BotController
	tg *TelegramTransport
}

func (o telegramPipelineOutput) StartTyping(chatID int64, threadID int) func() {
	if o.tg == nil {
		return func() {}
	}
	return o.tg.StartTyping(chatID, threadID)
}

func (o telegramPipelineOutput) NewProgress(chatID int64, threadID int) pipelinepkg.ProgressReporter {
	if o.bc == nil || o.bc.bot == nil {
		return noopPipelineProgress{}
	}
	return newProgressReporterWithThread(o.bc.bot, &telebot.Chat{ID: chatID}, threadID)
}

func (o telegramPipelineOutput) SendError(chatID int64, threadID int, text string) error {
	if o.tg == nil {
		return nil
	}
	return o.tg.SendError(context.Background(), chatID, threadID, text)
}

// SendReply sends a markdown reply via TelegramTransport and extracts the
// Telegram message ID for run-log tracking.
func (o telegramPipelineOutput) SendReply(chatID int64, threadID int, text string) (int64, error) {
	if o.tg == nil {
		return 0, nil
	}
	handle, err := o.tg.Send(context.Background(), transport.OutgoingMessage{
		ChatID:   chatID,
		ThreadID: threadID,
		Text:     text,
		Markdown: true,
	})
	if err != nil {
		return 0, err
	}
	if msg, ok := handle.(*telebot.Message); ok && msg != nil {
		return int64(msg.ID), nil
	}
	return 0, nil
}

// SendText sends a plain-text message via TelegramTransport and returns the
// opaque handle needed by DeleteMessage for the reconnect flow.
func (o telegramPipelineOutput) SendText(chatID int64, threadID int, text string) (transport.MessageHandle, error) {
	if o.tg == nil {
		return nil, nil
	}
	return o.tg.Send(context.Background(), transport.OutgoingMessage{
		ChatID:   chatID,
		ThreadID: threadID,
		Text:     text,
		Markdown: false,
	})
}

// DeleteMessage delegates to TelegramTransport.Delete. Safe no-op for nil
// handle or non-telebot.Message types.
func (o telegramPipelineOutput) DeleteMessage(message transport.MessageHandle) {
	if o.tg == nil || message == nil {
		return
	}
	if err := o.tg.Delete(context.Background(), message); err != nil {
		log.Printf("telegramPipelineOutput: DeleteMessage error: %v", err)
	}
}

// ConfirmMessage delegates to TelegramTransport.React for the 🎉 reaction.
func (o telegramPipelineOutput) ConfirmMessage(chatID int64, messageID int) {
	if o.tg == nil {
		return
	}
	if err := o.tg.React(context.Background(), chatID, messageID); err != nil {
		log.Printf("telegramPipelineOutput: ConfirmMessage error: %v", err)
	}
}

// ExecuteApprovedPlan triggers the orchestrator plan execution via BotController.
// This is Telegram-specific behavior not available in the generic transport output.
func (o telegramPipelineOutput) ExecuteApprovedPlan(chatID int64, threadID int, messageID int, cwd string, userID int64, plan *orchestrator.Plan) {
	if o.bc == nil {
		return
	}
	o.bc.executeApprovedPlan(&telebot.Chat{ID: chatID}, threadID, messageID, cwd, userID, plan)
}

// cmdExecutePlan handles /execute: executes a pending plan if one exists.
func (bc *BotController) cmdExecutePlan(chatID int64, threadID int, userID int64) (string, error) {
	if bc.pipeline == nil {
		return "Nenhum plano pendente.", nil
	}
	switch bc.pipeline.ExecutePendingPlan(chatID, threadID, userID) {
	case pipelinepkg.PlanExecuted:
		log.Printf("pipeline: executing pending plan for chat=%d thread=%d user=%d", chatID, threadID, userID)
		return "✅ Plano aprovado! Iniciando execução...", nil
	case pipelinepkg.PlanExpired:
		return "⏰ O plano pendente expirou. Peça para gerar um novo plano.", nil
	default:
		return "Nenhum plano pendente encontrado. Peça para gerar um novo plano.", nil
	}
}

// cmdCancelPlan handles /cancel: discards a pending plan if one exists.
func (bc *BotController) cmdCancelPlan(chatID int64, threadID int, userID int64) (string, error) {
	if bc.pipeline == nil {
		return "Nenhum plano pendente.", nil
	}
	if bc.pipeline.ClearPendingPlan(chatID, threadID, userID) {
		log.Printf("pipeline: cancelled pending plan for chat=%d thread=%d user=%d", chatID, threadID, userID)
		return "🗑️ Plano descartado.", nil
	}
	return "Nenhum plano pendente para cancelar.", nil
}

type noopPipelineProgress struct{}

func (noopPipelineProgress) ReportTool(_, _ string)  {}
func (noopPipelineProgress) ReportToolResult(string) {}
func (noopPipelineProgress) ReportText(string)       {}
func (noopPipelineProgress) Delete()                 {}
