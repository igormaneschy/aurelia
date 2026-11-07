package memorycontext

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestAIMemoryProviderSmoke runs only when a real personal ai-memory MCP
// endpoint is provided. It is intentionally skipped in normal CI so tests
// never touch a user's memory server.
func TestAIMemoryProviderSmoke(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("AURELIA_AI_MEMORY_SMOKE_URL"))
	tokenPath := strings.TrimSpace(os.Getenv("AURELIA_AI_MEMORY_SMOKE_TOKEN_PATH"))
	if baseURL == "" || tokenPath == "" {
		t.Skip("set AURELIA_AI_MEMORY_SMOKE_URL and AURELIA_AI_MEMORY_SMOKE_TOKEN_PATH to run")
	}
	workspace := strings.TrimSpace(os.Getenv("AURELIA_AI_MEMORY_SMOKE_WORKSPACE"))
	if workspace == "" {
		workspace = "personal"
	}
	project := strings.TrimSpace(os.Getenv("AURELIA_AI_MEMORY_SMOKE_PROJECT"))
	if project == "" {
		project = "aurelia"
	}
	provider := NewAIMemoryProvider(AIMemoryOptions{
		BaseURL:     baseURL,
		TokenPath:   tokenPath,
		MaxChars:    2000,
		RecentPages: 1,
	})
	ws, err := provider.WorkingSet(context.Background(), Scope{
		Workspace: workspace,
		Project:   project,
		CWD:       strings.TrimSpace(os.Getenv("AURELIA_AI_MEMORY_SMOKE_CWD")),
	})
	if err != nil {
		t.Fatalf("WorkingSet: %v", err)
	}
	if ws.Content == "" {
		t.Fatal("smoke working set is empty")
	}
	t.Logf("smoke: chars=%d sources=%d pending_handoffs=%d degraded=%v", ws.Chars, len(ws.Sources), ws.PendingHandoffs, ws.Degraded)
}
