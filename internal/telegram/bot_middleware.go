package telegram

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/bridge"
	memoryuxpkg "github.com/igormaneschy/aurelia/internal/memoryux"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/runtime"
)

func (bc *BotController) whitelistMiddleware() telebot.MiddlewareFunc {
	return func(next telebot.HandlerFunc) telebot.HandlerFunc {
		return func(c telebot.Context) error {
			sender := c.Sender()
			chat := c.Chat()

			switch chat.Type {
			case telebot.ChatPrivate:
				if sender != nil && bc.isAllowedUser(sender.ID) {
					return next(c)
				}
			case telebot.ChatGroup, telebot.ChatSuperGroup:
				// Require both: group on allowlist AND sender on user allowlist.
				// Group-only check would let any group member talk to the bot,
				// including users added after the group was whitelisted.
				if sender != nil && bc.isAllowedGroup(chat.ID) && bc.isAllowedUser(sender.ID) {
					return next(c)
				}
			}

			log.Printf("blocked unauthorized access: user=%d chat=%d type=%q\n",
				safeSenderID(sender), chat.ID, chat.Type)
			return nil
		}
	}
}

func safeSenderID(u *telebot.User) int64 {
	if u == nil {
		return 0
	}
	return u.ID
}

func (bc *BotController) registerContentRoutes() {
	bc.bot.Handle("/help", bc.handleHelpCommand)
	bc.bot.Handle("/cwd", bc.handleCwdCommand)
	bc.bot.Handle("/reset", bc.handleResetCommand)
	bc.bot.Handle("/new", bc.handleResetCommand)
	bc.bot.Handle("/compact", bc.handleResetCommand)
	bc.bot.Handle("/usage", bc.handleUsageCommand)
	bc.bot.Handle("/stop", bc.handleStopCommand)
	bc.bot.Handle("/status", bc.handleStatusCommand)
	bc.bot.Handle("/cron", bc.handleCronCommand)
	bc.bot.Handle("/agents", bc.handleAgentsCommand)
	bc.bot.Handle("/memory", bc.handleMemoryCommand)
	bc.bot.Handle("/model", bc.handleModelCommand)
	bc.bot.Handle("/users", bc.handleUsersCommand)
	bc.bot.Handle("/forgetme", bc.handleForgetMeCommand)
	bc.bot.Handle("\fforget_me_confirm", bc.handleForgetMeConfirm)
	bc.bot.Handle("\fforget_me_cancel", bc.handleForgetMeCancel)
	bc.bot.Handle(telebot.OnCallback, bc.handleModelCallback)
	bc.bot.Handle(telebot.OnText, bc.handleText)
	bc.bot.Handle(telebot.OnPhoto, bc.handlePhoto)
	bc.bot.Handle(telebot.OnDocument, bc.handleDocument)
	bc.bot.Handle(telebot.OnVoice, bc.handleVoice)
	bc.bot.Handle(telebot.OnAudio, bc.handleVoice)
}

func (bc *BotController) registerSlashMenu() {
	commands := []telebot.Command{
		{Text: "new", Description: "Nova sessão (limpa contexto)"},
		{Text: "usage", Description: "Ver uso de tokens da sessão"},
		{Text: "status", Description: "Ver estado atual da Aurelia"},
		{Text: "cwd", Description: "Definir diretório de trabalho"},
		{Text: "cron", Description: "Gerenciar agendamentos"},
		{Text: "agents", Description: "Listar agentes disponíveis"},
		{Text: "memory", Description: "Ver status da memória e criar checkpoints"},
		{Text: "model", Description: "Ver/trocar modelo ativo"},
		{Text: "stop", Description: "Interromper processamento ativo (preserva sessão)"},
		{Text: "users", Description: "Listar usuários autorizados (owner)"},
		{Text: "forgetme", Description: "Apagar meus dados e recomeçar"},
		{Text: "help", Description: "Mostrar comandos disponíveis"},
	}
	if err := bc.bot.SetCommands(commands); err != nil {
		log.Printf("Failed to set bot commands: %v", err)
	}
}

func (bc *BotController) handleHelpCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	return SendTextWithThread(bc.bot, c.Chat(), helpMessage(), c.Message().ThreadID)
}

func helpMessage() string {
	return "Comandos disponíveis:\n\n" +
		"/new — Nova sessão (limpa contexto, cancela o que estiver em andamento)\n" +
		"/stop — Interromper processamento ativo (preserva sessão)\n" +
		"/usage — Ver uso de tokens da sessão\n" +
		"/status — Ver estado atual + trabalho ativo + fila\n" +
		"/cwd <path> — Definir diretório de trabalho (tópicos herdam do grupo)\n" +
		"/cron — Gerenciar agendamentos\n" +
		"/agents — Listar agentes disponíveis (também roteáveis com @nome)\n" +
		"/memory — Ver status da memória e criar checkpoints\n" +
		"/model — Ver/trocar modelo ativo\n" +
		"/help — Mostrar esta mensagem\n\n" +
		"---\n\n" +
		"💡 Também entendo comandos naturais:\n" +
		"• \"agenda todo dia às 9h revisar emails\"\n" +
		"• \"muda modelo para claude-sonnet\"\n" +
		"• \"limpa o contexto\"\n" +
		"• \"quais modelos\"\n\n" +
		"🛑 Enquanto eu processo, você pode:\n" +
		"• \"para\" / \"cancela\" — interrompe o pedido atual\n" +
		"• \"na verdade...\" / \"corrigindo\" — substitui pelo novo pedido\n" +
		"• \"conseguiu?\" / \"status\" — pergunta sem entrar na fila\n" +
		"• outras mensagens entram na fila e rodam depois\n\n" +
		"Ou simplesmente envie uma mensagem e eu respondo."
}

func (bc *BotController) handleAgentsCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	if bc.agents == nil || len(bc.agents.Agents()) == 0 {
		return SendText(bc.bot, c.Chat(), "Nenhum agente configurado. Crie arquivos .md em ~/.aurelia/agents/")
	}
	var lines []string
	for _, a := range bc.agents.Agents() {
		line := fmt.Sprintf("• %s — %s", a.Name, a.Description)
		if a.Model != "" {
			line += fmt.Sprintf(" [%s]", a.Model)
		}
		if a.Schedule != "" {
			line += fmt.Sprintf(" (cron: %s)", a.Schedule)
		}
		lines = append(lines, line)
	}
	return SendText(bc.bot, c.Chat(), "Agentes disponíveis:\n\n"+strings.Join(lines, "\n"))
}

func (bc *BotController) handleCwdCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	args := strings.TrimSpace(c.Message().Payload)

	if args == "" {
		// Build resolution chain status
		topicCwd := ""
		if threadID > 0 {
			topicCwd = bc.currentCwd(chatID, threadID)
		}
		groupCwd := bc.currentCwd(chatID, 0)
		defaultCwd := bc.botCwd

		// Determine agent cwd if an agent is active
		agentCwd := ""
		text := c.Message().Text
		if bc.agents != nil {
			if agent := bc.agents.Route(text); agent != nil && agent.Cwd != "" {
				agentCwd = agent.Cwd
			}
		}

		// Show known project bindings from other chats when available
		var known []projectbinding.ProjectBinding
		if userID := safeSenderID(c.Sender()); userID > 0 && bc.bindings != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var err error
			known, err = bc.bindings.ListByUser(ctx, userID, 5)
			if err != nil {
				log.Printf("cwd: listKnownProjects error: %v", err)
				known = nil
			}
		}

		status := buildCwdStatusText(defaultCwd, groupCwd, topicCwd, agentCwd, threadID, known)
		return SendTextWithThread(bc.bot, c.Chat(), status, threadID)
	}

	if clearThreadID, clear, err := cwdClearThread(args, threadID); err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ "+err.Error(), threadID)
	} else if clear {
		if err := bc.clearCurrentCwd(chatID, clearThreadID); err != nil {
			return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), threadID)
		}
		msg := "✅ Binding de projeto removido deste tópico."
		if clearThreadID == 0 {
			msg = "✅ Binding de projeto removido do grupo."
		}
		return SendTextWithThread(bc.bot, c.Chat(), msg, threadID)
	}

	// Parse scope flag before setting
	target, err := parseCwdSetTarget(args, threadID)
	if err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), "❌ "+err.Error(), threadID)
	}

	userID := int64(0)
	if c.Sender() != nil {
		userID = c.Sender().ID
	}
	cwd, err := bc.setCurrentCwd(chatID, target.ThreadID, userID, target.Path)
	if err != nil {
		log.Printf("cwd: rejected binding chat=%d thread=%d err=%v", chatID, target.ThreadID, err)
		msg := fmt.Sprintf("❌ Diretório inválido, inexistente ou não permitido. Detalhe: %s", err.Error())
		return SendTextWithThread(bc.bot, c.Chat(), msg, threadID)
	}
	if bc.resolver != nil {
		if err := runtime.BootstrapConversationProjectMemory(bc.resolver, cwd, chatID, target.ThreadID); err != nil {
			log.Printf("cwd: failed to bootstrap project memory for %s: %v", cwd, err)
		}
	}
	// Invalidate memory cache — project changed, memory files may differ
	bc.invalidateMemoryDirs(chatID, target.ThreadID, userID, cwd)
	// If setting group-level from a topic, also invalidate the topic cache
	if target.ThreadID == 0 && threadID != 0 {
		bc.invalidateMemoryDirs(chatID, threadID, userID, cwd)
	}

	var msg string
	switch target.Scope {
	case "group":
		msg = fmt.Sprintf("✅ Projeto fixado para o grupo: `%s`\n\nOutros tópicos herdarão este caminho automaticamente. Essa configuração é persistente.", cwd)
	default:
		msg = fmt.Sprintf("✅ Projeto fixado para este tópico: `%s`\n\nEssa configuração é persistente até você trocar ou limpar com `/cwd clear`.", cwd)
		if !target.Explicit && threadID > 0 {
			msg += "\n\n💡 Para aplicar ao grupo todo, use `/cwd --group <caminho>`."
		}
	}
	return SendTextWithThread(bc.bot, c.Chat(), msg, threadID)
}

func (bc *BotController) handleStopCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	userID := safeSenderID(c.Sender())

	if bc.cancelActiveRun(chatID, threadID, userID) {
		return SendTextWithThread(bc.bot, c.Chat(), "🛑 Processamento interrompido. A sessão foi preservada.", threadID)
	}
	return SendTextWithThread(bc.bot, c.Chat(), "Nenhum processamento ativo para interromper.", threadID)
}

func (bc *BotController) handleResetCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	userID := safeSenderID(c.Sender())
	reply, err := bc.resetCurrentSession(chatID, threadID, true, userID)
	if err != nil {
		return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), threadID)
	}
	return SendTextWithThread(bc.bot, c.Chat(), reply, threadID)
}

func (bc *BotController) handleUsageCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	threadID := c.Message().ThreadID
	msg := "📊 O gerenciamento de tokens agora é feito pelo PI SDK (compaction automática).\n\n" +
		"O contexto é podado automaticamente conforme necessário — não é mais necessário " +
		"monitorar manualmente o uso de tokens."
	return SendTextWithThread(bc.bot, c.Chat(), msg, threadID)
}

func (bc *BotController) handleStatusCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	threadID := c.Message().ThreadID
	userID := safeSenderID(c.Sender())
	reply, err := bc.cmdStatus(c.Chat().ID, threadID, userID)
	if err != nil {
		return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), threadID)
	}
	return SendTextWithThread(bc.bot, c.Chat(), reply, threadID)
}

func (bc *BotController) handleCronCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	if bc.cronHandler == nil {
		return SendText(bc.bot, c.Chat(), "Cron não está disponível.")
	}
	userID := fmt.Sprintf("%d", c.Sender().ID)
	chatID := c.Chat().ID
	text := c.Message().Text

	reply, err := bc.cronHandler.HandleText(context.Background(), userID, chatID, text)
	if err != nil {
		return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), c.Message().ThreadID)
	}
	if reply != "" {
		return SendTextWithThread(bc.bot, c.Chat(), reply, c.Message().ThreadID)
	}
	return nil
}

const modelsPerPage = 10

func (bc *BotController) handleModelCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	args := strings.TrimSpace(c.Message().Payload)
	threadID := c.Message().ThreadID
	if args != "" {
		if !bc.isOwner(c) {
			return SendTextWithThread(bc.bot, c.Chat(), "Permissão negada.", threadID)
		}
		if isRefreshModelName(args) {
			reply, err := bc.cmdRefreshModels()
			if err != nil {
				return SendErrorWithThread(bc.bot, c.Chat(), fmt.Sprintf("Erro: %v", err), threadID)
			}
			return SendTextWithThread(bc.bot, c.Chat(), reply, threadID)
		}
		reply, err := bc.cmdSetModel(c, "/model "+args)
		if err != nil {
			return SendErrorWithThread(bc.bot, c.Chat(), fmt.Sprintf("Erro: %v", err), threadID)
		}
		return SendTextWithThread(bc.bot, c.Chat(), reply, threadID)
	}

	currentLine := bc.currentModelLine()
	if bc.activeModelLister() == nil {
		return SendTextWithThread(bc.bot, c.Chat(), currentLine+"\n\nBridge indisponível.", threadID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := bc.getModels(ctx, false)
	if err != nil {
		return SendTextWithThread(bc.bot, c.Chat(), currentLine+fmt.Sprintf("\n\nLista não disponível: %v", err), threadID)
	}
	if len(models) == 0 {
		// Cache was empty — force-refresh once; bridge may have been cold.
		models, err = bc.getModels(ctx, true)
		if err != nil {
			return SendTextWithThread(bc.bot, c.Chat(), currentLine+fmt.Sprintf("\n\nLista não disponível: %v", err), threadID)
		}
		if len(models) == 0 {
			return SendTextWithThread(bc.bot, c.Chat(), currentLine+"\n\nNenhum modelo disponível.", threadID)
		}
	}

	return bc.sendProviderMenu(c, false)
}

func (bc *BotController) sendProviderMenu(c telebot.Context, edit bool) error {
	return bc.sendProviderMenuMessage(c, edit, "")
}

func (bc *BotController) sendProviderMenuMessage(c telebot.Context, edit bool, prefix string) error {
	bc.modelCacheMu.Lock()
	models := bc.modelCache
	bc.modelCacheMu.Unlock()

	grouped := make(map[string]bool)
	for _, m := range models {
		grouped[m.Provider] = true
	}
	var providers []string
	for p := range grouped {
		providers = append(providers, p)
	}
	sort.Strings(providers)

	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	rows = append(rows, menu.Row(menu.Data("🔄 Atualizar modelos", "mdl_refresh")))
	for _, prov := range providers {
		rows = append(rows, menu.Row(menu.Data(prov, "mdl_prov_"+prov)))
	}
	rows = append(rows, menu.Row(menu.Data("❌ Cancelar", "mdl_cancel")))
	menu.Inline(rows...)

	currentLine := bc.currentModelLine()
	msg := currentLine + "\n\n**Selecione o provedor:**"
	if prefix != "" {
		msg = prefix + "\n\n" + msg
	}
	if edit {
		return c.Edit(msg, menu)
	}
	_, err := bc.bot.Send(c.Chat(), msg, &telebot.SendOptions{ThreadID: c.Message().ThreadID}, menu)
	return err
}

func (bc *BotController) handleModelCallback(c telebot.Context) error {
	// Acknowledge callback to stop Telegram loading spinner.
	// This runs for ALL callbacks (bootstrap, model, etc.), which is harmless.
	_ = bc.bot.Respond(c.Callback(), &telebot.CallbackResponse{})

	data := c.Data()

	// Telebot sends callback data as "\f<unique>|<payload>"
	// c.Data() returns the full callback data from Telegram (with \f prefix)
	// Strip the leading \f before checking
	if len(data) > 0 && data[0] == '\f' {
		data = data[1:]
	}

	// Split by | to get just the unique part (ignore payload we don't use)
	if idx := strings.IndexByte(data, '|'); idx >= 0 {
		data = data[:idx]
	}

	// Route by the model identifier prefix
	switch {
	case data == "mdl_refresh":
		if !bc.isOwner(c) {
			return c.Edit("Permissão negada.")
		}
		return bc.refreshModelsFromCallback(c)
	case strings.HasPrefix(data, "mdl_prov_"):
		return bc.showModelPage(c, strings.TrimPrefix(data, "mdl_prov_"), 0)
	case strings.HasPrefix(data, "mdl_set_"):
		if !bc.isOwner(c) {
			return c.Edit("Permissão negada.")
		}
		return bc.setModelFromCallback(c, strings.TrimPrefix(data, "mdl_set_"))
	case strings.HasPrefix(data, "mdl_next_"):
		return bc.showModelPage(c, strings.TrimPrefix(data, "mdl_next_"), 1)
	case strings.HasPrefix(data, "mdl_prev_"):
		return bc.showModelPage(c, strings.TrimPrefix(data, "mdl_prev_"), -1)
	case data == "mdl_back":
		return bc.sendProviderMenu(c, true)
	case data == "mdl_cancel":
		return c.Edit("✅ Operação cancelada. O modelo continua: **" + bc.config.ModelDisplayName() + "**.")
	default:
		return nil
	}
}

func (bc *BotController) refreshModelsFromCallback(c telebot.Context) error {
	// Always send a new message instead of editing — ensures visibility of
	// errors and allows users to see what models were refreshed. Editing can
	// fail silently if the message was deleted or timing issues occur.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if bc.activeModelLister() == nil {
		return SendTextWithThread(bc.bot, c.Chat(), "Bridge indisponível. Tente novamente em alguns minutos.", c.Message().ThreadID)
	}

	models, err := bc.getModels(ctx, true)
	if err != nil {
		return SendErrorWithThread(bc.bot, c.Chat(), fmt.Sprintf("Erro ao atualizar modelos: %v", err), c.Message().ThreadID)
	}
	if len(models) == 0 {
		return SendTextWithThread(bc.bot, c.Chat(), "Nenhum modelo disponível após atualização.", c.Message().ThreadID)
	}

	// The timestamp makes repeated refreshes visibly change in Telegram instead
	// of failing with "message is not modified" when the provider list is equal.
	prefix := formatModelRefreshSummary(models, time.Now())
	return bc.sendProviderMenuMessage(c, true, prefix)
}

func formatModelRefreshSummary(models []bridge.ModelInfo, now time.Time) string {
	summary := fmt.Sprintf("✅ Modelos atualizados: **%d** disponíveis (%s).", len(models), now.Format("15:04:05"))
	locals := formatLocalModelSummary(models)
	if locals == "" {
		return summary
	}
	return summary + "\n" + locals
}

func formatLocalModelSummary(models []bridge.ModelInfo) string {
	grouped := make(map[string][]string)
	var providers []string
	for _, m := range models {
		if !isLocalModelProvider(m.Provider) {
			continue
		}
		if _, ok := grouped[m.Provider]; !ok {
			providers = append(providers, m.Provider)
		}
		grouped[m.Provider] = append(grouped[m.Provider], m.ID)
	}
	if len(providers) == 0 {
		return ""
	}
	sort.Strings(providers)
	parts := make([]string, 0, len(providers))
	for _, provider := range providers {
		parts = append(parts, fmt.Sprintf("**%s**: %s", provider, strings.Join(grouped[provider], ", ")))
	}
	return "Locais: " + strings.Join(parts, "; ")
}

func (bc *BotController) showModelPage(c telebot.Context, data string, dir int) error {
	// data: "provider" for initial, "provider_PAGE" for pagination
	lastUnderscore := strings.LastIndex(data, "_")
	page := 0
	provider := data
	if lastUnderscore > 0 {
		if p, err := strconv.Atoi(data[lastUnderscore+1:]); err == nil {
			page = p + dir
			provider = data[:lastUnderscore]
		}
	}
	if page < 0 {
		page = 0
	}

	bc.modelCacheMu.Lock()
	models := bc.modelCache
	bc.modelCacheMu.Unlock()

	var filtered []bridge.ModelInfo
	for _, m := range models {
		if m.Provider == provider {
			filtered = append(filtered, m)
		}
	}
	if len(filtered) == 0 {
		return nil
	}

	totalPages := (len(filtered) + modelsPerPage - 1) / modelsPerPage
	if page >= totalPages {
		page = totalPages - 1
	}

	start := page * modelsPerPage
	end := start + modelsPerPage
	if end > len(filtered) {
		end = len(filtered)
	}

	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row
	for _, m := range filtered[start:end] {
		label := m.ID
		if m.SupportsImages {
			label += " 📷"
		}
		rows = append(rows, menu.Row(menu.Data(label, "mdl_set_"+provider+"_"+m.ID)))
	}

	// Navigation row
	navRow := []telebot.Btn{}
	if page > 0 {
		navRow = append(navRow, menu.Data("◀", "mdl_prev_"+provider+"_"+strconv.Itoa(page)))
	}
	navRow = append(navRow, menu.Data(fmt.Sprintf("%d/%d", page+1, totalPages), "mdl_nop"))
	if page < totalPages-1 {
		navRow = append(navRow, menu.Data("▶", "mdl_next_"+provider+"_"+strconv.Itoa(page)))
	}
	rows = append(rows, navRow)
	rows = append(rows, menu.Row(menu.Data("← Provedores", "mdl_back")))
	rows = append(rows, menu.Row(menu.Data("❌ Cancelar", "mdl_cancel")))
	menu.Inline(rows...)

	return c.Edit(fmt.Sprintf("**%s** — página %d/%d:", provider, page+1, totalPages), menu)
}

func (bc *BotController) setModelFromCallback(c telebot.Context, data string) error {
	firstUnderscore := strings.Index(data, "_")
	if firstUnderscore <= 0 || firstUnderscore == len(data)-1 {
		return c.Edit("Modelo inválido.")
	}
	provider := data[:firstUnderscore]
	modelID := data[firstUnderscore+1:]

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := bc.getModels(ctx, false)
	if err != nil {
		return c.Edit("Não consegui validar este modelo. Tente novamente.")
	}
	if !modelExists(models, provider, modelID) {
		return c.Edit("Modelo indisponível. Abra /model e tente novamente.")
	}

	bc.config.DefaultModel = modelID
	bc.config.DefaultProvider = provider

	chatID := c.Chat().ID
	threadID := callbackThreadID(c)
	userID := safeSenderID(c.Sender())
	resetMsg := bc.resetCurrentModelSession(chatID, threadID, userID)

	if err := bc.saveDefaultModel(provider, modelID); err != nil {
		log.Printf("model callback persist: %v", err)
	}
	bc.invalidateModelCache()

	if bc.refreshProviderEnv != nil {
		bc.refreshProviderEnv()
	}

	return c.Edit(fmt.Sprintf("✅ Modelo alterado para **%s**\nProvedor: **%s**\n\n%s", modelID, provider, resetMsg))
}

func modelExists(models []bridge.ModelInfo, provider, modelID string) bool {
	for _, m := range models {
		if m.Provider == provider && m.ID == modelID {
			return true
		}
	}
	return false
}

func (bc *BotController) handleMemoryCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	payload := strings.TrimSpace(c.Message().Payload)

	svc := memoryuxpkg.New(bc.memoryDir, bc.resolver)
	cwd := bc.currentCwd(chatID, threadID)

	parts := strings.Fields(payload)
	var cmd string
	var args string
	if len(parts) > 0 {
		cmd = strings.ToLower(parts[0])
		if len(parts) > 1 {
			args = strings.Join(parts[1:], " ")
		}
	}

	log.Printf("memory command: action=%s chat=%d thread=%d cwd_set=%t",
		cmd, chatID, threadID, cwd != "")

	switch cmd {
	case "", "status":
		status, err := svc.Status(chatID, threadID, cwd)
		if err != nil {
			return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), threadID)
		}
		log.Printf("memory command: status complete chat=%d layers=%d", chatID, len(status.Layers))
		return SendTextWithThread(bc.bot, c.Chat(), memoryuxpkg.FormatStatus(status), threadID)
	case "checkpoint":
		result, err := svc.WriteCheckpoint(chatID, threadID, cwd, args)
		if err != nil {
			return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), threadID)
		}
		if result.Path != "" {
			log.Printf("memory command: checkpoint written chat=%d layer=%s path=%s", chatID, result.Layer, result.Path)
		}
		return SendTextWithThread(bc.bot, c.Chat(), memoryuxpkg.FormatCheckpoint(result), threadID)
	default:
		msg := "🧠 **Memory Commands**\n\n" +
			"`/memory` or `/memory status` — Show active memory layers\n" +
			"`/memory checkpoint [note]` — Write/update a task checkpoint"
		return SendTextWithThread(bc.bot, c.Chat(), msg, threadID)
	}
}

func callbackThreadID(c telebot.Context) int {
	cb := c.Callback()
	if cb == nil || cb.Message == nil {
		return 0
	}
	return cb.Message.ThreadID
}

func (bc *BotController) handleUsersCommand(c telebot.Context) error {
	defer bc.confirmMessage(c.Message())
	reply, err := bc.cmdUsers(c)
	if err != nil {
		return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), c.Message().ThreadID)
	}
	return SendTextWithThread(bc.bot, c.Chat(), reply, c.Message().ThreadID)
}

func (bc *BotController) handleForgetMeCommand(c telebot.Context) error {
	reply, err := bc.cmdForgetMe(c)
	if err != nil {
		return SendErrorWithThread(bc.bot, c.Chat(), err.Error(), c.Message().ThreadID)
	}
	if reply != "" {
		return SendTextWithThread(bc.bot, c.Chat(), reply, c.Message().ThreadID)
	}
	return nil
}

func (bc *BotController) handleForgetMeConfirm(c telebot.Context) error {
	senderID := safeSenderID(c.Sender())
	_ = bc.bot.Respond(c.Callback(), &telebot.CallbackResponse{Text: "Apagando dados..."})

	// Cancel active runs for this user
	_ = bc.cancelActiveRunForUser(senderID)

	// Delete cron jobs
	if bc.cronHandler != nil {
		ctx := context.Background()
		jobs, _ := bc.cronHandler.service.ListJobsByOwner(ctx, fmt.Sprintf("%d", senderID))
		for _, j := range jobs {
			_ = bc.cronHandler.service.DeleteJobByOwner(ctx, fmt.Sprintf("%d", senderID), j.ID)
		}
	}

	// Delete user data
	if err := bc.userStore.Delete(senderID); err != nil {
		return err
	}

	// Edit the original message to show confirmation
	_, err := c.Bot().Send(c.Chat(), "Seus dados foram apagados. Próxima mensagem iniciará o onboarding.", &telebot.SendOptions{ThreadID: callbackThreadID(c)})
	return err
}

func (bc *BotController) handleForgetMeCancel(c telebot.Context) error {
	_ = bc.bot.Respond(c.Callback(), &telebot.CallbackResponse{Text: "Cancelado"})
	_, err := c.Bot().Send(c.Chat(), "Operação cancelada. Seus dados foram preservados.", &telebot.SendOptions{ThreadID: callbackThreadID(c)})
	return err
}

// cancelActiveRunForUser cancels all active runs for a given user across all chats/threads.
func (bc *BotController) cancelActiveRunForUser(userID int64) bool {
	if bc == nil || bc.pipeline == nil {
		return false
	}
	return bc.pipeline.CancelAllForUser(userID)
}
