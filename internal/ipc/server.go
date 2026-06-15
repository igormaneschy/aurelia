package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// Server handles incoming IPC connections from TUI clients over a Unix socket.
type Server struct {
	listener net.Listener
	// Handler is called for every incoming IPC message.
	// If nil, the server acknowledges messages but does nothing.
	Handler func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error)

	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
}

// NewServer creates a new IPC server that listens on the given Unix socket path.
// The socket file is created when Start is called. Call Close to clean up.
func NewServer(socketPath string) (*Server, error) {
	// Ensure parent directory exists.
	dir := filepath.Dir(socketPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create socket dir %s: %w", dir, err)
	}

	// Remove stale socket file if present.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket %s: %w", socketPath, err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socketPath, err)
	}

	// Set permissions so only the owner can connect.
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		return nil, fmt.Errorf("chmod socket %s: %w", socketPath, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		listener: listener,
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Start begins accepting connections in a background goroutine.
// It returns immediately. Call Close to stop.
func (s *Server) Start() {
	if s.started {
		return
	}
	s.started = true
	s.wg.Add(1)
	go s.acceptLoop()
}

// acceptLoop runs in a goroutine, accepting connections until the context
// is cancelled or the listener is closed.
func (s *Server) acceptLoop() {
	defer s.wg.Done()

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
		s.wg.Add(1)
		go s.handleConnection(conn)
	}
}

// handleConnection reads messages from a single client connection.
func (s *Server) handleConnection(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 1024*64), 1024*64) // 64KB read buffer

	for scanner.Scan() {
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
		slog.Debug("ipc: client read error", "error", err)
	}
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

// writeEvent marshals and writes a single IPCEvent as a JSON line.
func (s *Server) writeEvent(conn net.Conn, event IPCEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	return err
}

// Close gracefully shuts down the server: stops accepting new connections
// and waits for all handlers to finish.
func (s *Server) Close() error {
	s.cancel()

	// Stop accepting new connections.
	if s.listener != nil {
		s.listener.Close()
	}

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

// ensureSingleInstance attempts to lock the socket path so only one daemon
// runs at a time. It creates a lock file next to the socket.
// Returns a cleanup function or an error if another instance is already running.
func ensureSingleInstance(socketPath string) (cleanup func(), err error) {
	lockPath := socketPath + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another instance is running (lock: %s)", lockPath)
		}
		return nil, fmt.Errorf("create lock %s: %w", lockPath, err)
	}
	fmt.Fprintf(file, "%d\n", os.Getpid())
	file.Close()

	cleanup = func() {
		os.Remove(lockPath)
	}
	return cleanup, nil
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
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}
