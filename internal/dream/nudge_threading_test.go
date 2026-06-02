package dream

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type fakeNudgeRunLog struct {
	chatID    int64
	threadID  int
	messageID int64
}

func (f fakeNudgeRunLog) GetLastOutboundMessage(context.Context, string) (int64, int, int64, error) {
	return f.chatID, f.threadID, f.messageID, nil
}

type fakeNudgeRunLogWithError struct{}

func (f fakeNudgeRunLogWithError) GetLastOutboundMessage(context.Context, string) (int64, int, int64, error) {
	return 0, 0, 0, errors.New("db unavailable")
}

type nudgeSendCall struct {
	chatID           int64
	threadID         int
	replyToMessageID int64
}

type fakeNudgeSender struct {
	calls []nudgeSendCall
	errs  []error
}

func (f *fakeNudgeSender) SendNudge(_ context.Context, chatID int64, threadID int, replyToMessageID int64, _ string) error {
	f.calls = append(f.calls, nudgeSendCall{chatID: chatID, threadID: threadID, replyToMessageID: replyToMessageID})
	if len(f.errs) == 0 {
		return nil
	}
	err := f.errs[0]
	f.errs = f.errs[1:]
	return err
}

func TestSendNudgeReceipt_ThreadsUnderLastOutboundMessage(t *testing.T) {
	sender := &fakeNudgeSender{}
	d := New(nil, nil, nil, DreamConfig{
		RunLog:      fakeNudgeRunLog{chatID: 42, threadID: 7, messageID: 99},
		NudgeSender: sender,
	})

	d.sendNudgeReceipt(context.Background(), 42, 7, "/session.json", 2)

	if len(sender.calls) != 1 {
		t.Fatalf("SendNudge calls = %d, want 1", len(sender.calls))
	}
	call := sender.calls[0]
	if call.chatID != 42 || call.threadID != 7 || call.replyToMessageID != 99 {
		t.Fatalf("SendNudge call = %+v, want chat=42 thread=7 reply=99", call)
	}
}

func TestSendNudgeReceipt_MissingReplyTargetFallsBackToThread(t *testing.T) {
	sender := &fakeNudgeSender{errs: []error{fmt.Errorf("Bad Request: message to be replied not found")}}
	d := New(nil, nil, nil, DreamConfig{
		RunLog:      fakeNudgeRunLog{chatID: 42, threadID: 7, messageID: 99},
		NudgeSender: sender,
	})

	d.sendNudgeReceipt(context.Background(), 42, 7, "/session.json", 1)

	if len(sender.calls) != 2 {
		t.Fatalf("SendNudge calls = %d, want 2", len(sender.calls))
	}
	if sender.calls[0].replyToMessageID != 99 {
		t.Fatalf("first replyToMessageID = %d, want 99", sender.calls[0].replyToMessageID)
	}
	if sender.calls[1].threadID != 7 || sender.calls[1].replyToMessageID != 0 {
		t.Fatalf("fallback call = %+v, want thread=7 reply=0", sender.calls[1])
	}
}

func TestSendNudgeReceipt_ThreadMismatchFallsBackWithoutReply(t *testing.T) {
	sender := &fakeNudgeSender{}
	d := New(nil, nil, nil, DreamConfig{
		RunLog:      fakeNudgeRunLog{chatID: 42, threadID: 8, messageID: 99},
		NudgeSender: sender,
	})

	d.sendNudgeReceipt(context.Background(), 42, 7, "/session.json", 1)

	if len(sender.calls) != 1 {
		t.Fatalf("SendNudge calls = %d, want 1", len(sender.calls))
	}
	call := sender.calls[0]
	if call.chatID != 42 || call.threadID != 7 || call.replyToMessageID != 0 {
		t.Fatalf("SendNudge call = %+v, want chat=42 thread=7 reply=0", call)
	}
}

func TestSendNudgeReceipt_RunLogErrorFallsBackWithoutReply(t *testing.T) {
	sender := &fakeNudgeSender{}
	d := New(nil, nil, nil, DreamConfig{
		RunLog:      fakeNudgeRunLogWithError{},
		NudgeSender: sender,
	})

	d.sendNudgeReceipt(context.Background(), 42, 7, "/session.json", 1)

	if len(sender.calls) != 1 {
		t.Fatalf("SendNudge calls = %d, want 1", len(sender.calls))
	}
	call := sender.calls[0]
	if call.chatID != 42 || call.threadID != 7 || call.replyToMessageID != 0 {
		t.Fatalf("SendNudge call = %+v, want chat=42 thread=7 reply=0 (no reply when run log errors)", call)
	}
}
