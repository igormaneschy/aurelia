package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/harmonica"
)

const animTickInterval = 16 * time.Millisecond

// animTickMsg drives the harmonica spring loop (~60fps).
type animTickMsg struct{}

// animState holds spring targets for upcoming fade/pulse transitions (F6.2/F6.3).
type animState struct {
	enabled bool
	ticking bool

	spinnerSpring  harmonica.Spring
	responseSpring harmonica.Spring
	badgeSpring    harmonica.Spring

	spinnerPos, spinnerVel   float64
	responsePos, responseVel float64
	badgePos, badgeVel       float64

	spinnerTarget  float64
	responseTarget float64
	badgeTarget    float64
}

// ModelOptions configures optional TUI behavior at startup.
type ModelOptions struct {
	NoAnimations bool
	NoMouse      bool
	Transparent  bool
	// StartupSession opens this named session on connect (empty = default DM).
	StartupSession string
}

func animationsEnabledForTerm(term string, noAnimations bool) bool {
	if noAnimations {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(term)) {
	case "", "dumb", "vt100":
		return false
	default:
		return true
	}
}

func animFrameDelta() float64 {
	return animTickInterval.Seconds()
}

func newAnimState(enabled bool) animState {
	if !enabled {
		return animState{
			enabled:        false,
			spinnerPos:     1,
			responsePos:    1,
			badgePos:       1,
			spinnerTarget:  1,
			responseTarget: 1,
			badgeTarget:    1,
		}
	}
	dt := animFrameDelta()
	return animState{
		enabled:        true,
		spinnerSpring:  harmonica.NewSpring(dt, 8.0, 0.45),
		responseSpring: harmonica.NewSpring(dt, 10.0, 0.5),
		badgeSpring:    harmonica.NewSpring(dt, 6.0, 0.35),
		spinnerPos:     1,
		responsePos:    1,
		badgePos:       1,
		spinnerTarget:  1,
		responseTarget: 1,
		badgeTarget:    1,
	}
}

func (a animState) tickCmd() tea.Cmd {
	if !a.enabled {
		return nil
	}
	return tea.Tick(animTickInterval, func(time.Time) tea.Msg {
		return animTickMsg{}
	})
}

func (a *animState) beginTick() tea.Cmd {
	if !a.enabled || a.ticking {
		return nil
	}
	a.ticking = true
	return a.tickCmd()
}

func (a *animState) step() tea.Cmd {
	if !a.enabled || !a.ticking {
		return nil
	}
	a.spinnerPos, a.spinnerVel = a.spinnerSpring.Update(a.spinnerPos, a.spinnerVel, a.spinnerTarget)
	a.responsePos, a.responseVel = a.responseSpring.Update(a.responsePos, a.responseVel, a.responseTarget)
	a.badgePos, a.badgeVel = a.badgeSpring.Update(a.badgePos, a.badgeVel, a.badgeTarget)
	if a.badgeTarget > 1.15 && near(a.badgePos, a.badgeTarget) {
		a.badgeTarget = 1
	}
	if a.settled() {
		a.ticking = false
		return nil
	}
	return a.tickCmd()
}

// onStreamStart fades in the streaming response text.
func (a *animState) onStreamStart() tea.Cmd {
	if !a.enabled {
		return nil
	}
	a.spinnerTarget = 1
	a.spinnerPos = 1
	a.responseTarget = 1
	a.responsePos = 0
	return a.beginTick()
}

// onStreamEnd fades out the thinking spinner.
func (a *animState) onStreamEnd() tea.Cmd {
	if !a.enabled {
		return nil
	}
	a.spinnerTarget = 0
	a.responseTarget = 1
	return a.beginTick()
}

// pulseNewMessages animates the new-messages indicator.
func (a *animState) pulseNewMessages() tea.Cmd {
	if !a.enabled {
		return nil
	}
	a.badgeTarget = 1.3
	a.badgePos = 1
	return a.beginTick()
}

func (a animState) settled() bool {
	return near(a.spinnerPos, a.spinnerTarget) &&
		near(a.responsePos, a.responseTarget) &&
		near(a.badgePos, a.badgeTarget)
}

func near(a, b float64) bool {
	const eps = 0.002
	if a > b {
		return a-b < eps
	}
	return b-a < eps
}

// SpinnerOpacity returns the current spinner fade multiplier [0,1].
func (a animState) SpinnerOpacity() float64 {
	if !a.enabled {
		return 1
	}
	return clampFloat(a.spinnerPos, 0, 1)
}

// ResponseOpacity returns the current response fade multiplier [0,1].
func (a animState) ResponseOpacity() float64 {
	if !a.enabled {
		return 1
	}
	return clampFloat(a.responsePos, 0, 1)
}

// BadgeScale returns the current unread-badge pulse scale [1, ~1.3].
func (a animState) BadgeScale() float64 {
	if !a.enabled {
		return 1
	}
	return clampFloat(a.badgePos, 1, 1.35)
}

func fadeStyle(base lipgloss.Style, opacity float64) lipgloss.Style {
	if opacity >= 0.95 {
		return base
	}
	if opacity < 0.4 {
		return base.Faint(true)
	}
	return base
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
