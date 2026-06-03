package pipeline

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// toolCallTracker monitors cumulative tool calls and emits warnings when
// the model makes too many tool calls without producing a final result.
// This prevents silent tool-call explosions that lead to 30-min timeouts.
// Warnings are sent both to the chat (user-facing) and via steer (model-facing).
type toolCallTracker struct {
	mu                sync.Mutex
	count             int
	chatID            int64
	threadID          int
	output            Output
	steerFunc         func(string)
	startedAt         time.Time
	warningThreshold  int
	criticalThreshold int
}

func newToolCallTracker(chatID int64, threadID int, output Output, steerFunc func(string), warningThreshold int, criticalThreshold int) *toolCallTracker {
	// Use defaults when 0 is passed (e.g. no agent override).
	if warningThreshold <= 0 {
		warningThreshold = toolCallWarningThreshold
	}
	if criticalThreshold <= 0 {
		criticalThreshold = toolCallCriticalThreshold
	}
	return &toolCallTracker{
		chatID:            chatID,
		threadID:          threadID,
		output:            output,
		steerFunc:         steerFunc,
		startedAt:         time.Now(),
		warningThreshold:  warningThreshold,
		criticalThreshold: criticalThreshold,
	}
}

// increment records one tool call and emits progressive warnings when
// thresholds are crossed. At each threshold:
//  1. A Telegram message is sent to the user
//  2. A steer command is sent to the bridge asking the model to consolidate
//
// This dual approach ensures both the user and the model know about the explosion.
func (t *toolCallTracker) increment(toolName string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.count++
	count := t.count
	t.mu.Unlock()

	elapsed := time.Since(t.startedAt).Round(time.Second)
	elapsedStr := elapsed.String()
	if elapsed < time.Second {
		elapsedStr = "<1s"
	}
	switch count {
	case t.warningThreshold:
		t.sendWarning("🔍 Estou analisando bastante contexto. Vou consolidar os achados antes de continuar.")
		t.steer("Você já usou ferramentas %d vezes (%s) em %s. "+
			"Consolide o que já descobriu e apresente um resumo parcial agora.", count, toolName, elapsedStr)
	case t.criticalThreshold:
		t.sendWarning("⚠️ A análise está ficando extensa. Vou consolidar o progresso e apresentar um resumo parcial.")
		t.steer("Você já usou ferramentas %d vezes (%s) em %s. "+
			"Conclua imediatamente o que está fazendo e apresente um resumo parcial. "+
			"O limite de tempo está próximo.", count, toolName, elapsedStr)
	default:
		if count > t.criticalThreshold && count%t.criticalThreshold == 0 {
			t.sendWarning("⏳ Continuo analisando. Vou concluir em breve com um resumo dos resultados.")
			t.steer("Você já usou ferramentas %d vezes em %s. "+
				"Conclua e apresente um resumo parcial imediatamente.", count, elapsedStr)
		}
	}
}

func (t *toolCallTracker) sendWarning(msg string) {
	if t == nil || t.output == nil {
		return
	}
	if _, err := t.output.SendText(t.chatID, t.threadID, msg); err != nil {
		log.Printf("pipeline: toolCallTracker SendText failed for chat=%d: %v", t.chatID, err)
	}
}

func (t *toolCallTracker) steer(format string, args ...any) {
	if t == nil || t.steerFunc == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	t.steerFunc(msg)
}

func (t *toolCallTracker) countLocked() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.count
}

// ResetForNewTurn resets the loop detector for a new turn in a multi-turn
// PI session. Resets the warning flag, clears the call history (ring buffer
// and count), so patterns from previous turns don't trigger false positives.
// Called on turn_start events during multi-turn PI sessions.
func (d *loopDetector) ResetForNewTurn() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.warned = false
	d.ring = make([]toolCallSnapshot, loopDetectorWindow)
	d.next = 0
	d.count = 0
	d.mu.Unlock()
}

// ── Loop detection ────────────────────────────────────────────────────────

const (
	loopDetectorWindow  = 12 // track last N tool calls for pattern detection
	loopRepeatThreshold = 3  // same tool+input repeated N times → loop
	loopPingPongLength  = 4  // A-B-A-B pattern detected after this many calls
	loopOnlyReadLength  = 8  // only "read" calls in window → read spiral
)

// toolCallSnapshot fingerprints one tool_use event for pattern matching.
type toolCallSnapshot struct {
	name    string
	inputFp string // stable input fingerprint (JSON keys + first 200 chars)
}

// loopDetector tracks recent tool calls and detects repetitive patterns.
// When a loop is detected, it sends a steer command asking the model to
// break the cycle and present findings so far.
type loopDetector struct {
	mu        sync.Mutex
	ring      []toolCallSnapshot // circular buffer of recent calls
	next      int                // next write position in ring
	count     int                // total calls seen (for ring-fill tracking)
	warned    bool               // prevent repeated steer during same loop
	chatID    int64
	threadID  int
	output    Output
	steerFunc func(string)
}

func newLoopDetector(chatID int64, threadID int, output Output, steerFunc func(string)) *loopDetector {
	return &loopDetector{
		ring:      make([]toolCallSnapshot, loopDetectorWindow),
		chatID:    chatID,
		threadID:  threadID,
		output:    output,
		steerFunc: steerFunc,
	}
}

// record stores one tool call and returns (isLoop bool, patternType string).
// patternType is one of: "consecutive_repeat", "ping_pong", "tool_spiral", or "" if no loop.
func (d *loopDetector) record(toolName string, input any) (bool, string) {
	if d == nil {
		return false, ""
	}
	fp := fingerprintInput(input)
	snap := toolCallSnapshot{name: toolName, inputFp: fp}

	d.mu.Lock()
	d.ring[d.next] = snap
	d.next = (d.next + 1) % loopDetectorWindow
	d.count++
	isLoop, pattern := d.detectLocked()
	shouldWarn := isLoop && !d.warned
	if shouldWarn {
		d.warned = true
	}
	d.mu.Unlock()

	if shouldWarn {
		msg := "🔁 Vou consolidar o que já encontrei para evitar repetição."
		d.sendWarning(msg)
		d.steerLoop(msg, toolName)
		return true, pattern
	}
	return false, ""
}

// detectLocked checks for loop patterns in the ring buffer.
// Must be called with d.mu held. Returns (isLoop, patternType).
func (d *loopDetector) detectLocked() (bool, string) {
	filled := d.count
	if filled > loopDetectorWindow {
		filled = loopDetectorWindow
	}
	if filled < 4 {
		return false, ""
	}

	// Build ordered slice from ring buffer for analysis
	calls := make([]toolCallSnapshot, filled)
	// Ring buffer: oldest entry is at (d.next - filled + window) % window, newest at (d.next-1 + window) % window.
	// Re-arrange to chronological order:
	for i := 0; i < filled; i++ {
		pos := (d.next - filled + i + loopDetectorWindow) % loopDetectorWindow
		calls[i] = d.ring[pos]
	}

	// Pattern 1: same tool+input repeated consecutively
	if detectConsecutiveRepeat(calls, loopRepeatThreshold) {
		return true, "consecutive_repeat"
	}

	// Pattern 2: alternating A-B-A-B with same inputs
	if detectPingPong(calls, loopPingPongLength) {
		return true, "ping_pong"
	}

	// Pattern 3: only "read" calls in the window (read spiral)
	if detectToolSpiral(calls, "read", loopOnlyReadLength) {
		return true, "tool_spiral"
	}

	return false, ""
}

// detectConsecutiveRepeat returns true if the same tool+input appears
// threshold or more times consecutively at the end of calls.
func detectConsecutiveRepeat(calls []toolCallSnapshot, threshold int) bool {
	n := len(calls)
	if n < threshold {
		return false
	}
	last := calls[n-1]
	count := 1
	for i := n - 2; i >= 0; i-- {
		if calls[i].name == last.name && calls[i].inputFp == last.inputFp {
			count++
			if count >= threshold {
				return true
			}
		} else {
			break
		}
	}
	return false
}

// detectPingPong returns true if the last calls form an A-B-A-B pattern
// with matching inputs for each role.
func detectPingPong(calls []toolCallSnapshot, length int) bool {
	n := len(calls)
	if n < length {
		return false
	}
	// Check last `length` calls for A-B-A-B pattern
	tail := calls[n-length:]
	// Must have exactly 2 distinct (name, input) values alternating
	if tail[0].name == tail[1].name && tail[0].inputFp == tail[1].inputFp {
		return false // A-B-A-B requires alternation, not repetition
	}
	for i := 2; i < length; i++ {
		expected := i % 2
		if tail[i].name != tail[expected].name || tail[i].inputFp != tail[expected].inputFp {
			return false
		}
	}
	return true
}

// detectToolSpiral returns true if the last `minLen` calls are all the
// same tool (e.g., only "read" calls without making progress).
func detectToolSpiral(calls []toolCallSnapshot, toolName string, minLen int) bool {
	n := len(calls)
	if n < minLen {
		return false
	}
	// Apply HasPrefix for case-insensitive prefix matching (not exact equality).
	// This intentionally casts a wide net: any tool whose name starts with
	// toolName (e.g. "read") is treated as part of the spiral, including
	// "Read", "ReadFile", "reading", etc. This is a conscious trade-off: we
	// prefer catching real read spirals over avoiding false positives on
	// tool names like "readme_parser" which are unlikely in practice.
	for i := n - minLen; i < n; i++ {
		if !strings.HasPrefix(strings.ToLower(calls[i].name), strings.ToLower(toolName)) {
			return false
		}
	}
	return true
}

// fingerprintInput produces a stable string fingerprint of a tool call input.
// Uses JSON serialization to ensure deterministic output.
func fingerprintInput(input any) string {
	if input == nil {
		return ""
	}
	b, err := json.Marshal(input)
	if err != nil {
		return fmt.Sprintf("%v", input)
	}
	s := string(b)
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func (d *loopDetector) sendWarning(msg string) {
	if d == nil || d.output == nil {
		return
	}
	if _, err := d.output.SendText(d.chatID, d.threadID, msg); err != nil {
		log.Printf("pipeline: loopDetector SendText failed for chat=%d: %v", d.chatID, err)
	}
}

func (d *loopDetector) steerLoop(msg string, toolName string) {
	if d == nil || d.steerFunc == nil {
		return
	}
	// Extract last 3 distinct tools from ring buffer for context.
	recent := d.recentDistinctTools(3)
	var ctx string
	if len(recent) > 1 {
		ctx = fmt.Sprintf(" (últimas ferramentas: %s)", strings.Join(recent, " → "))
	}
	steerMsg := fmt.Sprintf(
		"Você está repetindo a chamada de ferramenta '%s' em ciclo%s. "+
			"Pare imediatamente. Apresente um resumo do que já descobriu até agora. "+
			"Se precisar de mais informações, peça orientação ao usuário.",
		toolName, ctx)
	d.steerFunc(steerMsg)
}

// recentDistinctTools returns up to n distinct tool names from the ring buffer
// in chronological order (oldest first, deduplicated by name).
func (d *loopDetector) recentDistinctTools(n int) []string {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.recentDistinctToolsLocked(n)
}

// recentDistinctToolsLocked returns up to n distinct tool names from the ring
// buffer. The caller must hold d.mu.
func (d *loopDetector) recentDistinctToolsLocked(n int) []string {
	filled := d.count
	if filled > loopDetectorWindow {
		filled = loopDetectorWindow
	}
	if filled == 0 {
		return nil
	}
	seen := make(map[string]bool, n)
	var result []string
	// Iterate from newest to oldest, collecting distinct names.
	for i := filled - 1; i >= 0 && len(result) < n; i-- {
		pos := (d.next - filled + i + loopDetectorWindow) % loopDetectorWindow
		name := d.ring[pos].name
		if seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	// Since we iterated newest-to-oldest, reverse to get oldest-to-newest.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result
}
