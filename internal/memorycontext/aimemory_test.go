package memorycontext

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIMemoryProviderReadsBoundedBriefing(t *testing.T) {
	server, briefingCalls := newMCPTestServer(t, false)
	defer server.Close()

	provider := NewAIMemoryProvider(AIMemoryOptions{
		BaseURL:     server.URL,
		Token:       "test-token",
		MaxChars:    500,
		RecentPages: 2,
	})
	scope := Scope{Workspace: "personal", Project: "aurelia", CWD: "/project"}
	ws, err := provider.WorkingSet(context.Background(), scope)
	if err != nil {
		t.Fatalf("WorkingSet: %v", err)
	}
	if !strings.Contains(ws.Content, "pending handoff: 1") {
		t.Fatalf("working set missing handoff count: %s", ws.Content)
	}
	if !strings.Contains(ws.Content, "rule one") || !strings.Contains(ws.Content, "Decision A") {
		t.Fatalf("working set missing rules/pages: %s", ws.Content)
	}
	if !strings.Contains(ws.Content, "Body for decisions/a.md") {
		t.Fatalf("working set missing page excerpt: %s", ws.Content)
	}
	if ws.Chars > 500 {
		t.Fatalf("working set chars = %d, want <= 500", ws.Chars)
	}
	if ws.PendingHandoffs != 1 {
		t.Fatalf("PendingHandoffs = %d, want 1", ws.PendingHandoffs)
	}
	if len(ws.Sources) != 2 {
		t.Fatalf("Sources = %v, want two pages", ws.Sources)
	}
	if *briefingCalls == 0 {
		t.Fatal("memory_briefing was not called")
	}

	status, err := provider.Status(context.Background(), scope)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Available || status.Pages != 3 || status.Observations != 7 || status.Sessions != 2 || status.PendingHandoffs != 1 {
		t.Fatalf("Status = %+v", status)
	}
}

func TestAIMemoryProviderParsesSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var result any
		if req.Method == "initialize" {
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "test"}}
		} else {
			result = map[string]any{"content": []map[string]any{{"type": "text", "text": `{"counts":{"pages_latest":1},"pending_handoff_count":0,"rules":[],"slots":[],"recent_pages":[]}`}}}
		}
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", payload)
	}))
	defer server.Close()

	provider := NewAIMemoryProvider(AIMemoryOptions{BaseURL: server.URL, Token: "test-token", RecentPages: -1})
	ws, err := provider.WorkingSet(context.Background(), Scope{Workspace: "personal", Project: "aurelia", CWD: "/project"})
	if err != nil {
		t.Fatalf("WorkingSet: %v", err)
	}
	if !strings.Contains(ws.Content, "Pages: 1") {
		t.Fatalf("SSE working set = %q", ws.Content)
	}
}

func TestAIMemoryProviderReturnsErrorForUnavailableServer(t *testing.T) {
	server, _ := newMCPTestServer(t, true)
	defer server.Close()
	provider := NewAIMemoryProvider(AIMemoryOptions{BaseURL: server.URL, Token: "test-token", RecentPages: -1})
	_, err := provider.WorkingSet(context.Background(), Scope{Workspace: "personal", Project: "aurelia", CWD: "/project"})
	if err == nil {
		t.Fatal("expected ai-memory error")
	}
}

func TestAIMemoryProviderReadsTokenFile(t *testing.T) {
	server, _ := newMCPTestServer(t, false)
	defer server.Close()
	tokenPath := filepath.Join(t.TempDir(), "auth-token")
	if err := os.WriteFile(tokenPath, []byte("test-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_MEMORY_AUTH_TOKEN", "")
	provider := NewAIMemoryProvider(AIMemoryOptions{BaseURL: server.URL, TokenPath: tokenPath, RecentPages: -1})
	ws, err := provider.WorkingSet(context.Background(), Scope{Workspace: "personal", Project: "aurelia", CWD: "/project"})
	if err != nil {
		t.Fatalf("WorkingSet: %v", err)
	}
	if !strings.Contains(ws.Content, "Pages: 3") {
		t.Fatalf("working set = %q", ws.Content)
	}
}

func TestAIMemoryProviderDoesNotCallWithoutProject(t *testing.T) {
	provider := NewAIMemoryProvider(AIMemoryOptions{BaseURL: "http://127.0.0.1:1", Token: "test-token"})
	ws, err := provider.WorkingSet(context.Background(), Scope{CWD: "/unmarked"})
	if err != nil || ws.Content != "" {
		t.Fatalf("WorkingSet = %+v, %v", ws, err)
	}
}

func newMCPTestServer(t *testing.T, failTools bool) (*httptest.Server, *int) {
	t.Helper()
	briefingCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if req.Method == "initialize" {
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "test"}})
			return
		}
		if req.Method != "tools/call" || failTools {
			http.Error(w, "tool unavailable", http.StatusInternalServerError)
			return
		}
		var call struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &call); err != nil {
			http.Error(w, "bad params", http.StatusBadRequest)
			return
		}
		switch call.Name {
		case "memory_briefing":
			briefingCalls++
			text := `{"counts":{"pages_latest":3,"observations":7,"sessions":2},"pending_handoff_count":1,"rules":["rule one"],"slots":[],"recent_pages":[{"path":"decisions/a.md","title":"Decision A"},{"path":"gotchas/b.md","title":"Gotcha B"}]}`
			writeRPCResult(t, w, req.ID, toolContent(text))
		case "memory_read_page":
			path, _ := call.Arguments["path"].(string)
			writeRPCResult(t, w, req.ID, toolContent("# Page\n\nBody for "+path))
		case "memory_status":
			writeRPCResult(t, w, req.ID, toolContent("status text"))
		default:
			http.Error(w, "unknown tool", http.StatusBadRequest)
		}
	}))
	return server, &briefingCalls
}

func toolContent(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

func writeRPCResult(t *testing.T, w http.ResponseWriter, id json.RawMessage, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func TestAIMemoryProviderCancelsOpenHandoffs(t *testing.T) {
	var canceled []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if req.Method == "initialize" {
			writeRPCResult(t, w, req.ID, map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{}, "serverInfo": map[string]any{"name": "test"}})
			return
		}
		var call struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &call); err != nil {
			http.Error(w, "bad params", http.StatusBadRequest)
			return
		}
		switch call.Name {
		case "memory_handoff_list":
			writeRPCResult(t, w, req.ID, toolContent(`{"handoffs":[{"id":"h1"},{"id":"h2"}]}`))
		case "memory_handoff_cancel":
			id, _ := call.Arguments["handoff_id"].(string)
			canceled = append(canceled, id)
			writeRPCResult(t, w, req.ID, toolContent(`{}`))
		default:
			http.Error(w, "unknown tool", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	provider := NewAIMemoryProvider(AIMemoryOptions{BaseURL: server.URL, Token: "test-token"})
	scope := Scope{Workspace: "personal", Project: "aurelia"}
	if err := provider.CancelHandoffs(context.Background(), scope); err != nil {
		t.Fatalf("CancelHandoffs: %v", err)
	}
	if len(canceled) != 2 || canceled[0] != "h1" || canceled[1] != "h2" {
		t.Fatalf("canceled = %v, want [h1 h2]", canceled)
	}
}
