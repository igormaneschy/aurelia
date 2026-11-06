package main

import (
	"testing"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

// Every IPC request must end with a terminal event (stream_end), including
// the ones that fail. A handler that returned after only the error event left
// the client waiting for the terminal event forever: the desktop client stuck
// on "A carregar histórico…" when opening a session that no longer existed.
func TestTUIHandler_ErrorPathsAlwaysEmitTerminalEvent(t *testing.T) {
	a, ctx, cleanup := testApp(t)
	defer cleanup()

	handler := makeTUIHandler(a)

	cases := []struct {
		name string
		msg  ipc.IPCMessage
	}{
		{
			name: "session_open unknown id",
			msg:  ipc.IPCMessage{Type: ipc.MsgTypeSessionOpen, ChatID: ipc.ReservedTUIChatID - 3},
		},
		{
			name: "session_rename unknown id",
			msg:  ipc.IPCMessage{Type: ipc.MsgTypeSessionRename, ChatID: ipc.ReservedTUIChatID - 3, Text: "x"},
		},
		{
			name: "session_create without name",
			msg:  ipc.IPCMessage{Type: ipc.MsgTypeSessionCreate},
		},
		{
			name: "session_rename default DM",
			msg:  ipc.IPCMessage{Type: ipc.MsgTypeSessionRename, ChatID: ipc.ReservedTUIChatID, Text: "x"},
		},
		{
			name: "session_delete default DM",
			msg:  ipc.IPCMessage{Type: ipc.MsgTypeSessionDelete, ChatID: ipc.ReservedTUIChatID},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			te := &testEmit{}
			if err := handler(ctx, tc.msg, te.emit); err != nil {
				t.Fatalf("handler error: %v", err)
			}

			var sawError, sawTerminal bool
			for _, ev := range te.events {
				switch ev.Type {
				case ipc.EventTypeError:
					sawError = true
				case ipc.EventTypeStreamEnd:
					sawTerminal = true
				}
			}
			if !sawError {
				t.Fatalf("expected an error event, got %+v", te.events)
			}
			if !sawTerminal {
				t.Fatalf("expected a terminal stream_end event after the error, got %+v", te.events)
			}
		})
	}
}
