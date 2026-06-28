package telegram

import (
	"testing"

	"github.com/igormaneschy/aurelia/internal/session"
)

// TestBotController_LazyGettersReturnStableInstance verifies that NudgeBuffer()
// and MemoryCache() return the same pointer on repeated calls (lazy init is
// stable). This is the foundation for sharing state across Telegram and TUI
// pipeline instances.
func TestBotController_LazyGettersReturnStableInstance(t *testing.T) {
	bc := &BotController{}

	buf1 := bc.NudgeBuffer()
	buf2 := bc.NudgeBuffer()
	if buf1 != buf2 {
		t.Error("NudgeBuffer(): repeated calls returned different instances")
	}
	if buf1 == nil {
		t.Error("NudgeBuffer(): returned nil, expected lazy-created instance")
	}

	cache1 := bc.MemoryCache()
	cache2 := bc.MemoryCache()
	if cache1 != cache2 {
		t.Error("MemoryCache(): repeated calls returned different instances")
	}
	if cache1 == nil {
		t.Error("MemoryCache(): returned nil, expected lazy-created instance")
	}
}

// TestBotController_DreamerReturnsNilWhenUnset verifies the nil-safe path:
// Dreamer() returns nil (not a panic) when no dreamer is configured.
func TestBotController_DreamerReturnsNilWhenUnset(t *testing.T) {
	bc := &BotController{}
	if d := bc.Dreamer(); d != nil {
		t.Errorf("Dreamer(): expected nil when unset, got %v", d)
	}
}

// TestBotController_NudgeBufferIsConcreteSessionType confirms the shared
// buffer is a real *session.NudgeBuffer, not a nil or wrapper.
func TestBotController_NudgeBufferIsConcreteSessionType(t *testing.T) {
	bc := &BotController{}
	buf := bc.NudgeBuffer()
	var _ *session.NudgeBuffer = buf
}
