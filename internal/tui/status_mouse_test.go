package tui

import (
	"testing"
)

func TestStatusBarHit_ModelLabel(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 24
	m.activeModel = "claude-sonnet"

	regions := m.statusBarRegions()
	var modelRegion *statusBarRegion
	for i := range regions {
		if regions[i].kind == statusBarHitModel {
			modelRegion = &regions[i]
			break
		}
	}
	if modelRegion == nil {
		t.Fatal("expected clickable model region")
	}

	mid := modelRegion.start + (modelRegion.end-modelRegion.start)/2
	if m.statusBarHit(mid) != statusBarHitModel {
		t.Fatalf("expected model hit at x=%d", mid)
	}
	if m.statusBarHit(0) == statusBarHitModel {
		t.Fatal("expected state label at x=0 not to be model hit")
	}
}

func TestHeaderProjectHit_WhenProjectSet(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.cwdPath = "/Users/igor/myproject"

	y := topMarginHeight + 1
	if !m.headerProjectHit(40, y) {
		t.Fatal("expected project segment click in header")
	}
	if m.headerProjectHit(40, y+5) {
		t.Fatal("expected miss below header line")
	}
}

func TestHeaderProjectHit_ChatModeMiss(t *testing.T) {
	m := NewModel("/tmp/test.sock", ThemeDark)
	m.state = stateChat
	m.width = 120
	m.height = 30
	m.cwdPath = "not set"

	y := topMarginHeight + 1
	if m.headerProjectHit(40, y) {
		t.Fatal("expected no project hit in chat mode")
	}
}