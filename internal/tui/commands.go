package tui

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	tea1 "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/igormaneschy/aurelia/internal/ipc"
)

type huhFormKind int

const (
	formKindModelProvider huhFormKind = iota
	formKindModelName
)

type modelCatalog struct {
	byProvider map[string][]string
	providers  []string
}

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

func isBareModelCommand(text string) bool {
	return strings.TrimSpace(text) == "/model"
}

func modelFallbackList(activeModel string) []string {
	models := []string{"auto"}
	if activeModel != "" {
		models = append(models, activeModel)
	}
	return models
}

func uniqueSortedModels(models []string) []string {
	seen := make(map[string]struct{}, len(models))
	out := make([]string, 0, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, ok := seen[model]; ok {
			continue
		}
		seen[model] = struct{}{}
		out = append(out, model)
	}
	sort.Strings(out)
	for i, model := range out {
		if model == "auto" {
			if i > 0 {
				copy(out[1:i+1], out[0:i])
				out[0] = "auto"
			}
			break
		}
	}
	return out
}

var modelIDPattern = regexp.MustCompile("`([^`]+)`")
var providerHeaderPattern = regexp.MustCompile(`^([a-zA-Z0-9_.-]+):$`)

func catalogFromModels(models []string) modelCatalog {
	catalog := modelCatalog{byProvider: make(map[string][]string)}
	for _, model := range uniqueSortedModels(models) {
		if model == "auto" {
			continue
		}
		provider, id := splitProviderModel(model)
		catalog.byProvider[provider] = append(catalog.byProvider[provider], id)
	}
	catalog.finalize()
	return catalog
}

func catalogFromDaemonText(body string) modelCatalog {
	catalog := modelCatalog{byProvider: make(map[string][]string)}
	currentProvider := ""
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := providerHeaderPattern.FindStringSubmatch(trimmed); len(m) == 2 {
			currentProvider = m[1]
			continue
		}
		matches := modelIDPattern.FindStringSubmatch(trimmed)
		if len(matches) < 2 {
			continue
		}
		id := strings.TrimSpace(matches[1])
		if id == "" {
			continue
		}
		provider := currentProvider
		if provider == "" {
			provider, id = splitProviderModel(id)
		}
		catalog.byProvider[provider] = append(catalog.byProvider[provider], id)
	}
	catalog.finalize()
	return catalog
}

func splitProviderModel(model string) (provider, id string) {
	model = strings.TrimSpace(model)
	if before, after, ok := strings.Cut(model, "/"); ok && before != "" && after != "" {
		return before, after
	}
	return "other", model
}

func (c *modelCatalog) finalize() {
	if len(c.byProvider) == 0 {
		return
	}
	c.providers = make([]string, 0, len(c.byProvider))
	for provider := range c.byProvider {
		c.providers = append(c.providers, provider)
	}
	sort.Strings(c.providers)
	for provider := range c.byProvider {
		c.byProvider[provider] = uniqueSortedModels(c.byProvider[provider])
	}
}

func (c modelCatalog) providerCount() int {
	if len(c.providers) > 0 {
		return len(c.providers)
	}
	return 0
}

func (c modelCatalog) providerOptions() []huh.Option[string] {
	opts := []huh.Option[string]{huh.NewOption("PI default (auto)", "auto")}
	for _, provider := range c.providers {
		count := len(c.byProvider[provider])
		label := provider
		if count > 0 {
			label = fmt.Sprintf("%s (%d)", provider, count)
		}
		opts = append(opts, huh.NewOption(label, provider))
	}
	return opts
}

func (c modelCatalog) modelOptions(provider string) []huh.Option[string] {
	models := c.byProvider[provider]
	opts := make([]huh.Option[string], 0, len(models))
	for _, model := range models {
		opts = append(opts, huh.NewOption(model, model))
	}
	return opts
}

func modelsFromDaemonText(body string) []string {
	catalog := catalogFromDaemonText(body)
	if catalog.providerCount() == 0 {
		return nil
	}
	var ids []string
	for _, provider := range catalog.providers {
		for _, model := range catalog.byProvider[provider] {
			ids = append(ids, model)
		}
	}
	return uniqueSortedModels(append([]string{"auto"}, ids...))
}

func fetchTUIModels(client *ipc.Client, chatID int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := contextWithTimeout(30 * time.Second)
		defer cancel()
		events, err := client.SendAndWait(ctx, ipc.IPCMessage{
			Type:      ipc.MsgTypeCommand,
			ChatID:    chatID,
			ThreadID:  0,
			UserID:    int64(os.Getuid()),
			Text:      "/model",
			RequestID: fmt.Sprintf("tui-models-%d", time.Now().UnixNano()),
		})
		if err != nil {
			return tuiModelsMsg{err: err}
		}
		for _, ev := range events {
			if ev.Type != ipc.EventTypeMessage || ev.Body == "" {
				continue
			}
			catalog := catalogFromDaemonText(ev.Body)
			if catalog.providerCount() > 0 {
				return tuiModelsMsg{catalog: catalog}
			}
			models := modelsFromDaemonText(ev.Body)
			if len(models) > 0 {
				return tuiModelsMsg{catalog: catalogFromModels(models)}
			}
		}
		return tuiModelsMsg{}
	}
}

func currentModelSelection(activeModel string) string {
	if activeModel == "" {
		return "auto"
	}
	return activeModel
}

func (c modelCatalog) providerForModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" || model == "auto" {
		return ""
	}
	if provider, id := splitProviderModel(model); provider != "other" {
		if containsString(c.byProvider[provider], id) {
			return provider
		}
	}
	for _, provider := range c.providers {
		if containsString(c.byProvider[provider], model) {
			return provider
		}
	}
	return ""
}

func newModelProviderForm(catalog modelCatalog, currentModel string) *huhForm {
	selected := "auto"
	if provider := catalog.providerForModel(currentModel); provider != "" {
		selected = provider
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select provider").
				Description("Pick a provider, then choose a model in the next step").
				Options(catalog.providerOptions()...).
				Value(&selected),
		),
	).WithShowHelp(true).WithWidth(60)
	return &huhForm{
		kind:     formKindModelProvider,
		form:     form,
		selected: selected,
		catalog:  catalog,
	}
}

func newModelNameForm(catalog modelCatalog, provider, currentModel string) *huhForm {
	models := catalog.byProvider[provider]
	selected := currentModel
	if selected == "" || !containsString(models, selected) {
		if len(models) > 0 {
			selected = models[0]
		}
	}
	options := catalog.modelOptions(provider)
	if len(options) == 0 {
		options = []huh.Option[string]{huh.NewOption("auto", "auto")}
		selected = "auto"
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Select model (%s)", provider)).
				Description("Choose a model for this session").
				Options(options...).
				Value(&selected),
		),
	).WithShowHelp(true).WithWidth(60)
	return &huhForm{
		kind:     formKindModelName,
		form:     form,
		selected: selected,
		catalog:  catalog,
		provider: provider,
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
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

func (hf *huhForm) chosenModel() string {
	if hf == nil {
		return ""
	}
	return strings.TrimSpace(hf.selected)
}

func (m Model) openModelSelect() (Model, tea.Cmd) {
	catalog := catalogFromModels(modelFallbackList(m.activeModel))
	m.formOpen = true
	m.activeForm = newModelProviderForm(catalog, m.activeModel)
	return m, tea.Batch(m.activeForm.init(), fetchTUIModels(m.ipcClient, m.activeSession))
}

func (m Model) refreshModelSelectForm(msg tuiModelsMsg) Model {
	if !m.formOpen || m.activeForm == nil {
		return m
	}
	catalog := msg.catalog
	if catalog.providerCount() == 0 {
		catalog = catalogFromModels(modelFallbackList(m.activeModel))
	}
	switch m.activeForm.kind {
	case formKindModelProvider:
		current := m.activeForm.chosenModel()
		if current == "" {
			current = currentModelSelection(m.activeModel)
		}
		m.activeForm = newModelProviderForm(catalog, current)
	case formKindModelName:
		provider := m.activeForm.provider
		current := m.activeForm.chosenModel()
		if current == "" {
			current = currentModelSelection(m.activeModel)
		}
		m.activeForm = newModelNameForm(catalog, provider, current)
	}
	return m
}

func (m Model) closeForm() Model {
	m.formOpen = false
	m.activeForm = nil
	return m
}

func (m Model) submitModelSelection(model string) (Model, tea.Cmd) {
	model = strings.TrimSpace(model)
	if model == "" {
		return m, nil
	}
	m = m.closeForm()
	m.waiting = true
	m.streamID++
	return m, tea.Batch(m.sendCommand("/model "+model), spinnerTickCmd())
}

func (m Model) advanceModelWizard(hf *huhForm) (Model, tea.Cmd) {
	choice := hf.chosenModel()
	switch hf.kind {
	case formKindModelProvider:
		if choice == "" || choice == "auto" {
			return m.submitModelSelection("auto")
		}
		m.formOpen = true
		m.activeForm = newModelNameForm(hf.catalog, choice, m.activeModel)
		return m, m.activeForm.init()
	case formKindModelName:
		return m.submitModelSelection(choice)
	default:
		return m.submitModelSelection(choice)
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
	default:
		if _, ok := msg.(tea.KeyMsg); !ok {
			return m, nil
		}
	}
	cmd := m.activeForm.update(msg)
	if m.activeForm.completed() {
		return m.advanceModelWizard(m.activeForm)
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