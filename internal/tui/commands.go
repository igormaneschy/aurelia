package tui

import (
	"encoding/json"
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

const (
	modelWizardProviderHints = "↑↓ navigate · enter select · r refresh · esc cancel"
	modelWizardModelHints    = "↑↓ navigate · enter select · b back · r refresh · esc cancel"
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

func (c modelCatalog) totalModels() int {
	n := 0
	for _, models := range c.byProvider {
		n += len(models)
	}
	return n
}

func preferRicherCatalog(a, b modelCatalog) modelCatalog {
	if b.totalModels() > a.totalModels() || b.providerCount() > a.providerCount() {
		return b
	}
	return a
}

// catalogProviderKey resolves wizard provider selections to a catalog key.
// It tolerates shorthand picks like "llamacpp" for "llamacpp-tailscale".
func (c modelCatalog) catalogProviderKey(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return ""
	}
	if containsString(c.providers, provider) {
		return provider
	}
	lower := strings.ToLower(provider)
	for _, p := range c.providers {
		if strings.ToLower(p) == lower {
			return p
		}
	}
	var best string
	for _, p := range c.providers {
		pl := strings.ToLower(p)
		if strings.HasPrefix(pl, lower) || strings.Contains(pl, lower) {
			if best == "" || len(p) < len(best) {
				best = p
			}
		}
	}
	if best != "" {
		return best
	}
	return provider
}

func (c modelCatalog) modelsForProvider(provider, currentModel string) []string {
	key := c.catalogProviderKey(provider)
	if models := c.byProvider[key]; len(models) > 0 {
		return models
	}
	if currentModel != "" {
		if p := c.providerForModel(currentModel); p != "" {
			if models := c.byProvider[p]; len(models) > 0 {
				return models
			}
		}
	}
	return c.byProvider[key]
}

func (c modelCatalog) modelOptions(provider string) []huh.Option[string] {
	models := c.modelsForProvider(provider, "")
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

func catalogFromIPCEvents(events []ipc.IPCEvent) modelCatalog {
	if catalog := catalogFromIPCModels(events); catalog.providerCount() > 0 {
		return catalog
	}
	for _, ev := range events {
		if ev.Type != ipc.EventTypeMessage || ev.Body == "" {
			continue
		}
		catalog := catalogFromDaemonText(ev.Body)
		if catalog.providerCount() > 0 {
			return catalog
		}
		models := modelsFromDaemonText(ev.Body)
		if len(models) > 0 {
			return catalogFromModels(models)
		}
	}
	return modelCatalog{}
}

func catalogFromIPCModels(events []ipc.IPCEvent) modelCatalog {
	for _, ev := range events {
		if ev.Type != ipc.EventTypeModels || ev.Body == "" {
			continue
		}
		var entries []ipc.TUIModelEntry
		if err := json.Unmarshal([]byte(ev.Body), &entries); err != nil {
			continue
		}
		catalog := modelCatalog{byProvider: make(map[string][]string)}
		for _, entry := range entries {
			provider := strings.TrimSpace(entry.Provider)
			id := strings.TrimSpace(entry.ID)
			if provider == "" || id == "" {
				continue
			}
			catalog.byProvider[provider] = append(catalog.byProvider[provider], id)
		}
		catalog.finalize()
		if catalog.providerCount() > 0 {
			return catalog
		}
	}
	return modelCatalog{}
}

func fetchTUIModelCatalog(client *ipc.Client, chatID int64) (modelCatalog, error) {
	ctx, cancel := contextWithTimeout(30 * time.Second)
	defer cancel()
	events, err := client.SendAndWait(ctx, ipc.IPCMessage{
		Type:      ipc.MsgTypeModels,
		ChatID:    chatID,
		ThreadID:  0,
		UserID:    int64(os.Getuid()),
		RequestID: fmt.Sprintf("tui-models-%d", time.Now().UnixNano()),
	})
	if err != nil {
		return modelCatalog{}, err
	}
	catalog := catalogFromIPCModels(events)
	if catalog.providerCount() > 0 {
		return catalog, nil
	}
	return modelCatalog{}, fmt.Errorf("no models in catalog response")
}

func fetchTUIModels(client *ipc.Client, chatID int64) tea.Cmd {
	return func() tea.Msg {
		catalog, err := fetchTUIModelCatalog(client, chatID)
		if err != nil {
			return tuiModelsMsg{err: err}
		}
		if catalog.providerCount() > 0 {
			return tuiModelsMsg{catalog: catalog}
		}
		return tuiModelsMsg{}
	}
}

func refreshAndFetchTUIModels(client *ipc.Client, chatID int64) tea.Cmd {
	return func() tea.Msg {
		// /model already force-refreshes the PI catalog via bridge ListModels(refresh=true).
		catalog, err := fetchTUIModelCatalog(client, chatID)
		if err != nil {
			return tuiModelsMsg{err: err, reloaded: true}
		}
		if catalog.providerCount() > 0 {
			return tuiModelsMsg{catalog: catalog, reloaded: true}
		}
		return tuiModelsMsg{reloaded: true}
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
	hf := &huhForm{
		kind:    formKindModelProvider,
		catalog: catalog,
	}
	if provider := catalog.providerForModel(currentModel); provider != "" {
		hf.selected = provider
	} else {
		hf.selected = "auto"
	}
	hf.form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select provider").
				Description(modelWizardProviderHints).
				Options(catalog.providerOptions()...).
				Value(&hf.selected),
		),
	).WithShowHelp(true).WithWidth(60)
	return hf
}

func newModelNameForm(catalog modelCatalog, provider, currentModel string) *huhForm {
	provider = catalog.catalogProviderKey(provider)
	models := catalog.modelsForProvider(provider, currentModel)
	hf := &huhForm{
		kind:     formKindModelName,
		catalog:  catalog,
		provider: provider,
	}
	hf.selected = currentModel
	if hf.selected == "" || !containsString(models, hf.selected) {
		if len(models) > 0 {
			hf.selected = models[0]
		}
	}
	options := catalog.modelOptions(provider)
	if len(options) == 0 {
		options = []huh.Option[string]{huh.NewOption("auto", "auto")}
		hf.selected = "auto"
	}
	hf.form = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Select model (%s)", provider)).
				Description(modelWizardModelHints).
				Options(options...).
				Value(&hf.selected),
		),
	).WithShowHelp(true).WithWidth(60)
	return hf
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
	return m, tea.Batch(m.initActiveForm(), fetchTUIModels(m.ipcClient, m.activeSession))
}

func (m Model) applyWizardCatalog(msg tuiModelsMsg) Model {
	if !m.formOpen || m.activeForm == nil {
		return m
	}
	catalog := msg.catalog
	if catalog.providerCount() == 0 || msg.err != nil {
		if msg.reloaded && m.activeForm.catalog.providerCount() > 0 {
			catalog = m.activeForm.catalog
		} else {
			catalog = catalogFromModels(modelFallbackList(m.activeModel))
		}
	}
	return m.refreshModelSelectForm(catalog)
}

func (m Model) refreshModelSelectForm(catalog modelCatalog) Model {
	if !m.formOpen || m.activeForm == nil {
		return m
	}
	switch m.activeForm.kind {
	case formKindModelProvider:
		selected := m.activeForm.chosenModel()
		if selected == "" {
			selected = "auto"
		}
		m.activeForm = newModelProviderForm(catalog, currentModelSelection(m.activeModel))
		if selected == "auto" || containsString(catalog.providers, selected) {
			m.activeForm.selected = selected
		}
	case formKindModelName:
		originalProvider := m.activeForm.provider
		provider := catalog.catalogProviderKey(originalProvider)
		current := m.activeForm.chosenModel()
		if current == "" {
			current = currentModelSelection(m.activeModel)
		}
		if len(catalog.modelsForProvider(provider, current)) == 0 {
			m.activeForm = newModelProviderForm(catalog, currentModelSelection(m.activeModel))
			if resolved := catalog.catalogProviderKey(originalProvider); containsString(catalog.providers, resolved) {
				m.activeForm.selected = resolved
			}
		} else {
			m.activeForm = newModelNameForm(catalog, provider, current)
		}
	}
	return m
}

func (m Model) closeForm() Model {
	m.formOpen = false
	m.activeForm = nil
	return m
}

func (m Model) backToModelProviderForm() (Model, tea.Cmd) {
	if m.activeForm == nil || m.activeForm.kind != formKindModelName {
		return m, nil
	}
	provider := m.activeForm.provider
	catalog := m.activeForm.catalog
	m.activeForm = newModelProviderForm(catalog, provider)
	m.activeForm.selected = provider
	return m, m.initActiveForm()
}

func (m Model) reloadModelWizard() (Model, tea.Cmd) {
	if !m.formOpen {
		return m, nil
	}
	return m, refreshAndFetchTUIModels(m.ipcClient, m.activeSession)
}

// handleModelWizardKey handles wizard-only shortcuts before huh sees the key.
// Returns handled=true when the key was consumed.
func (m Model) handleModelWizardKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "esc":
		return m.closeForm(), nil, true
	case "r":
		next, cmd := m.reloadModelWizard()
		return next, cmd, true
	case "b":
		if m.activeForm != nil && m.activeForm.kind == formKindModelName {
			next, cmd := m.backToModelProviderForm()
			return next, cmd, true
		}
	}
	return m, nil, false
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
		catalog := hf.catalog
		if m.activeForm != nil {
			catalog = preferRicherCatalog(hf.catalog, m.activeForm.catalog)
		}
		provider := catalog.catalogProviderKey(choice)
		m.formOpen = true
		m.activeForm = newModelNameForm(catalog, provider, m.activeModel)
		return m, m.initActiveForm()
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
	case tea.KeyMsg:
		if next, cmd, handled := m.handleModelWizardKey(msg); handled {
			return next, cmd
		}
	default:
		return m, nil
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