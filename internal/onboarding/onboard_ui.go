package onboarding

import (
	"fmt"
	"strings"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/deps"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

func (u *onboardingUI) View(resolver *runtime.PathResolver) string {
	var b strings.Builder
	b.WriteString("\x1b[2J\x1b[H")

	// Use a compact header on the model step so the viewport has room.
	if u.step == stepLLMModel {
		b.WriteString(colorize("AURELIA", colorBlue))
		_, _ = fmt.Fprintf(&b, " — Step %d/13\n", int(u.step)+1)
	} else {
		b.WriteString(renderOnboardingHeader())
		_, _ = fmt.Fprintf(&b, "Config file: %s\n", resolver.AppConfig())
		_, _ = fmt.Fprintf(&b, "Step %d/13\n\n", int(u.step)+1)
	}
	if u.message != "" {
		b.WriteString(colorize("! "+u.message, colorBlue))
		b.WriteString("\n\n")
	}

	switch u.step {
	case stepDependencies:
		b.WriteString("Dependencies\n\n")
		if u.depsResult == nil {
			r := deps.CheckAll()
			u.depsResult = &r
		}
		b.WriteString(renderDepsCheck(*u.depsResult))
		b.WriteString("\nNote: The PI SDK (inference engine) will be installed automatically.\n")
		b.WriteString("No need to install the PI CLI (pi) or run pi /login.\n")
		if u.depsResult.AllOK {
			b.WriteString("\nPress Enter to continue.\n")
		} else {
			b.WriteString("\n" + colorize("Install missing required dependencies before continuing.", colorRed) + "\n")
			b.WriteString("Press Ctrl+R to re-check. Press Ctrl+C to cancel.\n")
		}
	case stepLLMProvider:
		b.WriteString("LLM Provider\n")
		b.WriteString("Select the main chat model provider.\n\n")
		b.WriteString(renderMenu(llmProviderLabels(), u.menuIndex))
		b.WriteString("\nUse arrows and Enter.\n")
	case stepAnthropicAuthMode:
		b.WriteString("Anthropic Auth Mode\n")
		b.WriteString("Choose whether Anthropic should use an API key or a subscription (Max plan).\n\n")
		b.WriteString(renderMenu([]string{"API key", "Subscription (Max plan)"}, u.menuIndex))
		b.WriteString("\nUse arrows and Enter. Use left to go back.\n")
	case stepLLMKey:
		b.WriteString(u.renderInputStep(llmKeyLabel(u.cfg.LLMProvider), llmKeyHelp(u.cfg.LLMProvider), true))
	case stepLLMModel:
		b.WriteString("LLM Model\n")
		b.WriteString("Select the model for the chosen provider.\n\n")
		if usesProviderModelSearch(u.cfg) {
			_, _ = fmt.Fprintf(&b, "Search: %s\n", u.modelFilter)
		}
		_, _ = fmt.Fprintf(&b, "Capability filter: %s\n", u.modelCapabilityLabel())
		_, _ = fmt.Fprintf(&b, "Showing %d of %d models\n\n", len(u.modelOptions), len(u.allModelOptions))

		// Calculate viewport: terminal height minus fixed chrome lines.
		// Fixed chrome above the list: banner (~18) + config/step (3) + message (0-2) + model header (6-7).
		// Fixed chrome below: catalog source + help text (~4-5).
		// Instead of guessing, count what we've already written + the footer.
		linesAbove := strings.Count(b.String(), "\n")
		linesBelow := 5 // "\nCatalog source: ...\n\nType to filter...\n"
		viewportHeight := u.termHeight - linesAbove - linesBelow
		if viewportHeight < 5 {
			viewportHeight = 5
		}

		b.WriteString(renderModelMenuViewport(u.modelOptions, u.menuIndex, &u.scrollOffset, viewportHeight))
		_, _ = fmt.Fprintf(&b, "\nCatalog source: %s\n", u.modelSource)
		if usesProviderModelSearch(u.cfg) {
			b.WriteString("\nType to filter by model or provider. Use right to cycle capability filters. Use arrows and Enter. Backspace removes filter. Use left to go back.\n")
		} else {
			b.WriteString("\nUse right to cycle capability filters. Use arrows and Enter. Use left to go back.\n")
		}
	case stepSTTKey:
		b.WriteString(u.renderInputStep("Groq API key", "Used for speech transcription.", true))
	case stepTelegramToken:
		b.WriteString(u.renderInputStep("Telegram bot token", "Used by the Telegram bot interface.", true))
	case stepTelegramUsers:
		b.WriteString(u.renderInputStep("Telegram allowed user IDs", "Comma-separated list, e.g. 123,456.", false))
	case stepRuntimeMaxIterations:
		b.WriteString(u.renderInputStep("Max iterations", "Maximum loop iterations per run.", false))
	case stepVisionModel:
		b.WriteString(u.renderInputStep("Vision fallback model", "Optional: model for image inputs (e.g. qwen3.5-plus or opencode-go/qwen3.5-plus). Leave empty to skip.", false))
	case stepReview:
		b.WriteString("Review & Save\n")
		b.WriteString("Check the config before saving.\n\n")
		_, _ = fmt.Fprintf(&b, "LLM provider: %s\n", strings.ToUpper(u.cfg.LLMProvider))
		if config.NormalizeProvider(u.cfg.LLMProvider) == "anthropic" {
			_, _ = fmt.Fprintf(&b, "Anthropic auth mode: %s\n", u.cfg.AnthropicAuthMode)
		}
		_, _ = fmt.Fprintf(&b, "LLM model: %s\n", u.cfg.LLMModel)
		if usesAnthropicSubscription(u.cfg) {
			_, _ = fmt.Fprintf(&b, "Anthropic auth: subscription (no API key)\n")
		} else {
			_, _ = fmt.Fprintf(&b, "%s: %s\n", llmKeyLabel(u.cfg.LLMProvider), maskSecret(currentLLMKey(u.cfg)))
		}
		_, _ = fmt.Fprintf(&b, "Groq API key (STT): %s\n", maskSecret(u.cfg.GroqAPIKey))
		_, _ = fmt.Fprintf(&b, "Telegram bot token: %s\n", maskSecret(u.cfg.TelegramBotToken))
		_, _ = fmt.Fprintf(&b, "Telegram allowed user IDs: %s\n", formatInt64List(u.cfg.TelegramAllowedUserIDs))
		_, _ = fmt.Fprintf(&b, "Max iterations: %d\n", u.cfg.MaxIterations)
		if u.cfg.VisionModel != "" {
			display := u.cfg.VisionModel
			if u.cfg.VisionProvider != "" {
				display = u.cfg.VisionProvider + "/" + display
			}
			_, _ = fmt.Fprintf(&b, "Vision fallback: %s\n", display)
		} else {
			b.WriteString("Vision fallback: (none)\n")
		}
		b.WriteString("\n")
		b.WriteString(renderMenu(u.reviewOptions, u.menuIndex))
		b.WriteString("\nUse arrows and Enter. Use left to go back. Press Ctrl+C to cancel.\n")
	}

	return b.String()
}

func (u *onboardingUI) renderInputStep(label, help string, secret bool) string {
	var b strings.Builder
	b.WriteString(label)
	b.WriteString("\n")
	b.WriteString(help)
	b.WriteString("\n\n")
	display := u.input
	if secret {
		display = maskForInput(display)
	}
	b.WriteString("> ")
	b.WriteString(display)
	b.WriteString("\n\nType and press Enter. Use left to go back. Press Ctrl+C to cancel.\n")
	return b.String()
}

func (u *onboardingUI) HandleKey(ev keyEvent) (saved bool, cancelled bool, err error) {
	u.message = ""

	switch u.step {
	case stepDependencies:
		return u.handleDepsKey(ev)
	case stepLLMProvider:
		return u.handleMenuKey(ev, llmProviderChoices(), nextOnboardStep(u.cfg, stepLLMProvider), stepLLMProvider)
	case stepAnthropicAuthMode:
		return u.handleAnthropicAuthModeMenuKey(ev)
	case stepLLMModel:
		return u.handleModelMenuKey(ev)
	case stepReview:
		return u.handleReviewKey(ev)
	default:
		return u.handleInputKey(ev)
	}
}

func (u *onboardingUI) ensureDepsChecked() {
	if u.depsResult == nil {
		r := deps.CheckAll()
		u.depsResult = &r
	}
}

func (u *onboardingUI) handleDepsKey(ev keyEvent) (bool, bool, error) {
	u.ensureDepsChecked()
	switch ev.code {
	case keyEnter:
		if u.depsResult.AllOK {
			u.setStep(stepLLMProvider)
		} else {
			u.message = "required dependencies are missing"
		}
	case keyRune:
		// Ctrl+R is rune 18, but in raw mode it comes as byte 18 which won't match keyRune.
		// We handle re-check via the 'r' key as a convenience.
		if ev.r == 'r' || ev.r == 'R' {
			u.depsResult = nil // force re-check on next render
		}
	case keyQuit:
		return false, true, nil
	}
	return false, false, nil
}

func (u *onboardingUI) handleMenuKey(ev keyEvent, values []string, next onboardStep, prev onboardStep) (bool, bool, error) {
	switch ev.code {
	case keyUp:
		u.menuIndex = wrapIndex(u.menuIndex-1, len(values))
	case keyDown:
		u.menuIndex = wrapIndex(u.menuIndex+1, len(values))
	case keyEnter:
		targetStep := next
		switch u.step {
		case stepLLMProvider:
			u.cfg.LLMProvider = values[u.menuIndex]
			targetStep = nextOnboardStep(u.cfg, stepLLMProvider)
		}
		u.setStep(targetStep)
	case keyLeft:
		if u.step != prev {
			u.setStep(prev)
		}
	case keyQuit:
		return false, true, nil
	}
	return false, false, nil
}

func (u *onboardingUI) handleAnthropicAuthModeMenuKey(ev keyEvent) (bool, bool, error) {
	options := []string{"api_key", "subscription"}
	switch ev.code {
	case keyUp:
		u.menuIndex = wrapIndex(u.menuIndex-1, len(options))
	case keyDown:
		u.menuIndex = wrapIndex(u.menuIndex+1, len(options))
	case keyEnter:
		u.cfg.AnthropicAuthMode = options[u.menuIndex]
		u.setStep(nextOnboardStep(u.cfg, stepAnthropicAuthMode))
	case keyLeft:
		u.setStep(stepLLMProvider)
		u.menuIndex = selectedProviderIndex(u.cfg.LLMProvider)
	case keyQuit:
		return false, true, nil
	}
	return false, false, nil
}

func (u *onboardingUI) handleModelMenuKey(ev keyEvent) (bool, bool, error) {
	if len(u.modelOptions) == 0 {
		u.refreshModelOptions()
	}

	switch ev.code {
	case keyUp:
		u.menuIndex = wrapIndex(u.menuIndex-1, len(u.modelOptions))
	case keyDown:
		u.menuIndex = wrapIndex(u.menuIndex+1, len(u.modelOptions))
	case keyRight:
		u.modelCapability = nextModelCapabilityFilter(u.modelCapability)
		u.applyModelFilter()
	case keyRune:
		if usesProviderModelSearch(u.cfg) {
			u.modelFilter += string(ev.r)
			u.applyModelFilter()
		}
	case keyBackspace:
		if usesProviderModelSearch(u.cfg) && len(u.modelFilter) > 0 {
			u.modelFilter = u.modelFilter[:len(u.modelFilter)-1]
			u.applyModelFilter()
		}
	case keyEnter:
		if len(u.modelOptions) == 0 {
			u.message = "no models available for the selected provider"
			return false, false, nil
		}
		u.cfg.LLMModel = u.modelOptions[u.menuIndex].ID
		u.setStep(stepSTTKey)
	case keyLeft:
		u.setStep(previousOnboardStep(u.cfg, stepLLMModel))
	case keyQuit:
		return false, true, nil
	}
	return false, false, nil
}

func (u *onboardingUI) handleInputKey(ev keyEvent) (bool, bool, error) {
	switch ev.code {
	case keyRune:
		u.input += string(ev.r)
	case keyBackspace:
		if len(u.input) > 0 {
			u.input = u.input[:len(u.input)-1]
		}
	case keyLeft:
		u.setStep(previousOnboardStep(u.cfg, u.step))
	case keyEnter:
		if err := u.commitInput(); err != nil {
			u.message = err.Error()
			return false, false, nil
		}
		u.setStep(nextOnboardStep(u.cfg, u.step))
	case keyQuit:
		return false, true, nil
	}
	return false, false, nil
}

func (u *onboardingUI) handleReviewKey(ev keyEvent) (bool, bool, error) {
	switch ev.code {
	case keyUp:
		u.menuIndex = wrapIndex(u.menuIndex-1, len(u.reviewOptions))
	case keyDown:
		u.menuIndex = wrapIndex(u.menuIndex+1, len(u.reviewOptions))
	case keyLeft:
		u.setStep(stepRuntimeMaxIterations)
	case keyEnter:
		switch u.menuIndex {
		case 0:
			return true, false, nil
		case 1:
			u.setStep(stepRuntimeMaxIterations)
		case 2:
			return false, true, nil
		}
	case keyQuit:
		return false, true, nil
	}
	return false, false, nil
}

func (u *onboardingUI) commitInput() error {
	switch u.step {
	case stepLLMKey:
		setCurrentLLMKey(&u.cfg, strings.TrimSpace(u.input))
	case stepSTTKey:
		u.cfg.GroqAPIKey = strings.TrimSpace(u.input)
	case stepTelegramToken:
		token := strings.TrimSpace(u.input)
		if token == "" {
			return fmt.Errorf("token is required")
		}
		username, err := validateToken(token)
		if err != nil {
			return fmt.Errorf("invalid token: %v", err)
		}
		u.cfg.TelegramBotToken = token
		u.message = fmt.Sprintf("Token valid! Bot: @%s", username)
	case stepTelegramUsers:
		values, err := parseInt64List(u.input)
		if err != nil {
			return err
		}
		u.cfg.TelegramAllowedUserIDs = values
	case stepRuntimeMaxIterations:
		value, err := parsePositiveInt(strings.TrimSpace(u.input), "max iterations")
		if err != nil {
			return err
		}
		u.cfg.MaxIterations = value
	case stepVisionModel:
		raw := strings.TrimSpace(u.input)
		if raw == "" {
			u.cfg.VisionModel = ""
			u.cfg.VisionProvider = ""
		} else if strings.Contains(raw, "/") {
			parts := strings.SplitN(raw, "/", 2)
			u.cfg.VisionProvider = strings.TrimSpace(parts[0])
			u.cfg.VisionModel = strings.TrimSpace(parts[1])
		} else {
			u.cfg.VisionModel = raw
			u.cfg.VisionProvider = ""
		}
	}
	return nil
}

func (u *onboardingUI) currentInputValue() string {
	switch u.step {
	case stepLLMKey:
		return currentLLMKey(u.cfg)
	case stepSTTKey:
		return u.cfg.GroqAPIKey
	case stepTelegramToken:
		return u.cfg.TelegramBotToken
	case stepTelegramUsers:
		return formatInt64CSV(u.cfg.TelegramAllowedUserIDs)
	case stepRuntimeMaxIterations:
		return fmt.Sprintf("%d", u.cfg.MaxIterations)
	case stepVisionModel:
		if u.cfg.VisionModel == "" {
			return ""
		}
		if u.cfg.VisionProvider != "" {
			return u.cfg.VisionProvider + "/" + u.cfg.VisionModel
		}
		return u.cfg.VisionModel
	default:
		return ""
	}
}

func (u *onboardingUI) consumePendingAction() string {
	action := u.pendingAction
	u.pendingAction = ""
	return action
}

func (u *onboardingUI) refreshModelOptions() {
	options, source := resolveModelOptions(u.cfg)
	u.allModelOptions = append([]ModelOption(nil), options...)
	u.modelSource = source
	u.applyModelFilter()
}

func (u *onboardingUI) applyModelFilter() {
	u.modelOptions = filterModelOptions(u.cfg, u.allModelOptions, u.modelFilter, u.modelCapability)
	u.scrollOffset = 0
	if len(u.modelOptions) == 0 {
		u.menuIndex = 0
		return
	}
	if u.menuIndex >= len(u.modelOptions) {
		u.menuIndex = len(u.modelOptions) - 1
	}
	if u.menuIndex < 0 {
		u.menuIndex = 0
	}
}

func (u *onboardingUI) setStep(step onboardStep) {
	u.step = step
	u.input = u.currentInputValue()
	if step == stepLLMModel {
		u.modelFilter = ""
		u.modelCapability = modelCapabilityAll
		u.refreshModelOptions()
		u.menuIndex = selectedModelIndex(u.modelOptions, u.cfg.LLMModel)
		return
	}
	u.menuIndex = 0
}

func (u *onboardingUI) modelCapabilityLabel() string {
	switch u.modelCapability {
	case modelCapabilityVision:
		return "vision"
	case modelCapabilityTools:
		return "tools"
	case modelCapabilityFree:
		return "free"
	default:
		return "all"
	}
}

func nextModelCapabilityFilter(current modelCapabilityFilter) modelCapabilityFilter {
	switch current {
	case modelCapabilityAll:
		return modelCapabilityVision
	case modelCapabilityVision:
		return modelCapabilityTools
	case modelCapabilityTools:
		return modelCapabilityFree
	default:
		return modelCapabilityAll
	}
}

func renderMenu(options []string, selected int) string {
	var b strings.Builder
	for i, option := range options {
		prefix := "  "
		if i == selected {
			prefix = colorize("> ", colorBlue)
		}
		b.WriteString(prefix)
		b.WriteString(option)
		b.WriteString("\n")
	}
	return b.String()
}

func renderModelMenu(options []ModelOption, selected int) string {
	if len(options) == 0 {
		return "  No models available.\n"
	}

	labels := make([]string, 0, len(options))
	for _, option := range options {
		labels = append(labels, option.Label())
	}
	return renderMenu(labels, selected)
}

// renderModelMenuViewport renders a scrollable window of model options.
// It adjusts scrollOffset so the selected item is always visible,
// and shows scroll indicators when there are items above/below the viewport.
func renderModelMenuViewport(options []ModelOption, selected int, scrollOffset *int, viewportHeight int) string {
	if len(options) == 0 {
		return "  No models available.\n"
	}

	// If the list fits entirely, no scrolling needed.
	if len(options) <= viewportHeight {
		*scrollOffset = 0
		return renderModelMenu(options, selected)
	}

	// Adjust scroll offset to keep selected item visible.
	if selected < *scrollOffset {
		*scrollOffset = selected
	}
	if selected >= *scrollOffset+viewportHeight {
		*scrollOffset = selected - viewportHeight + 1
	}
	// Clamp.
	if *scrollOffset < 0 {
		*scrollOffset = 0
	}
	maxOffset := len(options) - viewportHeight
	if *scrollOffset > maxOffset {
		*scrollOffset = maxOffset
	}

	var b strings.Builder

	if *scrollOffset > 0 {
		_, _ = fmt.Fprintf(&b, "  %s(%d more above)%s\n", colorBlue, *scrollOffset, colorReset)
	}

	end := *scrollOffset + viewportHeight
	if *scrollOffset > 0 {
		end-- // make room for the "more above" indicator
	}
	remaining := len(options) - end
	if remaining > 0 {
		end-- // make room for the "more below" indicator
	}
	if end > len(options) {
		end = len(options)
	}

	for i := *scrollOffset; i < end; i++ {
		prefix := "  "
		if i == selected {
			prefix = colorize("> ", colorBlue)
		}
		b.WriteString(prefix)
		b.WriteString(options[i].Label())
		b.WriteString("\n")
	}

	if remaining > 0 {
		_, _ = fmt.Fprintf(&b, "  %s(%d more below)%s\n", colorBlue, remaining, colorReset)
	}

	return b.String()
}

func renderDepsCheck(result deps.CheckResult) string {
	var b strings.Builder
	for _, d := range result.Deps {
		switch {
		case d.Found && d.VersionOK:
			if d.Version != "" {
				_, _ = fmt.Fprintf(&b, "  %s %s v%s", colorize("[ok]", colorGreen), d.Name, d.Version)
			} else {
				_, _ = fmt.Fprintf(&b, "  %s %s", colorize("[ok]", colorGreen), d.Name)
			}
			if d.MinVersion != "" {
				_, _ = fmt.Fprintf(&b, " (requires >= %s)", d.MinVersion)
			}
			b.WriteString("\n")
		case d.Found && !d.VersionOK:
			_, _ = fmt.Fprintf(&b, "  %s %s v%s (requires >= %s)\n", colorize("[!!]", colorRed), d.Name, d.Version, d.MinVersion)
		case !d.Found && d.Required:
			_, _ = fmt.Fprintf(&b, "  %s %s — not found\n", colorize("[!!]", colorRed), d.Name)
			_, _ = fmt.Fprintf(&b, "        Install: %s\n", d.InstallURL)
		default: // !Found && !Required
			_, _ = fmt.Fprintf(&b, "  %s %s — not found (optional)\n", colorize("[--]", colorYellow), d.Name)
		}
	}
	return b.String()
}

func usesProviderModelSearch(cfg config.EditableConfig) bool {
	switch cfg.LLMProvider {
	case "openrouter", "opencode-go":
		return true
	default:
		return false
	}
}
