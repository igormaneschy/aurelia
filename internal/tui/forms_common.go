package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	tea1 "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

type huhFormKind int

const (
	formKindModelProvider huhFormKind = iota
	formKindModelName
	formKindCwd
	formKindNewSession
	formKindConfirm
)

type huhForm struct {
	kind     huhFormKind
	form     *huh.Form
	selected string
	catalog  modelCatalog
	provider string
}

type formInternalMsg struct {
	inner tea1.Msg
}

func (hf *huhForm) isModelForm() bool {
	return hf != nil && (hf.kind == formKindModelProvider || hf.kind == formKindModelName)
}

func (hf *huhForm) view() string {
	if hf == nil || hf.form == nil {
		return ""
	}
	return hf.form.View()
}

func (hf *huhForm) init() tea.Cmd {
	if hf == nil || hf.form == nil {
		return nil
	}
	return wrapFormCmd(hf.form.Init())
}

// initActiveForm initializes the huh form and replays the last terminal size.
// Huh selects render zero options until they receive a WindowSizeMsg (v1 bridge).
func (m Model) initActiveForm() tea.Cmd {
	if m.activeForm == nil {
		return nil
	}
	initCmd := m.activeForm.init()
	if m.width <= 0 || m.height <= 0 {
		return initCmd
	}
	width, height := m.width, m.height
	return tea.Sequence(initCmd, func() tea.Msg {
		return tea.WindowSizeMsg{Width: width, Height: height}
	})
}

func (hf *huhForm) update(msg tea.Msg) tea.Cmd {
	if hf == nil || hf.form == nil {
		return nil
	}
	bridged := bridgeToHuhMsg(msg)
	if bridged == nil {
		return nil
	}
	_, cmd := hf.form.Update(bridged)
	return wrapFormCmd(cmd)
}

func (hf *huhForm) completed() bool {
	return hf != nil && hf.form != nil && hf.form.State == huh.StateCompleted
}

func (hf *huhForm) aborted() bool {
	return hf != nil && hf.form != nil && hf.form.State == huh.StateAborted
}

func (m Model) closeForm() Model {
	m.formOpen = false
	m.activeForm = nil
	return m
}

func (m Model) handleFormKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.activeForm == nil {
		return m, nil, false
	}
	switch m.activeForm.kind {
	case formKindModelProvider, formKindModelName:
		return m.handleModelWizardKey(msg)
	case formKindCwd:
		return m.handleCwdFormKey(msg)
	case formKindNewSession:
		return m.handleNewSessionFormKey(msg)
	case formKindConfirm:
		return m.handleConfirmFormKey(msg)
	default:
		return m, nil, false
	}
}

func (m Model) advanceForm(hf *huhForm) (Model, tea.Cmd) {
	switch hf.kind {
	case formKindModelProvider, formKindModelName:
		return m.advanceModelWizard(hf)
	case formKindCwd:
		return m.advanceCwdForm(hf)
	case formKindNewSession:
		return m.advanceNewSessionForm(hf)
	case formKindConfirm:
		return m.advanceConfirmForm(hf)
	default:
		return m, nil
	}
}

func (m Model) updateActiveForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.formOpen || m.activeForm == nil {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(inputTextareaWidth(msg.Width))
		contentWidth := m.contentWidth()
		if m.viewportSet {
			m.viewport.SetWidth(contentWidth)
			m.viewport.SetHeight(viewportHeightForTerminal(msg.Height))
			m.updateViewport()
		}
		m.resizeSidebarTable()
	case formInternalMsg:
	case tea.KeyMsg:
		if next, cmd, handled := m.handleFormKey(msg); handled {
			return next, cmd
		}
	default:
		return m, nil
	}
	cmd := m.activeForm.update(msg)
	if m.activeForm.completed() {
		return m.advanceForm(m.activeForm)
	}
	if m.activeForm.aborted() {
		m = m.closeForm()
		return m, nil
	}
	return m, cmd
}

func (m Model) renderFormOverlay(bg string) string {
	if !m.formOpen || m.activeForm == nil {
		return bg
	}
	return m.overlayPanel(bg, m.activeForm.view())
}

func wrapFormCmd(cmd tea1.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		inner := cmd()
		if inner == nil {
			return nil
		}
		return formInternalMsg{inner: inner}
	}
}

func bridgeToHuhMsg(msg tea.Msg) tea1.Msg {
	switch msg := msg.(type) {
	case formInternalMsg:
		return msg.inner
	case tea.WindowSizeMsg:
		return tea1.WindowSizeMsg{Width: msg.Width, Height: msg.Height}
	case tea.KeyMsg:
		return bridgeKeyMsg(msg)
	default:
		return nil
	}
}

func bridgeKeyMsg(msg tea.KeyMsg) tea1.KeyMsg {
	s := msg.String()
	if key, ok := v1KeyByName[s]; ok {
		return key
	}
	parts := strings.Split(s, "+")
	if len(parts) == 1 {
		return tea1.KeyMsg{Type: tea1.KeyRunes, Runes: []rune(parts[0])}
	}
	keyPart := parts[len(parts)-1]
	var alt bool
	for _, part := range parts[:len(parts)-1] {
		switch part {
		case "alt":
			alt = true
		case "ctrl":
			if ctrlKey, ok := v1CtrlKeys[keyPart]; ok {
				k := ctrlKey
				k.Alt = alt
				return k
			}
		}
	}
	if key, ok := v1KeyByName[keyPart]; ok {
		key.Alt = alt
		return key
	}
	return tea1.KeyMsg{Type: tea1.KeyRunes, Runes: []rune(keyPart), Alt: alt}
}

var v1KeyByName = map[string]tea1.KeyMsg{
	"enter": {Type: tea1.KeyEnter}, "esc": {Type: tea1.KeyEscape},
	"up": {Type: tea1.KeyUp}, "down": {Type: tea1.KeyDown},
	"left": {Type: tea1.KeyLeft}, "right": {Type: tea1.KeyRight},
	"tab": {Type: tea1.KeyTab}, "backspace": {Type: tea1.KeyBackspace},
	"delete": {Type: tea1.KeyDelete}, "space": {Type: tea1.KeySpace},
	"pgup": {Type: tea1.KeyPgUp}, "pgdown": {Type: tea1.KeyPgDown},
	"home": {Type: tea1.KeyHome}, "end": {Type: tea1.KeyEnd},
	"shift+tab": {Type: tea1.KeyShiftTab},
}

var v1CtrlKeys = map[string]tea1.KeyMsg{
	"n": {Type: tea1.KeyCtrlN}, "p": {Type: tea1.KeyCtrlP},
	"j": {Type: tea1.KeyCtrlJ}, "m": {Type: tea1.KeyCtrlM},
	"o": {Type: tea1.KeyCtrlO},
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}