package telegram

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/telebot.v3"
)

func (bc *BotController) setupBootstrapRoutes() {
	bc.bot.Handle("/start", bc.handleStart)
	bc.bot.Handle("\fbtn_coder", bc.handleBootstrapChoice("coder"))
	bc.bot.Handle("\fbtn_assist", bc.handleBootstrapChoice("assist"))
	// Timezone preset choices
	for _, tz := range []string{"tz_lisbon", "tz_sao_paulo", "tz_new_york", "tz_utc", "tz_other"} {
		tzCopy := tz
		bc.bot.Handle("\fbtn_"+tzCopy, bc.handleBootstrapTimezone(tzCopy))
	}
}

func (bc *BotController) handleStart(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	identityExists := bootstrapIdentityExists(bc.personasDir)

	message, menu := bootstrapStartResponse(identityExists)
	if menu == nil {
		return SendContextText(c, message)
	}
	return SendContextText(c, message, menu)
}

func (bc *BotController) handleBootstrapChoice(choice string) func(telebot.Context) error {
	return func(c telebot.Context) error {
		_ = bc.bot.Respond(c.Callback(), &telebot.CallbackResponse{})

		// Write base preset as fallback (will be overwritten by LLM generation)
		preset, err := bootstrapPresetForChoice(choice)
		if err != nil {
			return SendContextText(c, bootstrapFailureMessage())
		}
		if err := writeBootstrapPreset(bc.personasDir, preset); err != nil {
			log.Printf("Bootstrap error: %v\n", err)
			return SendContextText(c, bootstrapFailureMessage())
		}

		bc.setPendingBootstrap(c.Sender().ID, bootstrapState{Choice: choice, Step: bootstrapStepAssistant})
		return SendContextText(c, bootstrapAssistantMessage())
	}
}

func bootstrapIdentityExists(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "IDENTITY.md"))
	return err == nil
}

// --- Timezone bootstrap step ---

var bootstrapTimezonePresets = map[string]string{
	"tz_lisbon":    "Europe/Lisbon",
	"tz_sao_paulo": "America/Sao_Paulo",
	"tz_new_york":  "America/New_York",
	"tz_utc":       "UTC",
}

func bootstrapTimezoneMessage() string {
	return "🌍 Qual é o seu fuso horário?\n\n" +
		"Escolha uma das opções abaixo ou digite o nome de um fuso IANA (ex: America/Sao_Paulo):"
}

func newBootstrapTimezoneMenu() *telebot.ReplyMarkup {
	menu := &telebot.ReplyMarkup{}
	btnLisbon := menu.Data("🇵🇹 Europe/Lisbon", "btn_tz_lisbon")
	btnSP := menu.Data("🇧🇷 America/Sao_Paulo", "btn_tz_sao_paulo")
	btnNY := menu.Data("🇺🇸 America/New_York", "btn_tz_new_york")
	btnUTC := menu.Data("🌐 UTC", "btn_tz_utc")
	btnOther := menu.Data("✏️ Outro (digitar manualmente)", "btn_tz_other")
	menu.Inline(
		menu.Row(btnSP),
		menu.Row(btnLisbon),
		menu.Row(btnNY),
		menu.Row(btnUTC),
		menu.Row(btnOther),
	)
	return menu
}

func (bc *BotController) handleBootstrapTimezone(choice string) func(telebot.Context) error {
	return func(c telebot.Context) error {
		_ = bc.bot.Respond(c.Callback(), &telebot.CallbackResponse{})

		senderID := safeSenderID(c.Sender())
		state, ok := bc.popPendingBootstrap(senderID)
		if !ok || state.Step != bootstrapStepTimezone {
			return SendContextText(c, "Estado de onboarding inválido. Use /start para reiniciar.")
		}

		if choice == "tz_other" {
			// Ask user to type IANA timezone manually
			bc.setPendingBootstrap(senderID, state)
			return SendContextText(c, "Digite o nome do fuso horário IANA (ex: America/Sao_Paulo, Europe/Lisbon).")
		}

		// Predefined choice — validate and save
		tz, ok := bootstrapTimezonePresets[choice]
		if !ok {
			return SendContextText(c, bootstrapFailureMessage())
		}
		return bc.saveBootstrapTimezone(c, senderID, tz)
	}
}

func (bc *BotController) completeBootstrapTimezone(c telebot.Context, state bootstrapState, text string) error {
	tz := strings.TrimSpace(text)
	if tz == "" {
		bc.setPendingBootstrap(c.Sender().ID, state)
		return SendContextText(c, "Por favor, digite um fuso horário válido (ex: America/Sao_Paulo).")
	}

	// Validate with time.LoadLocation
	_, err := time.LoadLocation(tz)
	if err != nil {
		bc.setPendingBootstrap(c.Sender().ID, state)
		return SendContextText(c, fmt.Sprintf("Fuso horário %q inválido. Tente novamente (ex: America/Sao_Paulo).", tz))
	}

	return bc.saveBootstrapTimezone(c, c.Sender().ID, tz)
}

func (bc *BotController) saveBootstrapTimezone(c telebot.Context, senderID int64, tz string) error {
	userID := senderID
	if bc.userStore == nil {
		return SendContextText(c, "Sistema de usuários não disponível.")
	}

	profile, err := bc.userStore.Get(userID)
	if err != nil {
		log.Printf("bootstrap: load profile for timezone user=%d: %v", userID, err)
		return SendContextText(c, bootstrapFailureMessage())
	}
	if profile == nil {
		return SendContextText(c, "Perfil não encontrado. Complete o onboarding primeiro com /start.")
	}
	profile.Timezone = tz
	if err := bc.userStore.Save(profile); err != nil {
		log.Printf("bootstrap: save profile timezone user=%d: %v", userID, err)
		return SendContextText(c, bootstrapFailureMessage())
	}

	return SendContextText(c, bootstrapSuccessMessage())
}
