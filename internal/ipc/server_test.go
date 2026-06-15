package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
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

func TestPanicRecovery(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "s.sock")
	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	server.Handler = func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
		panic("intentional panic")
	}
	server.Start()

	// First connection triggers a panic in the handler.
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}

	msg := IPCMessage{Type: MsgTypeSend, Text: "hello"}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	conn.Write(data)

	// Read until EOF — connection is closed after the panic is recovered.
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
	}
	conn.Close()

	// Second connection should work — server is still accepting and alive.
	conn2, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatal("server stopped accepting after handler panic")
	}
	defer conn2.Close()

	server.Handler = func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
		return []IPCEvent{{Type: EventTypeAck, Body: "recovered"}}, nil
	}

	msg2 := IPCMessage{Type: MsgTypeSend, Text: "test2"}
	data2, _ := json.Marshal(msg2)
	data2 = append(data2, '\n')
	conn2.Write(data2)

	scanner2 := bufio.NewScanner(conn2)
	if !scanner2.Scan() {
		t.Fatal("expected response after panic recovery")
	}
	var event IPCEvent
	json.Unmarshal([]byte(scanner2.Text()), &event)
	if event.Type != EventTypeAck {
		t.Errorf("expected ack, got %q", event.Type)
	}
}

func TestOversizedLine(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "o.sock")
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

	// Send a line larger than 64KB.
	data := make([]byte, 1024*65)
	for i := range data {
		data[i] = 'A'
	}
	data[1024*65-1] = '\n'
	conn.Write(data)

	// Server should respond with EventTypeError for oversized line.
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("expected error response for oversized line")
	}
	var event IPCEvent
	json.Unmarshal([]byte(scanner.Text()), &event)
	if event.Type != EventTypeError {
		t.Errorf("expected EventTypeError, got %q", event.Type)
	}
	if !strings.Contains(event.Error, "too long") {
		t.Errorf("expected 'too long' in error, got %q", event.Error)
	}
}

func TestServerInvalidMessageType(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "it.sock")
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

	msg := IPCMessage{Type: "invalid_type"}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	conn.Write(data)

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("expected error response for invalid message type")
	}
	var event IPCEvent
	json.Unmarshal([]byte(scanner.Text()), &event)
	if event.Type != EventTypeError {
		t.Errorf("expected EventTypeError, got %q", event.Type)
	}
	if !strings.Contains(event.Error, "unknown message type") {
		t.Errorf("expected 'unknown message type' in error, got %q", event.Error)
	}
}

func TestServerCloseWithActiveConnections(t *testing.T) {
	// Use a short temp dir to stay within macOS 104-char unix socket path limit.
	dir, err := os.MkdirTemp("", "au-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "s")
	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	server.Start()

	// Open a connection and send a message that blocks the handler.
	var wg sync.WaitGroup
	wg.Add(1)
	server.Handler = func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
		wg.Done()
		<-ctx.Done() // block until server context is cancelled
		return nil, ctx.Err()
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error: %v", err)
	}
	defer conn.Close()

	msg := IPCMessage{Type: MsgTypeSend, Text: "hello"}
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	conn.Write(data)

	// Wait for the handler to start blocking.
	wg.Wait()

	// Close should return within bounded time even with active connections.
	done := make(chan struct{})
	go func() {
		server.Close()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("Close() took >5s with active connections")
	}
}

func TestServerCloseIdempotent(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "ci.sock")
	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	server.Start()

	if err := server.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}
	if err := server.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}

func TestConnectionDeadline(t *testing.T) {
	// Override read deadline to make the test fast.
	oldRT := readTimeout
	readTimeout = 50 * time.Millisecond
	defer func() { readTimeout = oldRT }()

	socketPath := filepath.Join(t.TempDir(), "d.sock")
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

	// Don't send any data. The server should close the connection
	// after the read deadline expires (50ms).
	start := time.Now()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	elapsed := time.Since(start)
	if err == nil {
		t.Log("unexpectedly read data instead of timeout")
	}
	if elapsed > 5*time.Second {
		t.Fatal("connection not closed within deadline window")
	}
}

func TestWriteAllShortWrite(t *testing.T) {
	pr, pw := net.Pipe()

	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}

	wrapped := &shortWriteWriter{Writer: pw, maxFirst: 10}

	errCh := make(chan error, 1)
	go func() {
		errCh <- writeAll(wrapped, data)
		pw.Close()
	}()

	got, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("ReadAll() error: %v", err)
	}
	if len(got) != len(data) {
		t.Errorf("short write fix failed: got %d bytes, want %d", len(got), len(data))
	}
	for i, b := range got {
		if b != byte(i) {
			t.Errorf("byte %d: got %d, want %d", i, b, byte(i))
		}
	}

	if err := <-errCh; err != nil {
		t.Fatalf("writeAll error: %v", err)
	}
}

func TestServerConcurrentStartClose(t *testing.T) {
	// Use short temp dir to stay within macOS 104-char unix socket path limit.
	dir, err := os.MkdirTemp("", "au-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "s")

	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	// Start and Close concurrently. Must not panic or deadlock.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		server.Start()
	}()

	go func() {
		defer wg.Done()
		server.Close()
	}()

	wg.Wait()
}

func TestServerCloseBeforeStart(t *testing.T) {
	// Use short temp dir to stay within macOS 104-char unix socket path limit.
	dir, err := os.MkdirTemp("", "au-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socketPath := filepath.Join(dir, "s")

	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}

	// Close before Start should clean up cleanly.
	if err := server.Close(); err != nil {
		t.Fatalf("Close() before Start error: %v", err)
	}

	// Start after Close must be a no-op (not panic).
	server.Start()
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) {
	return 0, nil
}

func TestWriteAllZeroByte(t *testing.T) {
	err := writeAll(zeroWriter{}, []byte("hello"))
	if err == nil {
		t.Fatal("expected error for zero-byte write")
	}
	if err.Error() != "write returned 0 bytes without error" {
		t.Errorf("unexpected error message: got %q", err.Error())
	}
}

// shortWriteWriter simulates a writer that returns short writes on the first call.
type shortWriteWriter struct {
	io.Writer
	writeNum int
	maxFirst int
}

func (w *shortWriteWriter) Write(b []byte) (int, error) {
	w.writeNum++
	if w.writeNum == 1 && w.maxFirst > 0 && len(b) > w.maxFirst {
		// Write only maxFirst bytes to the underlying writer and report that count,
		// simulating a short write that writeAll must retry.
		n, err := w.Writer.Write(b[:w.maxFirst])
		if n < w.maxFirst {
			return n, err
		}
		return w.maxFirst, nil
	}
	return w.Writer.Write(b)
}
