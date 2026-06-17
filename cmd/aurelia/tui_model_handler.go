package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

// handleTUIModel processes /model commands from the TUI.
func handleTUIModel(ctx context.Context, a *app, chatID int64, threadID int, userID int64, text string) string {
	args := strings.TrimSpace(strings.TrimPrefix(text, "/model"))
	if args == "" {
		return buildTUIModelList(ctx, a)
	}
	if isTUIAutoModelName(args) {
		return setTUIModel(a, chatID, threadID, userID, "", "")
	}
	if strings.EqualFold(args, "refresh") {
		return refreshTUIModels(ctx, a)
	}
	return setTUIModelByName(ctx, a, chatID, threadID, userID, args)
}

func buildTUIModelList(ctx context.Context, a *app) string {
	currentLine := currentTUIModelLine(a)
	if a == nil || a.bridge == nil {
		return currentLine + "\n\nBridge unavailable to list models. Use /model auto to use the PI default."
	}

	models, err := listTUIModels(ctx, a)
	if err != nil {
		return currentLine + fmt.Sprintf("\n\nModel list unavailable: %v", err)
	}
	return formatTUIModelList(currentLine, models)
}

func refreshTUIModels(ctx context.Context, a *app) string {
	if a == nil || a.bridge == nil {
		return "Bridge unavailable to refresh models."
	}
	models, err := listTUIModels(ctx, a)
	if err != nil {
		return fmt.Sprintf("❌ Failed to refresh models: %v", err)
	}
	if len(models) == 0 {
		return "No models available after refresh."
	}
	return fmt.Sprintf("✅ Models refreshed: **%d** available. Use /model to list them.", len(models))
}

func setTUIModelByName(ctx context.Context, a *app, chatID int64, threadID int, userID int64, modelName string) string {
	if a == nil || a.bridge == nil {
		return "Bridge unavailable to validate models."
	}
	models, err := listTUIModels(ctx, a)
	if err != nil {
		return fmt.Sprintf("❌ Failed to query models: %v", err)
	}
	matched := findTUIModel(models, modelName)
	if matched == nil {
		return fmt.Sprintf("Model %q not found. Use /model to list available models.", modelName)
	}
	return setTUIModel(a, chatID, threadID, userID, matched.Provider, matched.ID)
}

func listTUIModels(ctx context.Context, a *app) ([]bridge.ModelInfo, error) {
	modelCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return a.bridge.ListModels(modelCtx, true)
}

func formatTUIModelList(currentLine string, models []bridge.ModelInfo) string {
	var lines []string
	lines = append(lines, currentLine)
	lines = append(lines, "\n\n**Available models:**")

	grouped := make(map[string][]string)
	var providerOrder []string
	for _, m := range models {
		if _, ok := grouped[m.Provider]; !ok {
			providerOrder = append(providerOrder, m.Provider)
		}
		display := fmt.Sprintf("`%s`", m.ID)
		if m.SupportsImages {
			display += " 📷"
		}
		grouped[m.Provider] = append(grouped[m.Provider], display)
	}
	sort.Strings(providerOrder)

	displayed := 0
	const maxTUIModelDisplay = 50
	for _, provider := range providerOrder {
		if displayed >= maxTUIModelDisplay {
			lines = append(lines, fmt.Sprintf("\n... and %d more models", len(models)-displayed))
			break
		}
		lines = append(lines, fmt.Sprintf("\n%s:", provider))
		for _, model := range grouped[provider] {
			if displayed >= maxTUIModelDisplay {
				break
			}
			lines = append(lines, fmt.Sprintf("  %s", model))
			displayed++
		}
	}

	lines = append(lines, "\n\nUse /model <name> to switch, /model auto for PI default, or /model refresh to refresh.")
	return strings.Join(lines, "\n")
}

func findTUIModel(models []bridge.ModelInfo, modelName string) *bridge.ModelInfo {
	needle := strings.TrimSpace(modelName)
	for i := range models {
		m := models[i]
		if strings.EqualFold(needle, m.ID) || strings.EqualFold(needle, m.Provider+"/"+m.ID) {
			return &m
		}
	}
	return nil
}

func setTUIModel(a *app, chatID int64, threadID int, userID int64, provider string, model string) string {
	if err := saveTUIDefaultModel(a, provider, model); err != nil {
		return fmt.Sprintf("❌ Failed to save model configuration: %v", err)
	}
	if a.sessions != nil {
		a.sessions.ClearSessionForUser(chatID, threadID, userID)
	}
	if provider == "" && model == "" {
		return "✅ Model changed to **PI default**\nTUI session reset. Next message will use PI automatic selection."
	}
	return fmt.Sprintf("✅ Model changed to **%s** (provider: **%s**)\nTUI session reset. Next message will use the new model.", model, provider)
}

func saveTUIDefaultModel(a *app, provider string, model string) error {
	if a == nil || a.config == nil {
		return fmt.Errorf("config is nil")
	}
	resolver := a.resolver
	if resolver == nil {
		var err error
		resolver, err = runtime.New()
		if err != nil {
			return fmt.Errorf("resolve instance: %w", err)
		}
	}

	data, err := os.ReadFile(resolver.AppConfig())
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	raw["default_provider"] = provider
	raw["default_model"] = model
	updated, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	updated = append(updated, '\n')

	tmp := resolver.AppConfig() + ".tmp"
	if err := os.WriteFile(tmp, updated, 0o600); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	if err := os.Rename(tmp, resolver.AppConfig()); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename config: %w", err)
	}
	a.config.DefaultProvider = provider
	a.config.DefaultModel = model
	setProviderEnv(a.config)
	return nil
}

func currentTUIModelLine(a *app) string {
	if a == nil || a.config == nil || a.config.IsModelAuto() {
		return "Current model: **PI default** (PI automatic selection)"
	}
	return fmt.Sprintf("Current model: **%s** (provider: **%s**)", a.config.DefaultModel, a.config.DefaultProvider)
}

func isTUIAutoModelName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "auto")
}
