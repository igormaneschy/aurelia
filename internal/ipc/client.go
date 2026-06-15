package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"time"
)

// DefaultDialTimeout is the default timeout for connecting to the daemon.
const DefaultDialTimeout = 5 * time.Second

// Client connects to the IPC server (daemon) over a Unix socket.
// It handles sending messages and reading streamed responses.
type Client struct {
	socketPath string
	dialTimeout time.Duration
}

// NewClient creates a new IPC client for the given socket path.
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath:  socketPath,
		dialTimeout: DefaultDialTimeout,
	}
}

// Send sends a message to the daemon and returns a response reader for
// consuming streaming events. The caller must close the reader when done.
func (c *Client) Send(ctx context.Context, msg IPCMessage) (*ResponseReader, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("dial daemon: %w", err)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("marshal message: %w", err)
	}
	data = append(data, '\n')

	if err := writeAll(conn, data); err != nil {
		conn.Close()
		return nil, fmt.Errorf("write message: %w", err)
	}

	return newResponseReader(conn), nil
}

// SendAndWait sends a message and reads all response events until a terminal
// event (EventTypeStreamEnd or EventTypeError). Returns all events.
// When a EventTypeError is received, the returned error contains the server's
// error message.
func (c *Client) SendAndWait(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
	reader, err := c.Send(ctx, msg)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	var events []IPCEvent
	for {
		event, err := reader.Read()
		if err != nil {
			return events, fmt.Errorf("read event: %w", err)
		}
		events = append(events, event)
		if event.Type == EventTypeStreamEnd {
			break
		}
		if event.Type == EventTypeError {
			errMsg := event.Error
			if errMsg == "" {
				errMsg = "server returned error"
			}
			return events, fmt.Errorf("server error: %s", errMsg)
		}
	}
	return events, nil
}

// Ping sends a simple ping to check if the daemon is reachable.
func (c *Client) Ping(ctx context.Context) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return fmt.Errorf("daemon not reachable: %w", err)
	}
	conn.Close()
	return nil
}

// dial establishes a connection to the daemon socket with timeout.
func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: c.dialTimeout}
	return dialer.DialContext(ctx, "unix", c.socketPath)
}

// ResponseReader allows reading streaming IPC events from a daemon connection.
// It implements io.Closer to clean up the underlying connection.
type ResponseReader struct {
	conn    net.Conn
	scanner *bufio.Scanner
}

// newResponseReader creates a ResponseReader with a properly sized scanner
// buffer (64KB, matching the server's maximum line size).
func newResponseReader(conn net.Conn) *ResponseReader {
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, maxLineSize), maxLineSize)
	return &ResponseReader{conn: conn, scanner: scanner}
}

// Read reads the next event from the stream. Returns io.EOF when the
// connection is closed by the server.
func (r *ResponseReader) Read() (IPCEvent, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			return IPCEvent{}, fmt.Errorf("read event: %w", err)
		}
		return IPCEvent{}, io.EOF
	}

	var event IPCEvent
	if err := json.Unmarshal([]byte(r.scanner.Text()), &event); err != nil {
		return IPCEvent{}, fmt.Errorf("unmarshal event: %w", err)
	}
	return event, nil
}

// Close closes the underlying connection.
func (r *ResponseReader) Close() error {
	return r.conn.Close()
}
