package config

import (
	"testing"

	"github.com/igormaneschy/aurelia/internal/runlog"
)

func TestRunlogRetentionDays_Default(t *testing.T) {
	cfg := &AppConfig{}
	if got := cfg.RunlogRetentionDays(); got != runlog.DefaultRetentionDays {
		t.Fatalf("RunlogRetentionDays() = %d, want %d", got, runlog.DefaultRetentionDays)
	}
}

func TestRunlogRetentionDays_Explicit(t *testing.T) {
	seven := 7
	cfg := &AppConfig{
		Observability: ObservabilityConfig{RetentionDays: &seven},
	}
	if got := cfg.RunlogRetentionDays(); got != 7 {
		t.Fatalf("RunlogRetentionDays() = %d, want 7", got)
	}
}

func TestRunlogRetentionDays_Disabled(t *testing.T) {
	cfg := &AppConfig{
		Observability: ObservabilityConfig{RetentionDays: ptrInt(0)},
	}
	if got := cfg.RunlogRetentionDays(); got != 0 {
		t.Fatalf("RunlogRetentionDays() = %d, want 0", got)
	}
}

func ptrInt(v int) *int { return &v }