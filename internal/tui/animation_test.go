package tui

import "testing"

func TestAnimationsEnabledForTerm(t *testing.T) {
	if animationsEnabledForTerm("xterm-256color", false) != true {
		t.Fatal("expected animations on xterm")
	}
	if animationsEnabledForTerm("dumb", false) != false {
		t.Fatal("expected animations off for dumb")
	}
	if animationsEnabledForTerm("xterm-256color", true) != false {
		t.Fatal("expected --no-animations override")
	}
}

func TestAnimState_BeginTickSettlesAtRest(t *testing.T) {
	a := newAnimState(true)
	if cmd := a.beginTick(); cmd == nil {
		t.Fatal("expected initial tick cmd")
	}
	if cmd := a.step(); cmd != nil {
		t.Fatal("expected tick loop to stop at equilibrium")
	}
	if !a.settled() {
		t.Fatal("expected springs settled")
	}
}

func TestAnimState_DisabledReturnsFullOpacity(t *testing.T) {
	a := newAnimState(false)
	if a.SpinnerOpacity() != 1 || a.ResponseOpacity() != 1 || a.BadgeScale() != 1 {
		t.Fatal("disabled animations should not alter opacity/scale")
	}
}

func TestNewModelWithOptions_NoAnimations(t *testing.T) {
	m := NewModelWithOptions("/tmp/test.sock", ThemeDark, ModelOptions{NoAnimations: true})
	if m.animations.enabled {
		t.Fatal("expected animations disabled")
	}
}