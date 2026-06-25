package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/huh"
)

const newSessionFormHints = "tab next · enter submit · esc cancel"

func isBareNewSessionCommand(text string) bool {
	return strings.TrimSpace(text) == "/new"
}

func (c modelCatalog) allModelOptions() []huh.Option[string] {
	opts := []huh.Option[string]{huh.NewOption("PI default (auto)", "auto")}
	for _, provider := range c.providers {
		for _, model := range c.byProvider[provider] {
			value := model
			label := model
			if provider != "other" {
				value = provider + "/" + model
				label = value
			}
			opts = append(opts, huh.NewOption(label, value))
		}
	}
	return opts
}

func newNewSessionForm(catalog modelCatalog, defaultName string) *huhForm {
	hf := &huhForm{
		kind:        formKindNewSession,
		catalog:     catalog,
		sessionName: defaultName,
	}
	hf.selected = "auto"
	options := catalog.allModelOptions()
	if len(options) == 0 {
		options = []huh.Option[string]{huh.NewOption("PI default (auto)", "auto")}
	}
	hf.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Session name").
				Description(newSessionFormHints).
				Placeholder("my-project").
				Value(&hf.sessionName),
			huh.NewSelect[string]().
				Title("Model").
				Description("Applied after the session is created").
				Options(options...).
				Value(&hf.selected),
		),
	).WithShowHelp(true).WithWidth(60)
	return hf
}

func (m Model) openNewSessionForm() (Model, tea.Cmd) {
	catalog := catalogFromModels(modelFallbackList(m.activeModel))
	name := fmt.Sprintf("session-%d", len(m.sessions))
	m.formOpen = true
	m.activeForm = newNewSessionForm(catalog, name)
	return m, tea.Batch(m.initActiveForm(), fetchTUIModels(m.ipcClient, m.activeSession))
}

func (m Model) applyNewSessionCatalog(msg tuiModelsMsg) Model {
	if !m.formOpen || m.activeForm == nil || m.activeForm.kind != formKindNewSession {
		return m
	}
	name := strings.TrimSpace(m.activeForm.sessionName)
	model := m.activeForm.selected
	catalog := msg.catalog
	if catalog.providerCount() == 0 || msg.err != nil {
		catalog = catalogFromModels(modelFallbackList(m.activeModel))
	}
	m.activeForm = newNewSessionForm(catalog, name)
	if name != "" {
		m.activeForm.sessionName = name
	}
	if model != "" {
		m.activeForm.selected = model
	}
	return m
}

func (m Model) handleNewSessionFormKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if msg.String() == "esc" {
		return m.closeForm(), nil, true
	}
	return m, nil, false
}

func (m Model) advanceNewSessionForm(hf *huhForm) (Model, tea.Cmd) {
	name := strings.TrimSpace(hf.sessionName)
	if name == "" {
		return m, nil
	}
	model := strings.TrimSpace(hf.selected)
	m.pendingSessionModel = model
	m = m.closeForm()
	return m, createTUISession(m.ipcClient, name)
}