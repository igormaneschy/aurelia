package tui

import (
	"fmt"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/stopwatch"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	streamProgressDelay      = 2 * time.Second
	defaultStreamTokenMax    = 2048
	maxRecentResponseLengths = 5
	streamStopwatchInterval  = 100 * time.Millisecond
)

type streamProgress struct {
	bar progress.Model
	stopwatch stopwatch.Model
	active bool
	showAfter time.Time
	tokenEst int
	tokenMax int
	recentLengths []int
}

func newStreamProgressBar(styles themeStyles) progress.Model {
	return progress.New(
		progress.WithWidth(40),
		progress.WithFillCharacters('█', '░'),
		progress.WithColors(lipgloss.Color(styles.ProgressFullColor), lipgloss.Color(styles.ProgressEmptyColor)),
		progress.WithoutPercentage(),
	)
}

func (sp *streamProgress) estimateTokenMax() int {
	if len(sp.recentLengths) == 0 { return defaultStreamTokenMax }
	sum := 0
	for _, l := range sp.recentLengths { sum += l }
	avg := sum / len(sp.recentLengths)
	if avg < 256 { return defaultStreamTokenMax }
	return avg
}

func (sp *streamProgress) recordLength(length int) {
	if length <= 0 { return }
	sp.recentLengths = append(sp.recentLengths, length)
	if len(sp.recentLengths) > maxRecentResponseLengths {
		sp.recentLengths = sp.recentLengths[len(sp.recentLengths)-maxRecentResponseLengths:]
	}
}

func (m *Model) initStreamProgress() tea.Cmd {
	m.streamProgress.active = true
	m.streamProgress.showAfter = time.Now().Add(streamProgressDelay)
	m.streamProgress.tokenEst = 0
	m.streamProgress.tokenMax = m.streamProgress.estimateTokenMax()
	m.streamProgress.bar = newStreamProgressBar(m.styles)
	m.streamProgress.stopwatch = stopwatch.New(stopwatch.WithInterval(streamStopwatchInterval))
	m.turnStart = time.Now()
	return m.streamProgress.stopwatch.Start()
}

func (m *Model) updateStreamProgress(chunkLen int) {
	if !m.streamProgress.active || chunkLen <= 0 { return }
	m.streamProgress.tokenEst += chunkLen
	if m.streamProgress.tokenEst > m.streamProgress.tokenMax {
		m.streamProgress.tokenMax = m.streamProgress.tokenEst + m.streamProgress.tokenEst/4
	}
}

func (m Model) streamProgressPercent() float64 {
	if !m.streamProgress.active || m.streamProgress.tokenMax <= 0 { return 0 }
	pct := float64(m.streamProgress.tokenEst) / float64(m.streamProgress.tokenMax)
	if pct > 0.95 { return 0.95 }
	return pct
}

func (m Model) showStreamProgress() bool {
	if !m.streamProgress.active || !m.waiting { return false }
	return !time.Now().Before(m.streamProgress.showAfter)
}

func (m Model) renderStreamProgress(width int) string {
	bar := m.streamProgress.bar
	bar.SetWidth(maxInt(10, width-4))
	return m.styles.ProgressBarStyle.Width(width).Render(bar.ViewAs(m.streamProgressPercent()))
}

func (m *Model) resetStreamProgress() {
	if m.streamProgress.active && m.streamProgress.tokenEst > 0 {
		m.streamProgress.recordLength(m.streamProgress.tokenEst)
	}
	m.streamProgress.active = false
	m.streamProgress.showAfter = time.Time{}
	m.streamProgress.tokenEst = 0
	m.streamProgress.tokenMax = 0
	m.turnStart = time.Time{}
}

func (m Model) updateStreamProgressMsgs(msg tea.Msg) (Model, tea.Cmd) {
	if !m.streamProgress.active { return m, nil }
	var cmd tea.Cmd
	switch msg.(type) {
	case stopwatch.TickMsg, stopwatch.StartStopMsg, stopwatch.ResetMsg:
		m.streamProgress.stopwatch, cmd = m.streamProgress.stopwatch.Update(msg)
	}
	return m, cmd
}

func formatElapsed(d time.Duration) string {
	if d < 0 { d = 0 }
	if d.Seconds() < 60 { return fmt.Sprintf("%.1fs", d.Seconds()) }
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}
