package onboarding

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/deps"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"golang.org/x/term"
)

const (
	colorBlue   = "\x1b[94m"
	colorGreen  = "\x1b[92m"
	colorYellow = "\x1b[93m"
	colorRed    = "\x1b[91m"
	colorReset  = "\x1b[0m"
)

type onboardStep int

const (
	stepDependencies onboardStep = iota
	stepLLMProvider
	stepAnthropicAuthMode
	stepLLMKey
	stepLLMModel
	stepSTTProvider
	stepSTTKey
	stepTelegramToken
	stepTelegramUsers
	stepRuntimeMaxIterations
	stepVisionModel
	stepReview
)

type keyCode int

const (
	keyUnknown keyCode = iota
	keyUp
	keyDown
	keyLeft
	keyRight
	keyEnter
	keyBackspace
	keyRune
	keyQuit
)

type keyEvent struct {
	code keyCode
	r    rune
}

type onboardingUI struct {
	cfg             config.EditableConfig
	step            onboardStep
	menuIndex       int
	scrollOffset    int
	termHeight      int
	input           string
	message         string
	modelSource     string
	allModelOptions []ModelOption
	modelOptions    []ModelOption
	modelFilter     string
	modelCapability modelCapabilityFilter
	reviewOptions   []string
	pendingAction   string
	depsResult      *deps.CheckResult
}

type modelCapabilityFilter int

const (
	modelCapabilityAll modelCapabilityFilter = iota
	modelCapabilityVision
	modelCapabilityTools
	modelCapabilityFree
)

var llmModelCatalog = listModels
var validateToken = validateTelegramToken

func RunOnboard(stdin io.Reader, stdout io.Writer) error {
	resolver, err := runtime.New()
	if err != nil {
		return fmt.Errorf("resolve instance root: %w", err)
	}
	if err := runtime.Bootstrap(resolver); err != nil {
		return fmt.Errorf("bootstrap instance directory: %w", err)
	}

	editable, err := config.LoadEditable(resolver)
	if err != nil {
		return fmt.Errorf("carregar configuracao: %w", err)
	}

	if term, ok := stdout.(*os.File); ok && termFd(term) != -1 {
		return runOnboardTUI(os.Stdin, term, resolver, editable)
	}
	return RunOnboardPrompt(stdin, stdout, resolver, editable)
}

func RunOnboardPrompt(stdin io.Reader, stdout io.Writer, resolver *runtime.PathResolver, current *config.EditableConfig) error {
	reader := bufio.NewReader(stdin)

	if err := writeString(stdout, renderOnboardingHeader()); err != nil {
		return err
	}
	if err := writef(stdout, "Config file: %s\n", resolver.AppConfig()); err != nil {
		return err
	}

	// Dependency check (plain text, no ANSI colors).
	result := deps.CheckAll()
	if err := writeln(stdout, "Checking dependencies..."); err != nil {
		return err
	}
	for _, d := range result.Deps {
		if err := writef(stdout, "  %s\n", d.FormatStatus()); err != nil {
			return err
		}
	}
	if err := writeln(stdout, ""); err != nil {
		return err
	}
	if !result.AllOK {
		return fmt.Errorf("required dependencies are missing — install them and try again")
	}

	if err := writeln(stdout, "Note: The PI SDK (inference engine) will be installed automatically."); err != nil {
		return err
	}
	if err := writeln(stdout, "No need to install the PI CLI (pi) or run pi /login."); err != nil {
		return err
	}
	if err := writeln(stdout, ""); err != nil {
		return err
	}

	if err := writeln(stdout, "Press Enter to keep the current value."); err != nil {
		return err
	}
	if err := writeln(stdout, ""); err != nil {
		return err
	}

	current.LLMProvider, _ = promptChoice(reader, stdout, "LLM provider", current.LLMProvider, llmProviderChoices())
	if config.NormalizeProvider(current.LLMProvider) == "anthropic" {
		current.AnthropicAuthMode, _ = promptChoice(reader, stdout, "Anthropic auth mode", current.AnthropicAuthMode, []string{"api_key", "subscription"})
	}
	if err := writef(stdout, "STT provider [%s]: %s\n\n", current.STTProvider, "Groq"); err != nil {
		return err
	}

	if usesAnthropicSubscription(*current) {
		if err := writef(stdout, "Anthropic auth mode: subscription (no API key needed).\n\n"); err != nil {
			return err
		}
	} else {
		currentKey := currentLLMKey(*current)
		currentKey, _ = promptString(reader, stdout, llmKeyLabel(current.LLMProvider), currentKey, true)
		setCurrentLLMKey(current, currentKey)
	}
	current.LLMModel, _ = promptLLMModel(reader, stdout, current)
	current.GroqAPIKey, _ = promptString(reader, stdout, "Groq API key", current.GroqAPIKey, true)
	for {
		current.TelegramBotToken, _ = promptString(reader, stdout, "Telegram bot token", current.TelegramBotToken, true)
		if current.TelegramBotToken == "" {
			if err := writeln(stdout, "Token is required."); err != nil {
				return err
			}
			continue
		}
		if err := writeln(stdout, "Validating token with Telegram..."); err != nil {
			return err
		}
		username, err := validateToken(current.TelegramBotToken)
		if err != nil {
			if err := writef(stdout, "Invalid token: %v\nPlease try again.\n\n", err); err != nil {
				return err
			}
			continue
		}
		if err := writef(stdout, "Token valid! Bot: @%s\n\n", username); err != nil {
			return err
		}
		break
	}
	current.TelegramAllowedUserIDs, _ = promptInt64List(reader, stdout, "Telegram allowed user IDs (comma-separated)", current.TelegramAllowedUserIDs)
	current.MaxIterations, _ = promptInt(reader, stdout, "Max iterations", current.MaxIterations)

	// Vision fallback model (optional)
	defaultVision := current.VisionModel
	if current.VisionProvider != "" {
		defaultVision = current.VisionProvider + "/" + current.VisionModel
	}
	visionRaw, _ := promptString(reader, stdout, "Vision fallback model (optional, e.g. provider/model)", defaultVision, false)
	visionRaw = strings.TrimSpace(visionRaw)
	if visionRaw == "" {
		current.VisionModel = ""
		current.VisionProvider = ""
	} else if strings.Contains(visionRaw, "/") {
		parts := strings.SplitN(visionRaw, "/", 2)
		current.VisionProvider = strings.TrimSpace(parts[0])
		current.VisionModel = strings.TrimSpace(parts[1])
	} else {
		current.VisionModel = visionRaw
		current.VisionProvider = ""
	}

	current.STTProvider = "groq"

	if err := config.SaveEditable(resolver, *current); err != nil {
		return fmt.Errorf("save app config: %w", err)
	}

	return renderSavedSummary(stdout, resolver, current)
}

func runOnboardTUI(stdin *os.File, stdout *os.File, resolver *runtime.PathResolver, current *config.EditableConfig) error {
	oldState, err := term.MakeRaw(int(stdin.Fd()))
	if err != nil {
		return fmt.Errorf("enable raw terminal mode: %w", err)
	}
	defer func() { _ = term.Restore(int(stdin.Fd()), oldState) }()

	ui := newOnboardingUI(*current)
	reader := bufio.NewReader(stdin)

	for {
		_, h, _ := term.GetSize(int(stdout.Fd()))
		if h <= 0 {
			h = 24
		}
		ui.termHeight = h
		if _, err := io.WriteString(stdout, rawTerminalFrame(ui.View(resolver))); err != nil {
			return err
		}

		ev, err := readKey(reader)
		if err != nil {
			return err
		}

		saved, cancelled, err := ui.HandleKey(ev)
		if err != nil {
			return err
		}
		if cancelled {
			clearScreen(stdout)
			if err := writeln(stdout, "Onboarding canceled."); err != nil {
				return err
			}
			return nil
		}
		if saved {
			if err := config.SaveEditable(resolver, ui.cfg); err != nil {
				return fmt.Errorf("save app config: %w", err)
			}
			clearScreen(stdout)
			return renderSavedSummary(stdout, resolver, &ui.cfg)
		}
		if action := ui.consumePendingAction(); action != "" {
			_ = action
		}
	}
}

func newOnboardingUI(cfg config.EditableConfig) *onboardingUI {
	if cfg.LLMProvider == "" {
		cfg.LLMProvider = "kimi"
	}
	if cfg.LLMModel == "" {
		cfg.LLMModel = config.DefaultEditableConfig().LLMModel
	}
	if cfg.AnthropicAuthMode == "" {
		cfg.AnthropicAuthMode = "api_key"
	}
	if cfg.STTProvider == "" {
		cfg.STTProvider = "groq"
	}
	modelOptions, modelSource := resolveModelOptions(cfg)
	return &onboardingUI{
		cfg:             cfg,
		allModelOptions: append([]ModelOption(nil), modelOptions...),
		modelOptions:    append([]ModelOption(nil), modelOptions...),
		modelSource:     modelSource,
		step:            stepDependencies,
		reviewOptions:   []string{"Save config", "Back", "Cancel"},
	}
}

// validateTelegramToken calls Telegram's getMe API to verify a bot token.
// Returns the bot's username on success, or an error if the token is invalid.
func validateTelegramToken(token string) (string, error) {
	if token == "" {
		return "", fmt.Errorf("token is empty")
	}
	url := fmt.Sprintf("https://api.telegram.org/bot%s/getMe", token)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to reach Telegram API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			Username string `json:"username"`
		} `json:"result"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode Telegram response: %w", err)
	}
	if !result.OK {
		return "", fmt.Errorf("invalid token: %s", result.Description)
	}
	return result.Result.Username, nil
}

// termFd returns the file descriptor of a terminal, or -1 if not a terminal.
func termFd(f *os.File) int {
	if stat, err := f.Stat(); err == nil && (stat.Mode()&os.ModeCharDevice) != 0 {
		return int(f.Fd())
	}
	return -1
}
