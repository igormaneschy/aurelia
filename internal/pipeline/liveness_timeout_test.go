package pipeline

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
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

func TestLivenessTimeout_RepeatedTelemetryStillEscalates(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ch := make(chan bridge.Event, 8)
	rec := &livenessRecorder{}
	policy := livenessPolicy{idle: 15 * time.Millisecond, urgentLead: 15 * time.Millisecond, cancelLead: 15 * time.Millisecond}
	canceled := make(chan struct{})
	var cancelOnce sync.Once
	telemetryIndex := 0
	out := livenessIdleTimeoutWrapper(ctx, ch, policy, func() {
		cancelOnce.Do(func() {
			close(canceled)
			stop()
		})
	}, rec.addOrigin, livenessHooks{
		probe: func(context.Context) error { return nil },
		warn:  func(severity string, _ time.Duration) { rec.addWarn(severity) },
	})

	go func() {
		ticker := time.NewTicker(3 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				typeName := repeatedTelemetryTypes[telemetryIndex%len(repeatedTelemetryTypes)]
				telemetryIndex++
				select {
				case ch <- bridge.Event{Type: typeName, Severity: "warning", Source: "bridge_health"}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	for range out {
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not cancel while telemetry repeated")
	}
	origins, warns, _ := rec.snapshot()
	if len(warns) != 2 || warns[0] != "warning" || warns[1] != "urgent" {
		t.Fatalf("warns = %v, want [warning urgent] despite telemetry", warns)
	}
	if len(origins) != 1 || origins[0] != timeoutOriginIdleBridge {
		t.Fatalf("origins = %v, want [%s]", origins, timeoutOriginIdleBridge)
	}
}

var repeatedTelemetryTypes = []string{
	"stall", "steer", "compaction_start", "compaction_end", "agent_start", "agent_end", "turn_start", "turn_end",
}

func TestLivenessTimeout_BlockedNotifyDoesNotStopEventConsumption(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ch := make(chan bridge.Event, 4)
	started := make(chan struct{})
	release := make(chan struct{})
	out := livenessIdleTimeoutWrapper(ctx, ch,
		livenessPolicy{idle: 10 * time.Millisecond, urgentLead: time.Hour, cancelLead: time.Hour},
		func() {}, func(string) {}, livenessHooks{
			probe: func(context.Context) error { return nil },
			notify: func(string, time.Duration) {
				select {
				case <-started:
				default:
					close(started)
				}
				<-release
			},
		})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("notification was not dispatched")
	}
	ch <- bridge.Event{Type: "stall", Severity: "warning"}
	select {
	case ev := <-out:
		if ev.Type != "stall" {
			t.Fatalf("event type = %q, want stall", ev.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked notification stopped event consumption")
	}
	close(release)
	stop()
	select {
	case _, ok := <-out:
		if ok {
			for range out {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wrapper did not close after cancellation")
	}
}

// TestLivenessTimeout_ProductiveEventDuringProbeDoesNotEscalate is the
// regression test for the post-probe drain: a productive event queued while
// a probe is in flight, with the probe result ready at the same instant,
// must never escalate or cancel — whichever case the select picks first. Go
// select chooses randomly among ready cases, so the channel-synchronized
// scenario repeats: with the drain, every outcome resets the window and no
// warn/origin/cancel can ever fire (without the drain, each trial fails with
// probability 1/2, so 30 trials detect it with probability 1-2^-30).
func TestLivenessTimeout_ProductiveEventDuringProbeDoesNotEscalate(t *testing.T) {
	for trial := 0; trial < 30; trial++ {
		ctx, stop := context.WithCancel(context.Background())
		ch := make(chan bridge.Event, 4)
		rec := &livenessRecorder{}
		probeStarted := make(chan struct{}, 1)
		probeRelease := make(chan struct{})
		neverRelease := make(chan struct{})
		var probeCalls atomic.Int32
		canceled := make(chan struct{})
		out := livenessIdleTimeoutWrapper(ctx, ch,
			livenessPolicy{idle: 10 * time.Millisecond, urgentLead: time.Hour, cancelLead: time.Hour},
			func() { close(canceled) }, rec.addOrigin, livenessHooks{
				probe: func(pctx context.Context) error {
					if probeCalls.Add(1) == 1 {
						probeStarted <- struct{}{}
						<-probeRelease
						return nil
					}
					// Later probes (after the reset restarts the window)
					// must stay in flight: they must not escalate.
					select {
					case <-pctx.Done():
						return pctx.Err()
					case <-neverRelease:
						return nil
					}
				},
				warn: func(severity string, _ time.Duration) { rec.addWarn(severity) },
			})

		select {
		case <-probeStarted:
		case <-time.After(2 * time.Second):
			t.Fatalf("trial %d: probe never started", trial)
		}
		// Queue a productive event while the probe is in flight, then let the
		// probe result race the queued event.
		ch <- bridge.Event{Type: "result", Content: "done"}
		close(probeRelease)
		select {
		case ev := <-out:
			if ev.Type != "result" {
				t.Fatalf("trial %d: event type = %q, want result", trial, ev.Type)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("trial %d: productive event never forwarded", trial)
		}
		origins, warns, _ := rec.snapshot()
		if len(warns) != 0 || len(origins) != 0 {
			t.Fatalf("trial %d: warns=%v origins=%v, want none (productive event during probe)", trial, warns, origins)
		}
		select {
		case <-canceled:
			t.Fatalf("trial %d: watchdog canceled despite productive event during probe", trial)
		default:
		}
		stop()
		for range out {
		}
	}
}

// TestLivenessTimeout_NotifyWorkerExitsOnInputClose is the regression test
// for the notification worker lifetime: the worker must exit when the input
// channel closes without context cancellation (the watchdog exits via the
// channel-close path and closes notifyQueue, which ends the worker's range).
//
// Leak detection uses a process-wide goroutine count, which is stable in this
// window: Go runs t.Parallel() tests only after all sequential tests finish,
// so no other test goroutine is active here; runtime-internal goroutines are
// already present in the baseline taken before the wrapper starts.
func TestLivenessTimeout_NotifyWorkerExitsOnInputClose(t *testing.T) {
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan bridge.Event, 4)
	notifyCalled := make(chan struct{}, 1)
	out := livenessIdleTimeoutWrapper(ctx, ch,
		livenessPolicy{idle: 10 * time.Millisecond, urgentLead: time.Hour, cancelLead: time.Hour},
		func() {}, func(string) {}, livenessHooks{
			probe: func(context.Context) error { return nil },
			notify: func(string, time.Duration) {
				select {
				case notifyCalled <- struct{}{}:
				default:
				}
			},
		})

	select {
	case <-notifyCalled:
	case <-time.After(2 * time.Second):
		t.Fatal("notification worker never ran")
	}
	// Close the input WITHOUT canceling ctx: the watchdog must exit and the
	// worker with it (no goroutine leak).
	close(ch)
	select {
	case _, ok := <-out:
		if ok {
			for range out {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wrapper did not close after input close")
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			t.Fatal("notification worker goroutine leaked after input close")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestLivenessTimeout_BlockedNotifyTimesOutAndWorkerContinues is the
// regression test for the bounded notification: a wedged notify hook (never
// returns) must not pin the worker — after notifyTimeout the worker moves on
// and still processes the next escalation, and the watchdog still reaches
// the safety cancel.
func TestLivenessTimeout_BlockedNotifyTimesOutAndWorkerContinues(t *testing.T) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	ch := make(chan bridge.Event, 4)
	release := make(chan struct{})
	notifyCalls := make(chan string, 4)
	canceled := make(chan struct{})
	policy := livenessPolicy{idle: 10 * time.Millisecond, urgentLead: 10 * time.Millisecond, cancelLead: 50 * time.Millisecond, notifyTimeout: 20 * time.Millisecond}
	out := livenessIdleTimeoutWrapper(ctx, ch, policy,
		func() { close(canceled) }, func(string) {}, livenessHooks{
			probe: func(context.Context) error { return nil },
			notify: func(severity string, _ time.Duration) {
				notifyCalls <- severity
				<-release // wedged transport: never returns until cleanup
			},
		})

	// warning dispatch ~10ms blocks the worker; the timeout (~30ms) must
	// free it to process the urgent dispatch (~20ms).
	for want := 1; want <= 2; want++ {
		select {
		case <-notifyCalls:
		case <-time.After(2 * time.Second):
			t.Fatalf("worker did not process notification %d (blocked notify must time out)", want)
		}
	}
	// The watchdog must still reach the safety cancel.
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog never canceled while notifications blocked")
	}
	close(release)
	for range out {
	}
}

// TestLivenessEventIsProductive_RequiresRealProgress fixes the productive
// classification: terminal events (result/error) always count; tool_use needs
// a name; tool_result needs content or a call id; assistant needs text —
// empty tool events from a wedged bridge must not reset the idle window.
func TestLivenessEventIsProductive_RequiresRealProgress(t *testing.T) {
	cases := []struct {
		name string
		ev   bridge.Event
		want bool
	}{
		{"result", bridge.Event{Type: "result", Content: "done"}, true},
		{"error", bridge.Event{Type: "error", Message: "boom"}, true},
		{"tool_use with name", bridge.Event{Type: "tool_use", Name: "Read"}, true},
		{"tool_use empty", bridge.Event{Type: "tool_use"}, false},
		{"tool_result with content", bridge.Event{Type: "tool_result", Content: "data"}, true},
		{"tool_result with call id", bridge.Event{Type: "tool_result", ToolCallID: "t1"}, true},
		{"tool_result empty", bridge.Event{Type: "tool_result"}, false},
		{"assistant with text", bridge.Event{Type: "assistant", Text: "hi"}, true},
		{"assistant empty", bridge.Event{Type: "assistant"}, false},
		{"telemetry", bridge.Event{Type: "stall", Severity: "warning"}, false},
	}
	for _, tc := range cases {
		if got := livenessEventIsProductive(tc.ev); got != tc.want {
			t.Errorf("%s: livenessEventIsProductive = %v, want %v", tc.name, got, tc.want)
		}
	}
}
