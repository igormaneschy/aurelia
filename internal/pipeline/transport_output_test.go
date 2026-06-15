package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/igormaneschy/aurelia/internal/transport"
)

// stubTransport is a minimal transport.Transport for testing transportOutput.
type stubTransport struct {
	name         string
	sendFn       func(ctx context.Context, msg transport.OutgoingMessage) (transport.MessageHandle, error)
	sendErrorFn  func(ctx context.Context, chatID int64, threadID int, text string) error
	startTypingFn func(chatID int64, threadID int) func()
	receiveFn    func() <-chan transport.IncomingMessage
	deleteFn     func(ctx context.Context, handle transport.MessageHandle) error
	reactFn      func(ctx context.Context, chatID int64, messageID int) error
}

func (s *stubTransport) Name() string { return s.name }
func (s *stubTransport) Send(ctx context.Context, msg transport.OutgoingMessage) (transport.MessageHandle, error) {
	if s.sendFn != nil {
		return s.sendFn(ctx, msg)
	}
	return nil, nil
}
func (s *stubTransport) SendError(ctx context.Context, chatID int64, threadID int, text string) error {
	if s.sendErrorFn != nil {
		return s.sendErrorFn(ctx, chatID, threadID, text)
	}
	return nil
}
func (s *stubTransport) StartTyping(chatID int64, threadID int) func() {
	if s.startTypingFn != nil {
		return s.startTypingFn(chatID, threadID)
	}
	return func() {}
}
func (s *stubTransport) Receive() <-chan transport.IncomingMessage {
	if s.receiveFn != nil {
		return s.receiveFn()
	}
	ch := make(chan transport.IncomingMessage)
	close(ch)
	return ch
}
func (s *stubTransport) Delete(ctx context.Context, handle transport.MessageHandle) error {
	if s.deleteFn != nil {
		return s.deleteFn(ctx, handle)
	}
	return nil
}
func (s *stubTransport) React(ctx context.Context, chatID int64, messageID int) error {
	if s.reactFn != nil {
		return s.reactFn(ctx, chatID, messageID)
	}
	return nil
}

func TestTransportOutput_SendReply(t *testing.T) {
	var sentMsg transport.OutgoingMessage
	st := &stubTransport{
		sendFn: func(ctx context.Context, msg transport.OutgoingMessage) (transport.MessageHandle, error) {
			sentMsg = msg
			return "handle-1", nil
		},
	}
	out := NewTransportOutput(st)

	msgID, err := out.SendReply(42, 7, "hello **world**")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgID != 0 {
		t.Errorf("SendReply should return 0 for generic transport, got %d", msgID)
	}
	if !sentMsg.Markdown {
		t.Error("SendReply should send with Markdown=true")
	}
	if sentMsg.Text != "hello **world**" {
		t.Errorf("unexpected text: %q", sentMsg.Text)
	}
}

func TestTransportOutput_SendText_ReturnsHandle(t *testing.T) {
	st := &stubTransport{
		sendFn: func(ctx context.Context, msg transport.OutgoingMessage) (transport.MessageHandle, error) {
			return "msg-handle", nil
		},
	}
	out := NewTransportOutput(st)

	handle, err := out.SendText(42, 7, "plain text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle != "msg-handle" {
		t.Errorf("expected handle %q, got %v", "msg-handle", handle)
	}
}

func TestTransportOutput_SendText_NilTransport(t *testing.T) {
	out := NewTransportOutput(nil)
	handle, err := out.SendText(1, 0, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handle != nil {
		t.Errorf("expected nil handle for nil transport, got %v", handle)
	}
}

func TestTransportOutput_DeleteMessage_Deletable(t *testing.T) {
	var deleted transport.MessageHandle
	st := &stubTransport{
		deleteFn: func(ctx context.Context, handle transport.MessageHandle) error {
			deleted = handle
			return nil
		},
	}
	out := NewTransportOutput(st)
	out.DeleteMessage("my-handle")
	if deleted != "my-handle" {
		t.Errorf("expected deletion of %q, got %v", "my-handle", deleted)
	}
}

func TestTransportOutput_DeleteMessage_NonDeletable_NoOp(t *testing.T) {
	// stubTransport without deleteFn — DeleteMessage should not panic.
	st := &stubTransport{}
	out := NewTransportOutput(st)
	out.DeleteMessage("anything") // should not panic
}

func TestTransportOutput_DeleteMessage_NilHandle_NoOp(t *testing.T) {
	var called bool
	st := &stubTransport{
		deleteFn: func(ctx context.Context, handle transport.MessageHandle) error {
			called = true
			return nil
		},
	}
	out := NewTransportOutput(st)
	out.DeleteMessage(nil)
	if called {
		t.Error("DeleteMessage with nil handle should not call DeleteFn")
	}
}

func TestTransportOutput_ConfirmMessage_Reactable(t *testing.T) {
	var chatID int64
	var messageID int
	st := &stubTransport{
		reactFn: func(ctx context.Context, cid int64, mid int) error {
			chatID = cid
			messageID = mid
			return nil
		},
	}
	out := NewTransportOutput(st)
	out.ConfirmMessage(99, 123)
	if chatID != 99 {
		t.Errorf("expected chatID 99, got %d", chatID)
	}
	if messageID != 123 {
		t.Errorf("expected messageID 123, got %d", messageID)
	}
}

func TestTransportOutput_ConfirmMessage_NonReactable_NoOp(t *testing.T) {
	// stubTransport without reactFn — ConfirmMessage should not panic.
	st := &stubTransport{}
	out := NewTransportOutput(st)
	out.ConfirmMessage(1, 42) // should not panic
}

func TestTransportOutput_ConfirmMessage_MessageIDZero_NoOp(t *testing.T) {
	var called bool
	st := &stubTransport{
		reactFn: func(ctx context.Context, cid int64, mid int) error {
			called = true
			return nil
		},
	}
	out := NewTransportOutput(st)
	out.ConfirmMessage(1, 0)
	if called {
		t.Error("ConfirmMessage with messageID 0 should not call ReactFn")
	}
}

func TestTransportOutput_DeleteMessage_NilTransport(t *testing.T) {
	out := NewTransportOutput(nil)
	// Should not panic.
	out.DeleteMessage("any-handle")
}

func TestTransportOutput_ConfirmMessage_NilTransport(t *testing.T) {
	out := NewTransportOutput(nil)
	// Should not panic.
	out.ConfirmMessage(1, 42)
}

func TestTransportOutput_ExecuteApprovedPlan_NoOp(t *testing.T) {
	out := NewTransportOutput(&stubTransport{})
	out.ExecuteApprovedPlan(1, 2, 3, "/tmp", 100, nil) // should not panic
}

func TestTransportOutput_SendError(t *testing.T) {
	var errText string
	st := &stubTransport{
		sendErrorFn: func(ctx context.Context, chatID int64, threadID int, text string) error {
			errText = text
			return nil
		},
	}
	out := NewTransportOutput(st)
	err := out.SendError(1, 0, "something broke")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if errText != "something broke" {
		t.Errorf("expected %q, got %q", "something broke", errText)
	}
}

func TestTransportOutput_SendError_NilTransport(t *testing.T) {
	out := NewTransportOutput(nil)
	err := out.SendError(1, 0, "error")
	if err != nil {
		t.Fatalf("expected nil error for nil transport, got %v", err)
	}
}

func TestTransportOutput_SendError_TransportFailure_Logged(t *testing.T) {
	st := &stubTransport{
		sendErrorFn: func(ctx context.Context, chatID int64, threadID int, text string) error {
			return errors.New("transport down")
		},
	}
	out := NewTransportOutput(st)
	err := out.SendError(1, 0, "error")
	if err == nil {
		t.Fatal("expected error from failing transport")
	}
}

func TestTransportOutput_StartTyping(t *testing.T) {
	var stopCalled bool
	st := &stubTransport{
		startTypingFn: func(chatID int64, threadID int) func() {
			return func() { stopCalled = true }
		},
	}
	out := NewTransportOutput(st)
	stop := out.StartTyping(1, 0)
	stop()
	if !stopCalled {
		t.Error("expected stop function to be called")
	}
}

func TestTransportOutput_StartTyping_NilTransport(t *testing.T) {
	out := NewTransportOutput(nil)
	stop := out.StartTyping(1, 0)
	stop() // should not panic
}

func TestTransportOutput_NewProgress_ReturnsNoOp(t *testing.T) {
	out := NewTransportOutput(&stubTransport{})
	pr := out.NewProgress(1, 0)
	if pr == nil {
		t.Fatal("expected non-nil progress reporter")
	}
	pr.ReportTool("test")
	pr.ReportToolResult("done")
	pr.ReportText("status")
	pr.Delete() // all should be no-ops
}
