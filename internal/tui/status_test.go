package tui

import (
	"testing"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

func TestCwdFromEventsExtractsStatusCWD(t *testing.T) {
	events := []ipc.IPCEvent{{
		Type: ipc.EventTypeMessage,
		Body: "**Aurelia Status**\n📂 CWD: `/Users/igor/dev/aurelia`\n",
	}}

	got := cwdFromEvents(events)
	want := "/Users/igor/dev/aurelia"
	if got != want {
		t.Fatalf("cwdFromEvents() = %q, want %q", got, want)
	}
}

func TestCwdFromTextHandlesProjectBindingRemoved(t *testing.T) {
	got := cwdFromText("✅ Project binding removed.")
	if got != "not set" {
		t.Fatalf("cwdFromText() = %q, want not set", got)
	}
}

func TestProjectNameReturnsBaseName(t *testing.T) {
	got := projectName("/Users/igor/dev/aurelia")
	if got != "aurelia" {
		t.Fatalf("projectName() = %q, want aurelia", got)
	}
}
