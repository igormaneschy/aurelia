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

type huhForm struct {
	form     *huh.Form
	selected string
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

func modelsFromDaemonText(body string) []string {
	matches := modelIDPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		ids = append(ids, match[1])
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
			models := modelsFromDaemonText(ev.Body)
			if len(models) > 0 {
				return tuiModelsMsg{models: models}
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

func newModelSelectForm(models []string, current string, onSubmit func(string) tea.Cmd) *huhForm {
	_ = onSubmit
	models = uniqueSortedModels(models)
	if len(models) == 0 {
		models = modelFallbackList(current)
	}
	selected := currentModelSelection(current)
	options := make([]huh.Option[string], 0, len(models))
	for _, model := range models {
		options = append(options, huh.NewOption(model, model))
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select model").
				Description("Choose a model for this session").
				Options(options...).
				Value(&selected),
		),
	).WithShowHelp(true).WithWidth(60)
	return &huhForm{form: form, selected: selected}
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
	if strings.TrimSpace(hf.selected) != "" {
		return hf.selected
	}
	return "auto"
}

func (m Model) openModelSelect() (Model, tea.Cmd) {
	models := modelFallbackList(m.activeModel)
	m.formOpen = true
	m.activeForm = newModelSelectForm(models, m.activeModel, nil)
	return m, tea.Batch(m.activeForm.init(), fetchTUIModels(m.ipcClient, m.activeSession))
}

func (m Model) refreshModelSelectForm(models []string) Model {
	if !m.formOpen || m.activeForm == nil {
		return m
	}
	current := m.activeForm.chosenModel()
	if current == "" {
		current = currentModelSelection(m.activeModel)
	}
	m.activeForm = newModelSelectForm(models, current, nil)
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
		return m.submitModelSelection(m.activeForm.chosenModel())
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
}