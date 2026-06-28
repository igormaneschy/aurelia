package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
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

	confirmAction confirmAction
	confirmed     bool

	sessionName string

	deleteChatID int64
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
	return hf.form.Init()
}

// initActiveForm initializes the huh form and replays the last terminal size.
// Huh selects render zero options until they receive a WindowSizeMsg.
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
	_, cmd := hf.form.Update(msg)
	return cmd
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
			m.syncViewportDimensions()
			m.updateViewport()
		}
		m.resizeSidebarTable()
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
	return m.renderModalOverlay(bg, m.activeForm.view(), false)
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}