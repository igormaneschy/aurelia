package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"
)

// maxLineSize is the maximum size of a single JSON line (scanner buffer).
const maxLineSize = 64 * 1024

// readTimeout is the maximum idle time between reads from a client connection.
// Exposed as a variable for test override.
var readTimeout = 60 * time.Second

// writeTimeout is the maximum time to write a single event.
// Exposed as a variable for test override.
var writeTimeout = 10 * time.Second

// Server handles incoming IPC connections from TUI clients over a Unix socket.
type Server struct {
	listener net.Listener
	// Handler is called for every incoming IPC message.
	// If nil, the server acknowledges messages but does nothing.
	Handler func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error)

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	lifecycleMu sync.Mutex
	started     bool
	closed      bool

	connsMu sync.Mutex
	conns   map[net.Conn]struct{}
}

// NewServer creates a new IPC server that listens on the given Unix socket path.
// The socket file is created when Start is called. Call Close to clean up.
func NewServer(socketPath string) (*Server, error) {
	// Ensure parent directory exists with owner-only access.
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir %s: %w", dir, err)
	}

	// Remove stale socket file if present.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket %s: %w", socketPath, err)
	}

	// Set umask so net.Listen creates the socket with 0o600 atomically,
	// avoiding the permission window between Listen and Chmod.
	// Use defer to ensure umask is restored even if net.Listen panics.
	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socketPath, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		listener: listener,
		ctx:      ctx,
		cancel:   cancel,
		conns:    make(map[net.Conn]struct{}),
	}, nil
}

// Start begins accepting connections in a background goroutine.
// It returns immediately. Safe to call multiple times. Call Close to stop.
func (s *Server) Start() {
	s.lifecycleMu.Lock()
	if s.started || s.closed {
		s.lifecycleMu.Unlock()
		return
	}
	s.started = true
	s.wg.Add(1) // under lock so Close cannot begin Wait() between Add and spawn
	s.lifecycleMu.Unlock()

	go s.acceptLoop()
}

// acceptLoop runs in a goroutine, accepting connections until the context
// is cancelled or the listener is closed.
func (s *Server) acceptLoop() {
	defer s.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("ipc: panic in acceptLoop", "error", r, "stack", string(debug.Stack()))
		}
	}()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			// Check if we're shutting down.
			if s.ctx.Err() != nil {
				return
			}
			slog.Warn("ipc: accept error", "error", err)
			continue
		}
		slog.Debug("ipc: client connected", "remote", conn.RemoteAddr())

		// Register before lifecycleMu check so Close() can force-close
		// this connection even if we decide not to spawn the handler.
		s.connsMu.Lock()
		s.conns[conn] = struct{}{}
		s.connsMu.Unlock()

		s.lifecycleMu.Lock()
		if s.closed {
			// Server is shutting down — don't spawn a handler. Close the
			// connection immediately; Close() will force-close it too but
			// double-close is safe for net.Conn.
			s.lifecycleMu.Unlock()
			conn.Close()
			continue
		}
		s.wg.Add(1)
		s.lifecycleMu.Unlock()

		go s.handleConnection(conn)
	}
}

// handleConnection reads messages from a single client connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("ipc: panic in handleConnection", "error", r, "stack", string(debug.Stack()))
		}
	}()

	// Connection was registered in acceptLoop before spawning this goroutine.
	defer func() {
		s.connsMu.Lock()
		delete(s.conns, conn)
		s.connsMu.Unlock()
	}()

	// Set read deadline to prevent slow-client DoS.
	conn.SetReadDeadline(time.Now().Add(readTimeout))

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, maxLineSize), maxLineSize)

	for scanner.Scan() {
		// Extend read deadline on each successful read.
		conn.SetReadDeadline(time.Now().Add(readTimeout))

		line := scanner.Text()
		if line == "" {
			continue
		}

		var msg IPCMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			slog.Warn("ipc: invalid message", "error", err)
			s.writeEvent(conn, IPCEvent{
				Type:  EventTypeError,
				Error: fmt.Sprintf("invalid message: %v", err),
			})
			continue
		}

		if err := validateMessage(msg); err != nil {
			slog.Warn("ipc: invalid message", "error", err)
			s.writeEvent(conn, IPCEvent{
				Type:  EventTypeError,
				Error: fmt.Sprintf("invalid message: %v", err),
			})
			continue
		}

		events, err := s.dispatch(s.ctx, msg)
		if err != nil {
			slog.Warn("ipc: handler error", "error", err)
			s.writeEvent(conn, IPCEvent{
				Type:  EventTypeError,
				Error: err.Error(),
			})
			continue
		}

		for _, event := range events {
			if err := s.writeEvent(conn, event); err != nil {
				slog.Warn("ipc: write error", "error", err)
				return // Connection is broken, stop reading.
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if err == bufio.ErrTooLong {
			s.writeEvent(conn, IPCEvent{
				Type:  EventTypeError,
				Error: "line too long (max 64KB)",
			})
		}
		slog.Debug("ipc: client read error", "error", err)
	}
}

// validateMessage checks that an IPCMessage has valid fields before dispatch.
func validateMessage(msg IPCMessage) error {
	switch msg.Type {
	case MsgTypeSend, MsgTypeSubscribe, MsgTypeCommand:
		// valid
	default:
		return fmt.Errorf("unknown message type %q", msg.Type)
	}
	if len(msg.Text) > MaxMessageTextLength {
		return fmt.Errorf("text too long (%d bytes, max %d)", len(msg.Text), MaxMessageTextLength)
	}
	if msg.ChatID < 0 {
		return fmt.Errorf("negative chat_id: %d", msg.ChatID)
	}
	if msg.ThreadID < 0 {
		return fmt.Errorf("negative thread_id: %d", msg.ThreadID)
	}
	if msg.UserID < 0 {
		return fmt.Errorf("negative user_id: %d", msg.UserID)
	}
	if len(msg.RequestID) > MaxRequestIDLength {
		return fmt.Errorf("request_id too long (%d bytes, max %d)", len(msg.RequestID), MaxRequestIDLength)
	}
	return nil
}

// dispatch routes a message to the configured handler or returns a default
// acknowledgement.
func (s *Server) dispatch(ctx context.Context, msg IPCMessage) ([]IPCEvent, error) {
	if s.Handler == nil {
		return []IPCEvent{{
			Type: EventTypeAck,
			Body: "received",
		}}, nil
	}
	return s.Handler(ctx, msg)
}

// writeAll writes data to w in a loop until all bytes are written or an error
// occurs, handling short writes.
func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("write returned 0 bytes without error")
		}
		data = data[n:]
	}
	return nil
}

// writeEvent marshals and writes a single IPCEvent as a JSON line.
func (s *Server) writeEvent(conn net.Conn, event IPCEvent) error {
	conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	data = append(data, '\n')
	return writeAll(conn, data)
}

// Close gracefully shuts down the server: stops accepting new connections,
// force-closes active connections, and waits for all handlers to finish.
// Safe to call multiple times.
func (s *Server) Close() error {
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		return nil
	}
	s.closed = true
	s.lifecycleMu.Unlock()

	s.cancel()

	// Stop accepting new connections.
	if s.listener != nil {
		s.listener.Close()
	}

	// Force-close active connections to unblock Wait immediately.
	s.connsMu.Lock()
	for conn := range s.conns {
		conn.Close()
	}
	s.connsMu.Unlock()

	// Wait for active connections to finish.
	s.wg.Wait()

	// Remove the socket file.
	socketPath := ""
	if s.listener != nil {
		socketPath = s.listener.Addr().String()
	}
	if socketPath != "" {
		if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove socket %s: %w", socketPath, err)
		}
	}
	return nil
}

// EnsureClose is a convenience wrapper for use with defer.
func (s *Server) EnsureClose() {
	if err := s.Close(); err != nil {
		slog.Warn("ipc: close error", "error", err)
	}
}

// Addr returns the listener's network address (the socket path).
func (s *Server) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// writeBatch writes zero or more events as JSON lines. Returns on first
// write error. Partially written events may remain — callers should treat
// the connection as broken after an error.
func writeBatch(w io.Writer, events ...IPCEvent) error {
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		data = append(data, '\n')
		if err := writeAll(w, data); err != nil {
			return err
		}
	}
	return nil
}
