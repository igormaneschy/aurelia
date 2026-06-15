package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientSendAndReceive(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "client.sock")

	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	server.Handler = func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
		return []IPCEvent{
			{Type: EventTypeMessage, Body: "response"},
			{Type: EventTypeStreamEnd, Done: true},
		}, nil
	}
	server.Start()

	client := NewClient(socketPath)
	events, err := client.SendAndWait(context.Background(), IPCMessage{
		Type: MsgTypeSend,
		Text: "hello",
	})
	if err != nil {
		t.Fatalf("SendAndWait() error: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != EventTypeMessage || events[0].Body != "response" {
		t.Errorf("unexpected first event: %+v", events[0])
	}
	if events[1].Type != EventTypeStreamEnd {
		t.Errorf("expected stream end, got %+v", events[1])
	}
}

func TestClientPing(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "ping.sock")
	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()
	server.Start()

	client := NewClient(socketPath)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestClientPingFailure(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "nope.sock")
	client := NewClient(socketPath)

	err := client.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error for missing socket")
	}
}

func TestClientSendStream(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "stream.sock")

	// Simulate a server that streams chunks.
	server, err := NewServer(socketPath)
	if err != nil {
		t.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	server.Handler = func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
		return []IPCEvent{
			{Type: EventTypeStreamChunk, Body: "part 1"},
			{Type: EventTypeStreamChunk, Body: "part 2"},
			{Type: EventTypeStreamEnd, Done: true},
		}, nil
	}
	server.Start()

	client := NewClient(socketPath)
	reader, err := client.Send(context.Background(), IPCMessage{Type: MsgTypeSend, Text: "stream test"})
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	defer reader.Close()

	var bodies []string
	for {
		event, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read() error: %v", err)
		}
		bodies = append(bodies, event.Body)
		if event.Type == EventTypeStreamEnd {
			break
		}
	}

	expected := []string{"part 1", "part 2"}
	for i, b := range expected {
		if i >= len(bodies) || bodies[i] != b {
			t.Errorf("expected bodies[%d]=%q, got %v", i, b, bodies)
		}
	}
}

func TestClientWriteError(t *testing.T) {
	// Test that sending with no server gives a clear error.
	client := NewClient("/nonexistent/socket.sock")
	_, err := client.Send(context.Background(), IPCMessage{Type: MsgTypeSend, Text: "x"})
	if err == nil {
		t.Fatal("expected error for nonexistent socket")
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Errorf("expected dial error, got: %v", err)
	}
}

func TestResponseReaderReadLine(t *testing.T) {
	// Simulate a connection that sends JSON lines.
	pr, pw := net.Pipe()

	reader := &ResponseReader{
		conn:    pr,
		scanner: bufio.NewScanner(pr),
	}

	event := IPCEvent{Type: EventTypeAck, Body: "ok"}
	data, _ := json.Marshal(event)
	data = append(data, '\n')

	go func() {
		pw.Write(data)
		pw.Close()
	}()

	got, err := reader.Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}
	if got.Type != EventTypeAck || got.Body != "ok" {
		t.Errorf("unexpected event: %+v", got)
	}

	// Next read should return EOF.
	_, err = reader.Read()
	if err == nil {
		t.Fatal("expected EOF after connection close")
	}
}

func BenchmarkClientSend(b *testing.B) {
	socketPath := filepath.Join(b.TempDir(), "bench.sock")
	server, err := NewServer(socketPath)
	if err != nil {
		b.Fatalf("NewServer() error: %v", err)
	}
	defer server.EnsureClose()

	server.Handler = func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
		return []IPCEvent{{Type: EventTypeAck, Body: fmt.Sprintf("ok:%s", msg.Text)}}, nil
	}
	server.Start()

	client := NewClient(socketPath)
	ctx := context.Background()
	msg := IPCMessage{Type: MsgTypeSend, Text: "bench"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		events, err := client.SendAndWait(ctx, msg)
		if err != nil {
			b.Fatalf("SendAndWait: %v", err)
		}
		if len(events) != 1 || events[0].Type != EventTypeAck {
			b.Fatalf("unexpected response: %+v", events)
		}
	}
}
