package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/ipc"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
)

func handleTUIHistory(ctx context.Context, a *app, msg ipc.IPCMessage, emit func(ipc.IPCEvent) error) error {
	history, err := loadTUIHistory(ctx, a, msg.ChatID, int(msg.ThreadID), msg.UserID)
	if err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: err.Error(), RequestID: msg.RequestID})
	}
	body, err := json.Marshal(history)
	if err != nil {
		return emit(ipc.IPCEvent{Type: ipc.EventTypeError, Error: fmt.Sprintf("marshal history: %s", err), RequestID: msg.RequestID})
	}
	if err := emit(ipc.IPCEvent{Type: ipc.EventTypeHistory, Body: string(body), RequestID: msg.RequestID}); err != nil {
		return err
	}
	return emit(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd, Done: true, RequestID: msg.RequestID})
}

func loadTUIHistory(ctx context.Context, a *app, chatID int64, threadID int, userID int64) ([]bridge.SessionHistoryMessage, error) {
	if a == nil || a.bridge == nil || a.sessions == nil {
		return []bridge.SessionHistoryMessage{}, nil
	}
	sessionFile := a.sessions.GetSession(chatID, threadID, userID)
	if sessionFile == "" {
		return []bridge.SessionHistoryMessage{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	opts := bridge.RequestOptions{
		Resume:   sessionFile,
		ChatID:   chatID,
		ThreadID: threadID,
		UserID:   userID,
		Cwd:      effectiveTUICwd(ctx, a, chatID, threadID),
	}
	if a.config != nil && !a.config.IsModelAuto() {
		opts.Provider = a.config.DefaultProvider
		opts.Model = a.config.DefaultModel
	}
	return a.bridge.GetSessionHistory(ctx, opts)
}

func effectiveTUICwd(ctx context.Context, a *app, chatID int64, threadID int) string {
	if a == nil {
		return ""
	}
	if a.bindings != nil {
		resolved, err := a.bindings.Resolve(ctx, projectbinding.ConversationKey{ChatID: chatID, ThreadID: threadID})
		if err == nil && resolved != nil && resolved.Binding != nil {
			return resolved.Binding.CWD
		}
	}
	if a.resolver != nil {
		return a.resolver.Root()
	}
	return ""
}
