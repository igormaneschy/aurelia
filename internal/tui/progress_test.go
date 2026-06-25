package tui

import ("strings"; "testing"; "time"; "github.com/igormaneschy/aurelia/internal/ipc")

func TestStreamProgress_HiddenBefore2s(t *testing.T) {
	m := testChatModel(); m.waiting = true; (&m).initStreamProgress()
	m.streamProgress.showAfter = time.Now().Add(2 * time.Second)
	if m.showStreamProgress() { t.Fatal("hidden") }
}

func TestStreamProgress_VisibleAfter2s(t *testing.T) {
	m := testChatModel(); m.width = 80; m.waiting = true; (&m).initStreamProgress()
	m.streamProgress.showAfter = time.Now().Add(-time.Second); (&m).updateStreamProgress(512)
	if !m.showStreamProgress() || m.renderStreamProgress(m.width) == "" { t.Fatal("visible") }
}

func TestStreamProgress_ResetOnStreamEnd(t *testing.T) {
	m := testChatModel(); m.waiting = true; (&m).initStreamProgress()
	m.streamProgress.showAfter = time.Now().Add(-time.Second); (&m).updateStreamProgress(1024)
	m2, _ := m.handleStreamEvent(ipc.IPCEvent{Type: ipc.EventTypeStreamEnd})
	s := m2.(Model)
	if s.streamProgress.active || s.waiting { t.Fatal("reset") }
}

func TestElapsedLabel_UsesTimerPrefix(t *testing.T) {
	m := testChatModel(); m.turnStart = time.Now().Add(-3200 * time.Millisecond)
	l := m.elapsedLabel()
	if !strings.HasPrefix(l, "⏱ ") { t.Fatalf("%q", l) }
}

func TestAttachProgress_VisibleWhileWaiting(t *testing.T) {
	m := testChatModel()
	m.width = 80
	m.waiting = true
	(&m).initAttachProgress(2)
	if !m.showAttachProgress() {
		t.Fatal("expected attach progress visible")
	}
	if got := m.renderAttachProgress(m.width); got == "" || !strings.Contains(got, "Sending 2 file") {
		t.Fatalf("render = %q", got)
	}
}

func TestAttachProgress_HiddenAfterReader(t *testing.T) {
	m := testChatModel()
	m.waiting = true
	(&m).initAttachProgress(1)
	m.reader = &ipc.ResponseReader{}
	if m.showAttachProgress() {
		t.Fatal("hidden once stream reader is active")
	}
}

func TestAttachProgress_ResetOnStreamReader(t *testing.T) {
	m := testChatModel()
	m.waiting = true
	m.streamID = 3
	(&m).initAttachProgress(2)
	updated, _ := m.Update(&streamReaderMsg{reader: nil, streamID: 3})
	s := updated.(Model)
	if s.attachProgress.active {
		t.Fatal("attach progress should reset on stream reader")
	}
	if !s.streamProgress.active {
		t.Fatal("stream progress should start on stream reader")
	}
}
