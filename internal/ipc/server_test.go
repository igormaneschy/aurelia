package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestServerStartAndStop(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "test.sock")

	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	server.Start()

	// Connect and send a ping.
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer conn.Close()

	msg := IPCMessage{Type: MsgTypeSend, Text: "hello"}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	// Read response.
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("expected response line")
	}
	var event IPCEvent
	if err := json.Unmarshal([]byte(scanner.Text()), &event); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if event.Type != EventTypeAck {
		t.Errorf("expected type %q, got %q", EventTypeAck, event.Type)
	}
}

func TestServerHandler(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "handler.sock")

	handlerCalled := false
	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	server.Handler = func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
		handlerCalled = true
		if msg.Type != MsgTypeCommand {
			t.Errorf("expected type %q, got %q", MsgTypeCommand, msg.Type)
		}
		if msg.Text != "/status" {
			t.Errorf("expected text %q, got %q", "/status", msg.Text)
		}
		return []IPCEvent{
			{Type: EventTypeMessage, Body: "ok"},
		}, nil
	}
	server.Start()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer conn.Close()

	msg := IPCMessage{Type: MsgTypeCommand, Text: "/status"}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	conn.Write(data)

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("expected response")
	}
	var event IPCEvent
	json.Unmarshal([]byte(scanner.Text()), &event)
	if event.Body != "ok" {
		t.Errorf("expected body %q, got %q", "ok", event.Body)
	}
	if !handlerCalled {
		t.Error("handler was not called")
	}
}

func TestServerInvalidJSON(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "invalid.sock")

	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()
	server.Start()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer conn.Close()

	// Send garbage.
	conn.Write([]byte("not json\n"))

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("expected error response")
	}
	var event IPCEvent
	json.Unmarshal([]byte(scanner.Text()), &event)
	if event.Type != EventTypeError {
		t.Errorf("expected error type, got %q", event.Type)
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "shutdown.sock")

	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	server.Start()

	// Close should not error.
	if err := server.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	// Socket file should be removed.
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Errorf("expected socket file to be removed, stat error: %v", err)
	}
}

func TestServerDoubleStart(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "double.sock")

	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	// Starting twice should not panic or error.
	server.Start()
	server.Start()

	// Should still accept connections.
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	conn.Close()
}

func TestEnsureSingleInstance(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")

	cleanup1, err := ensureSingleInstance(socketPath)
	if err != nil {
		t.Fatalf("first ensureSingleInstance() error: %v", err)
	}
	defer cleanup1()

	// Second call should fail.
	_, err = ensureSingleInstance(socketPath)
	if err == nil {
		t.Fatal("expected error for duplicate instance")
	}
}

func TestWriteBatch(t *testing.T) {
	pr, pw := net.Pipe()
	defer pw.Close()
	defer pr.Close()

	events := []IPCEvent{
		{Type: EventTypeAck, Body: "first"},
		{Type: EventTypeMessage, Body: "second"},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- writeBatch(pw, events...)
	}()

	scanner := bufio.NewScanner(pr)
	for _, expected := range events {
		if !scanner.Scan() {
			t.Fatal("expected more events")
		}
		var got IPCEvent
		if err := json.Unmarshal([]byte(scanner.Text()), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Type != expected.Type || got.Body != expected.Body {
			t.Errorf("got %+v, expected %+v", got, expected)
		}
	}

	if err := <-errCh; err != nil {
		t.Fatalf("writeBatch error: %v", err)
	}
}
