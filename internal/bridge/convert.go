package bridge

import (
	"github.com/igormaneschy/aurelia/internal/engine"
)

// ToEngineRequest converts a legacy bridge.Request to engine.Request.
func ToEngineRequest(br Request) engine.Request {
	req := engine.Request{
		Prompt:            br.Prompt,
		RequestID:         br.RequestID,
		SessionKey:        br.Options.Resume,
		Provider:          br.Options.Provider,
		Model:             br.Options.Model,
		Cwd:               br.Options.Cwd,
		AllowedTools:      br.Options.AllowedTools,
		DisallowedTools:   br.Options.DisallowedTools,
		Continue:          br.Options.Continue,
		NoUserSettings:    br.Options.NoUserSettings,
		PersistSession:    br.Options.PersistSession,
		StreamingBehavior: br.Options.StreamingBehavior,
		ChatID:            br.Options.ChatID,
		ThreadID:          br.Options.ThreadID,
		UserID:            br.Options.UserID,
	}
	if len(br.Options.Images) > 0 {
		req.Images = make([]engine.Image, len(br.Options.Images))
		for i, img := range br.Options.Images {
			req.Images[i] = engine.Image{
				Data:      img.Data,
				MediaType: img.MediaType,
				Path:      img.Path,
			}
		}
	}
	if br.Options.Security != nil {
		sec := br.Options.Security
		req.Security = &engine.SecurityPolicy{
			Enabled:           sec.Enabled,
			Profile:           sec.Profile,
			Mode:              sec.Mode,
			Cwd:               sec.Cwd,
			SensitivePaths:    sec.SensitivePaths,
			AllowedOutsideCWD: sec.AllowedOutsideCWD,
			ChatID:            sec.ChatID,
			ThreadID:          sec.ThreadID,
			UserID:            sec.UserID,
			AgentName:         sec.AgentName,
			RequestID:         sec.RequestID,
		}
	}
	return req
}

// OptionsFromEngine builds bridge.RequestOptions for PI-specific bridge methods.
func OptionsFromEngine(req engine.Request) RequestOptions {
	opts := RequestOptions{
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
		SystemPrompt:      req.SystemPrompt,
	}
	if len(req.Images) > 0 {
		opts.Images = make([]ImageAttachment, len(req.Images))
		for i, img := range req.Images {
			opts.Images[i] = ImageAttachment{
				Data:      img.Data,
				MediaType: img.MediaType,
				Path:      img.Path,
			}
		}
	}
	if req.Security != nil {
		sec := req.Security
		opts.Security = &SecurityContext{
			Enabled:           sec.Enabled,
			Profile:           sec.Profile,
			Mode:              sec.Mode,
			Cwd:               sec.Cwd,
			SensitivePaths:    sec.SensitivePaths,
			AllowedOutsideCWD: sec.AllowedOutsideCWD,
			ChatID:            sec.ChatID,
			ThreadID:          sec.ThreadID,
			UserID:            sec.UserID,
			AgentName:         sec.AgentName,
			RequestID:         sec.RequestID,
		}
	}
	return opts
}