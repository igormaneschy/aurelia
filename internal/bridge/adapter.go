package bridge

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/igormaneschy/aurelia/internal/engine"
)

// PIAdapter implements engine.Engine over the PI NDJSON bridge process.
type PIAdapter struct {
	b *Bridge
}

// NewPIAdapter wraps a Bridge as an engine.Engine.
func NewPIAdapter(b *Bridge) *PIAdapter {
	return &PIAdapter{b: b}
}

// Query streams a PI query request.
func (a *PIAdapter) Query(ctx context.Context, req engine.Request) (<-chan engine.Event, error) {
	brReq := toBridgeRequest(req)
	brReq.Command = "query"
	ch, err := a.b.Execute(ctx, brReq)
	if err != nil {
		return nil, err
	}
	return translateEventChannel(ch), nil
}

// Command executes a synchronous PI bridge command.
func (a *PIAdapter) Command(ctx context.Context, cmd engine.Command) (engine.Event, error) {
	brReq := toBridgeCommand(cmd)
	ev, err := a.b.ExecuteSync(ctx, brReq)
	if err != nil {
		return engine.Event{}, err
	}
	if ev == nil {
		return engine.Event{Type: engine.EventTypeError, RawType: "error", Err: fmt.Errorf("bridge: nil event")}, nil
	}
	return toEngineEvent(*ev), nil
}

// Stats returns session statistics for a session file key.
func (a *PIAdapter) Stats(ctx context.Context, sessionKey string, opts engine.StatsOptions) (engine.Stats, error) {
	ev, err := a.b.ExecuteSync(ctx, Request{
		Command: "get-session-stats",
		Options: RequestOptions{
			Resume:   sessionKey,
			ChatID:   opts.ChatID,
			ThreadID: opts.ThreadID,
			UserID:   opts.UserID,
		},
	})
	if err != nil {
		return engine.Stats{}, err
	}
	if ev == nil {
		return engine.Stats{}, nil
	}
	if ev.Type == "error" {
		msg := ev.Message
		if msg == "" {
			msg = ev.Content
		}
		return engine.Stats{}, fmt.Errorf("bridge: get-session-stats: %s", msg)
	}
	if ev.Content == "" {
		return engine.Stats{}, nil
	}
	var ss SessionStats
	if err := json.Unmarshal([]byte(ev.Content), &ss); err != nil {
		return engine.Stats{}, fmt.Errorf("bridge: get-session-stats parse: %w", err)
	}
	return toEngineStats(ss), nil
}

func toBridgeRequest(req engine.Request) Request {
	br := Request{
		Prompt:    req.Prompt,
		RequestID: req.RequestID,
		Options: RequestOptions{
			SystemPrompt:      req.SystemPrompt,
			Resume:            req.SessionKey,
			Provider:          req.Provider,
			Model:             req.Model,
			Cwd:               req.Cwd,
			AllowedTools:      req.AllowedTools,
			DisallowedTools:   req.DisallowedTools,
			Continue:          req.Continue,
			NoUserSettings:    req.NoUserSettings,
			PersistSession:    req.PersistSession,
			StreamingBehavior: req.StreamingBehavior,
			ChatID:            req.ChatID,
			ThreadID:          req.ThreadID,
			UserID:            req.UserID,
		},
	}
	if len(req.Images) > 0 {
		br.Options.Images = make([]ImageAttachment, len(req.Images))
		for i, img := range req.Images {
			br.Options.Images[i] = ImageAttachment{
				Data:      img.Data,
				MediaType: img.MediaType,
				Path:      img.Path,
			}
		}
	}
	if req.Security != nil {
		br.Options.Security = &SecurityContext{
			Enabled:           req.Security.Enabled,
			Profile:           req.Security.Profile,
			Mode:              req.Security.Mode,
			Cwd:               req.Security.Cwd,
			SensitivePaths:    req.Security.SensitivePaths,
			AllowedOutsideCWD: req.Security.AllowedOutsideCWD,
			ChatID:            req.Security.ChatID,
			ThreadID:          req.Security.ThreadID,
			UserID:            req.Security.UserID,
			AgentName:         req.Security.AgentName,
			RequestID:         req.Security.RequestID,
		}
	}
	return br
}

func toBridgeCommand(cmd engine.Command) Request {
	br := Request{
		Command:         cmd.Name,
		Prompt:          cmd.Payload,
		TargetRequestID: cmd.TargetRequestID,
		Refresh:         cmd.Refresh,
		Options: RequestOptions{
			Resume:   cmd.SessionKey,
			ChatID:   cmd.ChatID,
			ThreadID: cmd.ThreadID,
			UserID:   cmd.UserID,
		},
	}
	if cmd.Name == "steer" || cmd.Name == "follow-up" {
		br.Prompt = cmd.Payload
	}
	return br
}

func translateEventChannel(src <-chan Event) <-chan engine.Event {
	out := make(chan engine.Event, eventChannelBuffer)
	go func() {
		defer close(out)
		for ev := range src {
			out <- toEngineEvent(ev)
		}
	}()
	return out
}

func toEngineEvent(ev Event) engine.Event {
	out := engine.Event{
		RawType:      ev.Type,
		RequestID:    ev.RequestID,
		Content:      ev.Content,
		Text:         ev.Text,
		Message:      ev.Message,
		Name:         ev.Name,
		InputRaw:     ev.Input,
		SessionID:    ev.SessionID,
		SessionFile:  ev.SessionFile,
		Model:        ev.Model,
		Tools:        ev.Tools,
		CostUSD:      ev.CostUSD,
		DurationMs:   ev.DurationMs,
		NumTurns:     ev.NumTurns,
		InputTokens:  ev.InputTokens,
		OutputTokens: ev.OutputTokens,
		IsStreaming:  ev.IsStreaming,
		PendingCount: ev.PendingCount,
	}
	if ev.Input != nil {
		if b, err := json.Marshal(ev.Input); err == nil {
			out.Input = string(b)
		}
	}
	switch ev.Type {
	case "assistant":
		out.Type = engine.EventTypeText
	case "tool_use":
		out.Type = engine.EventTypeToolUse
	case "tool_result":
		out.Type = engine.EventTypeToolResult
	case "result":
		out.Type = engine.EventTypeDone
	case "error":
		out.Type = engine.EventTypeError
		msg := ev.Message
		if msg == "" {
			msg = ev.Content
		}
		if msg == "" {
			msg = "unknown bridge error"
		}
		out.Err = fmt.Errorf("%s", msg)
	case "system":
		out.Type = engine.EventTypeSystem
	default:
		out.Type = engine.EventTypeOther
	}
	return out
}

func toEngineStats(ss SessionStats) engine.Stats {
	return engine.Stats{
		SessionFile:     ss.SessionFile,
		SessionID:       ss.SessionID,
		InputTokens:     ss.InputTokens,
		OutputTokens:    ss.OutputTokens,
		TotalTokens:     ss.TotalTokens,
		ToolCalls:       ss.ToolCalls,
		UserMessages:    ss.UserMessages,
		Turns:           ss.UserMessages,
		Cost:            ss.Cost,
		ContextUsagePct: ss.ContextUsagePct,
	}
}