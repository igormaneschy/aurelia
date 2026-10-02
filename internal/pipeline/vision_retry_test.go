package pipeline

import (
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
)

func TestIsVisionRefusal(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		// Observed production refusal (Qwen3.8 via llama.cpp, 2026-10-02).
		{name: "observed PT refusal", text: "A imagem veio vazia, Igor — só o frame de legenda \"Analise esta imagem\" sem o conteúdo visual.", want: true},
		{name: "observed PT refusal retry", text: "De novo veio só o frame, Igor — a imagem não está chegando do meu lado. Pode tentar reenviar?", want: true},
		{name: "PT cannot see image", text: "Não consigo ver a imagem que você enviou.", want: true},
		{name: "PT cannot visualize", text: "Não consigo visualizar imagens.", want: true},
		{name: "PT no capability", text: "Não tenho capacidade de visão para analisar fotos.", want: true},
		{name: "EN cannot see", text: "I can't see any image in your message.", want: true},
		{name: "EN unable to view", text: "I'm unable to view images.", want: true},
		{name: "EN no image provided", text: "No image was provided with your request.", want: true},
		{name: "EN text only", text: "I'm a text-only model and can't process pictures.", want: true},
		// Negatives.
		{name: "empty", text: "", want: false},
		{name: "genuine analysis", text: "A imagem mostra um gato laranja sobre o sofá, com luz natural vindo da janela à esquerda.", want: false},
		{name: "unrelated cannot see", text: "Não consigo ver onde esse erro acontece sem o stack trace completo.", want: false},
		{name: "long mixed reply", text: "A imagem mostra um diagrama. " + strings.Repeat("Detalhe técnico da arquitetura. ", 100) + "não consigo ver", want: false},
	}
	for _, tc := range tests {
		if got := isVisionRefusal(tc.text); got != tc.want {
			t.Errorf("isVisionRefusal(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestNewVisionAttempt(t *testing.T) {
	cfg := func() *config.AppConfig {
		return &config.AppConfig{VisionModel: "qwen3.5-plus", VisionProvider: "opencode-go"}
	}
	img := []bridge.ImageAttachment{{Data: "abcd", MediaType: "image/jpeg"}}

	if got := newVisionAttempt(bridge.Request{}, cfg()); got != nil {
		t.Errorf("no images: got %+v, want nil", got)
	}
	req := bridge.Request{}
	req.Options.Images = img
	if got := newVisionAttempt(req, nil); got != nil {
		t.Errorf("nil config: got %+v, want nil", got)
	}
	if got := newVisionAttempt(req, &config.AppConfig{}); got != nil {
		t.Errorf("no fallback configured: got %+v, want nil", got)
	}
	same := bridge.Request{}
	same.Options.Images = img
	same.Options.Provider = "opencode-go"
	same.Options.Model = "qwen3.5-plus"
	if got := newVisionAttempt(same, cfg()); got != nil {
		t.Errorf("already on fallback: got %+v, want nil", got)
	}
	other := bridge.Request{}
	other.Options.Images = img
	other.Options.Provider = "llamacpp-tailscale"
	other.Options.Model = "/models/Qwen3.8-27B-UD-Q3_K_XL.gguf"
	got := newVisionAttempt(other, cfg())
	if got == nil || !got.canRetry || len(got.images) != 1 {
		t.Fatalf("eligible turn: got %+v, want retryable attempt", got)
	}
}

func TestHandleResultEvent_VisionRefusalRequestsRetry(t *testing.T) {
	s, _, ownership, _ := newOwnedResultService(t, "")
	s.config = &config.AppConfig{VisionModel: "qwen3.5-plus", VisionProvider: "opencode-go"}
	vision := &visionAttempt{
		images:   []bridge.ImageAttachment{{Data: "abcd", MediaType: "image/jpeg"}},
		canRetry: true,
	}
	var assistant strings.Builder
	ev := bridge.Event{Type: "result", Content: "A imagem veio vazia — só recebi o texto, sem o conteúdo visual."}
	if outcome := s.handleResultEvent(1, 0, 7, ev, &assistant, "Analise esta imagem.", 100, false, vision, ownership); outcome != OutcomeVisionRetry {
		t.Fatalf("outcome = %v, want OutcomeVisionRetry", outcome)
	}
	if !vision.retried {
		t.Fatal("vision.retried = false, want true after refusal")
	}
	if got := s.output.(*fakeOutput).lastReply; got != "" {
		t.Fatalf("reply = %q, want suppressed (no delivery before retry)", got)
	}
	if vision.refusalText == "" {
		t.Fatal("refusalText empty, want stashed refusal for fallback-failure path")
	}
}

func TestHandleResultEvent_VisionRetrySpentDeliversNormally(t *testing.T) {
	s, _, ownership, _ := newOwnedResultService(t, "")
	s.config = &config.AppConfig{VisionModel: "qwen3.5-plus", VisionProvider: "opencode-go"}
	// A refusal that already went through the retry must be delivered as-is
	// instead of looping.
	vision := &visionAttempt{
		images:   []bridge.ImageAttachment{{Data: "abcd", MediaType: "image/jpeg"}},
		canRetry: false,
		retried:  true,
	}
	var assistant strings.Builder
	ev := bridge.Event{Type: "result", Content: "A imagem veio vazia."}
	if outcome := s.handleResultEvent(1, 0, 7, ev, &assistant, "Analise esta imagem.", 100, false, vision, ownership); outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}
	if got := s.output.(*fakeOutput).lastReply; got != "A imagem veio vazia." {
		t.Fatalf("reply = %q, want refusal delivered after spent retry", got)
	}
}

func TestHandleResultEvent_NoVisionAttemptDeliversNormally(t *testing.T) {
	s, _, ownership, _ := newOwnedResultService(t, "")
	var assistant strings.Builder
	ev := bridge.Event{Type: "result", Content: "A imagem veio vazia."}
	if outcome := s.handleResultEvent(1, 0, 7, ev, &assistant, "Analise esta imagem.", 100, false, nil, ownership); outcome != OutcomeSuccess {
		t.Fatalf("outcome = %v, want OutcomeSuccess", outcome)
	}
}
