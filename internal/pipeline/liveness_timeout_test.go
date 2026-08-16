package pipeline

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
)

// livenessRecorder is a thread-safe capture of the watchdog hooks: the
// wrapper goroutine writes, the test reads.
type livenessRecorder struct {
	mu       sync.Mutex
	origins  []string
	warns    []string
	notifies []string
}

func (r *livenessRecorder) addOrigin(o string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.origins = append(r.origins, o)
}
func (r *livenessRecorder) addWarn(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.warns = append(r.warns, s)
}
func (r *livenessRecorder) addNotify(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifies = append(r.notifies, s)
}
func (r *livenessRecorder) snapshot() (origins, warns, notifies []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.origins...), append([]string(nil), r.warns...), append([]string(nil), r.notifies...)
}

// TestLivenessTimeout_AliveSilentEscalatesThenCancels covers A2's
// "silent but alive Bridge": the run is NOT canceled at the idle threshold;
// the user receives staged warnings and the safety cancel only happens after
// the grace budget, with the idle origin.
func TestLivenessTimeout_AliveSilentEscalatesThenCancels(t *testing.T) {
	ch := make(chan bridge.Event, 16) // never sends: the bridge is silent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := &livenessRecorder{}
	canceled := make(chan struct{})
	policy := livenessPolicy{idle: 20 * time.Millisecond, urgentLead: 20 * time.Millisecond, cancelLead: 20 * time.Millisecond}

	out := livenessIdleTimeoutWrapper(ctx, ch, policy, func() {
		close(canceled)
	}, rec.addOrigin, livenessHooks{
		probe:  func(context.Context) error { return nil }, // bridge alive
		warn:   func(severity string, _ time.Duration) { rec.addWarn(severity) },
		notify: func(severity string, _ time.Duration) { rec.addNotify(severity) },
	})
	defer func() { close(ch) }()
	<-out // goroutine owns the channel and exits after the safety cancel

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog never canceled after grace budget")
	}

	origins, warns, notifies := rec.snapshot()
	if len(origins) != 1 || origins[0] != timeoutOriginIdleBridge {
		t.Fatalf("origins = %v, want exactly [%s]", origins, timeoutOriginIdleBridge)
	}
	if len(warns) != 2 || warns[0] != "warning" || warns[1] != "urgent" {
		t.Fatalf("warns = %v, want [warning urgent]", warns)
	}
	if len(notifies) != 2 || notifies[0] != "warning" || notifies[1] != "urgent" {
		t.Fatalf("notifies = %v, want [warning urgent]", notifies)
	}
}

// TestLivenessTimeout_DeadBridgeCancelsAtFirstExpiry covers the dead/wedged
// bridge: the probe fails and the run is canceled with the process_death
// origin immediately at the first idle expiry, without staged warnings.
func TestLivenessTimeout_DeadBridgeCancelsAtFirstExpiry(t *testing.T) {
	ch := make(chan bridge.Event, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := &livenessRecorder{}
	canceled := make(chan struct{})
	policy := livenessPolicy{idle: 20 * time.Millisecond, urgentLead: 20 * time.Millisecond, cancelLead: 20 * time.Millisecond}

	out := livenessIdleTimeoutWrapper(ctx, ch, policy, func() {
		close(canceled)
	}, rec.addOrigin, livenessHooks{
		probe:  func(context.Context) error { return errors.New("bridge wedged") },
		warn:   func(severity string, _ time.Duration) { rec.addWarn(severity) },
		notify: func(severity string, _ time.Duration) { rec.addNotify(severity) },
	})
	defer func() { close(ch) }()
	<-out

	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog never canceled on dead bridge")
	}

	origins, warns, notifies := rec.snapshot()
	if len(origins) != 1 || origins[0] != timeoutOriginProcessDeath {
		t.Fatalf("origins = %v, want exactly [%s]", origins, timeoutOriginProcessDeath)
	}
	if len(warns) != 0 || len(notifies) != 0 {
		t.Fatalf("warns = %v notifies = %v, want no escalations on dead bridge", warns, notifies)
	}
}

// TestLivenessTimeout_EventsResetEscalation ensures productive events reset
// the idle window AND the escalation stage: after activity resumes, a fresh
// silence starts again at the warning stage instead of jumping to urgent or
// canceling immediately.
func TestLivenessTimeout_EventsResetEscalation(t *testing.T) {
	ch := make(chan bridge.Event, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec := &livenessRecorder{}
	canceled := make(chan struct{})
	policy := livenessPolicy{idle: 20 * time.Millisecond, urgentLead: 20 * time.Millisecond, cancelLead: 20 * time.Millisecond}

	out := livenessIdleTimeoutWrapper(ctx, ch, policy, func() {
		close(canceled)
	}, rec.addOrigin, livenessHooks{
		probe:  func(context.Context) error { return nil },
		warn:   func(severity string, _ time.Duration) { rec.addWarn(severity) },
		notify: func(severity string, _ time.Duration) { rec.addNotify(severity) },
	})

	// First silence reaches the warning stage.
	time.Sleep(25 * time.Millisecond)
	_, warns, _ := rec.snapshot()
	if len(warns) != 1 || warns[0] != "warning" {
		t.Fatalf("warns = %v, want [warning] after first idle expiry", warns)
	}

	// Activity resumes: event resets window and stage. The escalation must
	// restart at warning (never urgent/cancel while events keep flowing).
	ch <- bridge.Event{Type: "assistant", Content: "still here"}
	time.Sleep(25 * time.Millisecond)
	_, warns, _ = rec.snapshot()
	if len(warns) != 2 || warns[1] != "warning" {
		t.Fatalf("warns = %v, want second [warning] after reset (stage must restart)", warns)
	}
	origins, _, _ := rec.snapshot()
	if len(origins) != 0 {
		t.Fatalf("origins = %v, want none while events were flowing", origins)
	}

	// The fresh silence then runs the full escalation to the safety cancel.
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog never completed the escalation after reset")
	}
	origins, _, _ = rec.snapshot()
	if len(origins) != 1 || origins[0] != timeoutOriginIdleBridge {
		t.Fatalf("origins = %v, want exactly [%s] after reset cycle", origins, timeoutOriginIdleBridge)
	}
	close(ch)
	<-out
}

// TestLivenessTimeout_CtxDoneExits ensures the wrapper exits cleanly when the
// run context is canceled (user /stop, supersede, max execution).
func TestLivenessTimeout_CtxDoneExits(t *testing.T) {
	ch := make(chan bridge.Event, 16)
	ctx, cancel := context.WithCancel(context.Background())

	rec := &livenessRecorder{}
	cancelCalled := make(chan struct{}, 1)
	out := livenessIdleTimeoutWrapper(ctx, ch, livenessPolicy{idle: time.Hour, urgentLead: time.Hour, cancelLead: time.Hour},
		func() { cancelCalled <- struct{}{} },
		rec.addOrigin, livenessHooks{
			probe: func(context.Context) error { return nil },
		})

	cancel()
	select {
	case <-out:
	case <-time.After(2 * time.Second):
		t.Fatal("wrapper did not exit on ctx.Done")
	}
	select {
	case <-cancelCalled:
		t.Fatal("cancel must not be called on ctx.Done")
	default:
	}
	origins, _, _ := rec.snapshot()
	if len(origins) != 0 {
		t.Fatalf("origins = %v, want none", origins)
	}
}
