package bridge

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestEffectivePriority(t *testing.T) {
	if got := effectivePriority(Request{Command: "query"}); got != PriorityInteractive {
		t.Fatalf("default = %v, want interactive", got)
	}
	if got := effectivePriority(Request{
		Command: "query",
		Options: RequestOptions{Security: &SecurityContext{AgentName: "nudge"}},
	}); got != PriorityBackground {
		t.Fatalf("nudge = %v, want background", got)
	}
	if got := effectivePriority(Request{Command: "query", Priority: PriorityCron}); got != PriorityCron {
		t.Fatalf("explicit cron = %v", got)
	}
}

func TestRequestSlotTracker_BackgroundWaitsForInteractive(t *testing.T) {
	tracker := newRequestSlotTracker()
	ctx := context.Background()

	if err := tracker.acquire(ctx, PriorityInteractive); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := tracker.acquire(ctx, PriorityBackground); err != nil {
			t.Errorf("background acquire: %v", err)
		}
		tracker.release(PriorityBackground)
	}()

	select {
	case <-done:
		t.Fatal("background should wait while interactive is active")
	case <-time.After(50 * time.Millisecond):
	}

	tracker.release(PriorityInteractive)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background did not acquire after interactive released")
	}
}

func TestRequestSlotTracker_CronWaitsForInteractive(t *testing.T) {
	tracker := newRequestSlotTracker()
	ctx := context.Background()

	if err := tracker.acquire(ctx, PriorityInteractive); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := tracker.acquire(ctx, PriorityCron); err != nil {
			t.Errorf("cron acquire: %v", err)
			return
		}
		tracker.release(PriorityCron)
	}()

	time.Sleep(30 * time.Millisecond)
	tracker.release(PriorityInteractive)
	wg.Wait()
}

func TestRequestSlotTracker_BackgroundWaitsForCron(t *testing.T) {
	tracker := newRequestSlotTracker()
	ctx := context.Background()

	if err := tracker.acquire(ctx, PriorityCron); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = tracker.acquire(ctx, PriorityBackground)
		tracker.release(PriorityBackground)
	}()

	time.Sleep(30 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("background should wait while cron is active")
	default:
	}

	tracker.release(PriorityCron)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("background did not acquire after cron released")
	}
}

func TestBridge_PriorityQueue_BackgroundWaitsForInteractive(t *testing.T) {
	dir := t.TempDir()
	const slowJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', (line) => {
    let req;
    try { req = JSON.parse(line); } catch(e) { return; }
    const rid = req.request_id || "";
    if (req.command !== "query") return;
    const parts = (req.prompt || "").split(":");
    const tag = parts[0] || "done";
    const delay = parseInt(parts[1] || "0", 10) || 0;
    setTimeout(() => {
        process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:tag}) + "\n");
    }, delay);
});
`
	b := newMockBridge(t, dir, slowJS)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	interactiveDone := make(chan struct{})
	go func() {
		defer close(interactiveDone)
		ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "interactive:150"})
		if err != nil {
			t.Errorf("interactive Execute: %v", err)
			return
		}
		for range ch {
		}
	}()

	time.Sleep(20 * time.Millisecond)

	start := time.Now()
	ev, err := b.ExecuteSync(ctx, Request{
		Command:  "query",
		Prompt:   "background:10",
		Priority: PriorityBackground,
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("background ExecuteSync: %v", err)
	}
	if ev.Content != "background" {
		t.Fatalf("content = %q, want background", ev.Content)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("background started too early (%v), expected to wait for interactive", elapsed)
	}

	<-interactiveDone
}

func TestRequestSlotTracker_AcquireRespectsContextCancel(t *testing.T) {
	tracker := newRequestSlotTracker()
	if err := tracker.acquire(context.Background(), PriorityInteractive); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := tracker.acquire(ctx, PriorityCron); err == nil {
		t.Fatal("expected context error while waiting for slot")
		tracker.release(PriorityCron)
	}
	tracker.release(PriorityInteractive)
}