package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/internal/memoryux"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/pkg/images"
	"github.com/igormaneschy/aurelia/internal/pipeline"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/transport"
)

// tuiOutput implements pipeline.Output by writing IPC events through an emit
// function. It signals completion when a terminal output method is called.
// All emit calls are serialized with a mutex since the pipeline may invoke
// output methods from multiple goroutines.
type tuiOutput struct {
	mu       sync.Mutex
	emit     func(ipc.IPCEvent) error
	done     chan struct{}
	doneOnce sync.Once
	errored  bool // true if a terminal error was emitted
}

func newTUIOutput(emit func(ipc.IPCEvent) error) *tuiOutput {
	return &tuiOutput{
		emit: emit,
		done: make(chan struct{}),
	}
}

// markDone closes the done channel exactly once.
func (o *tuiOutput) markDone() {
	o.doneOnce.Do(func() {
		close(o.done)
	})
}

// send serializes an emit call under the mutex and returns the error.
func (o *tuiOutput) send(ev ipc.IPCEvent) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.emit(ev)
}

func (o *tuiOutput) StartTyping(_ int64, _ int) func() {
	return func() {}
}

func (o *tuiOutput) NewProgress(_ int64, _ int) pipeline.ProgressReporter {
	return &tuiProgress{out: o}
}

func (o *tuiOutput) SendError(_ int64, _ int, text string) error {
	o.mu.Lock()
	o.errored = true
	o.mu.Unlock()
	err := o.send(ipc.IPCEvent{Type: ipc.EventTypeError, Error: text})
	o.markDone()
	return err
}

func (o *tuiOutput) SendReply(_ int64, _ int, text string) (int64, error) {
	err := o.send(ipc.IPCEvent{Type: ipc.EventTypeMessage, Body: text})
	o.markDone()
	return 0, err
}

func (o *tuiOutput) SendText(_ int64, _ int, text string) (transport.MessageHandle, error) {
	err := o.send(ipc.IPCEvent{Type: ipc.EventTypeStreamChunk, Body: text})
	return nil, err
}

func (o *tuiOutput) DeleteMessage(_ transport.MessageHandle) {}

// ConfirmMessage marks done for local-handled paths (project preflight, etc.)
// that call SendText then ConfirmMessage without SendReply/SendError.
func (o *tuiOutput) ConfirmMessage(_ int64, _ int) {
	o.markDone()
}

func (o *tuiOutput) ExecuteApprovedPlan(_ int64, _ int, _ int, _ string, _ int64, _ *orchestrator.Plan) {
}

// tuiProgress streams assistant progress to the TUI via stream_chunk events.
// It tracks the last reported text to emit only deltas, avoiding duplication.
type tuiProgress struct {
	out      *tuiOutput
	lastText string
}

func (p *tuiProgress) ReportText(text string) {
	if text == "" || text == p.lastText {
		return
	}
	if len(text) > len(p.lastText) && text[:len(p.lastText)] == p.lastText {
		delta := text[len(p.lastText):]
		_ = p.out.send(ipc.IPCEvent{Type: ipc.EventTypeStreamChunk, Body: delta})
	} else {
		_ = p.out.send(ipc.IPCEvent{
			Type: ipc.EventTypeStreamChunk,
			Body: "\n" + text + "\n",
		})
	}
	p.lastText = text
}

func (p *tuiProgress) ReportTool(toolName string) {
	_ = p.out.send(ipc.IPCEvent{
		Type: ipc.EventTypeStreamChunk,
		Body: fmt.Sprintf("\n🔧 %s...\n", toolName),
	})
}

func (p *tuiProgress) ReportToolResult(_ string) {
	// No-op: do not leak raw tool result data.
}

func (p *tuiProgress) Delete() {}

// tuiRunGuard limits TUI pipeline runs to one at a time.
// Initialized in bootstrapApp and passed to makeTUIHandler.
type tuiRunGuard struct {
	mu     sync.Mutex
	active bool
}

// tryAcquire returns true if the run slot was acquired.
func (g *tuiRunGuard) tryAcquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active {
		return false
	}
	g.active = true
	return true
}

// release frees the run slot.
func (g *tuiRunGuard) release() {
	g.mu.Lock()
	g.active = false
	g.mu.Unlock()
}

// makeTUIHandler creates the IPC stream handler for TUI clients.
// All TUI requests are forced into the local TUI namespace:
// ChatID must be in [ReservedTUIChatIDFloor, ReservedTUIChatID]; any other
// value (including Telegram IDs) is forced to the default DM.
// ThreadID is always 0; UserID is always os.Getuid().
func makeTUIHandler(a *app) func(context.Context, ipc.IPCMessage, func(ipc.IPCEvent) error) error {
	return func(ctx context.Context, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
		// Emit ack for all messages.
		if err := emit(ipc.IPCEvent{Type: ipc.EventTypeAck, RequestID: msg.RequestID}); err != nil {
			return err
		}

		switch msg.Type {
		case ipc.MsgTypeCommand:
			forceTUIIDs(&msg)
			return handleTUICommand(ctx, a, msg, emit)
		case ipc.MsgTypeHistory:
			forceTUIIDs(&msg)
			return handleTUIHistory(ctx, a, msg, emit)
		case ipc.MsgTypeSend:
			forceTUIIDs(&msg)
			return handleTUISend(ctx, a, msg, emit)
		case ipc.MsgTypeSessions:
			return handleTUISessions(ctx, a, msg, emit)
		case ipc.MsgTypeSessionCreate:
			return handleTUISessionCreate(ctx, a, msg, emit)
		case ipc.MsgTypeSessionOpen:
			return handleTUISessionOpen(ctx, a, msg, emit)
		case ipc.MsgTypeSessionDelete:
			return handleTUISessionDelete(ctx, a, msg, emit)
		case ipc.MsgTypeProjectState:
			forceTUIIDs(&msg)
			return handleTUIProjectState(ctx, a, msg, emit)
		case ipc.MsgTypeSubscribe:
			// Terminal error: subscribe not supported.
			return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: "subscribe not supported", RequestID: msg.RequestID})
		default:
			return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("unknown message type: %s", msg.Type), RequestID: msg.RequestID})
		}
	}
}

// forceTUIIDs clamps client-supplied IDs into the local TUI namespace.
// A ChatID already in the reserved range is preserved (the client is
// selecting a named session); anything else is forced to the default DM.
// ThreadID is always 0; UserID is always the local OS user.
func forceTUIIDs(msg *ipc.IPCMessage) {
	if !ipc.IsReservedTUIID(msg.ChatID) {
		msg.ChatID = ipc.ReservedTUIChatID
	}
	msg.ThreadID = 0
	msg.UserID = int64(os.Getuid())
}

// handleTUICommand processes a TUI command (/cwd, /status, etc.).
func handleTUICommand(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	text := strings.TrimSpace(msg.Text)
	chatID, threadID, userID := msg.ChatID, int(msg.ThreadID), msg.UserID

	// Safety: these should already be forced by makeTUIHandler, but guard here too.
	if !ipc.IsReservedTUIID(chatID) {
		chatID = ipc.ReservedTUIChatID
	}
	if threadID != 0 {
		threadID = 0
	}

	var response string

	switch {
	case text == "/help" || text == "help":
		response = buildTUIHelp()
	case text == "/model" || strings.HasPrefix(text, "/model "):
		response = handleTUIModel(ctx, a, chatID, threadID, userID, text)
	case strings.HasPrefix(text, "/cwd"):
		response = handleTUICwd(ctx, a, chatID, threadID, userID, text)
	case strings.HasPrefix(text, "/status"):
		response = handleTUIStatus(ctx, a, chatID, threadID, userID)
	default:
		response = fmt.Sprintf("Unknown command: %s", text)
	}

	if err := emit(ipc.IPCEvent{Type: ipc.EventTypeMessage, Body: response, RequestID: msg.RequestID}); err != nil {
		return err
	}
	return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
}

func buildTUIHelp() string {
	return strings.TrimSpace(`**Aurelia TUI Help**

Type a message and press Enter to chat.

Commands:
- /help — show this help
- /status — show daemon, model, cwd, and session status
- /model — list available models
- /model <name> — switch model
- /model auto — use PI automatic model selection
- /model refresh — refresh the model list
- /cwd — show current project binding
- /cwd <path> — set the project working directory
- /cwd clear — remove the project binding
- /img <path> — attach an image (png, jpg, gif, webp)

Keyboard:
- Ctrl+P — toggle project state panel
- Ctrl+S or F2 — focus sidebar to navigate sessions
- In sidebar: ↑↓ navigate, enter open, n new, d delete, esc exit
- Esc — cancel the current response
- Ctrl+L — clear the screen
- Ctrl+X — clear pending images
- Ctrl+V — paste image from clipboard
- Ctrl+C — quit
- Alt+Enter or Ctrl+J — insert a newline

Images:
- Use /img <path> to attach images by file path
- Use Ctrl+V to paste images from clipboard (macOS/Linux)
- Drag-and-drop image files into the terminal
- Multiple images can be attached before sending`)
}

// handleTUISend processes a TUI send message through the pipeline.
func handleTUISend(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	chatID, threadID, userID := msg.ChatID, int(msg.ThreadID), msg.UserID

	text := strings.TrimSpace(msg.Text)
	if text == "" && len(msg.Images) == 0 {
		if err := emit(ipc.IPCEvent{Type: ipc.EventTypeMessage, Body: "Empty message", RequestID: msg.RequestID}); err != nil {
			return err
		}
		return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
	}

	// Concurrency guard: only one TUI pipeline run at a time.
	if a.tuiRunGuard != nil && !a.tuiRunGuard.tryAcquire() {
		return emit(ipc.IPCEvent{
			Type:      ipc.EventTypeError,
			Error:     "TUI request already in progress",
			RequestID: msg.RequestID,
		})
	}
	if a.tuiRunGuard != nil {
		defer a.tuiRunGuard.release()
	}

	// Create TUI output for pipeline events.
	output := newTUIOutput(emit)

	// Build pipeline config sharing the daemon's services.
	pipeCfg := pipeline.Config{
		AppConfig:  a.config,
		Bridge:     a.bridge,
		Agents:     a.agents,
		Sessions:   a.sessions,
		Resolver:   a.resolver,
		MemoryDir:  a.resolver.Memory(),
		ExePath:    a.resolver.Root(),
		BotCwd:     a.resolver.Root(),
		Output:     output,
		Bindings:   a.bindings,
		RunLog:     a.runLog,
		Continuity: a.continuity,
	}
	if a.bot != nil {
		pipeCfg.Profiles = a.bot.ProfileResolver()
		pipeCfg.Persona = a.bot.PersonaService()
		pipeCfg.UsersStore = a.bot.UserStore()
		pipeCfg.UserResolver = a.bot.UserResolver()
	}
	pipeSvc := pipeline.NewService(pipeCfg)

	// Convert images if present.
	var attachments []bridge.ImageAttachment
	if len(msg.Images) > 0 {
		var err error
		attachments, err = convertIPCImages(msg.Images, 0) // 0 = use default max
		if err != nil {
			// Log a sanitized version — no full local paths in routine logs.
			log.Printf("tui: image conversion error: %s", images.SanitizedError(err))
			// Use SanitizedError to avoid leaking full local paths in
			// user-visible error messages.
			return emit(ipc.IPCEvent{
				Type:      ipc.EventTypeError,
				Error:     fmt.Sprintf("Failed to process image: %s", images.SanitizedError(err)),
				RequestID: msg.RequestID,
			})
		}
	}

	// Launch pipeline processing (async).
	pipeErr := pipeSvc.Process(chatID, threadID, 0, text, attachments, userID, true)
	if pipeErr != nil {
		log.Printf("tui: pipeline process error: %s", pipeline.RedactSecrets(pipeErr.Error()))
		_ = output.SendError(chatID, threadID, pipeErr.Error())
	}

	// Wait for pipeline completion or context cancellation (client
	// disconnected). On cancellation, abort the pipeline so it stops
	// consuming tokens and running tools.
	select {
	case <-output.done:
	case <-ctx.Done():
		pipeSvc.Cancel(chatID, threadID, userID)
		return ctx.Err()
	}

	// Skip stream_end if a terminal error was already emitted (SendError).
	output.mu.Lock()
	errored := output.errored
	output.mu.Unlock()
	if errored {
		return nil
	}

	return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
}

// handleTUICwd processes /cwd commands from the TUI.
// All writes are forced into the reserved TUI namespace regardless of
// client-supplied IDs.
func handleTUICwd(ctx context.Context, a *app, chatID int64, threadID int, userID int64, text string) string {
	args := strings.TrimSpace(strings.TrimPrefix(text, "/cwd"))

	if args == "" {
		return buildTUICwdStatus(ctx, a, chatID, threadID)
	}

	if args == "clear" {
		key := projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID}
		if err := a.bindings.Delete(ctx, key); err != nil {
			return fmt.Sprintf("❌ Error clearing cwd: %s", err)
		}
		if a.sessions != nil {
			a.sessions.ClearCwd(chatID, threadID)
		}
		return "✅ Project binding removed."
	}

	// Validate and resolve path.
	cwd, err := runtime.ResolveProjectCwd(args)
	if err != nil {
		return fmt.Sprintf("❌ Invalid path: %s", err)
	}

	// Set binding — under the active TUI session's ChatID.
	binding := projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID},
		CWD:         cwd,
		ProjectSlug: runtime.ProjectSlug(cwd),
		Source:      projectbinding.BindingManual,
		CreatedBy:   userID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := a.bindings.Set(ctx, binding); err != nil {
		return fmt.Sprintf("❌ Error setting cwd: %s", err)
	}
	if a.sessions != nil {
		a.sessions.SetCwd(chatID, threadID, cwd)
	}
	return fmt.Sprintf("✅ Project set to: `%s`", cwd)
}

// buildTUICwdStatus returns a formatted cwd status string.
func buildTUICwdStatus(ctx context.Context, a *app, chatID int64, threadID int) string {
	resolved, err := a.bindings.Resolve(ctx, projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID})
	if err != nil {
		return fmt.Sprintf("Error reading cwd: %s", err)
	}

	var b strings.Builder
	b.WriteString("**Current Working Directory**\n")
	if resolved != nil && resolved.Binding != nil {
		fmt.Fprintf(&b, "📂 Path: `%s`\n", resolved.Binding.CWD)
		if resolved.Inherited {
			b.WriteString("   (inherited from group)\n")
		}
	} else {
		b.WriteString("📂 No project set.\n")
		b.WriteString("   Use `/cwd <path>` to set one.\n")
	}

	if a.sessions != nil {
		if cwd := a.sessions.GetCwd(chatID, threadID); cwd != "" {
			fmt.Fprintf(&b, "📁 Session CWD: `%s`\n", cwd)
		}
	}

	return b.String()
}

// handleTUIStatus returns a formatted status response for the TUI.
func handleTUIStatus(ctx context.Context, a *app, chatID int64, threadID int, userID int64) string {
	var b strings.Builder
	b.WriteString("**Aurelia Status**\n")

	bridgeStatus := "offline"
	if a.bridge != nil {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if err := a.bridge.Ping(pingCtx); err == nil {
			bridgeStatus = "online"
		}
	}
	fmt.Fprintf(&b, "🧠 Bridge: **%s**\n", bridgeStatus)

	if a.config != nil {
		fmt.Fprintf(&b, "⚙️ Model: **%s**\n", a.config.ModelDisplayName())
	}

	if a.bindings != nil {
		resolved, err := a.bindings.Resolve(ctx, projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID})
		if err == nil && resolved != nil && resolved.Binding != nil {
			fmt.Fprintf(&b, "📂 CWD: `%s`\n", resolved.Binding.CWD)
		} else {
			b.WriteString("📂 No project set.\n")
		}
	}

	if a.sessions != nil {
		if _, active := a.sessions.GetSessionWithState(chatID, threadID, userID); active {
			b.WriteString("💬 Session: active\n")
		} else {
			b.WriteString("💬 Session: none\n")
		}
	}

	return b.String()
}

// handleTUIProjectState gathers project state data and emits a
// EventTypeProjectState event. This is used by the TUI project panel (ctrl+p).
func handleTUIProjectState(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	chatID, threadID, userID := msg.ChatID, int(msg.ThreadID), msg.UserID

	payload := ipc.ProjectStatePayload{}

	// Resolve binding (CWD + source).
	fillTUIProjectBinding(ctx, a, chatID, threadID, &payload)

	// Bridge status.
	bridgeStatus := "offline"
	if a.bridge != nil {
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := a.bridge.Ping(pingCtx); err == nil {
			bridgeStatus = "online"
		}
	}
	payload.BridgeStatus = bridgeStatus

	// Model.
	if a.config != nil {
		payload.Model = a.config.ModelDisplayName()
	}

	// Active agent via profile resolver.
	fillTUIProjectAgent(a, userID, &payload)

	// Memory status via memoryux.
	fillTUIProjectMemory(ctx, a, chatID, threadID, payload.CWD, &payload)

	// Latest run log entry.
	fillTUIProjectRunLog(ctx, a, chatID, threadID, &payload)

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal project state: %w", err)
	}

	if err := emit(ipc.IPCEvent{
		Type:      ipc.EventTypeProjectState,
		Body:      string(body),
		RequestID: msg.RequestID,
	}); err != nil {
		return err
	}
	return emit(ipc.IPCEvent{
		Type:      ipc.EventTypeStreamEnd,
		Done:      true,
		RequestID: msg.RequestID,
	})
}

// fillTUIProjectBinding populates payload CWD and binding source fields.
func fillTUIProjectBinding(ctx context.Context, a *app, chatID int64, threadID int, payload *ipc.ProjectStatePayload) {
	if a.bindings == nil {
		payload.CWD = ""
		payload.BindingSource = "none"
		return
	}
	resolved, err := a.bindings.Resolve(ctx, projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID})
	if err != nil || resolved == nil || resolved.Binding == nil {
		payload.CWD = ""
		payload.BindingSource = "none"
		return
	}
	payload.CWD = resolved.Binding.CWD
	if resolved.Inherited {
		payload.BindingSource = "inherited"
		payload.BindingFrom = fmt.Sprintf("%d:%d", resolved.SourceKey.ChatID, resolved.SourceKey.ThreadID)
	} else {
		payload.BindingSource = "manual"
	}
}

// fillTUIProjectAgent populates payload ActiveAgent using the profile resolver.
func fillTUIProjectAgent(a *app, userID int64, payload *ipc.ProjectStatePayload) {
	if a.bot == nil {
		payload.ActiveAgent = "general"
		return
	}
	resolver := a.bot.ProfileResolver()
	if resolver == nil {
		payload.ActiveAgent = "general"
		return
	}
	// ResolveEffectiveForUser with empty text (no @mention) and isOwner=true
	// for the TUI (all local users are trusted).
	profile, _, err := resolver.ResolveEffectiveForUser("", "", true)
	if err != nil || profile == nil {
		payload.ActiveAgent = "general"
		return
	}
	payload.ActiveAgent = profile.Name
}

// fillTUIProjectMemory populates payload memory layers and checkpoint layer.
func fillTUIProjectMemory(ctx context.Context, a *app, chatID int64, threadID int, cwd string, payload *ipc.ProjectStatePayload) {
	if a.resolver == nil {
		return
	}
	memDir := a.resolver.Memory()
	if memDir == "" {
		return
	}
	svc := memoryux.New(memDir, a.resolver)
	status, err := svc.Status(chatID, threadID, cwd)
	if err != nil {
		log.Printf("tui: project state memory status error: %v", err)
		return
	}
	payload.CheckpointLayer = status.CheckpointLayer
	payload.MemoryLayers = make([]ipc.ProjectStateMemoryLayer, 0, len(status.Layers))
	for _, l := range status.Layers {
		payload.MemoryLayers = append(payload.MemoryLayers, ipc.ProjectStateMemoryLayer{
			Name:      l.Name,
			Scope:     l.Scope,
			Exists:    l.Exists,
			FileCount: l.MarkdownFiles,
		})
	}
}

// fillTUIProjectRunLog populates payload latest run from the run log store.
func fillTUIProjectRunLog(ctx context.Context, a *app, chatID int64, threadID int, payload *ipc.ProjectStatePayload) {
	if a.runLog == nil {
		return
	}
	rec, err := a.runLog.Latest(ctx, chatID, threadID)
	if err != nil {
		log.Printf("tui: project state runlog error: %v", err)
		return
	}
	if rec == nil {
		return
	}
	checkpoint := pipeline.RedactSecrets(rec.Checkpoint)
	// Truncate to 200 runes after redaction (as required by contract).
	if utf8.RuneCountInString(checkpoint) > 200 {
		var b strings.Builder
		count := 0
		for _, r := range checkpoint {
			if count >= 200 {
				break
			}
			b.WriteRune(r)
			count++
		}
		checkpoint = b.String() + "..."
	}
	payload.LatestRun = &ipc.ProjectStateRun{
		Status:     string(rec.Status),
		Checkpoint: checkpoint,
		AgentName:  rec.AgentName,
		StartedAt:  rec.StartedAt,
		DurationMs: rec.DurationMs,
	}
}
