//go:build linux || darwin

package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ipc")
	if err != nil {
		t.Fatalf("MkdirTemp() error: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "s.sock")
}

func readNextEvent(conn net.Conn) (IPCEvent, error) {
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return IPCEvent{}, err
		}
		return IPCEvent{}, net.ErrClosed
	}
	var event IPCEvent
	if err := json.Unmarshal([]byte(scanner.Text()), &event); err != nil {
		return IPCEvent{}, err
	}
	return event, nil
}

func TestConnPeerUIDMatchesClient(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "peer.sock")

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error: %v", err)
	}
	defer ln.Close()

	serverConnCh := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			t.Errorf("Accept() error: %v", err)
			return
		}
		serverConnCh <- conn
	}()

	client, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer client.Close()

	serverConn := <-serverConnCh
	defer serverConn.Close()

	got, err := connPeerUID(serverConn)
	if err != nil {
		t.Fatalf("connPeerUID() error: %v", err)
	}
	want := os.Getuid()
	if got != want {
		t.Fatalf("connPeerUID() = %d, want %d", got, want)
	}
}

func TestServerRejectsMismatchedUserID(t *testing.T) {
	socketPath := shortSocketPath(t)

	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	rejected := false
	server.Handler = func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
		rejected = true
		return []IPCEvent{{Type: EventTypeAck}}, nil
	}
	server.Start()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer conn.Close()

	msg := IPCMessage{Type: MsgTypeSend, Text: "hello", UserID: 999999}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	event, err := readNextEvent(conn)
	if err != nil {
		t.Fatalf("readNextEvent() error: %v", err)
	}
	if event.Type != EventTypeError {
		t.Fatalf("expected error event, got %q", event.Type)
	}
	if !strings.Contains(event.Error, "authentication failed") {
		t.Fatalf("expected auth error, got %q", event.Error)
	}
	if rejected {
		t.Fatal("handler should not run for mismatched user_id")
	}
}

func TestServerAcceptsMatchingUserID(t *testing.T) {
	socketPath := shortSocketPath(t)

	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	handlerCalled := false
	server.Handler = func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
		handlerCalled = true
		return []IPCEvent{{Type: EventTypeAck, Body: "ok"}}, nil
	}
	server.Start()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer conn.Close()

	msg := IPCMessage{Type: MsgTypeSend, Text: "hello", UserID: int64(os.Getuid())}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	event, err := readNextEvent(conn)
	if err != nil {
		t.Fatalf("readNextEvent() error: %v", err)
	}
	if event.Type == EventTypeError {
		t.Fatalf("unexpected error: %s", event.Error)
	}
	if !handlerCalled {
		t.Fatal("handler was not called for matching user_id")
	}
}