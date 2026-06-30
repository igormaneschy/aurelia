package telegram

import (
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/users"
)

// TestBotController wires a minimal BotController for handler tests (TUI, IPC).
func TestBotController(profileResolver *profiles.Resolver, userStore *users.Store) *BotController {
	bc := &BotController{
		profiles:  profileResolver,
		userStore: userStore,
	}
	if userStore != nil {
		bc.userResolver = userStore.Resolver()
	}
	return bc
}