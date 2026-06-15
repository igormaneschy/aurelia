package pipeline

import (
	"context"
	"log"
	"time"

	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/transport"
)

const transportTimeout = 30 * time.Second

// transportOutput is a generic pipeline.Output implementation that works
// over any transport.Transport. It does not import Telegram-specific types
// and can be used by any transport surface (Telegram, TUI, etc.).
type transportOutput struct {
	tp transport.Transport
}

// NewTransportOutput creates a new Output backed by the given Transport.
func NewTransportOutput(tp transport.Transport) Output {
	return &transportOutput{tp: tp}
}

// StartTyping delegates to the transport's typing indicator.
func (o *transportOutput) StartTyping(chatID int64, threadID int) func() {
	if o.tp == nil {
		return func() {}
	}
	return o.tp.StartTyping(chatID, threadID)
}

// NewProgress returns a no-op progress reporter. Transport-specific
// implementations (e.g. Telegram) should override this with a real
// progress reporter if needed.
func (o *transportOutput) NewProgress(_ int64, _ int) ProgressReporter {
	return &noopProgress{}
}

// SendError delegates to the transport's error-formatted send.
func (o *transportOutput) SendError(chatID int64, threadID int, text string) error {
	if o.tp == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), transportTimeout)
	defer cancel()
	return o.tp.SendError(ctx, chatID, threadID, text)
}

// SendReply sends a markdown reply via the transport. Returns 0 for the
// outbound message ID (the generic transport does not expose numeric IDs).
func (o *transportOutput) SendReply(chatID int64, threadID int, text string) (int64, error) {
	if o.tp == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), transportTimeout)
	defer cancel()
	_, err := o.tp.Send(ctx, transport.OutgoingMessage{
		ChatID:   chatID,
		ThreadID: threadID,
		Text:     text,
		Markdown: true,
	})
	return 0, err
}

// SendText sends a plain-text message via the transport and returns the
// opaque MessageHandle for later operations (delete, react).
func (o *transportOutput) SendText(chatID int64, threadID int, text string) (transport.MessageHandle, error) {
	if o.tp == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), transportTimeout)
	defer cancel()
	return o.tp.Send(ctx, transport.OutgoingMessage{
		ChatID:   chatID,
		ThreadID: threadID,
		Text:     text,
		Markdown: false,
	})
}

// DeleteMessage deletes a message if the transport supports it.
// Safe no-op for nil handle or non-deletable transports.
func (o *transportOutput) DeleteMessage(message transport.MessageHandle) {
	if message == nil {
		return
	}
	if o.tp == nil {
		return
	}
	dt, ok := o.tp.(transport.DeletableTransport)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := dt.Delete(ctx, message); err != nil {
		log.Printf("transport_output: DeleteMessage error: %s", redactSecrets(err.Error()))
	}
}

// ConfirmMessage adds a reaction/confirmation if the transport supports it.
// Safe no-op for non-reactable transports.
func (o *transportOutput) ConfirmMessage(chatID int64, messageID int) {
	if messageID == 0 {
		return
	}
	if o.tp == nil {
		return
	}
	rt, ok := o.tp.(transport.ReactableTransport)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rt.React(ctx, chatID, messageID); err != nil {
		log.Printf("transport_output: ConfirmMessage error: %s", redactSecrets(err.Error()))
	}
}

// ExecuteApprovedPlan is a no-op in the generic transport output.
// Telegram-specific plan execution is handled by telegramPipelineOutput.
func (o *transportOutput) ExecuteApprovedPlan(_ int64, _ int, _ int, _ string, _ int64, _ *orchestrator.Plan) {
}

// noopProgress is a no-op progress reporter for transports that don't
// support progress reporting (e.g. TUI).
type noopProgress struct{}

func (noopProgress) ReportTool(_ string)       {}
func (noopProgress) ReportToolResult(_ string) {}
func (noopProgress) ReportText(_ string)       {}
func (noopProgress) Delete()                   {}
