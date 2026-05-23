package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	eventChannelBuffer = 128
	maxNDJSONLineSize  = 10 * 1024 * 1024 // 10 MB safety limit per NDJSON line
)

// safeClose closes a channel, recovering from panic if already closed.
func safeClose(ch chan Event) {
	defer func() { _ = recover() }()
	close(ch)
}

// Bridge manages a long-lived TypeScript bridge process and communicates via
// stdin/stdout using NDJSON. Multiple requests are multiplexed over a single
// process using request_id correlation.
type Bridge struct {
	bridgeDir string // directory containing bridge/index.ts

	// command and args override the default "npx tsx index.ts" for testing.
	command string
	args    []string

	mu     sync.Mutex // guards stdin writes and process lifecycle
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader

	// pending maps request_id → channel for routing events.
	pending   map[string]chan Event
	pendingMu sync.Mutex

	started  bool
	stopping bool

	// reqCounter generates unique request IDs.
	reqCounter atomic.Uint64

	// done is closed when the reader goroutine exits.
	done chan struct{}

	// onDeath is called when the process exits unexpectedly (not via Stop).
	onDeath func()

	// droppedEvents counts events dropped due to slow consumers.
	droppedEvents atomic.Uint64
}

// New creates a Bridge that runs in bridgeDir.
// If bundlePath is non-empty, uses `node <filename>`. Otherwise falls back to `npx tsx index.ts`.
func New(bridgeDir string, bundlePath string) *Bridge {
	cmd := "npx"
	args := []string{"tsx", "index.ts"}
	if bundlePath != "" {
		cmd = "node"
		// Use just the filename since cmd.Dir is set to bridgeDir.
		// --experimental-strip-types allows TypeScript syntax in the bundle.
		args = []string{"--experimental-strip-types", filepath.Base(bundlePath)}
	}
	done := make(chan struct{})
	close(done) // closed = no process running, Stop() won't block
	return &Bridge{
		bridgeDir: bridgeDir,
		command:   cmd,
		args:      args,
		pending:   make(map[string]chan Event),
		done:      done,
	}
}

// SetOnDeath registers a callback invoked when the bridge process exits
// unexpectedly. It is NOT called during intentional Stop().
func (b *Bridge) SetOnDeath(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onDeath = fn
}

// Start launches the bridge process. Safe to call multiple times — no-op if
// already running.
func (b *Bridge) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.startLocked()
}

func (b *Bridge) startLocked() error {
	if b.started {
		return nil
	}

	cmd := exec.Command(b.command, b.args...)
	cmd.Dir = b.bridgeDir

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("bridge: stdin pipe: %w", err)
	}

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("bridge: stdout pipe: %w", err)
	}

	// Stderr goes to parent stderr for debugging.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("bridge: start process: %w", err)
	}

	b.cmd = cmd
	b.stdin = stdinPipe
	b.reader = bufio.NewReaderSize(stdoutPipe, 64*1024)
	b.started = true
	b.stopping = false
	b.done = make(chan struct{})

	go b.readLoop()

	return nil
}

// readLoop runs in a goroutine, reading stdout and routing events to pending
// request channels. When the process exits, all pending channels are closed.
func (b *Bridge) readLoop() {
	defer close(b.done)
	defer func() {
		if r := recover(); r != nil {
			slog.Error("bridge: panic in readLoop", "error", r)
			b.cleanupAfterPanic()
		}
	}()

	// Use bufio.Scanner with maxNDJSONLineSize limit to prevent OOM from
	// oversized NDJSON lines. A token exceeding the limit produces ErrTooLong
	// without allocating the entire line.
	scanner := bufio.NewScanner(b.reader)
	scanner.Buffer(make([]byte, 64*1024), maxNDJSONLineSize)

	for scanner.Scan() {
		buf := bytes.TrimRight(scanner.Bytes(), "\n\r")
		if len(buf) == 0 {
			continue
		}

		var ev Event
		if parseErr := json.Unmarshal(buf, &ev); parseErr != nil {
			log.Printf("bridge: failed to parse NDJSON line: %v", parseErr)
			continue
		}
		rid := ev.RequestID

		b.pendingMu.Lock()
		ch, ok := b.pending[rid]
		b.pendingMu.Unlock()

		if ok {
			if ev.IsTerminal() {
				b.sendTerminalEvent(ch, ev, rid)
			} else {
				// Non-blocking send — channel has buffer.
				select {
				case ch <- ev:
				default:
					b.droppedEvents.Add(1)
					slog.Warn("bridge: dropped event", "type", ev.Type, "rid", rid)
				}
			}

			if ev.IsTerminal() {
				b.pendingMu.Lock()
				delete(b.pending, rid)
				b.pendingMu.Unlock()
				safeClose(ch)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			slog.Error("bridge: NDJSON line exceeds max size", "max", maxNDJSONLineSize)
		} else if err != io.EOF {
			slog.Error("bridge: read error", "error", err)
		}
	}

	// Notify listener if this was an unexpected exit (not Stop).
	b.mu.Lock()
	stopping := b.stopping
	cb := b.onDeath
	b.mu.Unlock()
	if !stopping && cb != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("bridge: panic in onDeath callback", "error", r)
				}
			}()
			cb()
		}()
	}

	// Process exited or stdout closed — close all pending channels.
	b.pendingMu.Lock()
	for rid, ch := range b.pending {
		safeClose(ch)
		delete(b.pending, rid)
	}
	b.pendingMu.Unlock()

	b.mu.Lock()
	b.started = false
	b.cmd = nil
	b.mu.Unlock()
}

func (b *Bridge) sendTerminalEvent(ch chan Event, ev Event, rid string) {
	select {
	case ch <- ev:
		return
	default:
	}

	// Preserve terminal delivery by evicting one buffered non-terminal event
	// instead of dropping result/error and making the consumer think the bridge died.
	select {
	case <-ch:
		b.droppedEvents.Add(1)
		slog.Warn("bridge: dropped buffered event to deliver terminal", "type", ev.Type, "rid", rid)
	default:
	}
	select {
	case ch <- ev:
	default:
		b.droppedEvents.Add(1)
		slog.Error("bridge: terminal event could not be delivered", "type", ev.Type, "rid", rid)
	}
}

// Stop kills the bridge process. Safe to call multiple times.
func (b *Bridge) Stop() {
	b.mu.Lock()
	if !b.started || b.stopping {
		b.mu.Unlock()
		return
	}
	b.stopping = true
	stdin := b.stdin
	cmd := b.cmd
	done := b.done
	b.mu.Unlock()

	// Close stdin — the TS bridge exits on stdin close.
	if stdin != nil {
		_ = stdin.Close()
	}

	// Wait for reader goroutine to finish (it will close all pending channels).
	if done != nil {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			log.Printf("bridge: timeout waiting for reader goroutine, forcing kill")
		}
	}

	// Ensure process is reaped.
	if cmd != nil {
		if cmd.Process != nil && (cmd.ProcessState == nil || !cmd.ProcessState.Exited()) {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}

	// Reset state so the bridge can be restarted.
	b.mu.Lock()
	b.started = false
	b.stopping = false
	b.cmd = nil
	b.stdin = nil
	b.reader = nil
	b.pendingMu.Lock()
	b.pending = make(map[string]chan Event)
	b.pendingMu.Unlock()
	b.mu.Unlock()
}

// cleanupAfterPanic is called from readLoop's recover to close all pending
// channels, reset process state, and notify the death listener when the bridge
// panics unexpectedly.
func (b *Bridge) cleanupAfterPanic() {
	b.pendingMu.Lock()
	for rid, ch := range b.pending {
		safeClose(ch)
		delete(b.pending, rid)
	}
	b.pendingMu.Unlock()

	var cmd *exec.Cmd
	var cb func()
	var stopping bool
	b.mu.Lock()
	cmd = b.cmd
	cb = b.onDeath
	stopping = b.stopping
	b.started = false
	b.cmd = nil
	b.stdin = nil
	b.reader = nil
	b.mu.Unlock()

	// Kill the OS process if still alive (prevents zombie processes).
	if cmd != nil && cmd.Process != nil && (cmd.ProcessState == nil || !cmd.ProcessState.Exited()) {
		_ = cmd.Process.Kill()
	}
	if cmd != nil {
		_ = cmd.Wait()
	}

	// Notify death listener only on unexpected panic (not during Stop).
	if !stopping && cb != nil {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("bridge: panic in onDeath callback", "error", r)
				}
			}()
			cb()
		}()
	}
}

// DroppedEvents returns the number of events dropped due to slow consumers.
func (b *Bridge) DroppedEvents() uint64 {
	return b.droppedEvents.Load()
}

// Execute sends a request to the long-lived Bridge process and returns a
// channel of events for that request. The process stays alive after the
// request completes.
func (b *Bridge) Execute(ctx context.Context, req Request) (<-chan Event, error) {
	b.mu.Lock()
	if !b.started {
		if err := b.startLocked(); err != nil {
			b.mu.Unlock()
			return nil, err
		}
	}
	b.mu.Unlock()

	// Assign request_id if not set.
	if req.RequestID == "" {
		req.RequestID = fmt.Sprintf("req-%d", b.reqCounter.Add(1))
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("bridge: marshal request: %w", err)
	}

	ch := make(chan Event, eventChannelBuffer)

	b.pendingMu.Lock()
	b.pending[req.RequestID] = ch
	b.pendingMu.Unlock()

	// Write request to stdin (don't close stdin!).
	b.mu.Lock()
	if !b.started {
		b.mu.Unlock()
		b.pendingMu.Lock()
		delete(b.pending, req.RequestID)
		b.pendingMu.Unlock()
		safeClose(ch)
		return nil, fmt.Errorf("bridge: process died before write")
	}
	_, err = b.stdin.Write(append(payload, '\n'))
	b.mu.Unlock()

	if err != nil {
		b.pendingMu.Lock()
		delete(b.pending, req.RequestID)
		b.pendingMu.Unlock()
		safeClose(ch)
		return nil, fmt.Errorf("bridge: write request: %w", err)
	}

	// Wrap channel with context cancellation.
	out := make(chan Event, eventChannelBuffer)
	go func() {
		cleanupPending := func() {
			b.pendingMu.Lock()
			delete(b.pending, req.RequestID)
			b.pendingMu.Unlock()
		}
		defer func() {
			if r := recover(); r != nil {
				slog.Error("bridge: panic in Execute proxy", "error", r)
				cleanupPending()
			}
		}()
		defer close(out)
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					cleanupPending()
					return
				}
			case <-ctx.Done():
				cleanupPending()
				return
			}
		}
	}()

	return out, nil
}

// CancelRequest asks the bridge process to cancel an in-flight request.
func (b *Bridge) CancelRequest(ctx context.Context, requestID string) error {
	if requestID == "" {
		return nil
	}
	ev, err := b.ExecuteSync(ctx, Request{Command: "cancel", TargetRequestID: requestID})
	if err != nil {
		return fmt.Errorf("bridge: cancel request %s: %w", requestID, err)
	}
	if ev.Type == "error" {
		return fmt.Errorf("bridge: cancel request %s: %s", requestID, ev.Message)
	}
	return nil
}

// ExecuteSync sends a request and blocks until a terminal event (result or error)
// is received. It returns that event. Intermediate events are discarded.
func (b *Bridge) ExecuteSync(ctx context.Context, req Request) (*Event, error) {
	ch, err := b.Execute(ctx, req)
	if err != nil {
		return nil, err
	}

	var last *Event
	for ev := range ch {
		ev := ev
		last = &ev
		if ev.IsTerminal() {
			// Drain remaining events (shouldn't be any, but be safe).
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("bridge: panic in ExecuteSync drain", "error", r)
					}
				}()
				for range ch { //nolint:revive
				}
			}()
			return last, nil
		}
	}

	if last != nil {
		return last, nil
	}
	return nil, fmt.Errorf("bridge: process exited without producing any events")
}

// Ping verifies the bridge process can start and respond to a ping command.
func (b *Bridge) Ping(ctx context.Context) error {
	ev, err := b.ExecuteSync(ctx, Request{Command: "ping"})
	if err != nil {
		return fmt.Errorf("bridge: ping failed: %w", err)
	}
	if ev.Type == "error" {
		return fmt.Errorf("bridge: ping returned error: %s", ev.Message)
	}
	if ev.Type != "pong" {
		return fmt.Errorf("bridge: ping expected pong, got %q", ev.Type)
	}
	return nil
}

// RotateSessionResult holds the result of a rotate-session command.
type RotateSessionResult struct {
	Success       bool   `json:"success"`
	OldSessionFile string `json:"old_session_file,omitempty"`
	OldSessionID  string `json:"old_session_id,omitempty"`
	NewSessionFile string `json:"new_session_file,omitempty"`
	NewSessionID  string `json:"new_session_id,omitempty"`
	SummaryLength int    `json:"summary_length,omitempty"`
	TokensBefore  int    `json:"tokens_before,omitempty"`
}

// RotateSession rotates a session by compacting the old one and creating a new
// one seeded with the structured summary. Returns the new session file path.
func (b *Bridge) RotateSession(ctx context.Context, opts RequestOptions) (*RotateSessionResult, error) {
	ev, err := b.ExecuteSync(ctx, Request{
		Command: "rotate-session",
		Options: opts,
	})
	if err != nil {
		return nil, fmt.Errorf("bridge: rotate-session: %w", err)
	}
	if ev.Type == "error" {
		return nil, fmt.Errorf("bridge: rotate-session error: %s", ev.Message)
	}
	if ev.Content == "" {
		return nil, nil
	}
	var result RotateSessionResult
	if err := json.Unmarshal([]byte(ev.Content), &result); err != nil {
		return nil, fmt.Errorf("bridge: rotate-session parse: %w", err)
	}
	return &result, nil
}

// CompactSessionResult holds the result of a compact-session command.
type CompactSessionResult struct {
	Success     bool   `json:"success"`
	TokensBefore int   `json:"tokens_before"`
	Summary     string `json:"summary,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	SessionFile string `json:"session_file,omitempty"`
}

// CompactSession requests proactive compaction of a PI session.
// Returns the compaction result with tokens before/after.
func (b *Bridge) CompactSession(ctx context.Context, opts RequestOptions) (*CompactSessionResult, error) {
	ev, err := b.ExecuteSync(ctx, Request{
		Command: "compact-session",
		Options: opts,
	})
	if err != nil {
		return nil, fmt.Errorf("bridge: compact-session: %w", err)
	}
	if ev.Type == "error" {
		return nil, fmt.Errorf("bridge: compact-session error: %s", ev.Message)
	}
	if ev.Content == "" {
		return nil, nil
	}
	var result CompactSessionResult
	if err := json.Unmarshal([]byte(ev.Content), &result); err != nil {
		return nil, fmt.Errorf("bridge: compact-session parse: %w", err)
	}
	return &result, nil
}

// GetSessionStats returns PI session statistics for the given session file.
// Uses the get-session-stats bridge command. Returns nil stats without error
// when session is missing or bridge doesn't support the command.
func (b *Bridge) GetSessionStats(ctx context.Context, opts RequestOptions) (*SessionStats, error) {
	ev, err := b.ExecuteSync(ctx, Request{
		Command: "get-session-stats",
		Options: opts,
	})
	if err != nil {
		return nil, fmt.Errorf("bridge: get-session-stats: %w", err)
	}
	if ev.Type == "error" {
		return nil, fmt.Errorf("bridge: get-session-stats error: %s", ev.Message)
	}
	if ev.Content == "" {
		return nil, nil
	}
	var stats SessionStats
	if err := json.Unmarshal([]byte(ev.Content), &stats); err != nil {
		return nil, fmt.Errorf("bridge: get-session-stats parse: %w", err)
	}
	return &stats, nil
}

// ModelInfo describes a model available through the bridge.
type ModelInfo struct {
	Provider       string `json:"provider"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	SupportsImages bool   `json:"supportsImages"`
}

// ListModels returns all models with configured auth from the PI model registry.
func (b *Bridge) ListModels(ctx context.Context) ([]ModelInfo, error) {
	ev, err := b.ExecuteSync(ctx, Request{Command: "list-models"})
	if err != nil {
		return nil, fmt.Errorf("bridge: list-models failed: %w", err)
	}
	if ev.Type == "error" {
		return nil, fmt.Errorf("bridge: list-models error: %s", ev.Message)
	}
	if ev.Content == "" {
		return nil, nil
	}
	var models []ModelInfo
	if err := json.Unmarshal([]byte(ev.Content), &models); err != nil {
		return nil, fmt.Errorf("bridge: list-models parse: %w", err)
	}
	return models, nil
}
