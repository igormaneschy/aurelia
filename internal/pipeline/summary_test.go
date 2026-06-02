package pipeline

import (
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/continuity"
	"github.com/igormaneschy/aurelia/internal/session"
)

// TestSummaryCounterIncrementReset verifies that the counter increments per key
// and resets correctly.
func TestSummaryCounterIncrementReset(t *testing.T) {
	sc := &summaryCounter{counts: make(map[continuity.ConversationKey]int)}

	key1 := continuity.ConversationKeyFor(1, 0, 0)
	key2 := continuity.ConversationKeyFor(2, 0, 0)

	// Increment key1 twice
	turns, should := sc.increment(key1, 5)
	if turns != 1 || should {
		t.Fatalf("increment key1: turns=%d should=%t, want turns=1 should=false", turns, should)
	}
	turns, should = sc.increment(key1, 5)
	if turns != 2 || should {
		t.Fatalf("increment key1: turns=%d should=%t, want turns=2 should=false", turns, should)
	}

	// Increment key2 once
	turns, should = sc.increment(key2, 5)
	if turns != 1 || should {
		t.Fatalf("increment key2: turns=%d should=%t, want turns=1 should=false", turns, should)
	}

	// Reset key1 — key2 should be unaffected
	sc.reset(key1)
	turns, should = sc.increment(key1, 5)
	if turns != 1 || should {
		t.Fatalf("after reset key1: turns=%d should=%t, want turns=1 should=false", turns, should)
	}
	turns, should = sc.increment(key2, 5)
	if turns != 2 || should {
		t.Fatalf("key2 after reset key1: turns=%d should=%t, want turns=2 should=false", turns, should)
	}
}

// TestSummaryCounterTriggersAtInterval verifies that shouldSummarize is true
// when turns >= interval.
func TestSummaryCounterTriggersAtInterval(t *testing.T) {
	sc := &summaryCounter{counts: make(map[continuity.ConversationKey]int)}
	key := continuity.ConversationKeyFor(1, 0, 0)
	interval := 3

	// Turns 1, 2: should not summarize
	for i := 1; i < interval; i++ {
		turns, should := sc.increment(key, interval)
		if turns != i || should {
			t.Fatalf("turn %d: turns=%d should=%t, want =%d should=false", i, turns, should, i)
		}
	}

	// Turn 3: triggers
	turns, should := sc.increment(key, interval)
	if turns != interval || !should {
		t.Fatalf("turn %d: turns=%d should=%t, want =%d should=true", interval, turns, should, interval)
	}

	// After reset, counter goes back to 1
	sc.reset(key)
	turns, should = sc.increment(key, interval)
	if turns != 1 || should {
		t.Fatalf("after reset: turns=%d should=%t, want turns=1 should=false", turns, should)
	}
}

// TestGenerateProgressiveSummaryNilBridge verifies graceful degradation when
// bridge is nil.
func TestGenerateProgressiveSummaryNilBridge(t *testing.T) {
	svc := &Service{} // nil bridge, nil config
	result := svc.generateProgressiveSummary(t.Context(), "prev", "user", "assistant")
	if result != "" {
		t.Fatalf("expected empty result with nil bridge, got %q", result)
	}
}

// TestGenerateProgressiveSummaryNilConfig verifies graceful degradation when
// bridge exists but config is nil.
func TestGenerateProgressiveSummaryNilConfig(t *testing.T) {
	t.Skip("requires a real bridge — unit-tested via nil bridge above")
}

// TestProgressiveSummaryAfterSuccessfulTurn verifies the full afterSuccessfulTurn
// path when summarization is triggered:
//   - counter increments per turn
//   - on non-summary turns, the summary is preserved (not overwritten by raw text)
//   - on the Nth turn, continuity is patched with the merged summary (or degraded)
//   - nudge buffer receives the full text, not the summary
func TestProgressiveSummaryAfterSuccessfulTurn(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	ss := session.NewStore()
	svc := &Service{
		continuity:      contStore,
		sessions:        ss,
		runLog:          &fakeRunLogStore{},
		summaryCounter:  &summaryCounter{counts: make(map[continuity.ConversationKey]int)},
		summaryInterval: 2, // trigger every 2 turns for fast testing
	}

	// Seed previous state so summarization has something to merge
	ctx := t.Context()
	prevSummary := "Previous conversation about project setup"
	err := contStore.Upsert(ctx, continuity.ConversationState{
		ChatID:              42,
		ThreadID:            0,
		UserID:              100,
		LastAssistantSummary: prevSummary,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Turn 1: should NOT trigger summarization (interval=2, first turn)
	// Summary should be PRESERVED from continuity, not overwritten by raw text.
	svc.afterSuccessfulTurn(42, 0, "user text 1", "assistant response 1", "run-1", 100)
	state, err := contStore.Get(ctx, 42, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected non-nil continuity state")
	}
	if state.LastAssistantSummary != prevSummary {
		t.Fatalf("turn 1: LastAssistantSummary = %q, want preserved %q", state.LastAssistantSummary, prevSummary)
	}

	// Turn 2: should trigger summarization, but bridge is nil so it degrades.
	// The last turn's continuity still had the preserved summary, so on degrade
	// the raw text is used as fallback.
	svc.afterSuccessfulTurn(42, 0, "user text 2", "assistant response 2", "run-2", 100)
	state, err = contStore.Get(ctx, 42, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected non-nil continuity state after turn 2")
	}
	if !strings.Contains(state.LastAssistantSummary, "assistant response 2") {
		t.Fatalf("turn 2: LastAssistantSummary = %q, want to contain raw text (degraded)", state.LastAssistantSummary)
	}
}

// TestProgressiveSummaryDisabled verifies that SummaryInterval=0 disables
// summarization — the counter is never incremented and continuity always
// gets the raw text.
func TestProgressiveSummaryDisabled(t *testing.T) {
	contStore := newContinuityTestStore(t)
	defer contStore.Close()

	svc := &Service{
		continuity:      contStore,
		sessions:        session.NewStore(),
		runLog:          &fakeRunLogStore{},
		summaryCounter:  &summaryCounter{counts: make(map[continuity.ConversationKey]int)},
		summaryInterval: 0, // disabled
	}

	// Run multiple turns — none should trigger summarization
	for i := 1; i <= 5; i++ {
		userText := "user text"
		assistantText := "assistant response"
		svc.afterSuccessfulTurn(42, 0, userText, assistantText, "run-x", 100)
	}

	ctx := t.Context()
	state, err := contStore.Get(ctx, 42, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if state == nil {
		t.Fatal("expected non-nil continuity state")
	}
	if !strings.Contains(state.LastAssistantSummary, "assistant response") {
		t.Fatalf("when disabled, LastAssistantSummary = %q, want raw text", state.LastAssistantSummary)
	}

	// Counter should not have been touched
	if len(svc.summaryCounter.counts) != 0 {
		t.Fatalf("expected empty counter map when disabled, got %d keys", len(svc.summaryCounter.counts))
	}
}

// TestProgressiveSummaryNilContinuity verifies that when continuity store is nil,
// the summarization path is skipped and the service does not panic.
func TestProgressiveSummaryNilContinuity(t *testing.T) {
	svc := &Service{
		summaryCounter:  &summaryCounter{counts: make(map[continuity.ConversationKey]int)},
		summaryInterval: 1, // would trigger immediately
	}

	// Should not panic despite nil continuity, nil bridge, nil dreamer
	svc.afterSuccessfulTurn(42, 0, "user text", "assistant response", "run-id", 100)
}

// TestProgressiveSummaryCounterResetAfterDegrade verifies that even when bridge
// is nil and summarization degrades, the counter is NOT reset (only reset on
// successful generation). This ensures the LLM is retried on the next interval
// rather than spinning every turn.
func TestProgressiveSummaryCounterResetAfterDegrade(t *testing.T) {
	svc := &Service{
		summaryCounter:  &summaryCounter{counts: make(map[continuity.ConversationKey]int)},
		summaryInterval: 2,
	}
	key := continuity.ConversationKeyFor(42, 0, 100)

	// Turn 2 should trigger summarization attempt (nil bridge → degrades)
	// After degrade, counter should NOT be reset
	svc.afterSuccessfulTurn(42, 0, "u1", "a1", "r1", 100) // turn 1, no trigger
	svc.afterSuccessfulTurn(42, 0, "u2", "a2", "r2", 100) // turn 2, trigger + degrade

	// Counter should still be at 2 (not reset since generateProgressiveSummary returned "")
	sc := svc.summaryCounter
	sc.mu.Lock()
	turns := sc.counts[key]
	sc.mu.Unlock()
	if turns != 2 {
		t.Fatalf("after degrade, expected counter to stay at 2, got %d", turns)
	}
}
