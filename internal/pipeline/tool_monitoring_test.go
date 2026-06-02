package pipeline

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ── detectConsecutiveRepeat tests ──────────────────────────────────────

func TestDetectConsecutiveRepeat(t *testing.T) {
	tests := []struct {
		name      string
		calls     []toolCallSnapshot
		threshold int
		want      bool
	}{
		{
			name:      "empty calls",
			calls:     []toolCallSnapshot{},
			threshold: 3,
			want:      false,
		},
		{
			name:      "fewer than threshold",
			calls:     []toolCallSnapshot{{name: "read", inputFp: "a"}, {name: "read", inputFp: "a"}},
			threshold: 3,
			want:      false,
		},
		{
			name: "exactly threshold consecutive",
			calls: []toolCallSnapshot{
				{name: "read", inputFp: "a"},
				{name: "read", inputFp: "a"},
				{name: "read", inputFp: "a"},
			},
			threshold: 3,
			want:      true,
		},
		{
			name: "more than threshold consecutive",
			calls: []toolCallSnapshot{
				{name: "read", inputFp: "a"},
				{name: "read", inputFp: "a"},
				{name: "read", inputFp: "a"},
				{name: "read", inputFp: "a"},
			},
			threshold: 3,
			want:      true,
		},
		{
			name: "non-consecutive — interrupted by different input",
			calls: []toolCallSnapshot{
				{name: "read", inputFp: "a"},
				{name: "read", inputFp: "b"},
				{name: "read", inputFp: "a"},
				{name: "read", inputFp: "a"},
			},
			threshold: 3,
			want:      false,
		},
		{
			name: "prefix of calls matches, newer ones differ",
			calls: []toolCallSnapshot{
				{name: "read", inputFp: "a"},
				{name: "read", inputFp: "a"},
				{name: "read", inputFp: "a"},
				{name: "write", inputFp: "x"},
			},
			threshold: 3,
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectConsecutiveRepeat(tt.calls, tt.threshold)
			if got != tt.want {
				t.Errorf("detectConsecutiveRepeat() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── detectPingPong tests ───────────────────────────────────────────────

func TestDetectPingPong(t *testing.T) {
	readA := toolCallSnapshot{name: "read", inputFp: "fp_a"}
	readB := toolCallSnapshot{name: "read", inputFp: "fp_b"}
	writeC := toolCallSnapshot{name: "write", inputFp: "fp_c"}

	tests := []struct {
		name   string
		calls  []toolCallSnapshot
		length int
		want   bool
	}{
		{
			name:   "too few calls",
			calls:  []toolCallSnapshot{readA, readB, readA},
			length: 4,
			want:   false,
		},
		{
			name:   "A-B-A-B pattern with same tool but different inputs",
			calls:  []toolCallSnapshot{readA, readB, readA, readB},
			length: 4,
			want:   true,
		},
		{
			name:   "A-B-A-B with different tools",
			calls:  []toolCallSnapshot{readA, writeC, readA, writeC},
			length: 4,
			want:   true,
		},
		{
			name:   "not alternating — same tool repeats",
			calls:  []toolCallSnapshot{readA, readA, readB, readB},
			length: 4,
			want:   false,
		},
		{
			name:   "three tools in tail — not A-B-A-B",
			calls:  []toolCallSnapshot{readA, readB, writeC, readA},
			length: 4,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectPingPong(tt.calls, tt.length)
			if got != tt.want {
				t.Errorf("detectPingPong() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── detectToolSpiral tests ─────────────────────────────────────────────

func TestDetectToolSpiral(t *testing.T) {
	tests := []struct {
		name     string
		calls    []toolCallSnapshot
		toolName string
		minLen   int
		want     bool
	}{
		{
			name:     "not enough calls",
			calls:    []toolCallSnapshot{{name: "read"}, {name: "read"}, {name: "read"}},
			toolName: "read",
			minLen:   4,
			want:     false,
		},
		{
			name:     "exact match, exact count — exactly minLen same tools",
			calls:    []toolCallSnapshot{{name: "read"}, {name: "read"}, {name: "read"}, {name: "read"}},
			toolName: "read",
			minLen:   4,
			want:     true,
		},
		{
			name: "interleaved with other tool — not pure spiral",
			calls: []toolCallSnapshot{
				{name: "read"}, {name: "read"}, {name: "read"},
				{name: "write"}, {name: "read"}, {name: "read"},
			},
			toolName: "read",
			minLen:   4,
			want:     false,
		},
		{
			name: "case-insensitive: Read matches read",
			calls: []toolCallSnapshot{
				{name: "Read"}, {name: "read"}, {name: "Read"}, {name: "read"},
			},
			toolName: "read",
			minLen:   4,
			want:     true,
		},
		{
			name: "prefix match: ReadFile matches read",
			calls: []toolCallSnapshot{
				{name: "ReadFile"}, {name: "ReadFile"},
				{name: "ReadFile"}, {name: "ReadFile"},
				{name: "ReadFile"}, {name: "ReadFile"},
				{name: "ReadFile"}, {name: "ReadFile"},
			},
			toolName: "read",
			minLen:   8,
			want:     true,
		},
		{
			name: "prefix match partial failure: mixed in middle",
			calls: []toolCallSnapshot{
				{name: "ReadFile"}, {name: "ReadFile"}, {name: "ReadFile"},
				{name: "ReadFile"}, {name: "Write"}, {name: "ReadFile"},
				{name: "ReadFile"}, {name: "ReadFile"},
			},
			toolName: "read",
			minLen:   8,
			want:     false,
		},
		{
			name: "different case toolName — REad matches read",
			calls: []toolCallSnapshot{
				{name: "REad"}, {name: "read"}, {name: "Read"}, {name: "reAD"},
			},
			toolName: "read",
			minLen:   4,
			want:     true,
		},
		{
			name: "longer tail than minLen — only last minLen checked",
			calls: []toolCallSnapshot{
				{name: "write"}, {name: "read"}, {name: "read"},
				{name: "read"}, {name: "read"}, {name: "read"},
				{name: "read"}, {name: "read"}, {name: "read"},
			},
			toolName: "read",
			minLen:   8,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectToolSpiral(tt.calls, tt.toolName, tt.minLen)
			if got != tt.want {
				t.Errorf("detectToolSpiral() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── loopDetector.ResetForNewTurn test ──────────────────────────────────

func TestLoopDetector_ResetForNewTurn(t *testing.T) {
	d := newLoopDetector(1, 0, nil, func(s string) {})
	d.warned = true

	// Simulate a loop detection that set warned=true
	d.ResetForNewTurn()

	if d.warned {
		t.Error("ResetForNewTurn() did not reset warned flag")
	}
}

func TestLoopDetector_ResetForNewTurn_Nil(t *testing.T) {
	var d *loopDetector
	// Should not panic
	d.ResetForNewTurn()
}

// ── toolCallTracker elapsed time in steer ──────────────────────────────

func TestToolCallTracker_SteerIncludesElapsed(t *testing.T) {
	var mu sync.Mutex
	var steerMsgs []string

	tracker := &toolCallTracker{
		chatID:    1,
		threadID:  0,
		output:    nil,
		startedAt: time.Now().Add(-42 * time.Second), // simulate 42s elapsed
		steerFunc: func(msg string) {
			mu.Lock()
			steerMsgs = append(steerMsgs, msg)
			mu.Unlock()
		},
	}

	// Manually set count to warning threshold to avoid 20 increment calls
	tracker.mu.Lock()
	tracker.count = toolCallWarningThreshold - 1
	tracker.mu.Unlock()
	tracker.increment("Bash")

	mu.Lock()
	msg := steerMsgs[0]
	mu.Unlock()

	if !strings.Contains(msg, "42s") {
		t.Errorf("steer message should contain elapsed time '42s', got: %s", msg)
	}
	if !strings.Contains(msg, "Bash") {
		t.Errorf("steer message should contain tool name 'Bash', got: %s", msg)
	}
}

func TestToolCallTracker_NilTracker(t *testing.T) {
	var tracker *toolCallTracker
	// Should not panic
	tracker.increment("read")
	if tracker.countLocked() != 0 {
		t.Error("nil tracker countLocked() should return 0")
	}
}
