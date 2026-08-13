package pipeline

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
)

// fakeModelCataloger implements ModelCataloger for tests without a live bridge.
type fakeModelCataloger struct {
	supports map[string]bool // key "provider:model"
	err      error           // if set, all lookups return this error
}

func (f *fakeModelCataloger) ModelSupportsImages(_ context.Context, provider, model string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	key := provider + ":" + model
	supports, ok := f.supports[key]
	if !ok {
		return false, fmt.Errorf("model %s not found in fake catalog", key)
	}
	return supports, nil
}

func (f *fakeModelCataloger) ModelSupportsImagesByID(_ context.Context, modelID string) (supports bool, found bool, err error) {
	if f.err != nil {
		return false, false, f.err
	}
	var match *bool
	for key, s := range f.supports {
		// keys are "provider:model"
		colon := -1
		for i, c := range key {
			if c == ':' {
				colon = i
				break
			}
		}
		if colon < 0 {
			continue
		}
		if key[colon+1:] == modelID {
			if match != nil {
				return false, false, nil // ambiguous — multiple providers
			}
			cp := s
			match = &cp
		}
	}
	if match == nil {
		return false, false, nil // not found
	}
	return *match, true, nil
}

// cat registers a provider:model with SupportsImages=true.
func cat(provider, model string) map[string]bool {
	return map[string]bool{provider + ":" + model: true}
}

// TestApplyVisionFallback_ModelSupportsImages verifies that when the selected
// provider/model is present in the model catalog with SupportsImages=true, the
// vision fallback is NOT applied and the request retains the selected model.
//
// Behavior assertion: Given an image request whose selected provider/model is
// present in the model catalog with SupportsImages=true, when the pipeline
// prepares the bridge request, then req.Options.Provider and req.Options.Model
// remain the selected values and configured vision fallback is not applied.
func TestApplyVisionFallback_ModelSupportsImages(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4-vision",
		VisionModel:     "claude-3-vision",
		VisionProvider:  "anthropic",
	}
	s := &Service{
		config:         cfg,
		modelCataloger: &fakeModelCataloger{supports: cat("openai", "gpt-4-vision")},
	}

	req := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "openai",
			Model:    "gpt-4-vision",
		},
	}
	images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

	s.applyVisionFallback(context.Background(), &req, images)

	if req.Options.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", req.Options.Provider, "openai")
	}
	if req.Options.Model != "gpt-4-vision" {
		t.Errorf("Model = %q, want %q", req.Options.Model, "gpt-4-vision")
	}
}

// TestApplyVisionFallback_ModelDoesNotSupportImages verifies that when the
// selected provider/model is present in the catalog with SupportsImages=false,
// the configured vision fallback is applied.
//
// Behavior assertion: Given an image request whose selected provider/model is
// present in the model catalog with SupportsImages=false, when the pipeline
// prepares the bridge request, then configured VisionModel is applied and
// configured VisionProvider is applied when non-empty.
func TestApplyVisionFallback_ModelDoesNotSupportImages(t *testing.T) {
	t.Run("fallback with provider override", func(t *testing.T) {
		cfg := &config.AppConfig{
			DefaultProvider: "openai",
			DefaultModel:    "gpt-4",
			VisionModel:     "gpt-4-vision",
			VisionProvider:  "openai",
		}
		s := &Service{
			config:         cfg,
			modelCataloger: &fakeModelCataloger{supports: map[string]bool{"openai:gpt-4": false}},
		}

		req := bridge.Request{
			Options: bridge.RequestOptions{
				Provider: "openai",
				Model:    "gpt-4",
			},
		}
		images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

		s.applyVisionFallback(context.Background(), &req, images)

		if req.Options.Model != "gpt-4-vision" {
			t.Errorf("Model = %q, want %q", req.Options.Model, "gpt-4-vision")
		}
		if req.Options.Provider != "openai" {
			t.Errorf("Provider = %q, want %q", req.Options.Provider, "openai")
		}
	})

	t.Run("fallback model-only preserves existing provider", func(t *testing.T) {
		cfg := &config.AppConfig{
			DefaultProvider: "anthropic",
			DefaultModel:    "claude-3",
			VisionModel:     "claude-3-vision",
			VisionProvider:  "", // no provider override
		}
		s := &Service{
			config:         cfg,
			modelCataloger: &fakeModelCataloger{supports: map[string]bool{"anthropic:claude-3": false}},
		}

		req := bridge.Request{
			Options: bridge.RequestOptions{
				Provider: "anthropic",
				Model:    "claude-3",
			},
		}
		images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

		s.applyVisionFallback(context.Background(), &req, images)

		if req.Options.Model != "claude-3-vision" {
			t.Errorf("Model = %q, want %q", req.Options.Model, "claude-3-vision")
		}
		// Provider remains unchanged when VisionProvider is empty.
		if req.Options.Provider != "anthropic" {
			t.Errorf("Provider = %q, want %q (should preserve current provider)", req.Options.Provider, "anthropic")
		}
	})
}

// TestApplyVisionFallback_CatalogUnavailable verifies that when the model
// catalog is unavailable or returns an error, the configured vision fallback
// is still applied (fail-safe).
//
// Behavior assertion: Given an image request and the model catalog is
// unavailable, empty, or does not contain the selected provider/model, when a
// configured vision fallback exists, then the existing fail-open fallback
// behavior remains: use configured VisionModel and optional VisionProvider.
func TestApplyVisionFallback_CatalogUnavailable(t *testing.T) {
	t.Run("catalog lookup error uses fallback", func(t *testing.T) {
		cfg := &config.AppConfig{
			DefaultProvider: "openai",
			DefaultModel:    "gpt-4",
			VisionModel:     "gpt-4-vision",
			VisionProvider:  "openai",
		}
		s := &Service{
			config:         cfg,
			modelCataloger: &fakeModelCataloger{err: errors.New("bridge not ready")},
		}

		req := bridge.Request{
			Options: bridge.RequestOptions{
				Provider: "openai",
				Model:    "gpt-4",
			},
		}
		images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

		s.applyVisionFallback(context.Background(), &req, images)

		if req.Options.Model != "gpt-4-vision" {
			t.Errorf("Model = %q, want %q (fallback on error)", req.Options.Model, "gpt-4-vision")
		}
		if req.Options.Provider != "openai" {
			t.Errorf("Provider = %q, want %q", req.Options.Provider, "openai")
		}
	})

	t.Run("model not in catalog uses fallback", func(t *testing.T) {
		cfg := &config.AppConfig{
			DefaultProvider: "openai",
			DefaultModel:    "gpt-4",
			VisionModel:     "gpt-4-vision",
		}
		s := &Service{
			config:         cfg,
			modelCataloger: &fakeModelCataloger{supports: map[string]bool{}},
		}

		req := bridge.Request{
			Options: bridge.RequestOptions{
				Provider: "openai",
				Model:    "gpt-4",
			},
		}
		images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

		s.applyVisionFallback(context.Background(), &req, images)

		if req.Options.Model != "gpt-4-vision" {
			t.Errorf("Model = %q, want %q (fallback on missing model)", req.Options.Model, "gpt-4-vision")
		}
	})

	t.Run("no cataloger available uses fallback", func(t *testing.T) {
		cfg := &config.AppConfig{
			DefaultProvider: "openai",
			DefaultModel:    "gpt-4",
			VisionModel:     "gpt-4-vision",
		}
		s := &Service{
			config:         cfg,
			modelCataloger: nil, // no cataloger
		}

		req := bridge.Request{
			Options: bridge.RequestOptions{
				Provider: "openai",
				Model:    "gpt-4",
			},
		}
		images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

		s.applyVisionFallback(context.Background(), &req, images)

		if req.Options.Model != "gpt-4-vision" {
			t.Errorf("Model = %q, want %q (fallback without cataloger)", req.Options.Model, "gpt-4-vision")
		}
	})
}

// TestApplyVisionFallback_NoImages verifies that requests without images never
// alter provider/model, regardless of vision fallback configuration.
//
// Behavior assertion: Given a request without images, when the pipeline prepares
// the bridge request, then no vision fallback decision changes provider/model.
func TestApplyVisionFallback_NoImages(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4",
		VisionModel:     "gpt-4-vision",
		VisionProvider:  "openai",
	}
	s := &Service{
		config:         cfg,
		modelCataloger: &fakeModelCataloger{},
	}

	t.Run("nil images", func(t *testing.T) {
		req := bridge.Request{
			Options: bridge.RequestOptions{
				Provider: "openai",
				Model:    "gpt-4",
			},
		}
		s.applyVisionFallback(context.Background(), &req, nil)

		if req.Options.Model != "gpt-4" {
			t.Errorf("Model = %q, want %q", req.Options.Model, "gpt-4")
		}
		if req.Options.Provider != "openai" {
			t.Errorf("Provider = %q, want %q", req.Options.Provider, "openai")
		}
	})

	t.Run("empty images", func(t *testing.T) {
		req := bridge.Request{
			Options: bridge.RequestOptions{
				Provider: "openai",
				Model:    "gpt-4",
			},
		}
		s.applyVisionFallback(context.Background(), &req, []bridge.ImageAttachment{})

		if req.Options.Model != "gpt-4" {
			t.Errorf("Model = %q, want %q", req.Options.Model, "gpt-4")
		}
		if req.Options.Provider != "openai" {
			t.Errorf("Provider = %q, want %q", req.Options.Provider, "openai")
		}
	})
}

// TestApplyVisionFallback_ProfileOverride verifies that when a profile-level
// model override is set and that model supports images, the profile model is
// kept instead of switching to the configured vision fallback.
//
// Behavior assertion: Given a profile-level model override, when that override
// is cataloged as image-capable, then image requests keep the profile model
// instead of forcing configured vision fallback.
func TestApplyVisionFallback_ProfileOverride(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4", // profile overrides this
		VisionModel:     "claude-3-vision",
		VisionProvider:  "anthropic",
	}
	s := &Service{
		config:         cfg,
		modelCataloger: &fakeModelCataloger{supports: map[string]bool{"anthropic:claude-3-haiku": true}},
	}

	// Profile model override (gpt-4-haiku) supports images.
	req := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "anthropic",
			Model:    "claude-3-haiku", // profile override
		},
	}
	images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

	s.applyVisionFallback(context.Background(), &req, images)

	if req.Options.Model != "claude-3-haiku" {
		t.Errorf("Model = %q, want %q (profile model should be kept)", req.Options.Model, "claude-3-haiku")
	}
	if req.Options.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", req.Options.Provider, "anthropic")
	}
}

// TestApplyVisionFallback_ProviderQualifiedProfileModel verifies that a profile
// model with a provider-qualified name (e.g. "openai/gpt-5.4") is parsed and
// looked up correctly even when req.Options.Provider is empty (auto mode).
//
// This is the most common real-world profile-override pattern:
//   - Config is auto mode → applyConfiguredModelOptions is a no-op
//   - Profile sets pp.Model = "openai/gpt-5.4"
//   - req.Options.Provider="" and req.Options.Model="openai/gpt-5.4"
//   - The fallback must parse "openai/gpt-5.4" and check that specific model
func TestApplyVisionFallback_ProviderQualifiedProfileModel(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "", // auto mode — no default provider
		DefaultModel:    "",
		VisionModel:     "claude-3-vision",
		VisionProvider:  "anthropic",
	}
	s := &Service{
		config: cfg,
		modelCataloger: &fakeModelCataloger{supports: map[string]bool{
			"openai:gpt-5.4": true, // the parsed model supports images
		}},
	}

	// Profile set Model to "openai/gpt-5.4" without setting Provider.
	req := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "",
			Model:    "openai/gpt-5.4",
		},
	}
	images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

	s.applyVisionFallback(context.Background(), &req, images)

	// Model supports images — must be normalized to separate provider/model fields.
	if req.Options.Provider != "openai" {
		t.Errorf("Provider = %q, want %q (should be normalized from parsed model)", req.Options.Provider, "openai")
	}
	if req.Options.Model != "gpt-5.4" {
		t.Errorf("Model = %q, want %q (should be normalized, without provider prefix)", req.Options.Model, "gpt-5.4")
	}
}

// TestApplyVisionFallback_ProviderQualifiedModelNotFound verifies that when a
// provider-qualified model is parsed but not found in the catalog, the
// configured vision fallback is applied (fail-safe).
func TestApplyVisionFallback_ProviderQualifiedModelNotFound(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "",
		DefaultModel:    "",
		VisionModel:     "claude-3-vision",
		VisionProvider:  "anthropic",
	}
	s := &Service{
		config: cfg,
		modelCataloger: &fakeModelCataloger{supports: map[string]bool{
			"openai:gpt-4": true, // a different model
		}},
	}

	req := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "",
			Model:    "openai/gpt-5.4", // not in catalog
		},
	}
	images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

	s.applyVisionFallback(context.Background(), &req, images)

	if req.Options.Model != "claude-3-vision" {
		t.Errorf("Model = %q, want %q (fallback on not found)", req.Options.Model, "claude-3-vision")
	}
}

// TestApplyVisionFallback_UnqualifiedModelSingleMatch verifies that an
// unqualified model (no "/" in name, Provider="") is looked up by model ID
// across the catalog. When exactly one provider offers this model ID and it
// supports images, the fallback must NOT be applied.
//
// This matches profiles that set pp.Model = "gpt-4" without a provider prefix
// when config is in auto mode and only one provider in the catalog offers
// that model ID.
func TestApplyVisionFallback_UnqualifiedModelSingleMatch(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "",
		DefaultModel:    "",
		VisionModel:     "claude-3-vision",
		VisionProvider:  "anthropic",
	}
	s := &Service{
		config: cfg,
		modelCataloger: &fakeModelCataloger{supports: map[string]bool{
			"openai:gpt-4-vision": true, // single provider offers this model
		}},
	}

	req := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "",
			Model:    "gpt-4-vision",
		},
	}
	images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

	s.applyVisionFallback(context.Background(), &req, images)

	if req.Options.Model != "gpt-4-vision" {
		t.Errorf("Model = %q, want %q (single catalog match should be kept)", req.Options.Model, "gpt-4-vision")
	}
	if req.Options.Provider != "" {
		t.Errorf("Provider = %q, want %q (should remain empty)", req.Options.Provider, "")
	}
}

// TestApplyVisionFallback_UnqualifiedModelAmbiguous verifies that when an
// unqualified model ID matches multiple providers in the catalog, capability
// is treated as unknown and the configured vision fallback is applied.
func TestApplyVisionFallback_UnqualifiedModelAmbiguous(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "",
		DefaultModel:    "",
		VisionModel:     "claude-3-vision",
		VisionProvider:  "anthropic",
	}
	// Two different providers both offer "gpt-4" — ambiguous.
	s := &Service{
		config: cfg,
		modelCataloger: &fakeModelCataloger{supports: map[string]bool{
			"openai:gpt-4":    true,
			"anthropic:gpt-4": true,
		}},
	}

	req := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "",
			Model:    "gpt-4",
		},
	}
	images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

	s.applyVisionFallback(context.Background(), &req, images)

	if req.Options.Model != "claude-3-vision" {
		t.Errorf("Model = %q, want %q (fallback on ambiguous)", req.Options.Model, "claude-3-vision")
	}
}

// TestApplyVisionFallback_UnqualifiedModelNotFound verifies that when an
// unqualified model ID has zero matches in the catalog, the configured
// vision fallback is applied (fail-safe).
func TestApplyVisionFallback_UnqualifiedModelNotFound(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "",
		DefaultModel:    "",
		VisionModel:     "claude-3-vision",
		VisionProvider:  "anthropic",
	}
	s := &Service{
		config: cfg,
		modelCataloger: &fakeModelCataloger{supports: map[string]bool{
			"openai:gpt-4": true, // different model
		}},
	}

	req := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "",
			Model:    "gpt-4-vision", // not in catalog
		},
	}
	images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

	s.applyVisionFallback(context.Background(), &req, images)

	if req.Options.Model != "claude-3-vision" {
		t.Errorf("Model = %q, want %q (fallback on not found)", req.Options.Model, "claude-3-vision")
	}
}

// TestApplyVisionFallback_RequestLocal verifies that the fallback decision does
// not leak between requests — a subsequent image-less request keeps defaults.
func TestApplyVisionFallback_RequestLocal(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4",
		VisionModel:     "gpt-4-vision",
		VisionProvider:  "openai",
	}
	s := &Service{
		config:         cfg,
		modelCataloger: &fakeModelCataloger{supports: map[string]bool{"openai:gpt-4": false}},
	}

	// First request WITH images — fallback kicks in.
	req1 := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "openai",
			Model:    "gpt-4",
		},
	}
	images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}
	s.applyVisionFallback(context.Background(), &req1, images)

	if req1.Options.Model != "gpt-4-vision" {
		t.Errorf("req1 Model = %q, want %q", req1.Options.Model, "gpt-4-vision")
	}

	// Second request WITHOUT images — defaults preserved.
	req2 := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "openai",
			Model:    "gpt-4",
		},
	}
	s.applyVisionFallback(context.Background(), &req2, nil)

	if req2.Options.Model != "gpt-4" {
		t.Errorf("req2 Model = %q, want %q (should be default, not fallback)", req2.Options.Model, "gpt-4")
	}
	if req2.Options.Provider != "openai" {
		t.Errorf("req2 Provider = %q, want %q (should be default)", req2.Options.Provider, "openai")
	}
}

// TestApplyVisionFallback_NoFallbackConfigured verifies that when no vision
// fallback is configured, the current model is retained even with images.
func TestApplyVisionFallback_NoFallbackConfigured(t *testing.T) {
	cfg := &config.AppConfig{
		DefaultProvider: "openai",
		DefaultModel:    "gpt-4",
	}
	s := &Service{
		config:         cfg,
		modelCataloger: &fakeModelCataloger{supports: map[string]bool{"openai:gpt-4": false}},
	}

	req := bridge.Request{
		Options: bridge.RequestOptions{
			Provider: "openai",
			Model:    "gpt-4",
		},
	}
	images := []bridge.ImageAttachment{{Path: "/tmp/test.png"}}

	s.applyVisionFallback(context.Background(), &req, images)

	// No fallback configured — nothing to switch to, model stays unchanged.
	if req.Options.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", req.Options.Model, "gpt-4")
	}
	if req.Options.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", req.Options.Provider, "openai")
	}
}
