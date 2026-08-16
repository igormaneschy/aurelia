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
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	eventChannelBuffer    = 128
	maxNDJSONLineSize     = 10 * 1024 * 1024 // 10 MB safety limit per NDJSON line
	maxBridgeRequestBytes = 256 * 1024
)

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

	// pending maps request_id → stream for routing events.
	pending   map[string]*requestStream
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

	// envAllowlist restricts the child process environment. When nil, the bridge
	// inherits os.Environ() (insecure — leaks daemon secrets).
	envAllowlist []string

	slots        *requestSlotTracker
	streamBudget *aggregateStreamBudget
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
		bridgeDir:    bridgeDir,
		command:      cmd,
		args:         args,
		pending:      make(map[string]*requestStream),
		done:         done,
		slots:        newRequestSlotTracker(),
		streamBudget: newAggregateStreamBudget(),
	}
}

// SetOnDeath registers a callback invoked when the bridge process exits
// unexpectedly. It is NOT called during intentional Stop().
func (b *Bridge) SetOnDeath(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onDeath = fn
}

// SetEnvAllowlist restricts the bridge process environment to the given
// "KEY=VALUE" pairs. When not set (or nil), the bridge inherits the full
// daemon environment. Call this once after New and before Start.
func (b *Bridge) SetEnvAllowlist(env []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.envAllowlist = env
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
	if b.envAllowlist != nil {
		cmd.Env = b.envAllowlist
	}

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
			log.Printf("bridge: failed to parse NDJSON line: %s", boundedEventText(parseErr.Error(), 512))
			continue
		}
		ev = normalizeEvent(ev)
		rid := ev.RequestID

		b.pendingMu.Lock()
		stream, ok := b.pending[rid]
		b.pendingMu.Unlock()

		if ok {
			if ev.IsTerminal() {
				b.sendTerminalEvent(stream, ev, rid)
			} else if !stream.deliver(ev) {
				b.droppedEvents.Add(1)
				slog.Warn("bridge: dropped event", "type", ev.Type, "rid", rid)
			}

			if ev.IsTerminal() {
				b.pendingMu.Lock()
				delete(b.pending, rid)
				b.pendingMu.Unlock()
				stream.close()
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

	// Process exited or stdout closed — close all pending streams.
	b.pendingMu.Lock()
	for rid, stream := range b.pending {
		stream.close()
		delete(b.pending, rid)
	}
	b.pendingMu.Unlock()

	b.mu.Lock()
	cmd := b.cmd
	stoppedDuringWait := b.stopping
	b.started = false
	b.cmd = nil
	b.stdin = nil
	b.reader = nil
	b.mu.Unlock()

	// Kill the child process if still alive on unexpected exit, then reap it.
	// This handles the case where stdout closed (e.g. scanner error) but the
	// Node process did not exit, preventing zombies.
	// During intentional Stop(), the Stop() method already handles kill + Wait.
	if cmd != nil && !stoppedDuringWait {
		if cmd.Process != nil && (cmd.ProcessState == nil || !cmd.ProcessState.Exited()) {
			if err := cmd.Process.Kill(); err != nil {
				slog.Error("bridge: failed to kill process in readLoop exit", "error", err)
			}
		}
		doneWait := make(chan struct{}, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("bridge: panic in readLoop process wait", "error", r)
				}
			}()
			_ = cmd.Wait()
			doneWait <- struct{}{}
		}()
		select {
		case <-doneWait:
		case <-time.After(5 * time.Second):
			slog.Error("bridge: timeout waiting for process exit in readLoop")
		}
	}
}

func (b *Bridge) sendTerminalEvent(stream *requestStream, ev Event, rid string) {
	dropped, ok := stream.deliverTerminal(ev)
	if dropped > 0 {
		b.droppedEvents.Add(uint64(dropped))
		slog.Warn("bridge: dropped buffered events to deliver terminal", "count", dropped, "type", ev.Type, "rid", rid)
	}
	if !ok {
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
			if err := cmd.Process.Kill(); err != nil {
				slog.Error("bridge: failed to kill process", "error", err)
			}
		}
		doneWait := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("bridge: panic in Stop process wait", "error", r)
				}
			}()
			_ = cmd.Wait()
			close(doneWait)
		}()
		select {
		case <-doneWait:
		case <-time.After(5 * time.Second):
			slog.Error("bridge: timeout waiting for process exit after kill")
		}
	}

	// Reset state so the bridge can be restarted.
	b.mu.Lock()
	b.started = false
	b.stopping = false
	b.cmd = nil
	b.stdin = nil
	b.reader = nil
	b.pendingMu.Lock()
	b.pending = make(map[string]*requestStream)
	b.pendingMu.Unlock()
	b.mu.Unlock()
}

// cleanupAfterPanic is called from readLoop's recover to close all pending
// channels, reset process state, and notify the death listener when the bridge
// panics unexpectedly.
func (b *Bridge) cleanupAfterPanic() {
	b.pendingMu.Lock()
	for rid, stream := range b.pending {
		stream.close()
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
		if err := cmd.Process.Kill(); err != nil {
			slog.Error("bridge: failed to kill process in cleanupAfterPanic", "error", err)
		}
	}
	if cmd != nil {
		doneWait := make(chan struct{})
		go func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("bridge: panic in cleanupAfterPanic process wait", "error", r)
				}
			}()
			_ = cmd.Wait()
			close(doneWait)
		}()
		select {
		case <-doneWait:
		case <-time.After(5 * time.Second):
			slog.Error("bridge: timeout waiting for process exit in cleanupAfterPanic")
		}
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
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Assign/validate the transport identity and command schema before starting
	// the child or touching pending state. Invalid values must fail closed, not
	// become a shared empty routing key.
	if req.RequestID == "" {
		req.RequestID = fmt.Sprintf("req-%d", b.reqCounter.Add(1))
	} else if !validRequestID(req.RequestID) {
		return nil, fmt.Errorf("bridge: invalid request_id")
	}
	if req.TargetRequestID != "" && !validRequestID(req.TargetRequestID) {
		return nil, fmt.Errorf("bridge: invalid target_request_id")
	}
	if err := validateBridgeRequest(req); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("bridge: marshal request: %w", err)
	}
	if len(payload) > maxBridgeRequestBytes {
		return nil, fmt.Errorf("bridge: request exceeds maximum serialized size (%d bytes)", maxBridgeRequestBytes)
	}

	b.mu.Lock()
	if !b.started {
		if err := b.startLocked(); err != nil {
			b.mu.Unlock()
			return nil, err
		}
	}
	b.mu.Unlock()

	b.mu.Lock()
	if b.streamBudget == nil {
		b.streamBudget = newAggregateStreamBudget()
	}
	streamBudget := b.streamBudget
	b.mu.Unlock()
	controlStream := req.Command == "cancel"
	var acquired bool
	if controlStream {
		acquired = streamBudget.acquireControl()
	} else {
		acquired = streamBudget.acquire()
	}
	if !acquired {
		return nil, fmt.Errorf("bridge: too many active request streams")
	}
	stream := newRequestStream(eventChannelBuffer, streamBudget)
	if controlStream {
		stream = newControlRequestStream(eventChannelBuffer, streamBudget)
	}

	b.pendingMu.Lock()
	if _, exists := b.pending[req.RequestID]; exists {
		b.pendingMu.Unlock()
		stream.close()
		return nil, fmt.Errorf("bridge: duplicate pending request_id")
	}
	b.pending[req.RequestID] = stream
	b.pendingMu.Unlock()

	priority := effectivePriority(req)
	trackPriority := b.slots != nil && !commandBypassesPriorityQueue(req.Command)
	if trackPriority {
		if err := b.slots.acquire(ctx, priority); err != nil {
			b.pendingMu.Lock()
			delete(b.pending, req.RequestID)
			b.pendingMu.Unlock()
			stream.close()
			return nil, err
		}
	}

	// Write request to stdin (don't close stdin!).
	b.mu.Lock()
	if !b.started {
		b.mu.Unlock()
		if trackPriority {
			b.slots.release(priority)
		}
		b.pendingMu.Lock()
		delete(b.pending, req.RequestID)
		b.pendingMu.Unlock()
		stream.close()
		return nil, fmt.Errorf("bridge: process died before write")
	}
	_, err = b.stdin.Write(append(payload, '\n'))
	b.mu.Unlock()

	if err != nil {
		if trackPriority {
			b.slots.release(priority)
		}
		b.pendingMu.Lock()
		delete(b.pending, req.RequestID)
		b.pendingMu.Unlock()
		stream.close()
		return nil, fmt.Errorf("bridge: write request: %w", err)
	}

	// Wrap channel with context cancellation.
	// The event payload is normalized to maxEventPayloadBytes, so this fixed
	// channel is a bounded (and explicit) additional byte budget for the
	// consumer-facing proxy: eventChannelBuffer * maxEventPayloadBytes.
	out := make(chan Event, eventChannelBuffer)
	go func() {
		cleanupPending := func() {
			b.pendingMu.Lock()
			delete(b.pending, req.RequestID)
			b.pendingMu.Unlock()
			stream.close()
		}
		defer func() {
			if trackPriority {
				b.slots.release(priority)
			}
		}()
		defer func() {
			if r := recover(); r != nil {
				slog.Error("bridge: panic in Execute proxy", "error", r)
				cleanupPending()
			}
		}()
		defer stream.discardQueued()
		defer close(out)

		// Hard timeout to prevent goroutine leak if bridge process hangs
		// and never closes ch. Matches pipeline.bridgeExecutionTimeout (30m).
		timer := time.NewTimer(30 * time.Minute)
		defer timer.Stop()

		forward := func(ev Event) bool {
			select {
			case out <- ev:
				return true
			case <-ctx.Done():
				cleanupPending()
				return false
			case <-timer.C:
				slog.Error("bridge: Execute proxy goroutine timed out after 30 minutes",
					"request_id", req.RequestID)
				cleanupPending()
				return false
			}
		}

		drainOverflow := func() {
			for {
				if ov, has := stream.dequeueOverflow(); has {
					if !forward(ov) {
						return
					}
					continue
				}
				return
			}
		}

		for {
			// Prefer the fast channel so events keep FIFO order (overflow only
			// holds spillover after the channel buffer fills).
			select {
			case ev, ok := <-stream.ch:
				if !ok {
					drainOverflow()
					return
				}
				stream.consumeFast(ev)
				if !forward(ev) {
					return
				}
				continue
			default:
			}

			if ev, ok := stream.dequeueOverflow(); ok {
				if !forward(ev) {
					return
				}
				continue
			}

			select {
			case ev, ok := <-stream.ch:
				if !ok {
					drainOverflow()
					return
				}
				stream.consumeFast(ev)
				if !forward(ev) {
					return
				}
			case <-ctx.Done():
				cleanupPending()
				return
			case <-timer.C:
				slog.Error("bridge: Execute proxy goroutine timed out after 30 minutes",
					"request_id", req.RequestID)
				cleanupPending()
				return
			}
		}
	}()

	return out, nil
}

// CancelRequest asks the bridge process to cancel an in-flight request.
func (b *Bridge) CancelRequest(ctx context.Context, requestID string) error {
	if !validRequestID(requestID) {
		return fmt.Errorf("bridge: invalid cancellation request_id")
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
				timer := time.NewTimer(30 * time.Second)
				defer timer.Stop()
				for {
					select {
					case _, ok := <-ch:
						if !ok {
							return
						}
					case <-timer.C:
						slog.Warn("bridge: ExecuteSync drain timed out after 30s",
							"request_id", req.RequestID)
						return
					}
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
	Success        bool   `json:"success"`
	OldSessionFile string `json:"old_session_file,omitempty"`
	OldSessionID   string `json:"old_session_id,omitempty"`
	NewSessionFile string `json:"new_session_file,omitempty"`
	NewSessionID   string `json:"new_session_id,omitempty"`
	SummaryLength  int    `json:"summary_length,omitempty"`
	TokensBefore   int    `json:"tokens_before,omitempty"`
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

// GetSessionHistory returns UI-safe user/assistant messages from a PI session.
// If opts.Resume is empty the bridge returns an empty history without creating
// a new session.
func (b *Bridge) GetSessionHistory(ctx context.Context, opts RequestOptions) ([]SessionHistoryMessage, error) {
	ev, err := b.ExecuteSync(ctx, Request{
		Command: "get-session-history",
		Options: opts,
	})
	if err != nil {
		return nil, fmt.Errorf("bridge: get-session-history: %w", err)
	}
	if ev.Type == "error" {
		return nil, fmt.Errorf("bridge: get-session-history error: %s", ev.Message)
	}
	if ev.Content == "" {
		return nil, nil
	}
	var messages []SessionHistoryMessage
	if err := json.Unmarshal([]byte(ev.Content), &messages); err != nil {
		// An oversized history truncated by the bridge sanitizer (or any
		// other malformed payload) must not surface as a fatal error: the
		// TUI treats history as best-effort. Log for diagnosis and degrade
		// to an empty history instead.
		slog.Warn("bridge: get-session-history parse error; returning empty history",
			"error", err, "content_bytes", len(ev.Content))
		return []SessionHistoryMessage{}, nil
	}
	return messages, nil
}

// ModelInfo describes a model available through the bridge.
type ModelInfo struct {
	Provider       string `json:"provider"`
	ID             string `json:"id"`
	Name           string `json:"name"`
	SupportsImages bool   `json:"supportsImages"`
}

// ListModels returns all models with configured auth from the PI model registry.
// When refresh is true, the bridge reloads models.json and resets any cached
// provider state before returning the list.
func (b *Bridge) ListModels(ctx context.Context, refresh bool) ([]ModelInfo, error) {
	ev, err := b.ExecuteSync(ctx, Request{Command: "list-models", Refresh: refresh})
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
		// A truncated/malformed payload must not surface as a fatal error:
		// the TUI model picker treats the catalog as best-effort. Log for
		// diagnosis and degrade to an empty catalog instead.
		slog.Warn("bridge: list-models parse error; returning empty catalog",
			"error", err, "content_bytes", len(ev.Content))
		return []ModelInfo{}, nil
	}
	return models, nil
}

// AllowlistEnv builds an environment allowlist for the bridge process from
// the current process environment. It keeps essential vars (PATH, HOME, USER,
// SHELL, TMPDIR, PI_CODING_AGENT_DIR), locale vars (LC_*, LANG), and any
// extra named vars (such as provider API keys).
//
// The returned slice is suitable for passing to Bridge.SetEnvAllowlist.
// When no extra keys are needed, callers may also pass a manually constructed
// slice of "KEY=VALUE" pairs.
//
// Example:
//
//	bridge.SetEnvAllowlist(bridge.AllowlistEnv("ANTHROPIC_API_KEY"))
func AllowlistEnv(extraKeys ...string) []string {
	keep := map[string]bool{
		"PATH":                true,
		"HOME":                true,
		"USER":                true,
		"SHELL":               true,
		"TMPDIR":              true,
		"PI_CODING_AGENT_DIR": true,
	}
	for _, k := range extraKeys {
		keep[k] = true
	}

	var env []string
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if keep[key] || strings.HasPrefix(key, "LC_") || key == "LANG" {
			env = append(env, e)
		}
	}
	return env
}
