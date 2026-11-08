package telegram

import (
	"testing"
)

// TestBotController_LazyGettersReturnStableInstance verifies that TokenGuard()
// returns the same pointer on repeated calls (lazy init is stable). This is the
// foundation for sharing state across Telegram and TUI pipeline instances.
func TestBotController_LazyGettersReturnStableInstance(t *testing.T) {
	bc := &BotController{}

	guard1 := bc.TokenGuard()
	guard2 := bc.TokenGuard()
	if guard1 != guard2 {
		t.Error("TokenGuard(): repeated calls returned different instances")
	}
	if guard1 == nil {
		t.Error("TokenGuard(): returned nil, expected lazy-created instance")
	}
}
