package transport

import (
	"context"
	"testing"
)

// MockTransport implements Transport for testing pipeline components
// without a real chat surface.
type MockTransport struct {
	NameFn        func() string
	SendFn        func(ctx context.Context, msg OutgoingMessage) error
	SendErrorFn   func(ctx context.Context, chatID int64, threadID int, text string) error
	StartTypingFn func(chatID int64, threadID int) func()
	ReceiveFn     func() <-chan IncomingMessage
}

func (m *MockTransport) Name() string {
	if m.NameFn != nil {
		return m.NameFn()
	}
	return "mock"
}

func (m *MockTransport) Send(ctx context.Context, msg OutgoingMessage) error {
	if m.SendFn != nil {
		return m.SendFn(ctx, msg)
	}
	return nil
}

func (m *MockTransport) SendError(ctx context.Context, chatID int64, threadID int, text string) error {
	if m.SendErrorFn != nil {
		return m.SendErrorFn(ctx, chatID, threadID, text)
	}
	return nil
}

func (m *MockTransport) StartTyping(chatID int64, threadID int) func() {
	if m.StartTypingFn != nil {
		return m.StartTypingFn(chatID, threadID)
	}
	return func() {}
}

func (m *MockTransport) Receive() <-chan IncomingMessage {
	if m.ReceiveFn != nil {
		return m.ReceiveFn()
	}
	ch := make(chan IncomingMessage)
	close(ch)
	return ch
}

// compile-time check that MockTransport satisfies Transport.
var _ Transport = (*MockTransport)(nil)

func TestMockTransport_ImplementsTransport(t *testing.T) {
	// Guarantee the mock type can be used wherever Transport is required.
	var tp Transport = &MockTransport{}
	_ = tp
}

func TestIncomingMessage_Fields(t *testing.T) {
	msg := IncomingMessage{
		ChatID:   12345,
		ThreadID: 42,
		UserID:   67890,
		Text:     "hello world",
		Source:   "telegram",
		Images:   nil,
	}

	if msg.ChatID != 12345 {
		t.Errorf("ChatID = %d, want 12345", msg.ChatID)
	}
	if msg.ThreadID != 42 {
		t.Errorf("ThreadID = %d, want 42", msg.ThreadID)
	}
	if msg.UserID != 67890 {
		t.Errorf("UserID = %d, want 67890", msg.UserID)
	}
	if msg.Text != "hello world" {
		t.Errorf("Text = %q, want 'hello world'", msg.Text)
	}
	if msg.Source != "telegram" {
		t.Errorf("Source = %q, want 'telegram'", msg.Source)
	}
}

func TestIncomingMessage_DefaultFields(t *testing.T) {
	msg := IncomingMessage{}
	if msg.Text != "" {
		t.Errorf("Text = %q, want empty string", msg.Text)
	}
	if msg.Images != nil {
		t.Errorf("Images = %v, want nil", msg.Images)
	}
}
