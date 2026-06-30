package main

import (
	"fmt"
	"strings"

	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/users"
)

func tuiProfileResolver(a *app) *profiles.Resolver {
	if a == nil || a.bot == nil {
		return nil
	}
	return a.bot.ProfileResolver()
}

func tuiUserStore(a *app) *users.Store {
	if a == nil || a.bot == nil {
		return nil
	}
	return a.bot.UserStore()
}

func tuiActiveProfileName(a *app, userID int64) string {
	store := tuiUserStore(a)
	if store == nil {
		return "general"
	}
	profile, err := store.Get(userID)
	if err != nil || profile == nil {
		return "general"
	}
	return profiles.ActiveDefault(profile.ActiveMode)
}

// handleTUIMode processes /mode commands from the TUI.
func handleTUIMode(a *app, userID int64, text string) string {
	args := strings.TrimSpace(strings.TrimPrefix(text, "/mode"))
	if strings.HasPrefix(strings.ToLower(args), "explain ") {
		name := strings.TrimSpace(args[len("explain "):])
		return tuiExplainProfile(a, userID, name)
	}
	return tuiSetOrQueryMode(a, userID, args)
}

// handleTUIAgents processes /agents commands from the TUI.
func handleTUIAgents(a *app, userID int64, text string) string {
	args := strings.TrimSpace(strings.TrimPrefix(text, "/agents"))
	if strings.HasPrefix(strings.ToLower(args), "explain ") {
		name := strings.TrimSpace(args[len("explain "):])
		return tuiExplainProfile(a, userID, name)
	}
	verbose := strings.EqualFold(args, "verbose")
	return tuiListProfiles(a, userID, verbose)
}

func tuiSetOrQueryMode(a *app, userID int64, args string) string {
	resolver := tuiProfileResolver(a)
	store := tuiUserStore(a)
	if resolver == nil || store == nil {
		return "❌ Profile system unavailable."
	}

	profile, err := store.Get(userID)
	if err != nil {
		return fmt.Sprintf("❌ Error loading user profile: %s", err)
	}
	if profile == nil {
		return "❌ User profile not found. Complete Telegram onboarding first."
	}

	if args == "" {
		display := profiles.ActiveDefault(profile.ActiveMode)
		return fmt.Sprintf("Perfil ativo: **%s**.\n\nUse `@perfil` para aplicar outro perfil só na próxima mensagem.", display)
	}

	target := strings.TrimSpace(args)
	switch strings.ToLower(target) {
	case "general", "auto", "geral":
		profile.ActiveMode = ""
		if err := store.Save(profile); err != nil {
			return fmt.Sprintf("❌ Error saving profile: %s", err)
		}
		return "✅ Perfil alterado para **general**."
	}

	name := tuiNormalizeProfileSelection(target)
	selected := resolver.GetVisibleForUser(userID, name, true)
	if selected == nil {
		return fmt.Sprintf("❌ Profile %q not found. Use /agents to list available profiles.", target)
	}

	profile.ActiveMode = selected.Name
	if err := store.Save(profile); err != nil {
		return fmt.Sprintf("❌ Error saving profile: %s", err)
	}
	return fmt.Sprintf("✅ Perfil alterado para **%s**.\nUse `@%s <pedido>` para aplicar só nesta mensagem.", selected.Name, selected.Name)
}

func tuiListProfiles(a *app, userID int64, verbose bool) string {
	resolver := tuiProfileResolver(a)
	if resolver == nil {
		return "❌ Profile system unavailable."
	}

	all := resolver.ListVisibleForUser(userID, true)
	if len(all) == 0 {
		return "No profiles available. Builtins (general, developer, researcher) are always available when the resolver is wired."
	}

	displayActive := tuiActiveProfileName(a, userID)
	var lines []string
	lines = append(lines, fmt.Sprintf("**Perfis disponíveis** (%d)", len(all)))
	lines = append(lines, fmt.Sprintf("Perfil ativo: **%s**", displayActive))
	lines = append(lines, "")
	for _, p := range all {
		lines = append(lines, profiles.FormatCatalogLine(p, displayActive, verbose))
	}
	lines = append(lines, "")
	lines = append(lines, "Use `/mode <perfil>` to set the default profile.")
	lines = append(lines, "Use `@perfil <pedido>` for a one-shot profile on the next message.")
	lines = append(lines, "Use `/agents verbose` for execution hints.")
	return strings.Join(lines, "\n")
}

func tuiExplainProfile(a *app, userID int64, name string) string {
	if strings.TrimSpace(name) == "" {
		return "Usage: /mode explain <profile> or /agents explain <profile>"
	}

	resolver := tuiProfileResolver(a)
	if resolver == nil {
		return "❌ Profile system unavailable."
	}

	target := tuiNormalizeProfileSelection(name)
	selected := resolver.GetVisibleForUser(userID, target, true)
	if selected == nil {
		return fmt.Sprintf("❌ Profile %q not found. Use /agents to list available profiles.", name)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("**%s**", selected.Name))
	if selected.Description != "" {
		lines = append(lines, selected.Description)
	}
	lines = append(lines, "")
	lines = append(lines, "**Usage:**")
	lines = append(lines, fmt.Sprintf("• `/mode %s` — set as default profile", selected.Name))
	lines = append(lines, fmt.Sprintf("• `@%s <request>` — use once on the next message", selected.Name))
	lines = append(lines, "")
	lines = append(lines, "Safe summary only — prompt body, model, cwd, and tool policy stay hidden.")
	if len(selected.Tags) > 0 {
		tags := make([]string, len(selected.Tags))
		for i, t := range selected.Tags {
			tags[i] = "#" + t
		}
		lines = append(lines, strings.Join(tags, " "))
	}
	return strings.Join(lines, "\n")
}

func tuiNormalizeProfileSelection(name string) string {
	if normalized, err := users.NormalizeMode(name); err == nil && normalized != "" {
		return normalized
	}
	return strings.TrimSpace(name)
}