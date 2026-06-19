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

// messageQueueSize is the capacity of the per-connection message channel
// between the reader goroutine and the processing loop. If a remote client
// queues more than this many valid messages while a stream handler blocks,
// the reader treats it as backpressure, emits a clear error if possible,
// and cancels the connection. This is acceptable because a local IPC client
// queuing > capacity while a stream is active is not a normal TUI flow.
// Exposed as a var for test override.
var messageQueueSize = 10

// readTimeout is the maximum idle time between reads from a client connection.
// Exposed as a variable for test override.
var readTimeout = 60 * time.Second

// writeTimeout is the maximum time to write a single event.
// Exposed as a variable for test override.
var writeTimeout = 10 * time.Second

// Server handles incoming IPC connections from TUI clients over a Unix socket.
type Server struct {
	listener net.Listener

	// Handler is called for every incoming IPC message and returns events
	// to be written to the connection synchronously.
	// If nil, the server acknowledges messages but does nothing.
	// Deprecated: prefer StreamHandler for streaming responses.
	Handler func(ctx context.Context, msg IPCMessage) ([]IPCEvent, error)

	// StreamHandler is the preferred handler for streaming IPC responses.
	// It receives an emit function that can be called multiple times to
	// stream events back to the client. The handler returns when the
	// stream is complete. If set, StreamHandler takes precedence over Handler.
	StreamHandler func(ctx context.Context, msg IPCMessage, emit func(IPCEvent) error) error

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
			_ = conn.Close()
			continue
		}
		s.wg.Add(1)
		s.lifecycleMu.Unlock()

		go s.handleConnection(conn)
	}
}

// connMessage carries a parsed IPC message from the reader goroutine to the
// processing loop. The reader goroutine owns all socket reads and sends only
// validated messages here; invalid JSON and validation errors are handled
// inline with error events written directly to the connection.
type connMessage struct {
	msg IPCMessage
}

// handleConnection manages the lifecycle of one client connection. It spins
// up a reader goroutine that owns all socket reads/scanner work and sends
// parsed, validated IPCMessages into an internal channel. The main goroutine
// consumes queued messages sequentially and invokes StreamHandler/Handler for
// each, preserving existing synchronous handler semantics.
//
// Write serialisation: all writes to the connection (error responses from the
// reader, emit calls from StreamHandler, and dispatch responses) share a
// per-connection mutex (writeMu). This prevents interleaved JSON lines.
//
// Queue saturation: if the internal message queue is full (backpressure),
// the reader emits a clear error, cancels connCtx, and exits. This prevents
// the queue-full deadlock where a reader blocked on send cannot observe EOF.
//
// Reader goroutine lifecycle: handleConnection waits for the reader via
// readerWg after conn.Close() has unblocked the scanner. The defer ordering
// guarantees conn.Close() runs before readerWg.Wait() (conn.Close is
// registered before readerWg.Wait, so in LIFO, conn.Close runs after, i.e.
// BEFORE readerWg.Wait).
func (s *Server) handleConnection(conn net.Conn) {
	var readerWg sync.WaitGroup

	defer s.wg.Done()
	defer readerWg.Wait()
	defer func() { _ = conn.Close() }()
	defer func() {
		if r := recover(); r != nil {
			slog.Error("ipc: panic in handleConnection", "error", r, "stack", string(debug.Stack()))
		}
	}()

	defer func() {
		s.connsMu.Lock()
		delete(s.conns, conn)
		s.connsMu.Unlock()
	}()

	connCtx, connCancel := context.WithCancel(s.ctx)
	defer connCancel()

	// Per-connection write serialisation: all writes to conn share this
	// mutex so that error responses from the reader goroutine and emit calls
	// from the processing loop never interleave.
	var writeMu sync.Mutex
	writeLocked := func(ev IPCEvent) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
		data, err := json.Marshal(ev)
		if err != nil {
			return fmt.Errorf("marshal event: %w", err)
		}
		data = append(data, '\n')
		return writeAll(conn, data)
	}

	// Channel for parsed messages from the reader goroutine.
	msgCh := make(chan connMessage, messageQueueSize)

	// Reader goroutine: owns all socket reads. Exits on scanner EOF, read
	// error, connCtx cancellation, or queue saturation. On exit it cancels
	// connCtx (so the active handler aborts) and closes msgCh (so the
	// processing loop drains remaining queued messages and exits).
	readerWg.Add(1)
	go func() {
		defer readerWg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("ipc: panic in reader goroutine", "error", r, "stack", string(debug.Stack()))
			}
		}()
		defer close(msgCh)
		defer connCancel()

		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))

		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, maxLineSize), maxLineSize)

		for scanner.Scan() {
			_ = conn.SetReadDeadline(time.Now().Add(readTimeout))

			line := scanner.Text()
			if line == "" {
				continue
			}

			var msg IPCMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				slog.Warn("ipc: invalid message", "error", err)
				if e := writeLocked(IPCEvent{
					Type:  EventTypeError,
					Error: fmt.Sprintf("invalid message: %v", err),
				}); e != nil {
					slog.Warn("ipc: write error", "error", e)
				}
				continue
			}

			if err := validateMessage(msg); err != nil {
				slog.Warn("ipc: invalid message", "error", err, "request_id", msg.RequestID)
				if e := writeLocked(IPCEvent{
					Type:      EventTypeError,
					Error:     fmt.Sprintf("invalid message: %v", err),
					RequestID: msg.RequestID,
				}); e != nil {
					slog.Warn("ipc: write error", "error", e)
				}
				continue
			}

			select {
			case msgCh <- connMessage{msg: msg}:
			case <-connCtx.Done():
				return
			default:
				// Queue full — backpressure. Cancel connCtx first so the
				// active handler aborts immediately, then attempt a
				// best-effort notification write to the client.
				// Deferred connCancel() in handleConnection covers this
				// path as well, so cancelling here is safe whether or
				// not the reader's own defer also calls it.
				slog.Warn("ipc: message queue full, cancelling connection")
				connCancel()
				if e := writeLocked(IPCEvent{
					Type:  EventTypeError,
					Error: "server busy: too many queued messages",
				}); e != nil {
					slog.Warn("ipc: write error", "error", e)
				}
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if err == bufio.ErrTooLong {
				if e := writeLocked(IPCEvent{
					Type:  EventTypeError,
					Error: "line too long (max 64KB)",
				}); e != nil {
					slog.Warn("ipc: write error", "error", e)
				}
			}
			slog.Debug("ipc: client read error", "error", err)
		}
	}()

	// Processing loop: consume messages from the reader in FIFO order.
	// Each message is dispatched to StreamHandler (preferred) or Handler.
	// The loop exits when msgCh is closed (reader goroutine exited).
	for cm := range msgCh {
		if s.StreamHandler != nil {
			handlerCtx, handlerCancel := context.WithCancel(connCtx)

			emit := func(event IPCEvent) error {
				if event.RequestID == "" && cm.msg.RequestID != "" {
					event.RequestID = cm.msg.RequestID
				}
				return writeLocked(event)
			}
			if err := s.StreamHandler(handlerCtx, cm.msg, emit); err != nil {
				slog.Warn("ipc: stream handler error", "error", err, "request_id", cm.msg.RequestID)
				if e := writeLocked(IPCEvent{
					Type:      EventTypeError,
					Error:     err.Error(),
					RequestID: cm.msg.RequestID,
				}); e != nil {
					slog.Warn("ipc: write error", "error", e)
				}
			}
			handlerCancel()
		} else {
			events, err := s.dispatch(s.ctx, cm.msg)
			if err != nil {
				slog.Warn("ipc: handler error", "error", err, "request_id", cm.msg.RequestID)
				if e := writeLocked(IPCEvent{
					Type:      EventTypeError,
					Error:     err.Error(),
					RequestID: cm.msg.RequestID,
				}); e != nil {
					slog.Warn("ipc: write error", "error", e)
				}
				continue
			}
			for _, event := range events {
				if event.RequestID == "" && cm.msg.RequestID != "" {
					event.RequestID = cm.msg.RequestID
				}
				if e := writeLocked(event); e != nil {
					slog.Warn("ipc: write error", "error", e)
					return
				}
			}
		}
	}
}

// validateMessage checks that an IPCMessage has valid fields before dispatch.
func validateMessage(msg IPCMessage) error {
	switch msg.Type {
	case MsgTypeSend, MsgTypeSubscribe, MsgTypeCommand, MsgTypeHistory,
		MsgTypeSessions, MsgTypeSessionCreate, MsgTypeSessionOpen, MsgTypeSessionDelete,
		MsgTypeProjectState:
		// valid
	default:
		return fmt.Errorf("unknown message type %q", msg.Type)
	}
	if len(msg.Text) > MaxMessageTextLength {
		return fmt.Errorf("text too long (%d bytes, max %d)", len(msg.Text), MaxMessageTextLength)
	}
	if msg.ChatID < 0 && !IsReservedTUIID(msg.ChatID) {
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
	// Validate images if present.
	if len(msg.Images) > MaxImageCount {
		return fmt.Errorf("too many images (%d, max %d)", len(msg.Images), MaxImageCount)
	}
	var totalImageBytes int
	for i, img := range msg.Images {
		if img.Path == "" && img.Data == "" {
			return fmt.Errorf("image[%d]: path or data required", i)
		}
		if img.MediaType == "" {
			return fmt.Errorf("image[%d]: media_type required", i)
		}
		if !isSupportedImageMIME(img.MediaType) {
			return fmt.Errorf("image[%d]: unsupported media_type %q", i, img.MediaType)
		}
		totalImageBytes += len(img.Data)
	}
	if totalImageBytes > MaxTotalImageBytes {
		return fmt.Errorf("total image data too large (%d bytes, max %d)", totalImageBytes, MaxTotalImageBytes)
	}
	// Validate attachments if present.
	if len(msg.Attachments) > MaxAttachmentCount {
		return fmt.Errorf("too many attachments (%d, max %d)", len(msg.Attachments), MaxAttachmentCount)
	}
	for i := range msg.Attachments {
		if msg.Attachments[i].Path == "" {
			return fmt.Errorf("attachment[%d]: path required", i)
		}
		msg.Attachments[i].Path = filepath.Clean(msg.Attachments[i].Path)
		if len(msg.Attachments[i].Path) > 4096 {
			return fmt.Errorf("attachment[%d]: path too long (%d bytes, max 4096)", i, len(msg.Attachments[i].Path))
		}
		if !filepath.IsAbs(msg.Attachments[i].Path) {
			return fmt.Errorf("attachment[%d]: path must be absolute (got %q)", i, msg.Attachments[i].Path)
		}
	}
	return nil
}

// isSupportedImageMIME checks if the MIME type is supported for images.
func isSupportedImageMIME(mimeType string) bool {
	switch mimeType {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
		return true
	default:
		return false
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
		_ = s.listener.Close()
	}

	// Force-close active connections to unblock Wait immediately.
	s.connsMu.Lock()
	for conn := range s.conns {
		_ = conn.Close()
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
