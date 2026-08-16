package bridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// longLivedMockJS returns JavaScript that acts as a long-lived bridge process:
// reads multiple lines from stdin, responds to each with events including request_id.
const longLivedMockJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    let req;
    try {
        req = JSON.parse(line);
    } catch(e) {
        process.stdout.write(JSON.stringify({event:"error",message:"invalid json"}) + "\n");
        return;
    }

    const rid = req.request_id || "";

    if (req.command === "ping") {
        process.stdout.write(JSON.stringify({event:"pong",request_id:rid}) + "\n");
    } else if (req.command === "query") {
        process.stdout.write(JSON.stringify({event:"system",request_id:rid,session_id:"test-123",session_file:"/tmp/test-session.jsonl",tools:["Read"],model:"claude-3"}) + "\n");
        process.stdout.write(JSON.stringify({event:"assistant",request_id:rid,text:"hello world"}) + "\n");
        process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:"done",cost_usd:0.01,session_id:"test-123",session_file:"/tmp/test-session.jsonl",duration_ms:100,num_turns:1}) + "\n");
    } else {
        process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"unknown command: " + req.command}) + "\n");
    }
});

rl.on('close', () => {
    process.exit(0);
});
`

// newMockBridge creates a long-lived Bridge that uses `node mock.js`.
func newMockBridge(t *testing.T, dir string, jsBody string) *Bridge {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "mock.js"), []byte(jsBody), 0644); err != nil {
		t.Fatal(err)
	}
	b := New(dir, "")
	b.command = "node"
	b.args = []string{"mock.js"}
	t.Cleanup(func() { b.Stop() })
	return b
}

func TestBridge_Execute_ParsesEvents(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, longLivedMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "test"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var events []Event
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(events), events)
	}

	// system event
	if events[0].Type != "system" {
		t.Errorf("event[0].Type = %q, want %q", events[0].Type, "system")
	}
	if events[0].SessionID != "test-123" {
		t.Errorf("event[0].SessionID = %q, want %q", events[0].SessionID, "test-123")
	}
	if events[0].SessionFile != "/tmp/test-session.jsonl" {
		t.Errorf("event[0].SessionFile = %q, want %q", events[0].SessionFile, "/tmp/test-session.jsonl")
	}
	if len(events[0].Tools) != 1 || events[0].Tools[0] != "Read" {
		t.Errorf("event[0].Tools = %v, want [Read]", events[0].Tools)
	}
	if events[0].Model != "claude-3" {
		t.Errorf("event[0].Model = %q, want %q", events[0].Model, "claude-3")
	}

	// assistant event
	if events[1].Type != "assistant" {
		t.Errorf("event[1].Type = %q, want %q", events[1].Type, "assistant")
	}
	if events[1].Text != "hello world" {
		t.Errorf("event[1].Text = %q, want %q", events[1].Text, "hello world")
	}

	// result event
	if events[2].Type != "result" {
		t.Errorf("event[2].Type = %q, want %q", events[2].Type, "result")
	}
	if events[2].Content != "done" {
		t.Errorf("event[2].Content = %q, want %q", events[2].Content, "done")
	}
	if events[2].CostUSD != 0.01 {
		t.Errorf("event[2].CostUSD = %f, want %f", events[2].CostUSD, 0.01)
	}
	if events[2].NumTurns != 1 {
		t.Errorf("event[2].NumTurns = %d, want %d", events[2].NumTurns, 1)
	}
}

func TestBridge_ExecuteSync_ReturnsResult(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, longLivedMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ev, err := b.ExecuteSync(ctx, Request{Command: "query", Prompt: "test"})
	if err != nil {
		t.Fatalf("ExecuteSync() error: %v", err)
	}
	if ev.Type != "result" {
		t.Errorf("Type = %q, want %q", ev.Type, "result")
	}
	if ev.Content != "done" {
		t.Errorf("Content = %q, want %q", ev.Content, "done")
	}
	if ev.CostUSD != 0.01 {
		t.Errorf("CostUSD = %f, want %f", ev.CostUSD, 0.01)
	}
}

func TestBridge_ListModels_PropagatesRefreshFlag(t *testing.T) {
	dir := t.TempDir()

	// The mock fails unless it sees refresh=true, so a regression that drops
	// the Refresh field or sends false will surface as a test failure.
	listModelsMockJS := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    if (req.command === "list-models") {
        if (!req.refresh) {
            process.stdout.write(JSON.stringify({event:"error", request_id:rid, message:"expected refresh=true"}) + "\n");
            return;
        }
        const models = [{provider:"test", id:"model", name:"Model", supportsImages:false}];
        process.stdout.write(JSON.stringify({event:"result", request_id:rid, content:JSON.stringify(models)}) + "\n");
    } else {
        process.stdout.write(JSON.stringify({event:"error", request_id:rid, message:"unknown command"}) + "\n");
    }
});
rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, listModelsMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := b.ListModels(ctx, true)
	if err != nil {
		t.Fatalf("ListModels() error: %v", err)
	}
	if len(models) != 1 || models[0].Provider != "test" || models[0].ID != "model" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestBridge_Execute_ErrorEvent(t *testing.T) {
	dir := t.TempDir()

	errorMockJS := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"something went wrong"}) + "\n");
});
rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, errorMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "test"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var events []Event
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "error" {
		t.Errorf("Type = %q, want %q", events[0].Type, "error")
	}
	if events[0].Message != "something went wrong" {
		t.Errorf("Message = %q, want %q", events[0].Message, "something went wrong")
	}
}

func TestBridge_Execute_ContextCancel(t *testing.T) {
	dir := t.TempDir()

	// Script that reads requests but never responds — simulates a hanging query.
	hangMockJS := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', () => {
    // Intentionally don't respond — simulate hang.
});
rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, hangMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "test"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for channel to close after context cancel")
	case _, ok := <-ch:
		if ok {
			for range ch {
			}
		}
	}
}

func TestBridge_Execute_DuplicateRequestIDReleasesReservedStream(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', () => {});
rl.on('close', () => process.exit(0));
`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, err := b.Execute(ctx, Request{Command: "query", Prompt: "first", RequestID: "duplicate-1"})
	if err != nil {
		t.Fatalf("first Execute() error: %v", err)
	}
	if _, err := b.Execute(ctx, Request{Command: "query", Prompt: "second", RequestID: "duplicate-1"}); err == nil {
		t.Fatal("duplicate Execute() unexpectedly succeeded")
	}

	b.mu.Lock()
	budget := b.streamBudget
	b.mu.Unlock()
	budget.mu.Lock()
	active := budget.active
	budget.mu.Unlock()
	if active != 1 {
		t.Fatalf("active streams after duplicate request = %d, want 1", active)
	}

	cancel()
	for range first {
	}
}

const cancelRequestMockJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
const active = new Set();
rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    if (req.command === "query") {
        active.add(rid);
        process.stdout.write(JSON.stringify({event:"system",request_id:rid,session_id:"sess",session_file:"/tmp/sess.jsonl"}) + "\n");
        return;
    }
    if (req.command === "cancel") {
        const target = req.target_request_id || "";
        active.delete(target);
        process.stdout.write(JSON.stringify({event:"error",request_id:target,message:"request canceled"}) + "\n");
        process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:"canceled " + target}) + "\n");
        return;
    }
});
rl.on('close', () => process.exit(0));
`

func TestBridge_CancelRequest(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, cancelRequestMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := b.Execute(ctx, Request{Command: "query", RequestID: "query-1", Prompt: "test"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if ev := <-ch; ev.Type != "system" {
		t.Fatalf("first event = %q, want system", ev.Type)
	}
	if err := b.CancelRequest(ctx, "query-1"); err != nil {
		t.Fatalf("CancelRequest() error: %v", err)
	}

	ev := <-ch
	if ev.Type != "error" || ev.Message != "request canceled" {
		t.Fatalf("cancel event = %+v, want request canceled error", ev)
	}
}

func TestBridge_CancelRequest_AllowedAtOrdinaryStreamCap(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, cancelRequestMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	streams := make([]<-chan Event, 0, maxActiveRequestStreams)
	for i := 0; i < maxActiveRequestStreams; i++ {
		requestID := fmt.Sprintf("cap-%d", i)
		ch, err := b.Execute(ctx, Request{Command: "query", RequestID: requestID, Prompt: "hold"})
		if err != nil {
			t.Fatalf("Execute(%s) error: %v", requestID, err)
		}
		if ev := <-ch; ev.Type != "system" {
			t.Fatalf("first event for %s = %q, want system", requestID, ev.Type)
		}
		streams = append(streams, ch)
	}

	if err := b.CancelRequest(ctx, "cap-0"); err != nil {
		t.Fatalf("CancelRequest at ordinary stream cap failed: %v", err)
	}
	for range streams[0] {
	}
}

func TestBridge_Ping(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, longLivedMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := b.Ping(ctx); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestBridge_Execute_ToolUseEvent(t *testing.T) {
	dir := t.TempDir()

	toolMockJS := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    process.stdout.write(JSON.stringify({event:"system",request_id:rid,session_id:"s1"}) + "\n");
    process.stdout.write(JSON.stringify({event:"tool_use",request_id:rid,name:"Read",input:{file_path:"/tmp/test.txt"}}) + "\n");
    process.stdout.write(JSON.stringify({event:"tool_result",request_id:rid,content:"file contents here"}) + "\n");
    process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:"done"}) + "\n");
});
rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, toolMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "test"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var events []Event
	for ev := range ch {
		events = append(events, ev)
	}

	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	if events[1].Type != "tool_use" {
		t.Errorf("event[1].Type = %q, want %q", events[1].Type, "tool_use")
	}
	if events[1].Name != "Read" {
		t.Errorf("event[1].Name = %q, want %q", events[1].Name, "Read")
	}
	if events[2].Type != "tool_result" {
		t.Errorf("event[2].Type = %q, want %q", events[2].Type, "tool_result")
	}
	if events[2].Content != "file contents here" {
		t.Errorf("event[2].Content = %q, want %q", events[2].Content, "file contents here")
	}
}

func TestEvent_IsTerminal(t *testing.T) {
	tests := []struct {
		eventType string
		want      bool
	}{
		{"result", true},
		{"error", true},
		{"system", false},
		{"assistant", false},
		{"tool_use", false},
		{"tool_result", false},
		{"pong", true},
	}
	for _, tt := range tests {
		ev := Event{Type: tt.eventType}
		if got := ev.IsTerminal(); got != tt.want {
			t.Errorf("Event{Type:%q}.IsTerminal() = %v, want %v", tt.eventType, got, tt.want)
		}
	}
}

func TestBridge_ExecuteSync_ErrorEvent(t *testing.T) {
	dir := t.TempDir()

	errorMockJS := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"auth failed"}) + "\n");
});
rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, errorMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ev, err := b.ExecuteSync(ctx, Request{Command: "query", Prompt: "test"})
	if err != nil {
		t.Fatalf("ExecuteSync() error: %v", err)
	}
	if ev.Type != "error" {
		t.Errorf("Type = %q, want %q", ev.Type, "error")
	}
	if ev.Message != "auth failed" {
		t.Errorf("Message = %q, want %q", ev.Message, "auth failed")
	}
}

func TestBridge_LongLived_MultipleRequests(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, longLivedMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Request 1: ping
	if err := b.Ping(ctx); err != nil {
		t.Fatalf("Ping 1 error: %v", err)
	}

	// Request 2: query
	ev, err := b.ExecuteSync(ctx, Request{Command: "query", Prompt: "first"})
	if err != nil {
		t.Fatalf("Query 1 error: %v", err)
	}
	if ev.Type != "result" {
		t.Errorf("Query 1: Type = %q, want %q", ev.Type, "result")
	}

	// Request 3: another query on the SAME process
	ev, err = b.ExecuteSync(ctx, Request{Command: "query", Prompt: "second"})
	if err != nil {
		t.Fatalf("Query 2 error: %v", err)
	}
	if ev.Type != "result" {
		t.Errorf("Query 2: Type = %q, want %q", ev.Type, "result")
	}

	// Request 4: ping again
	if err := b.Ping(ctx); err != nil {
		t.Fatalf("Ping 2 error: %v", err)
	}
}

func TestAllowlistEnv_FiltersCorrectly(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("HOME", "/home/test")
	t.Setenv("USER", "testuser")
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("TMPDIR", "/tmp")
	t.Setenv("PI_CODING_AGENT_DIR", "/home/test/.aurelia/pi-agent")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-anthropic")
	t.Setenv("TELEGRAM_BOT_TOKEN", "should-not-leak-12345")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("MY_RANDOM_SECRET", "super-secret")

	env := AllowlistEnv("ANTHROPIC_API_KEY")

	got := make(map[string]string)
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		got[k] = v
	}

	// Essential vars present.
	if got["PATH"] != "/usr/bin" {
		t.Errorf("PATH = %q, want /usr/bin", got["PATH"])
	}
	if got["HOME"] != "/home/test" {
		t.Errorf("HOME = %q, want /home/test", got["HOME"])
	}
	if got["USER"] != "testuser" {
		t.Errorf("USER = %q, want testuser", got["USER"])
	}
	if got["SHELL"] != "/bin/zsh" {
		t.Errorf("SHELL = %q, want /bin/zsh", got["SHELL"])
	}
	if got["TMPDIR"] != "/tmp" {
		t.Errorf("TMPDIR = %q, want /tmp", got["TMPDIR"])
	}
	if got["PI_CODING_AGENT_DIR"] != "/home/test/.aurelia/pi-agent" {
		t.Errorf("PI_CODING_AGENT_DIR = %q", got["PI_CODING_AGENT_DIR"])
	}

	// Provider key present.
	if got["ANTHROPIC_API_KEY"] != "sk-test-anthropic" {
		t.Errorf("ANTHROPIC_API_KEY = %q", got["ANTHROPIC_API_KEY"])
	}

	// Locale var present.
	if got["LC_ALL"] != "en_US.UTF-8" {
		t.Errorf("LC_ALL = %q", got["LC_ALL"])
	}

	// Telegram token NOT present.
	if _, ok := got["TELEGRAM_BOT_TOKEN"]; ok {
		t.Error("TELEGRAM_BOT_TOKEN found in allowlist, should be excluded")
	}

	// Arbitrary secret NOT present.
	if _, ok := got["MY_RANDOM_SECRET"]; ok {
		t.Error("MY_RANDOM_SECRET found in allowlist, should be excluded")
	}
}

func TestBridge_SetEnvAllowlist_AppliedToCmd(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, longLivedMockJS)

	allowlist := []string{"PATH=/usr/bin", "HOME=/home/test"}
	b.SetEnvAllowlist(allowlist)

	if err := b.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer b.Stop()

	b.mu.Lock()
	cmd := b.cmd
	b.mu.Unlock()

	if cmd == nil {
		t.Fatal("cmd is nil after Start")
	}
	if cmd.Env == nil {
		t.Fatal("cmd.Env is nil — allowlist not applied")
	}
	if len(cmd.Env) != len(allowlist) {
		t.Fatalf("cmd.Env length = %d, want %d: %v", len(cmd.Env), len(allowlist), cmd.Env)
	}
	for i, e := range cmd.Env {
		if e != allowlist[i] {
			t.Fatalf("cmd.Env[%d] = %q, want %q", i, e, allowlist[i])
		}
	}
}

func TestBridge_SetEnvAllowlist_NoAllowlistInheritsEnv(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, longLivedMockJS)

	// Don't call SetEnvAllowlist — should inherit os.Environ().
	if err := b.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer b.Stop()

	b.mu.Lock()
	cmd := b.cmd
	b.mu.Unlock()

	if cmd == nil {
		t.Fatal("cmd is nil after Start")
	}
	// Without SetEnvAllowlist, cmd.Env should be nil (= inherit parent env).
	if cmd.Env != nil {
		t.Fatalf("cmd.Env = %v, want nil (inherit parent)", cmd.Env)
	}
}

func TestStopBeforeStart(t *testing.T) {
	b := New("/nonexistent", "")
	done := make(chan struct{})
	go func() {
		b.Stop()
		close(done)
	}()
	select {
	case <-done:
		// OK — Stop returned
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() blocked on bridge that was never started")
	}
}

func TestBridge_OnDeath_CalledOnCrash(t *testing.T) {
	dir := t.TempDir()

	// Script that exits immediately after first request — simulates crash.
	crashMockJS := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', () => {
    process.exit(1);
});
rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, crashMockJS)

	called := make(chan struct{})
	b.SetOnDeath(func() {
		close(called)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send a request — process will crash
	ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "crash"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Drain channel
	for range ch {
	}

	select {
	case <-called:
		// OK — callback was invoked
	case <-time.After(5 * time.Second):
		t.Fatal("OnDeath callback was not called after process crash")
	}
}

func TestBridge_OnDeath_NotCalledOnStop(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, longLivedMockJS)

	called := false
	b.SetOnDeath(func() {
		called = true
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Use bridge to ensure process is running
	if err := b.Ping(ctx); err != nil {
		t.Fatalf("Ping error: %v", err)
	}

	// Stop intentionally
	b.Stop()

	if called {
		t.Fatal("OnDeath callback should not be called on intentional Stop")
	}
}

func TestBridge_OnDeath_NilCallback(t *testing.T) {
	dir := t.TempDir()

	crashMockJS := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', () => {
    process.exit(1);
});
rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, crashMockJS)
	// No SetOnDeath — should not panic

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "crash"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	for range ch {
	}

	// Give readLoop time to complete
	time.Sleep(100 * time.Millisecond)
}

// floodMockJS sends count assistant events followed by a result event.
const floodMockJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    let req;
    try { req = JSON.parse(line); } catch(e) { return; }
    const rid = req.request_id || "";
    if (req.command === "query") {
        const count = parseInt(req.prompt) || 400;
        for (let i = 0; i < count; i++) {
            process.stdout.write(JSON.stringify({event:"assistant",request_id:rid,text:"msg " + i}) + "\n");
        }
        process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:"done"}) + "\n");
    }
});

rl.on('close', () => { process.exit(0); });
`

func TestRequestStream_BoundedOverflowDropsAndPreservesTerminal(t *testing.T) {
	s := newRequestStream(1)
	accepted := 1 + eventOverflowBuffer
	for i := 0; i < accepted; i++ {
		if !s.deliver(Event{Type: "assistant", Text: "buffered"}) {
			t.Fatalf("buffered event %d was dropped before capacity", i)
		}
	}

	dropped := 0
	for i := 0; i < 50; i++ {
		if !s.deliver(Event{Type: "assistant", Text: "overflow"}) {
			dropped++
		}
	}
	terminalDropped, ok := s.deliverTerminal(Event{Type: "result", Content: "done"})
	if !ok {
		t.Fatal("terminal result was not preserved after overflow exhaustion")
	}
	dropped += terminalDropped
	if dropped != 51 {
		t.Fatalf("dropped events = %d, want 51 (50 overflow + 1 terminal eviction)", dropped)
	}

	seenTerminal := false
	for {
		select {
		case ev := <-s.ch:
			if ev.Type == "result" {
				seenTerminal = true
			}
		default:
			for {
				ev, has := s.dequeueOverflow()
				if !has {
					break
				}
				if ev.Type == "result" {
					seenTerminal = true
				}
			}
			if !seenTerminal {
				t.Fatal("terminal result was lost from the bounded stream")
			}
			s.close()
			return
		}
	}
}

func TestBridge_OverflowBuffer_DeliveredInOrder(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, floodMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "300", RequestID: "flood-order"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var count int
	var gotResult bool
	for ev := range ch {
		if ev.Type == "result" {
			gotResult = true
			break
		}
		want := fmt.Sprintf("msg %d", count)
		if ev.Text != want {
			t.Fatalf("event %d text = %q, want %q", count, ev.Text, want)
		}
		count++
	}
	if count != 300 {
		t.Fatalf("received %d assistant events, want 300", count)
	}
	if !gotResult {
		t.Fatal("expected terminal result event")
	}
	if dropped := b.DroppedEvents(); dropped != 0 {
		t.Fatalf("unexpected drops: %d", dropped)
	}
}

func TestBridge_Stop_And_Restart(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "mock.js"), []byte(longLivedMockJS), 0644); err != nil {
		t.Fatal(err)
	}

	b := New(dir, "")
	b.command = "node"
	b.args = []string{"mock.js"}
	t.Cleanup(func() { b.Stop() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start and use
	if err := b.Ping(ctx); err != nil {
		t.Fatalf("Ping 1 error: %v", err)
	}

	// Stop
	b.Stop()

	// Use again — should auto-restart
	if err := b.Ping(ctx); err != nil {
		t.Fatalf("Ping after restart error: %v", err)
	}
}

func TestBridge_OnDeath_PanicCallback_DoesNotEscape(t *testing.T) {
	dir := t.TempDir()

	crashMockJS := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', () => {
    process.exit(1);
});
rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, crashMockJS)

	// Callback that panics — the recover in the goroutine wrapper must catch it.
	b.SetOnDeath(func() {
		panic("test onDeath panic")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "crash"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Drain channel — process crashes, channel gets closed by readLoop.
	for range ch {
	}

	// If we get here, the recover caught the panic and the daemon survived.
	// Give a brief moment for any deferred recovery to complete.
	time.Sleep(100 * time.Millisecond)
}

func TestBridge_ExecuteSync_DrainRecover(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, longLivedMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Normal flow: ExecuteSync receives a terminal event, spawns the drain
	// goroutine (with defer recover), and returns. This verifies the code
	// path compiles and runs without panicking.
	ev, err := b.ExecuteSync(ctx, Request{Command: "query", Prompt: "test"})
	if err != nil {
		t.Fatalf("ExecuteSync() error: %v", err)
	}
	if ev.Type != "result" {
		t.Errorf("Type = %q, want %q", ev.Type, "result")
	}
	if ev.Content != "done" {
		t.Errorf("Content = %q, want %q", ev.Content, "done")
	}
}

func TestBridge_CleanupAfterPanic_KillsProcess(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, longLivedMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := b.Ping(ctx); err != nil {
		t.Fatalf("Ping error: %v", err)
	}

	b.mu.Lock()
	cmd := b.cmd
	b.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		t.Fatal("process should be running before cleanupAfterPanic")
	}

	// Call cleanupAfterPanic directly — this simulates what happens when
	// readLoop panics and the deferred recover invokes cleanupAfterPanic.
	b.cleanupAfterPanic()

	// Process should be killed and reaped (Kill + Wait inside cleanupAfterPanic).
	// Note: ProcessState.Exited() returns false when killed by a signal
	// (SIGKILL), which is correct behavior. We verify Wait() was called by
	// checking ProcessState is non-nil.
	if cmd.ProcessState == nil {
		t.Fatal("ProcessState is nil — cleanupAfterPanic did not call Wait(); process may be a zombie")
	}
	t.Logf("ProcessState=%s (exit code=%d)", cmd.ProcessState, cmd.ProcessState.ExitCode())

	// Bridge state should be reset.
	b.mu.Lock()
	started := b.started
	alive := b.cmd != nil
	b.mu.Unlock()
	if started {
		t.Error("bridge should be marked as not started after cleanupAfterPanic")
	}
	if alive {
		t.Error("bridge cmd should be nil after cleanupAfterPanic")
	}

	// Wait for readLoop to finish (pipe broke after Kill; scanner will exit).
	select {
	case <-b.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for readLoop to finish after cleanupAfterPanic")
	}
}

const sessionStatsMockJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";

    if (req.command === "compact-session") {
        // Emit compaction events, then result. delta_tokens is negative
        // (effective reduction) and duration_measured=true with 0ms — the
        // authoritative presence marker survives even a zero duration.
        process.stdout.write(JSON.stringify({event:"compaction_start",request_id:rid,reason:"manual"}) + "\n");
        process.stdout.write(JSON.stringify({event:"compaction_end",request_id:rid,reason:"manual",tokens_before:150000,tokens_after:140000,delta_tokens:-10000,success:true,duration_measured:true,duration_ms:0}) + "\n");
        process.stdout.write(JSON.stringify({
            event: "result",
            request_id: rid,
            content: JSON.stringify({
                success: true,
                tokens_before: 150000,
                summary: "Session compacted successfully. Previous topics: ...",
                session_id: "abc123",
                session_file: "/tmp/sessions/test.jsonl",
            }),
        }) + "\n");
    } else if (req.command === "get-session-stats") {
        process.stdout.write(JSON.stringify({
            event: "result",
            request_id: rid,
            content: JSON.stringify({
                session_file: "/tmp/sessions/test.jsonl",
                session_id: "abc123",
                user_messages: 3,
                assistant_messages: 2,
                tool_calls: 1,
                tool_results: 1,
                total_messages: 5,
                input_tokens: 100,
                output_tokens: 50,
                cache_read_tokens: 10,
                cache_write_tokens: 20,
                total_tokens: 150,
                cost: 0.005,
                context_usage_pct: 45.5,
            }),
        }) + "\n");
    } else if (req.command === "get-session-history") {
        process.stdout.write(JSON.stringify({
            event: "result",
            request_id: rid,
            content: JSON.stringify([
                { sender: "Igor", text: "hello" },
                { sender: "Aurelia", text: "hi there" },
            ]),
        }) + "\n");
    } else if (req.command === "ping") {
        process.stdout.write(JSON.stringify({event:"pong",request_id:rid}) + "\n");
    } else {
        process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"unknown command: " + req.command}) + "\n");
    }
});

rl.on('close', () => process.exit(0));
`

const cancelCompactionMockJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
const active = new Set();
rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    if (req.command === "compact-session") {
        active.add(rid);
        process.stdout.write(JSON.stringify({event:"compaction_start",request_id:rid,reason:"manual"}) + "\n");
        return;
    }
    if (req.command === "cancel") {
        const target = req.target_request_id || "";
        active.delete(target);
        process.stdout.write(JSON.stringify({event:"error",request_id:target,message:"request canceled"}) + "\n");
        process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:"canceled"}) + "\n");
    }
});
rl.on('close', () => process.exit(0));
`

const malformedCompactionMockJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    if (req.command === "compact-session") {
        process.stdout.write(JSON.stringify({event:"compaction_start",request_id:rid,reason:"manual"}) + "\n");
        process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:"not-json"}) + "\n");
    }
});
rl.on('close', () => process.exit(0));
`

func TestBridge_GetSessionStats(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, sessionStatsMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := b.GetSessionStats(ctx, RequestOptions{
		Resume: "/tmp/sessions/test.jsonl",
	})
	if err != nil {
		t.Fatalf("GetSessionStats() error: %v", err)
	}
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.SessionFile != "/tmp/sessions/test.jsonl" {
		t.Fatalf("SessionFile = %q, want %q", stats.SessionFile, "/tmp/sessions/test.jsonl")
	}
	if stats.SessionID != "abc123" {
		t.Fatalf("SessionID = %q, want %q", stats.SessionID, "abc123")
	}
	if stats.InputTokens != 100 {
		t.Fatalf("InputTokens = %d, want 100", stats.InputTokens)
	}
	if stats.OutputTokens != 50 {
		t.Fatalf("OutputTokens = %d, want 50", stats.OutputTokens)
	}
	if stats.Cost != 0.005 {
		t.Fatalf("Cost = %f, want 0.005", stats.Cost)
	}
	if stats.UserMessages != 3 {
		t.Fatalf("UserMessages = %d, want 3", stats.UserMessages)
	}
	if stats.AssistantMessages != 2 {
		t.Fatalf("AssistantMessages = %d, want 2", stats.AssistantMessages)
	}
	if stats.TotalTokens != 150 {
		t.Fatalf("TotalTokens = %d, want 150", stats.TotalTokens)
	}
	if stats.ContextUsagePct != 45.5 {
		t.Fatalf("ContextUsagePct = %f, want 45.5", stats.ContextUsagePct)
	}
}

func TestBridge_GetSessionHistory(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, sessionStatsMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages, err := b.GetSessionHistory(ctx, RequestOptions{
		Resume: "/tmp/sessions/test.jsonl",
	})
	if err != nil {
		t.Fatalf("GetSessionHistory() error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 history messages, got %d", len(messages))
	}
	if messages[0].Sender != "Igor" || messages[0].Text != "hello" {
		t.Fatalf("unexpected first message: %#v", messages[0])
	}
	if messages[1].Sender != "Aurelia" || messages[1].Text != "hi there" {
		t.Fatalf("unexpected second message: %#v", messages[1])
	}
}

// truncatedHistoryMockJS returns a get-session-history payload that was cut
// mid-string — the failure mode of an oversized history truncated by the
// bridge sanitizer (sanitizeOutEvent's MAX_EVENT_TEXT_RUNES cap). The Go
// side must degrade to an empty history instead of a fatal parse error.
const truncatedHistoryMockJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    if (req.command === "get-session-history") {
        // Deliberately invalid: JSON string truncated mid-escape.
        process.stdout.write(JSON.stringify({
            event: "result",
            request_id: rid,
            content: "[{\"sender\":\"Igor\",\"text\":\"long message...\\u00",
        }) + "\n");
    } else {
        process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"unexpected command: " + req.command}) + "\n");
    }
});

rl.on('close', () => {
    process.exit(0);
});
`

func TestBridge_GetSessionHistory_TruncatedPayloadDegradesToEmpty(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, truncatedHistoryMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	messages, err := b.GetSessionHistory(ctx, RequestOptions{
		Resume: "/tmp/sessions/test.jsonl",
	})
	if err != nil {
		t.Fatalf("GetSessionHistory() should degrade, got error: %v", err)
	}
	if messages == nil {
		t.Fatal("expected non-nil empty history, got nil")
	}
	if len(messages) != 0 {
		t.Fatalf("expected empty history, got %d messages", len(messages))
	}
}

func TestBridge_GetSessionStats_Error(t *testing.T) {
	dir := t.TempDir()

	errorMock := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"session not found: /tmp/missing.jsonl"}) + "\n");
});

rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, errorMock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := b.GetSessionStats(ctx, RequestOptions{
		Resume: "/tmp/missing.jsonl",
	})
	if err == nil {
		t.Fatal("expected error for missing session")
	}
	if !strings.Contains(err.Error(), "session not found: /tmp/missing.jsonl") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBridge_GetSessionStats_EmptyContent(t *testing.T) {
	dir := t.TempDir()

	emptyMock := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:""}) + "\n");
});

rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, emptyMock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stats, err := b.GetSessionStats(ctx, RequestOptions{})
	if err != nil {
		t.Fatalf("GetSessionStats() error: %v", err)
	}
	if stats != nil {
		t.Fatal("expected nil stats for empty content")
	}
}

func TestBridge_CompactSession_Success(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, sessionStatsMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := b.CompactSession(ctx, RequestOptions{
		Resume: "/tmp/sessions/test.jsonl",
	})
	if err != nil {
		t.Fatalf("CompactSession() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.TokensBefore != 150000 {
		t.Fatalf("TokensBefore = %d, want 150000", result.TokensBefore)
	}
	if result.SessionID != "abc123" {
		t.Fatalf("SessionID = %q, want %q", result.SessionID, "abc123")
	}
	if result.SessionFile != "/tmp/sessions/test.jsonl" {
		t.Fatalf("SessionFile = %q", result.SessionFile)
	}
}

// TestBridge_CompactSessionWithEvents_StreamsIntermediateEvents verifies the
// event-aware path: bounded compaction_start/end events are delivered to the
// callback (including a measured 0ms duration), the terminal is handled
// exactly once, and the wrapper result matches CompactSession.
func TestBridge_CompactSessionWithEvents_StreamsIntermediateEvents(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, sessionStatsMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var got []CompactSessionEvent
	result, err := b.CompactSessionWithEvents(ctx, RequestOptions{
		Resume: "/tmp/sessions/test.jsonl",
	}, func(ev CompactSessionEvent) {
		got = append(got, ev)
	})
	if err != nil {
		t.Fatalf("CompactSessionWithEvents() error: %v", err)
	}
	if result == nil || !result.Success || result.TokensBefore != 150000 {
		t.Fatalf("unexpected result: %+v", result)
	}

	if len(got) != 2 {
		t.Fatalf("intermediate events = %d, want 2 (start + end)", len(got))
	}
	if got[0].Type != "compaction_start" || got[0].Reason != "manual" {
		t.Fatalf("compaction_start event = %+v, want type=compaction_start reason=manual", got[0])
	}
	end := got[1]
	if end.Type != "compaction_end" || !end.Success || end.Reason != "manual" {
		t.Fatalf("compaction_end event = %+v, want success manual end", end)
	}
	if end.TokensBefore != 150000 || end.TokensAfter == nil || *end.TokensAfter != 140000 ||
		end.DeltaTokens == nil || *end.DeltaTokens != -10000 {
		t.Fatalf("compaction_end tokens = %+v, want before=150000 after=140000 delta=-10000", end)
	}
	// duration_measured=true is the authoritative presence marker, present
	// even when the measured duration is 0ms.
	if !end.DurationMeasured || end.DurationMs != 0 {
		t.Fatalf("compaction_end duration = measured=%v ms=%d, want measured=true ms=0", end.DurationMeasured, end.DurationMs)
	}
}

func TestBridge_CompactSessionWithEvents_CancelReturnsContextErrorAndOneEnd(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, cancelCompactionMockJS)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan CompactSessionEvent, 4)
	resultCh := make(chan struct {
		result *CompactSessionResult
		err    error
	}, 1)
	go func() {
		result, err := b.CompactSessionWithEvents(ctx, RequestOptions{}, func(ev CompactSessionEvent) {
			events <- ev
		})
		resultCh <- struct {
			result *CompactSessionResult
			err    error
		}{result: result, err: err}
	}()

	select {
	case start := <-events:
		if start.Type != "compaction_start" || start.RequestID == "" {
			t.Fatalf("start = %+v, want validated request correlation", start)
		}
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("compaction did not emit start before cancellation")
	}

	select {
	case outcome := <-resultCh:
		if !errors.Is(outcome.err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", outcome.err)
		}
		if outcome.result != nil {
			t.Fatalf("result = %+v, want nil after cancellation", outcome.result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled compaction did not terminate")
	}

	close(events)
	var got []CompactSessionEvent
	// The callback channel was consumed once above; the remaining buffered item
	// is the synthesized terminal end event.
	for ev := range events {
		got = append(got, ev)
	}
	if len(got) != 1 || got[0].Type != "compaction_end" || got[0].Success || got[0].ErrorClass != "compaction_error" {
		t.Fatalf("events after cancellation = %+v, want exactly one bounded end", got)
	}
	if got[0].RequestID == "" {
		t.Fatal("compaction_end lost request_id correlation")
	}
}

func TestBridge_CompactSessionWithEvents_MalformedResultEmitsOneEnd(t *testing.T) {
	b := newMockBridge(t, t.TempDir(), malformedCompactionMockJS)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var events []CompactSessionEvent
	_, err := b.CompactSessionWithEvents(ctx, RequestOptions{}, func(ev CompactSessionEvent) {
		events = append(events, ev)
	})
	if err == nil || !strings.Contains(err.Error(), "compact-session parse") {
		t.Fatalf("error = %v, want parse error", err)
	}
	if len(events) != 2 || events[0].Type != "compaction_start" || events[1].Type != "compaction_end" || events[1].Success {
		t.Fatalf("events = %+v, want one start and one failed end", events)
	}
}

func TestBridge_CompactSessionWithEvents_ResultWithoutEndSynthesizesMatchingSuccess(t *testing.T) {
	dir := t.TempDir()
	resultWithoutEndMock := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });
rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    if (req.command !== "compact-session") return;
    process.stdout.write(JSON.stringify({event:"compaction_start",request_id:rid,reason:"manual"}) + "\n");
    process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:JSON.stringify({success:true,tokens_before:10})}) + "\n");
});
rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, resultWithoutEndMock)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var events []CompactSessionEvent
	result, err := b.CompactSessionWithEvents(ctx, RequestOptions{}, func(ev CompactSessionEvent) {
		events = append(events, ev)
	})
	if err != nil || result == nil || !result.Success {
		t.Fatalf("result = %+v, error = %v, want successful result", result, err)
	}
	if len(events) != 2 || events[0].Type != "compaction_start" || events[1].Type != "compaction_end" {
		t.Fatalf("events = %+v, want one start and one synthesized end", events)
	}
	if !events[1].Success || events[1].ErrorClass != "" {
		t.Fatalf("synthesized end = %+v, want success without error class", events[1])
	}
}

func TestBridge_RejectsOversizedSerializedRequestBeforeStart(t *testing.T) {
	b := New(t.TempDir(), "")
	_, err := b.Execute(context.Background(), Request{
		Command:   "query",
		RequestID: "oversized-1",
		Prompt:    strings.Repeat("x", maxBridgeRequestBytes),
	})
	if err == nil || !strings.Contains(err.Error(), "maximum serialized size") {
		t.Fatalf("error = %v, want serialized request size rejection", err)
	}
	b.mu.Lock()
	started := b.started
	b.mu.Unlock()
	if started {
		t.Fatal("oversized request started the bridge before validation")
	}
}

func TestBridge_CompactSession_Error(t *testing.T) {
	dir := t.TempDir()

	compactErrorMock := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"compaction failed: session not found"}) + "\n");
});

rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, compactErrorMock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := b.CompactSession(ctx, RequestOptions{
		Resume: "/tmp/missing.jsonl",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "compaction failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

const lifecycleEventMockJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";

    if (req.command === "query") {
        // Emit lifecycle events before the result
        process.stdout.write(JSON.stringify({event:"agent_start",request_id:rid}) + "\n");
        process.stdout.write(JSON.stringify({event:"turn_start",request_id:rid}) + "\n");
        process.stdout.write(JSON.stringify({event:"tool_use",request_id:rid,name:"Read",input:{path:"test.go"}}) + "\n");
        process.stdout.write(JSON.stringify({event:"tool_result",request_id:rid,content:"file contents"}) + "\n");
        process.stdout.write(JSON.stringify({event:"turn_end",request_id:rid}) + "\n");
        process.stdout.write(JSON.stringify({event:"agent_end",request_id:rid}) + "\n");
        process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:"done"}) + "\n");
    } else {
        process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"unknown command: " + req.command}) + "\n");
    }
});

rl.on('close', () => process.exit(0));
`

func TestBridge_LifecycleEvents(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, lifecycleEventMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := b.Execute(ctx, Request{Command: "query", Prompt: "test"})
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	var events []Event
	for ev := range ch {
		events = append(events, ev)
	}

	// Expected: agent_start, turn_start, tool_use, tool_result, turn_end, agent_end, result
	if len(events) != 7 {
		t.Fatalf("expected 7 events, got %d: %+v", len(events), events)
	}

	expectedTypes := []string{"agent_start", "turn_start", "tool_use", "tool_result", "turn_end", "agent_end", "result"}
	for i, et := range expectedTypes {
		if events[i].Type != et {
			t.Errorf("events[%d].Type = %q, want %q", i, events[i].Type, et)
		}
	}

	// Verify terminal event has expected content
	last := events[len(events)-1]
	if last.Type != "result" || last.Content != "done" {
		t.Errorf("last event = %+v, want result/done", last)
	}
}

func TestBridge_CompactSession_EmptyContent(t *testing.T) {
	dir := t.TempDir()

	emptyCompactMock := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:""}) + "\n");
});

rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, emptyCompactMock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := b.CompactSession(ctx, RequestOptions{})
	if err != nil {
		t.Fatalf("CompactSession() error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result for empty content")
	}
}

const rotateSessionMockJS = `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";

    if (req.command === "rotate-session") {
        process.stdout.write(JSON.stringify({
            event: "result",
            request_id: rid,
            content: JSON.stringify({
                success: true,
                old_session_file: "/tmp/sessions/old.jsonl",
                old_session_id: "old-abc-123",
                new_session_file: "/tmp/sessions/new.jsonl",
                new_session_id: "new-xyz-789",
                summary_length: 450,
                tokens_before: 180000,
            }),
        }) + "\n");
    } else if (req.command === "ping") {
        process.stdout.write(JSON.stringify({event:"pong",request_id:rid}) + "\n");
    } else {
        process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"unknown command: " + req.command}) + "\n");
    }
});

rl.on('close', () => process.exit(0));
`

func TestBridge_RotateSession_Success(t *testing.T) {
	dir := t.TempDir()
	b := newMockBridge(t, dir, rotateSessionMockJS)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := b.RotateSession(ctx, RequestOptions{
		Resume: "/tmp/sessions/old.jsonl",
	})
	if err != nil {
		t.Fatalf("RotateSession() error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.Success {
		t.Fatal("expected success")
	}
	if result.OldSessionFile != "/tmp/sessions/old.jsonl" {
		t.Fatalf("OldSessionFile = %q, want %q", result.OldSessionFile, "/tmp/sessions/old.jsonl")
	}
	if result.NewSessionFile != "/tmp/sessions/new.jsonl" {
		t.Fatalf("NewSessionFile = %q, want %q", result.NewSessionFile, "/tmp/sessions/new.jsonl")
	}
	if result.OldSessionID != "old-abc-123" {
		t.Fatalf("OldSessionID = %q, want %q", result.OldSessionID, "old-abc-123")
	}
	if result.NewSessionID != "new-xyz-789" {
		t.Fatalf("NewSessionID = %q, want %q", result.NewSessionID, "new-xyz-789")
	}
	if result.SummaryLength != 450 {
		t.Fatalf("SummaryLength = %d, want 450", result.SummaryLength)
	}
	if result.TokensBefore != 180000 {
		t.Fatalf("TokensBefore = %d, want 180000", result.TokensBefore)
	}
}

func TestBridge_RotateSession_Error(t *testing.T) {
	dir := t.TempDir()

	rotateErrorMock := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    process.stdout.write(JSON.stringify({event:"error",request_id:rid,message:"rotate failed: session not found"}) + "\n");
});

rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, rotateErrorMock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := b.RotateSession(ctx, RequestOptions{
		Resume: "/tmp/missing.jsonl",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "rotate failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBridge_RotateSession_EmptyContent(t *testing.T) {
	dir := t.TempDir()

	emptyRotateMock := `
const readline = require('readline');
const rl = readline.createInterface({ input: process.stdin, terminal: false });

rl.on('line', (line) => {
    const req = JSON.parse(line);
    const rid = req.request_id || "";
    process.stdout.write(JSON.stringify({event:"result",request_id:rid,content:""}) + "\n");
});

rl.on('close', () => process.exit(0));
`
	b := newMockBridge(t, dir, emptyRotateMock)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := b.RotateSession(ctx, RequestOptions{})
	if err != nil {
		t.Fatalf("RotateSession() error: %v", err)
	}
	if result != nil {
		t.Fatal("expected nil result for empty content")
	}
}
