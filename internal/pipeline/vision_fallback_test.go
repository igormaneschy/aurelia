package pipeline

import (
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
)

// TestApplyConfiguredModelOptionsAndVisionFallback verifies that:
//   - With no vision fallback, the configured default model+provider are used.
//   - With vision fallback configured, the fallback model+provider override
//     the defaults for image messages.
//   - The override is request-local and does not corrupt the default/active
//     model state for subsequent requests.
func TestApplyConfiguredModelOptionsAndVisionFallback(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4",
		VisionModel:     "gpt-4-vision",
		VisionProvider:  "openai",
	}
	s := &Service{config: cfg}

	// ── Case 1: no vision fallback configured — defaults are used ──
	t.Run("no fallback uses configured defaults", func(t *testing.T) {
		cfgNoFallback := &config.AppConfig{
			DefaultProvider: "anthropic",
			DefaultModel:    "claude-3",
		}
		sNoFallback := &Service{config: cfgNoFallback}

		req := bridge.Request{Options: bridge.RequestOptions{}}
		sNoFallback.applyConfiguredModelOptions(&req.Options)

		if req.Options.Provider != "anthropic" {
			t.Errorf("Provider = %q, want %q", req.Options.Provider, "anthropic")
		}
		if req.Options.Model != "claude-3" {
			t.Errorf("Model = %q, want %q", req.Options.Model, "claude-3")
		}
	})

	// ── Case 2: vision fallback with provider+model — overrides both ──
	t.Run("vision fallback overrides model and provider", func(t *testing.T) {
		req := bridge.Request{Options: bridge.RequestOptions{}}
		s.applyConfiguredModelOptions(&req.Options)

		if req.Options.Provider != "openai" {
			t.Errorf("before fallback Provider = %q, want %q", req.Options.Provider, "openai")
		}
		if req.Options.Model != "gpt-4" {
			t.Errorf("before fallback Model = %q, want %q", req.Options.Model, "gpt-4")
		}

		images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}
		s.applyVisionFallback(&req, images)

		if req.Options.Model != "gpt-4-vision" {
			t.Errorf("after fallback Model = %q, want %q", req.Options.Model, "gpt-4-vision")
		}
		if req.Options.Provider != "openai" {
			t.Errorf("after fallback Provider = %q, want %q", req.Options.Provider, "openai")
		}
	})

	// ── Case 3: vision fallback model-only (no provider override) ──
	t.Run("vision fallback model only preserves existing provider", func(t *testing.T) {
		cfgModelOnly := &config.AppConfig{
			DefaultProvider: "anthropic",
			DefaultModel:    "claude-3",
			VisionModel:     "claude-3-vision",
			VisionProvider:  "", // no provider override
		}
		sModelOnly := &Service{config: cfgModelOnly}

		req := bridge.Request{Options: bridge.RequestOptions{}}
		sModelOnly.applyConfiguredModelOptions(&req.Options)

		if req.Options.Provider != "anthropic" {
			t.Errorf("before fallback Provider = %q, want %q", req.Options.Provider, "anthropic")
		}

		images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}
		sModelOnly.applyVisionFallback(&req, images)

		if req.Options.Model != "claude-3-vision" {
			t.Errorf("after fallback Model = %q, want %q", req.Options.Model, "claude-3-vision")
		}
		// Provider should remain unchanged when VisionProvider is empty.
		if req.Options.Provider != "anthropic" {
			t.Errorf("after fallback Provider = %q, want %q (should preserve default)", req.Options.Provider, "anthropic")
		}
	})

	// ── Case 4: no images — fallback is NOT applied ──
	t.Run("no images does not apply fallback", func(t *testing.T) {
		req := bridge.Request{Options: bridge.RequestOptions{}}
		s.applyConfiguredModelOptions(&req.Options)

		// No images — fallback should be a no-op.
		s.applyVisionFallback(&req, nil)

		if req.Options.Model != "gpt-4" {
			t.Errorf("Model = %q, want %q (should remain default)", req.Options.Model, "gpt-4")
		}
		if req.Options.Provider != "openai" {
			t.Errorf("Provider = %q, want %q (should remain default)", req.Options.Provider, "openai")
		}
	})

	// ── Case 5: request-local — subsequent request without images is unaffected ──
	t.Run("subsequent request preserves default model", func(t *testing.T) {
		// First request WITH images.
		req1 := bridge.Request{Options: bridge.RequestOptions{}}
		s.applyConfiguredModelOptions(&req1.Options)
		images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}
		s.applyVisionFallback(&req1, images)

		if req1.Options.Model != "gpt-4-vision" {
			t.Errorf("req1 Model = %q, want %q", req1.Options.Model, "gpt-4-vision")
		}

		// Second request WITHOUT images — must use defaults.
		req2 := bridge.Request{Options: bridge.RequestOptions{}}
		s.applyConfiguredModelOptions(&req2.Options)

		if req2.Options.Model != "gpt-4" {
			t.Errorf("req2 Model = %q, want %q (should be default, not fallback)", req2.Options.Model, "gpt-4")
		}
		if req2.Options.Provider != "openai" {
			t.Errorf("req2 Provider = %q, want %q (should be default)", req2.Options.Provider, "openai")
		}
	})
}
