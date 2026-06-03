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

	if isContinueResumeText(text) && bc.sessions != nil {
		chatID := c.Chat().ID
		threadID := c.Message().ThreadID
		if sessionFile, active := bc.sessions.GetSessionWithState(chatID, threadID, senderID); sessionFile != "" && !active {
			if err := SendContextText(c, interruptedResumeAck(sessionFile)); err != nil {
				return err
			}
			return bc.runPipeline(chatID, threadID, c.Message().ID, interruptedResumePrompt(), images, senderID, c.Chat().Type == telebot.ChatPrivate)
		}
	}

	if cmd := MatchCommand(text); cmd != nil {
		return bc.handleCommand(c, cmd)
	}

	return bc.runPipeline(c.Chat().ID, c.Message().ThreadID, c.Message().ID, text, images, senderID, c.Chat().Type == telebot.ChatPrivate)
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
		Output:       telegramPipelineOutput{bc: bc, tp: NewTelegramTransport(bc.bot)},
		Orchestrator: bc.orchestrator,
		Dreamer:      bc.dreamer,
		ProjectIndex: bc.projectIndex,
		Bindings:     bc.bindings,
		RunLog:       bc.runLog,
		Continuity:   bc.continuity,
		UsersStore:   userStore,
		UserResolver: userResolver,
	})
	bc.nudgeBuffer = bc.pipeline.NudgeBuffer()
	return bc.pipeline
}

type telegramPipelineOutput struct {
	bc *BotController
	tp transport.Transport
}

func (o telegramPipelineOutput) StartTyping(chatID int64, threadID int) func() {
	if o.tp == nil {
		return func() {}
	}
	return o.tp.StartTyping(chatID, threadID)
}

func (o telegramPipelineOutput) NewProgress(chatID int64, threadID int) pipelinepkg.ProgressReporter {
	if o.bc == nil || o.bc.bot == nil {
		return noopPipelineProgress{}
	}
	return newProgressReporterWithThread(o.bc.bot, &telebot.Chat{ID: chatID}, threadID)
}

func (o telegramPipelineOutput) SendError(chatID int64, threadID int, text string) error {
	if o.tp == nil {
		return nil
	}
	return o.tp.SendError(context.Background(), chatID, threadID, text)
}

func (o telegramPipelineOutput) SendReply(chatID int64, threadID int, text string) (int64, error) {
	if o.bc == nil || o.bc.bot == nil {
		return 0, nil
	}
	return sendTextWithSender(o.bc.bot, &telebot.Chat{ID: chatID}, text, telegramMessageLimit, threadID)
}

func (o telegramPipelineOutput) SendText(chatID int64, threadID int, text string) (any, error) {
	if o.tp == nil {
		return nil, nil
	}
	// Use TelegramTransport's SendText if available — it returns a message ref
	// needed by DeleteMessage for the reconnect flow.
	if tg, ok := o.tp.(*TelegramTransport); ok {
		return tg.SendText(chatID, threadID, text)
	}
	// Generic fallback: send via the transport, return no ref (deletion no-op).
	err := o.tp.Send(context.Background(), transport.OutgoingMessage{
		ChatID:   chatID,
		ThreadID: threadID,
		Text:     text,
		Markdown: false,
	})
	return nil, err
}

func (o telegramPipelineOutput) DeleteMessage(message any) {
	msg, ok := message.(*telebot.Message)
	if !ok || msg == nil || o.bc == nil || o.bc.bot == nil {
		return
	}
	_ = o.bc.bot.Delete(msg)
}

func (o telegramPipelineOutput) ConfirmMessage(chatID int64, messageID int) {
	if o.bc == nil || o.bc.bot == nil || messageID == 0 {
		return
	}
	ReactToMessage(o.bc.bot, &telebot.Chat{ID: chatID}, messageID, "🎉")
}

func (o telegramPipelineOutput) ExecuteApprovedPlan(chatID int64, threadID int, messageID int, cwd string, userID int64, plan *orchestrator.Plan) {
	if o.bc == nil {
		return
	}
	o.bc.executeApprovedPlan(&telebot.Chat{ID: chatID}, threadID, messageID, cwd, userID, plan)
}

type noopPipelineProgress struct{}

func (noopPipelineProgress) ReportTool(string)       {}
func (noopPipelineProgress) ReportToolResult(string) {}
func (noopPipelineProgress) ReportText(string)       {}
func (noopPipelineProgress) Delete()                 {}
