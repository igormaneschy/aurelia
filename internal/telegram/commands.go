package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"gopkg.in/telebot.v3"

	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/cron"
	memoryuxpkg "github.com/igormaneschy/aurelia/internal/memoryux"
	pipelinepkg "github.com/igormaneschy/aurelia/internal/pipeline"
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/users"
)

// CommandType identifies a system command that can be handled locally without LLM.
type CommandType int

const (
	CmdCronCreate CommandType = iota
	CmdCronList
	CmdCronCancel
	CmdSessionReset
	CmdStatus
	CmdListAgents
	CmdListModels
	CmdSetModel
	CmdRefreshModels
	CmdMemoryStatus
	CmdMemoryCheckpoint
	CmdUsers
	CmdForgetMe
	CmdDebugLast
	CmdDebugRun
	CmdDebugErrors
	CmdSetMode
	CmdExplainProfile // Phase 2: /mode explain <name>, /agents explain <name>
)

// MatchedCommand represents a message that was identified as a system command.
type MatchedCommand struct {
	Type CommandType
	Text string // original message text
}

// commandRule defines a pattern for matching a command.
type commandRule struct {
	cmdType CommandType
	phrases []string // phrase matches (checked in order)
	exact   bool     // if true, the entire message must equal one of the phrases
}

// commandPhrases are normalized (lowercase, accent-stripped) at init time so
// MatchCommand can do a single accent-strip on the input and compare.
var commandRules = []commandRule{
	// cron_list (check before cron_create to avoid "lista agendamentos" matching create)
	{CmdCronList, []string{
		"meus agendamentos",
		"o que ta agendado",
		"lista agendamentos", "listar agendamentos",
	}, false},
	// cron_cancel (check before cron_create to avoid "cancela" partial matching)
	{CmdCronCancel, []string{
		"cancela o agendamento", "cancela agendamento", "cancelar agendamento",
		"cancele o agendamento", "cancele agendamento",
		"remove o agendamento", "remove agendamento",
		"remove o lembrete", "remove lembrete", "remover lembrete",
		"desativa agendamento", "desativar agendamento",
		"exclui agendamento", "excluir agendamento",
		"deleta agendamento", "deletar agendamento",
		"apaga agendamento", "apagar agendamento",
	}, false},
	// explain_profile — /mode explain <name> and /agents explain <name>.
	// Keep before the generic /mode rule.
	{CmdExplainProfile, []string{
		"/mode explain ", "/modo explain ", "/perfil explain ",
		"/agents explain ",
	}, false},
	// mode_set — slash commands use substring match; natural language uses exact
	{CmdSetMode, []string{
		"/mode ", "/modo ", "/perfil ",
	}, false},
	// mode_set_exact — natural-language mode commands must be exact (no "modo devops" false positive)
	{CmdSetMode, []string{
		"modo dev", "modo desenvolvedor", "modo developer",
		"modo pesquisa", "modo pesquisador", "modo researcher",
		"modo geral",
		"perfil dev", "perfil developer", "perfil desenvolvedor",
		"perfil pesquisa", "perfil pesquisador", "perfil researcher",
		"perfil geral",
	}, true},
	// mode_query (exact match)
	{CmdSetMode, []string{
		"/mode",
		"qual meu modo", "qual o meu modo",
		"meu modo atual",
		"qual meu perfil", "qual o meu perfil",
		"meu perfil atual",
	}, true},
	// cron_create
	{CmdCronCreate, []string{
		"agenda ", "agendar ", "agende ",
		"cria um lembrete", "cria um agendamento", "criar lembrete", "criar agendamento",
		"crie um lembrete", "crie um agendamento",
		"me lembra ", "me lembre ",
	}, false},
	// session_reset ("reset" is exact-only but other phrases use substring)
	{CmdSessionReset, []string{
		"nova conversa",
		"limpa o contexto", "limpar contexto", "limpa contexto",
		"comeca de novo",
	}, false},
	{CmdSessionReset, []string{
		"reset",
	}, true},
	// status (exact match only — "status" is too common as a word)
	{CmdStatus, []string{
		"status",
		"ta funcionando",
	}, true},
	// list_agents — exact-only per Prompt Profiles spec §9.2 acceptance 6
	{CmdListAgents, []string{
		"quais agents", "quais perfis",
		"lista agents", "lista perfis",
		"listar agents", "listar perfis",
		"meus agents", "meus perfis",
	}, true},
	// list_models
	{CmdListModels, []string{
		"quais modelos", "lista modelos", "listar modelos",
		"quais provedores", "lista provedores", "listar provedores",
	}, false},
	// set_model
	{CmdSetModel, []string{
		"muda modelo", "mudar modelo", "troca modelo", "trocar modelo",
		"escolhe modelo", "seleciona modelo",
		"/model ",
	}, false},
	// refresh_models — natural-language alias for /model refresh.
	// Substring match keeps "atualiza a lista de modelos" and variants working.
	{CmdRefreshModels, []string{
		"atualiza modelos", "atualizar modelos", "atualiza a lista",
		"refresh modelos", "recarregar modelos",
	}, false},
	// memory_status
	{CmdMemoryStatus, []string{
		"memory status",
		"status memoria", "status da memoria",
	}, true},
	// memory_checkpoint
	{CmdMemoryCheckpoint, []string{
		"memory checkpoint",
		"checkpoint memoria", "checkpoint de memoria",
	}, false},
	// users (owner only)
	{CmdUsers, []string{
		"users", "usuarios", "lista usuarios", "lista usuários",
	}, true},
	// forget_me
	{CmdForgetMe, []string{
		"forget-me", "forget me", "apagar meus dados",
		"deletar meus dados", "esquecer",
	}, true},
	// debug_last (exact match only)
	{CmdDebugLast, []string{
		"/debug last",
		"ultima execucao", "última execução", "debug status",
	}, true},
	// debug_run (starts with prefix — handled in handler)
	{CmdDebugRun, []string{
		"/debug run",
	}, false},
	// debug_errors
	{CmdDebugErrors, []string{
		"/debug errors",
		"ultimos erros", "últimos erros", "debug erros",
	}, true},
}

// accentReplacer maps common Portuguese diacritics to ASCII. Shared with the
// concurrent-message classifier so command matching and intent detection use
// the same normalization.
var accentReplacer = strings.NewReplacer(
	"á", "a", "à", "a", "ã", "a", "â", "a",
	"é", "e", "ê", "e",
	"í", "i",
	"ó", "o", "ô", "o", "õ", "o",
	"ú", "u",
	"ç", "c",
)

func stripAccents(s string) string {
	return accentReplacer.Replace(s)
}

// slashBotMentionRE matches a slash command followed by an @botname mention
// (e.g., "/mode@ManeDev_bot"). Telegram clients append this in group and
// topic chats to disambiguate which bot should handle the command. We strip
// it before command matching so the same rules work in private and group
// contexts. Only strips mentions of slash commands — leaves other @mentions
// in the text untouched.
var slashBotMentionRE = regexp.MustCompile(`^(/[A-Za-z0-9_]+)@[A-Za-z0-9_]+`)

// stripBotMention removes an optional @botname suffix from a slash command
// at the start of text. Returns text unchanged when no match.
func stripBotMention(text string) string {
	return slashBotMentionRE.ReplaceAllString(text, "$1")
}

// MatchCommand checks if a message is a system command. Returns nil if no match.
// Uses keyword matching with a narrative-context heuristic to avoid false positives.
// Diacritics are stripped so "começa de novo" and "comeca de novo" match equally.
// A leading @botname suffix on slash commands is stripped so the same rules
// apply in private chats and in group/topic chats (where Telegram appends
// "@ManeDev_bot" to disambiguate the target bot).
func MatchCommand(text string) *MatchedCommand {
	lower := stripAccents(strings.ToLower(strings.TrimSpace(text)))
	lower = stripBotMention(lower)
	if lower == "" {
		return nil
	}

	for _, rule := range commandRules {
		for _, phrase := range rule.phrases {
			if rule.exact {
				// Strip trailing punctuation for exact match comparison
				cleaned := strings.TrimRightFunc(lower, func(r rune) bool {
					return unicode.IsPunct(r) || unicode.IsSpace(r)
				})
				if cleaned == phrase {
					return &MatchedCommand{Type: rule.cmdType, Text: text}
				}
				continue
			}

			idx := strings.Index(lower, phrase)
			if idx < 0 {
				continue
			}
			// Anti-false-positive: if the phrase is not at the start,
			// check if the preceding text looks like narrative context.
			if idx > 0 && looksNarrative(lower[:idx]) {
				continue
			}
			return &MatchedCommand{Type: rule.cmdType, Text: text}
		}
	}
	return nil
}

// looksNarrative returns true if the prefix text suggests the keyword appears
// inside a narrative sentence rather than as a command. We check if there are
// word characters before the keyword (excluding small connectors like "me", "um").
func looksNarrative(prefix string) bool {
	trimmed := strings.TrimRightFunc(prefix, unicode.IsSpace)
	if trimmed == "" {
		return false
	}

	// Count significant words (3+ chars) in the prefix
	words := strings.Fields(trimmed)
	significant := 0
	for _, w := range words {
		if len(w) >= 3 {
			significant++
		}
	}

	// If there are 2+ significant words before the keyword, it's likely narrative.
	// "ontem eu tentei agendar" → 3 significant words → narrative
	// "me lembra" → 0 significant words (only "me") → not narrative
	return significant >= 2
}

// handleCommand dispatches a matched command to the appropriate handler.
// Returns nil if the command was handled (response sent to Telegram).
func (bc *BotController) handleCommand(c telebot.Context, cmd *MatchedCommand) error {
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	userID := safeSenderID(c.Sender())
	log.Printf("command: type=%d chat=%d thread=%d user=%d len=%d", cmd.Type, chatID, threadID, userID, len(cmd.Text))
	defer bc.confirmMessage(c.Message())

	var reply string
	var err error

	switch cmd.Type {
	case CmdSessionReset:
		reply, err = bc.cmdSessionReset(chatID, threadID, userID, c.Chat().Type == telebot.ChatPrivate)
	case CmdCronList:
		reply, err = bc.cmdCronList(chatID, threadID, userID)
	case CmdCronCancel:
		reply, err = bc.cmdCronCancel(chatID, threadID, userID, cmd.Text)
	case CmdCronCreate:
		reply, err = bc.cmdCronCreate(c, cmd.Text)
	case CmdStatus:
		reply, err = bc.cmdStatus(chatID, threadID, userID)
	case CmdListAgents:
		reply, err = bc.cmdListAgents(c)
	case CmdListModels:
		reply, err = bc.cmdListModels()
	case CmdSetModel:
		if !bc.isOwner(c) {
			reply = "Permissão negada. Apenas o owner pode trocar o modelo."
			break
		}
		reply, err = bc.cmdSetModel(c, cmd.Text)
	case CmdRefreshModels:
		if !bc.isOwner(c) {
			reply = "Permissão negada. Apenas o owner pode atualizar a lista de modelos."
			break
		}
		reply, err = bc.cmdRefreshModels()
	case CmdMemoryStatus:
		reply, err = bc.cmdMemoryStatus(c.Chat().ID, c.Message().ThreadID, userID, c.Chat().Type == telebot.ChatPrivate)
	case CmdMemoryCheckpoint:
		reply, err = bc.cmdMemoryCheckpoint(c.Chat().ID, c.Message().ThreadID, userID, c.Chat().Type == telebot.ChatPrivate, cmd.Text)
	case CmdUsers:
		reply, err = bc.cmdUsers(c)
	case CmdForgetMe:
		reply, err = bc.cmdForgetMe(c)
	case CmdDebugLast:
		if !bc.isOwner(c) {
			reply = "Permissão negada. Comando apenas para o owner."
			break
		}
		reply, err = bc.cmdDebugLast(chatID, threadID)
	case CmdDebugRun:
		if !bc.isOwner(c) {
			reply = "Permissão negada. Comando apenas para o owner."
			break
		}
		reply, err = bc.cmdDebugRun(chatID, threadID, cmd.Text)
	case CmdDebugErrors:
		if !bc.isOwner(c) {
			reply = "Permissão negada. Comando apenas para o owner."
			break
		}
		reply, err = bc.cmdDebugErrors()
	case CmdSetMode:
		reply, err = bc.cmdSetMode(c, cmd.Text)
	case CmdExplainProfile:
		reply, err = bc.cmdExplainProfile(c, cmd.Text)
	default:
		return fmt.Errorf("unknown command type: %d", cmd.Type)
	}

	if err != nil {
		log.Printf("command error: type=%d err=%v", cmd.Type, pipelinepkg.RedactSecrets(err.Error()))
		return SendErrorWithThread(bc.bot, c.Chat(), fmt.Sprintf("Erro ao executar comando: %v", err), threadID)
	}

	return SendTextReplyWithThread(bc.bot, c.Chat(), reply, threadID)
}

// --- P1 handlers ---

func (bc *BotController) cmdSessionReset(chatID int64, threadID int, userID int64, isPrivateChat bool) (string, error) {
	return bc.resetCurrentSession(chatID, threadID, true, userID, isPrivateChat)
}

func (bc *BotController) resetCurrentSession(chatID int64, threadID int, invalidate bool, userID int64, isPrivateChat bool) (string, error) {
	canceledActive := bc.cancelActiveRun(chatID, threadID, userID)
	if bc.dreamer != nil && bc.sessions != nil {
		cwd := bc.currentCwdForContext(chatID, threadID, userID, isPrivateChat)
		sessionFile := bc.sessions.GetSession(chatID, threadID, userID)
		bc.dreamer.FlushNudge(chatID, threadID, userID, cwd, sessionFile, bc.nudgeBuffer)
		if invalidate {
			bc.invalidateMemoryDirs(chatID, threadID, userID, cwd)
		}
	}
	if bc.sessions != nil {
		bc.sessions.ClearSessionForUser(chatID, threadID, userID)
	}
	log.Printf("command: session reset for chat=%d thread=%d user=%d", chatID, threadID, userID)
	return formatResetSummary(canceledActive), nil
}

func (bc *BotController) cancelActiveRun(chatID int64, threadID int, userID ...int64) bool {
	if bc == nil || bc.pipeline == nil {
		return false
	}
	return bc.pipeline.Cancel(chatID, threadID, userID...)
}

func formatResetSummary(canceledActive bool) string {
	prefix := ""
	if canceledActive {
		prefix = "🛑 Processamento em andamento interrompido.\n"
	}
	return prefix + "🗑️ Sessão resetada. Próxima mensagem inicia conversa nova."
}

func (bc *BotController) cmdCronList(chatID int64, threadID int, userID int64) (string, error) {
	if bc.cronHandler == nil {
		return "Sistema de agendamentos não disponível.", nil
	}
	ctx := context.Background()
	userIDStr := fmt.Sprintf("%d", userID)
	jobs, err := bc.cronHandler.service.ListJobsByOwner(ctx, userIDStr)
	if err != nil {
		return "", fmt.Errorf("falha ao listar agendamentos: %w", err)
	}
	// Filter by current chat and thread so jobs from other groups/topics
	// or different threads are not exposed.
	var filtered []cron.CronJob
	for _, j := range jobs {
		if j.TargetChatID == chatID && j.TargetThreadID == threadID {
			filtered = append(filtered, j)
		}
	}
	if len(filtered) == 0 {
		return "Nenhum agendamento encontrado neste chat.", nil
	}
	return formatCronJobs(filtered), nil
}

func (bc *BotController) cmdCronCancel(chatID int64, threadID int, userID int64, text string) (string, error) {
	if bc.cronHandler == nil {
		return "Sistema de agendamentos não disponível.", nil
	}

	jobID := extractLastWord(text)
	if jobID == "" || !looksLikeJobID(jobID) {
		return "Não encontrei o ID do agendamento na sua mensagem. Use 'meus agendamentos' pra ver a lista com os IDs.", nil
	}

	ctx := context.Background()
	userIDStr := fmt.Sprintf("%d", userID)

	// Verify the job belongs to this owner + current chat + thread before deletion.
	// Fetch owner's jobs and find the matching ID that is in this chat+thread.
	ownerJobs, err := bc.cronHandler.service.ListJobsByOwner(ctx, userIDStr)
	if err != nil {
		return "", fmt.Errorf("falha ao buscar agendamentos: %w", err)
	}
	var match *cron.CronJob
	for _, j := range ownerJobs {
		if (j.ID == jobID || strings.HasPrefix(j.ID, jobID)) && j.TargetChatID == chatID && j.TargetThreadID == threadID {
			match = &j
			break
		}
	}
	if match == nil {
		return fmt.Sprintf("Nenhum agendamento com ID `%s` foi encontrado neste chat.", jobID), nil
	}

	if err := bc.cronHandler.service.DeleteJobByOwner(ctx, userIDStr, match.ID); err != nil {
		if errMsg := err.Error(); strings.Contains(strings.ToLower(errMsg), "not found") {
			return fmt.Sprintf("Nenhum agendamento com ID `%s` foi encontrado.", jobID), nil
		}
		return "", fmt.Errorf("falha ao cancelar agendamento: %w", err)
	}

	return fmt.Sprintf("Agendamento `%s` removido.", jobID), nil
}

// extractLastWord returns the last whitespace-separated token from text.
func extractLastWord(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

// looksLikeJobID checks if a string looks like a UUID prefix (alphanumeric, 4+ chars).
func looksLikeJobID(s string) bool {
	if len(s) < 4 {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'f') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

const cronParseTimeout = 10 * time.Second

// cronCreateParsed holds the extracted scheduling parameters from LLM response.
type cronCreateParsed struct {
	Type     string `json:"type"`
	CronExpr string `json:"cron_expr,omitempty"`
	RunAt    string `json:"run_at,omitempty"`
	Prompt   string `json:"prompt"`
}

// fenceRe strips markdown code fences (with optional "json" language tag)
// from LLM responses before JSON parsing.
var fenceRe = regexp.MustCompile("(?s)^```(?:json)?\\s*(.+?)\\s*```$")

// parseCronCreateResponse extracts JSON from the LLM response, tolerating markdown fences.
func parseCronCreateResponse(raw string) (*cronCreateParsed, error) {
	// Strip markdown code fences if present
	cleaned := strings.TrimSpace(raw)
	if m := fenceRe.FindStringSubmatch(cleaned); m != nil {
		cleaned = strings.TrimSpace(m[1])
	}

	var parsed cronCreateParsed
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, fmt.Errorf("JSON parse error: %w", err)
	}

	if parsed.Prompt == "" {
		return nil, fmt.Errorf("missing prompt in response")
	}

	switch parsed.Type {
	case "cron":
		if parsed.CronExpr == "" {
			return nil, fmt.Errorf("missing cron_expr for cron type")
		}
	case "once":
		if parsed.RunAt == "" {
			return nil, fmt.Errorf("missing run_at for once type")
		}
	default:
		return nil, fmt.Errorf("unknown type %q", parsed.Type)
	}

	return &parsed, nil
}

func (bc *BotController) cmdCronCreate(c telebot.Context, text string) (string, error) {
	if bc.cronHandler == nil {
		return "Sistema de agendamentos não disponível.", nil
	}

	userID := safeSenderID(c.Sender())
	tzName, loc := bc.userTimezone(userID)
	now := time.Now().In(loc)

	// Fast path: try local regex parser before paying the LLM round-trip.
	// Pass location-aware now so "tomorrow" and "today" use user-local days.
	parsed := cronFastParse(text, now)
	if parsed == nil {
		if bc.bridge == nil {
			return "Processador não disponível para interpretar o agendamento.", nil
		}
		llmParsed, err := bc.parseCronWithLLM(text, now, displayTimezoneName(tzName))
		if err != nil {
			return "Não consegui interpretar o agendamento. Tente algo como: \"agenda todo dia às 9h revisar emails\"", nil
		}
		if llmParsed == nil {
			return "Não entendi o agendamento. Tente algo como: \"agenda todo dia às 9h revisar emails\"", nil
		}
		parsed = llmParsed
	} else {
		log.Printf("command: cron_create fast-path matched (type=%s)", parsed.Type)
	}

	ctx := context.Background()
	chatID := c.Chat().ID
	threadID := c.Message().ThreadID
	userIDStr := fmt.Sprintf("%d", userID)

	// DefaultCWD fallback applies only in private chats (no topic).
	// In groups / supergroups / topics the user must set /cwd explicitly —
	// DefaultCWD is a per-user convenience, not a shared project context.
	isPrivateChat := c.Chat().Type == telebot.ChatPrivate
	cwd := bc.currentCwdForContext(chatID, threadID, userID, isPrivateChat)

	var (
		jobID string
		err   error
	)
	switch parsed.Type {
	case "cron":
		jobID, err = bc.cronHandler.service.AddRecurringJob(ctx, userIDStr, chatID, threadID, parsed.CronExpr, parsed.Prompt, cwd, tzName)
	case "once":
		jobID, err = bc.cronHandler.service.AddOnceJob(ctx, userIDStr, chatID, threadID, parsed.RunAt, parsed.Prompt, cwd)
	default:
		return "Não consegui determinar o tipo de agendamento.", nil
	}

	if err != nil {
		return "", fmt.Errorf("falha ao criar agendamento: %w", err)
	}

	switch parsed.Type {
	case "cron":
		return fmt.Sprintf("Agendamento recorrente criado (`%s`)\nSchedule: `%s`\nPrompt: %s", jobID, parsed.CronExpr, parsed.Prompt), nil
	default:
		return fmt.Sprintf("Agendamento pontual criado (`%s`)\nQuando: %s\nPrompt: %s", jobID, parsed.RunAt, parsed.Prompt), nil
	}
}

func (bc *BotController) cmdStatus(chatID int64, threadID int, userID int64) (string, error) {
	var lines []string
	lines = append(lines, "**Status da Aurelia**\n")

	// Bridge status
	bridgeStatus := "desligado"
	if bc.bridge != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := bc.bridge.Ping(ctx); err == nil {
			bridgeStatus = "online"
		} else {
			bridgeStatus = "offline"
		}
	}
	lines = append(lines, fmt.Sprintf("🧠 Processador: **%s**", bridgeStatus))
	lines = append(lines, statusWorkLines(bc.currentWorkStatus(chatID, threadID, userID))...)

	// Agents
	agentCount := 0
	if bc.agents != nil {
		agentCount = len(bc.agents.Agents())
	}
	lines = append(lines, fmt.Sprintf("🤖 Perfis disponíveis: **%d**", agentCount))

	// Cron jobs
	if bc.cronHandler != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		jobs, err := bc.cronHandler.service.ListJobs(ctx, chatID)
		if err == nil {
			active := 0
			for _, j := range jobs {
				if j.Active {
					active++
				}
			}
			lines = append(lines, fmt.Sprintf("⏰ Agendamentos ativos: **%d**", active))
		}
	}

	// Model
	if bc.config != nil {
		lines = append(lines, fmt.Sprintf("⚙️ Modelo: **%s**", bc.config.ModelDisplayName()))
	}

	if bc.sessions != nil {
		if cwd := bc.currentCwd(chatID, threadID); cwd != "" {
			lines = append(lines, fmt.Sprintf("📂 Diretório: `%s`", cwd))
		}

		if _, active := bc.sessions.GetSessionWithState(chatID, threadID, userID); active {
			lines = append(lines, "💬 Sessão: conversa ativa")
		} else {
			lines = append(lines, "💬 Sessão: nenhuma conversa ativa no momento")
		}
	}

	if bc.pipeline != nil {
		snap := bc.pipeline.GetActiveToolSnapshot(chatID, threadID, userID)
		if snap.ToolCount > 0 {
			lines = append(lines, "🔧 Trabalho atual: usando ferramentas")
			if snap.LoopWarned {
				lines = append(lines, "⚠️ Notei repetição no trabalho atual e já pedi uma consolidação.")
			}
		}
	}

	return strings.Join(lines, "\n"), nil
}

// statusRunLogSummary returns formatted lines describing the latest run state,
// including run_id, status, provider/model, duration, cost, and a checkpoint
// excerpt. The output is concise and redacted.
func statusRunLogSummary(rl runlog.Store, chatID int64, threadID int) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	record, err := rl.Latest(ctx, chatID, threadID)
	if err != nil || record == nil {
		return nil
	}

	emoji := "⬜"
	switch record.Status {
	case runlog.RunRunning:
		emoji = "🟡"
	case runlog.RunCompleted:
		emoji = "✅"
	case runlog.RunTimedOut:
		emoji = "⏰"
	case runlog.RunCanceled:
		emoji = "🛑"
	case runlog.RunFailed:
		emoji = "❌"
	}

	when := "agora"
	if !record.StartedAt.IsZero() {
		ago := time.Since(record.StartedAt).Round(time.Second)
		if ago > 0 {
			when = ago.String() + " atrás"
		}
	}

	var lines []string

	// Line 1: status + run_id + elapsed
	rid := record.RunID
	if len(rid) > 8 {
		rid = rid[:8]
	}
	statusLine := fmt.Sprintf("%s Última execução: **%s** · `%s` (%s)", emoji, record.Status, rid, when)

	// Append duration when available
	if record.DurationMs > 0 {
		dur := time.Duration(record.DurationMs) * time.Millisecond
		statusLine += fmt.Sprintf(" · %s", dur.Round(time.Second))
	}
	lines = append(lines, statusLine)

	// Line 2: provider/model when available
	if record.Provider != "" || record.Model != "" {
		modelLine := fmt.Sprintf("⚙️ Modelo: **%s/%s**", record.Provider, record.Model)
		if record.UsedFallback {
			modelLine += " ⚠️ fallback"
		}
		lines = append(lines, modelLine)
	}

	// Line 3: tokens/cost when available
	if record.CostUSD > 0 || record.InputTokens > 0 {
		lines = append(lines, fmt.Sprintf("💰 tokens: %d in / %d out · $%.4f",
			record.InputTokens, record.OutputTokens, record.CostUSD))
	}

	// Error/timeout info for terminal failures
	if record.Status == runlog.RunFailed || record.Status == runlog.RunTimedOut {
		errMsg := record.Error
		if record.TimeoutOrigin != "" {
			errMsg = fmt.Sprintf("timeout: %s", record.TimeoutOrigin)
		}
		if errMsg != "" {
			// Redact secrets but show the error class.
			safeErr := statusRedactSecrets(errMsg)
			runes := []rune(safeErr)
			if len(runes) > 200 {
				runes = runes[:200]
				safeErr = string(runes) + "..."
			}
			lines = append(lines, fmt.Sprintf("❌ Erro: `%s`", safeErr))
		}
	}

	// Agent name when available
	if record.AgentName != "" {
		lines = append(lines, fmt.Sprintf("🤖 Agent: **%s**", record.AgentName))
	}

	// Checkpoint excerpt (redacted, rune-safe truncation, max 200 chars)
	if record.Checkpoint != "" {
		excerpt := statusRedactSecrets(record.Checkpoint)
		runes := []rune(excerpt)
		if len(runes) > 200 {
			runes = runes[:200]
			excerpt = string(runes) + "..."
		}
		lines = append(lines, fmt.Sprintf("📋 Checkpoint: `%s`", excerpt))
	}

	// Continuation hint for non-completed runs (includes failed — retryable)
	if record.Status != runlog.RunCompleted {
		lines = append(lines, "💡 Digite **\"continua\"** para retomar de onde parou.")
	}

	return lines
}

func (bc *BotController) currentWorkStatus(chatID int64, threadID int, userID int64) (string, int) {
	if bc == nil || bc.pipeline == nil {
		return "", 0
	}
	return bc.pipeline.WorkStatus(chatID, threadID, userID)
}

func statusWorkLines(description string, queueSize int) []string {
	if strings.TrimSpace(description) == "" {
		return nil
	}
	lines := []string{fmt.Sprintf("⏳ Em andamento: %s", description)}
	if queueSize > 0 {
		lines = append(lines, "📨 Mensagens recentes já foram entregues ao PI SDK.")
	}
	return lines
}

func (bc *BotController) cmdListAgents(c telebot.Context) (string, error) {
	userID := safeSenderID(c.Sender())
	isOwner := bc.isOwner(c)

	// Use profiles resolver for canonical list (includes legacy agents + builtins + canonical).
	var all []*profiles.PromptProfile
	if bc.profiles != nil {
		all = bc.profiles.ListVisible(isOwner)
	} else if bc.agents != nil {
		// Fallback: legacy agents only when resolver not available.
		for _, a := range bc.agents.Agents() {
			all = append(all, profiles.FromAgent(a))
		}
	}
	if len(all) == 0 {
		return "Nenhum perfil disponível. Perfis built-in (general, developer, researcher) estão sempre disponíveis.", nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("**Perfis disponíveis** (%d)\n", len(all)))
	for _, p := range all {
		desc := p.Description
		if desc == "" {
			desc = "(sem descrição)"
		}
		lines = append(lines, fmt.Sprintf("- **%s**: %s", p.Name, desc))
	}

	// Mode section
	modeSection := bc.buildModeListSection(userID)
	if modeSection != "" {
		lines = append(lines, modeSection)
	}

	return strings.Join(lines, "\n"), nil
}

// buildModeListSection returns a formatted "Modos disponíveis" section.
// Returns "" when user store is unavailable or profile load fails.
func (bc *BotController) buildModeListSection(userID int64) string {
	activeMode := ""
	if bc.userStore != nil {
		if profile, err := bc.userStore.Get(userID); err == nil && profile != nil {
			activeMode = profile.ActiveMode
		} else if err != nil {
			log.Printf("command: list agents failed to load profile user=%d: %v", userID, err)
		}
	}
	// Default to "general" when empty
	displayActive := activeMode
	if displayActive == "" {
		displayActive = "general"
	}
	profiles := []string{"general", "developer", "researcher"}
	var lines []string
	lines = append(lines, "\n**Perfis disponíveis**")
	for _, p := range profiles {
		marker := ""
		if p == displayActive {
			marker = " (● ativo)"
		}
		lines = append(lines, fmt.Sprintf("- **%s**%s", p, marker))
	}
	lines = append(lines, "\nUse /mode <perfil> para definir o perfil padrão.")
	lines = append(lines, "Use @perfil <pedido> para aplicar um perfil só nesta mensagem.")
	return strings.Join(lines, "\n")
}

func (bc *BotController) cmdListModels() (string, error) {
	// Always show current model first
	currentLine := bc.currentModelLine()

	if bc.activeModelLister() == nil {
		return currentLine + "\n\nProcessador não disponível para listar modelos. Use /model auto para usar o PI default.", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	models, err := bc.getModels(ctx, false)
	if err != nil {
		return currentLine + fmt.Sprintf("\n\nLista não disponível: %v", err), nil
	}

	var lines []string
	lines = append(lines, currentLine)
	lines = append(lines, "\n\n**Modelos disponíveis:**")

	// Group by provider (limit to 25 for Telegram readability)
	type provInfo struct{ models []string }
	grouped := make(map[string]*provInfo)
	var providerOrder []string
	for _, m := range models {
		if grouped[m.Provider] == nil {
			grouped[m.Provider] = &provInfo{}
			providerOrder = append(providerOrder, m.Provider)
		}
		display := fmt.Sprintf("`%s`", m.ID)
		if m.SupportsImages {
			display += " 📷"
		}
		grouped[m.Provider].models = append(grouped[m.Provider].models, display)
	}

	sortModelProviderOrder(providerOrder, bc.config.DefaultProvider)

	displayed := 0
	const maxDisplay = 25
	for _, prov := range providerOrder {
		if displayed >= maxDisplay {
			remaining := len(models) - displayed
			lines = append(lines, fmt.Sprintf("\n... e mais %d modelos", remaining))
			break
		}
		lines = append(lines, fmt.Sprintf("\n%s:", prov))
		for _, m := range grouped[prov].models {
			if displayed >= maxDisplay {
				break
			}
			lines = append(lines, fmt.Sprintf("  %s", m))
			displayed++
		}
	}

	lines = append(lines, "\n\nUse /model <nome> para trocar ou /model auto para usar o PI default.")
	lines = append(lines, "Lista em cache (5 min) — diga 'atualiza modelos' para refresh imediato.")
	return strings.Join(lines, "\n"), nil
}

func sortModelProviderOrder(providers []string, currentProvider string) {
	sort.SliceStable(providers, func(i, j int) bool {
		leftRank := modelProviderDisplayRank(providers[i], currentProvider)
		rightRank := modelProviderDisplayRank(providers[j], currentProvider)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return strings.ToLower(providers[i]) < strings.ToLower(providers[j])
	})
}

func modelProviderDisplayRank(provider, currentProvider string) int {
	if currentProvider != "" && provider == currentProvider {
		return 0
	}
	if isLocalModelProvider(provider) {
		return 1
	}
	return 2
}

func isLocalModelProvider(provider string) bool {
	normalized := strings.ToLower(provider)
	return strings.Contains(normalized, "ollama") || strings.Contains(normalized, "lm-studio")
}

func (bc *BotController) cmdRefreshModels() (string, error) {
	if bc.activeModelLister() == nil {
		return "Processador não disponível para atualizar modelos.", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	models, err := bc.getModels(ctx, true)
	if err != nil {
		return "", fmt.Errorf("falha ao atualizar modelos: %w", err)
	}
	if len(models) == 0 {
		return "Nenhum modelo disponível após atualização.", nil
	}
	return fmt.Sprintf("✅ Modelos atualizados: **%d** disponíveis. Use /model para abrir o menu.", len(models)), nil
}

func (bc *BotController) cmdSetModel(c telebot.Context, text string) (string, error) {
	if !bc.isOwner(c) {
		return "Permissão negada. Apenas o owner pode trocar o modelo.", nil
	}

	// Extract the model name from the text
	modelName := extractModelName(text)
	if modelName == "" {
		return "Use /model <nome>, /model auto ou 'muda modelo para <nome>' para trocar.\n\n" +
			"Digite 'lista modelos' para ver as opções disponíveis.", nil
	}
	if isRefreshModelName(modelName) {
		return bc.cmdRefreshModels()
	}
	if isAutoModelName(modelName) {
		return bc.setModelAuto(c)
	}
	if bc.activeModelLister() == nil {
		return "Processador não disponível.", nil
	}

	// Validate: check if the model exists in PI registry.
	// Always force-refresh: the user just typed a specific model name, so they
	// expect validation against the current PI state, not a 5-min-old cache.
	// A stale cache would falsely reject a model added to PI moments ago,
	// forcing the user to run /model refresh first. The 1-15s latency is
	// a fair price for accurate validation.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	available, err := bc.getModels(ctx, true)
	if err != nil {
		return "", fmt.Errorf("falha ao consultar modelos: %w", err)
	}

	var matched *bridge.ModelInfo
	for _, m := range available {
		fullID := m.ID
		fullWithProvider := m.Provider + "/" + m.ID
		if strings.EqualFold(modelName, fullID) || strings.EqualFold(modelName, fullWithProvider) {
			matched = &m
			break
		}
	}

	if matched == nil {
		return fmt.Sprintf("Modelo %q não encontrado. Use 'lista modelos' para ver as opções.", modelName), nil
	}

	oldProvider, oldModel := bc.currentModelPair()
	if err := bc.saveDefaultModel(matched.Provider, matched.ID); err != nil {
		return "", fmt.Errorf("falha ao salvar configuração: %w", err)
	}
	logModelChange("command", oldProvider, oldModel, matched.Provider, matched.ID)

	// Re-export provider env so the bridge sees the new API key on its next query.
	if bc.refreshProviderEnv != nil {
		bc.refreshProviderEnv()
	}

	// Invalidate the model cache after provider env changes so the next
	// /model listing reflects the new API key + provider configuration.
	// Without this, a stale cache could list models from the previous
	// provider (whose API key may no longer be valid).
	bc.invalidateModelCache()

	userID := safeSenderID(c.Sender())
	resetMsg := bc.resetCurrentModelSession(c.Chat().ID, c.Message().ThreadID, userID)

	return fmt.Sprintf("✅ Modelo alterado para **%s** (provedor: **%s**)\n%s\nPróxima mensagem usará o novo modelo.", matched.ID, matched.Provider, resetMsg), nil
}

func (bc *BotController) setModelAuto(c telebot.Context) (string, error) {
	return bc.setModelAutoForScope(c.Chat().ID, c.Message().ThreadID, safeSenderID(c.Sender()))
}

func (bc *BotController) setModelAutoForScope(chatID int64, threadID int, userID int64) (string, error) {
	oldProvider, oldModel := bc.currentModelPair()
	if err := bc.saveDefaultModel("", ""); err != nil {
		return "", fmt.Errorf("falha ao salvar configuração: %w", err)
	}
	logModelChange("auto", oldProvider, oldModel, "", "")
	bc.invalidateModelCache()
	if bc.refreshProviderEnv != nil {
		bc.refreshProviderEnv()
	}
	resetMsg := bc.resetCurrentModelSession(chatID, threadID, userID)
	return "✅ Modelo alterado para **PI default**\n" + resetMsg + "\nPróxima mensagem usará a seleção automática do PI.", nil
}

func (bc *BotController) resetCurrentModelSession(chatID int64, threadID int, userID ...int64) string {
	uid := int64(0)
	if len(userID) > 0 {
		uid = userID[0]
	}
	if bc.sessions != nil {
		bc.sessions.ClearSessionForUser(chatID, threadID, uid)
	}
	return formatModelResetSummary(threadID)
}

func formatModelResetSummary(threadID int) string {
	if threadID > 0 {
		return "Sessão deste tópico foi resetada."
	}
	return "Sessão privada resetada."
}

// extractModelName pulls the model name from a set-model command. Returns
// empty when the text doesn't match a known prefix — earlier versions used
// the last word as a fallback, which caused arbitrary messages classified as
// CmdSetModel to attempt model switches with garbage (e.g. "amigo").
func extractModelName(text string) string {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)

	prefixes := []string{
		"/model ",
		"muda modelo para ",
		"mudar modelo para ",
		"troca modelo para ",
		"trocar modelo para ",
		"escolhe modelo ",
		"seleciona modelo ",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}

	return ""
}

func isAutoModelName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "auto")
}

func isRefreshModelName(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "refresh")
}

func (bc *BotController) applyConfiguredModelOptions(options *bridge.RequestOptions) {
	if bc == nil || bc.config == nil || bc.config.IsModelAuto() || options == nil {
		return
	}
	if bc.config.DefaultProvider != "" {
		options.Provider = bc.config.DefaultProvider
	}
	if bc.config.DefaultModel != "" {
		options.Model = bc.config.DefaultModel
	}
}

func (bc *BotController) currentModelLine() string {
	if bc == nil || bc.config == nil || bc.config.IsModelAuto() {
		return "Modelo atual: **PI default** (seleção automática do PI)"
	}
	return fmt.Sprintf("Modelo atual: **%s** (provedor: **%s**)", bc.config.DefaultModel, bc.config.DefaultProvider)
}

func (bc *BotController) currentModelPair() (string, string) {
	if bc == nil || bc.config == nil {
		return "", ""
	}
	return bc.config.DefaultProvider, bc.config.DefaultModel
}

func logModelChange(source, oldProvider, oldModel, newProvider, newModel string) {
	log.Printf("model change: source=%s old_provider=%q old_model=%q new_provider=%q new_model=%q", source, oldProvider, oldModel, newProvider, newModel)
}

func (bc *BotController) invalidateModelCache() {
	if bc == nil {
		return
	}
	bc.modelCacheMu.Lock()
	defer bc.modelCacheMu.Unlock()
	bc.modelCache = nil
	bc.modelCacheExpiry = time.Time{}
}

func (bc *BotController) cmdMemoryStatus(chatID int64, threadID int, userID int64, isPrivateChat bool) (string, error) {
	svc := memoryuxpkg.New(bc.memoryDir, bc.resolver)
	cwd := bc.currentCwdForContext(chatID, threadID, userID, isPrivateChat)
	log.Printf("memory command: action=status chat=%d thread=%d cwd_set=%t", chatID, threadID, cwd != "")
	status, err := svc.Status(chatID, threadID, cwd)
	if err != nil {
		return "", fmt.Errorf("memory status: %w", err)
	}
	log.Printf("memory command: status complete chat=%d layers=%d", chatID, len(status.Layers))
	return memoryuxpkg.FormatStatus(status), nil
}

func (bc *BotController) cmdMemoryCheckpoint(chatID int64, threadID int, userID int64, isPrivateChat bool, text string) (string, error) {
	// Extract note after the command phrase
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)

	prefixes := []string{
		"memory checkpoint",
		"checkpoint memoria",
		"checkpoint de memoria",
	}
	note := ""
	for _, prefix := range prefixes {
		if strings.HasPrefix(lower, prefix) {
			note = strings.TrimSpace(trimmed[len(prefix):])
			break
		}
	}

	// Append active mode tag to the checkpoint note.
	note = appendModeTag(note, bc.userActiveMode(userID))

	svc := memoryuxpkg.New(bc.memoryDir, bc.resolver)
	cwd := bc.currentCwdForContext(chatID, threadID, userID, isPrivateChat)
	log.Printf("memory command: action=checkpoint chat=%d thread=%d cwd_set=%t", chatID, threadID, cwd != "")
	result, err := svc.WriteCheckpoint(chatID, threadID, cwd, note)
	if err != nil {
		return "", fmt.Errorf("memory checkpoint: %w", err)
	}
	if result.Path != "" {
		log.Printf("memory command: checkpoint written chat=%d layer=%s path=%s", chatID, result.Layer, result.Path)
	}
	return memoryuxpkg.FormatCheckpoint(result), nil
}

// --- Mode helpers and handler ---

// userActiveMode returns the normalized active mode from the user's profile.
// Returns "" for general/default mode or when the store is unavailable.
func (bc *BotController) userActiveMode(userID int64) string {
	if bc.userStore == nil {
		return ""
	}
	profile, err := bc.userStore.Get(userID)
	if err != nil || profile == nil {
		if err != nil {
			log.Printf("command: failed to load profile for mode user=%d: %v", userID, err)
		}
		return ""
	}
	return profile.ActiveMode
}

// userTimezone returns the IANA timezone name and *time.Location for a user.
// Falls back to UTC on any error (logs the error but does not block).
func (bc *BotController) userTimezone(userID int64) (string, *time.Location) {
	if bc.userStore == nil {
		return "", time.UTC
	}
	profile, err := bc.userStore.Get(userID)
	if err != nil || profile == nil || profile.Timezone == "" {
		if err != nil {
			log.Printf("command: failed to load profile for timezone user=%d: %v", userID, err)
		}
		return "", time.UTC
	}
	name, loc, err := users.NormalizeTimezone(profile.Timezone)
	if err != nil {
		log.Printf("telegram: invalid profile timezone user=%d timezone=%q: %v", userID, profile.Timezone, err)
		return "", time.UTC
	}
	return name, loc
}

// displayTimezoneName returns a human-readable timezone name for prompt display.
func displayTimezoneName(tzName string) string {
	if tzName == "" {
		return "UTC"
	}
	return tzName
}

// appendModeTag appends a [mode:xxx] tag to the checkpoint note when the
// user has an active mode. General/default mode appends nothing.
func appendModeTag(note, mode string) string {
	mode, _ = users.NormalizeMode(mode)
	if mode == "" {
		return note
	}
	tag := "[mode:" + mode + "]"
	if strings.TrimSpace(note) == "" {
		return tag
	}
	return strings.TrimSpace(note) + " " + tag
}

// cmdSetMode handles /mode set and query commands.
func (bc *BotController) cmdSetMode(c telebot.Context, text string) (string, error) {
	userID := safeSenderID(c.Sender())
	if bc.userStore == nil {
		return "Sistema de usuários não disponível.", nil
	}

	profile, err := bc.userStore.Get(userID)
	if err != nil {
		return "", fmt.Errorf("load profile: %w", err)
	}
	if profile == nil {
		return "Perfil não encontrado. Complete o onboarding primeiro.", nil
	}
	if profile.UserID != userID {
		log.Printf("command: cmdSetMode profile user mismatch: loaded=%d sender=%d", profile.UserID, userID)
		return "", fmt.Errorf("profile user mismatch: loaded %d != sender %d", profile.UserID, userID)
	}

	// Detect query vs set
	modeText := extractModeTarget(text)
	if modeText == "" {
		// Query current mode
		display := profile.ActiveMode
		if display == "" {
			display = "general"
		}
		return fmt.Sprintf("Perfil ativo: **%s**.\n\nUse @perfil para aplicar outro perfil só na próxima mensagem.", display), nil
	}

	// Set mode. Builtin aliases are normalized; canonical/legacy profiles may
	// also be used when visible to this user.
	normalized := normalizeProfileSelection(modeText)
	selected := bc.getVisibleProfile(normalized, bc.isOwner(c))
	if selected == nil {
		return fmt.Sprintf("Perfil %q não encontrado ou indisponível para este usuário. Use /agents para ver os perfis disponíveis.", modeText), nil
	}
	normalized = selected.Name

	profile.ActiveMode = normalized
	if err := bc.userStore.Save(profile); err != nil {
		return "", fmt.Errorf("save profile: %w", err)
	}

	display := normalized
	if display == "" {
		display = "general"
	}
	return fmt.Sprintf("✅ Perfil alterado para **%s**. O perfil afeta como a Aurelia empacota seu pedido para o SDK.\nUse @perfil para aplicar outro perfil só na próxima mensagem.", display), nil
}

// extractModeTarget extracts the target mode from a mode command text.
// Returns "" when the message is a query (no target mode).
func extractModeTarget(text string) string {
	trimmed := strings.TrimSpace(text)
	lower := stripAccents(strings.ToLower(trimmed))

	// "/mode" alone → query
	if lower == "/mode" {
		return ""
	}
	// Query phrases
	for _, q := range []string{"qual meu modo", "qual o meu modo", "meu modo atual", "qual meu perfil", "qual o meu perfil", "meu perfil atual"} {
		if lower == q {
			return ""
		}
	}

	// "/mode developer", "/modo dev", "/perfil developer" etc.
	for _, prefix := range []string{"/mode ", "/modo ", "/perfil "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}

	// "modo dev", "modo desenvolvedor", "perfil dev", "perfil developer" etc.
	for _, prefix := range []string{"modo ", "perfil "} {
		if strings.HasPrefix(lower, prefix) {
			return strings.TrimSpace(trimmed[len(prefix):])
		}
	}

	return ""
}

// cmdExplainProfile handles /mode explain <name> and /agents explain <name>.
// Shows the profile's description, usage hints, and safe summary without
// exposing sensitive metadata (model, cwd, tools).
func (bc *BotController) cmdExplainProfile(c telebot.Context, text string) (string, error) {
	trimmed := strings.TrimSpace(text)
	lower := stripAccents(strings.ToLower(trimmed))

	// Extract profile name after "explain " prefix.
	var name string
	for _, prefix := range []string{"/mode explain ", "/modo explain ", "/perfil explain ", "/agents explain "} {
		if strings.HasPrefix(lower, prefix) {
			name = strings.TrimSpace(trimmed[len(prefix):])
			break
		}
	}
	if name == "" {
		return "Uso: /mode explain <perfil> ou /agents explain <perfil>", nil
	}

	// Normalize the name.
	normalized, _ := users.NormalizeMode(name)
	if normalized == "" {
		// Not a builtin mode — try profile registry.
		normalized = name
	}

	profile := bc.getVisibleProfile(normalized, bc.isOwner(c))
	if profile == nil {
		return fmt.Sprintf("Perfil %q não encontrado ou indisponível para este usuário. Use /agents para ver os perfis disponíveis.", name), nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("**%s**", profile.Name))
	if profile.Description != "" {
		lines = append(lines, profile.Description)
	}

	// Usage hints.
	lines = append(lines, "\n**Uso:**")
	lines = append(lines, fmt.Sprintf("• `/mode %s` — define como perfil padrão", profile.Name))
	lines = append(lines, fmt.Sprintf("• `@%s <pedido>` — aplica só nesta mensagem", profile.Name))

	lines = append(lines, "\nResumo seguro: instruções internas, cwd, modelo e política de ferramentas ficam ocultos por padrão.")

	if len(profile.Tags) > 0 {
		tags := make([]string, len(profile.Tags))
		for i, t := range profile.Tags {
			tags[i] = "#" + t
		}
		lines = append(lines, "\n"+strings.Join(tags, " "))
	}

	return strings.Join(lines, "\n"), nil
}

func normalizeProfileSelection(name string) string {
	if normalized, err := users.NormalizeMode(name); err == nil && normalized != "" {
		return normalized
	}
	return strings.TrimSpace(name)
}

// getProfile looks up a Prompt Profile by name. Checks the profiles resolver
// first, then falls back to legacy agent registry.
func (bc *BotController) getProfile(name string) *profiles.PromptProfile {
	if bc.profiles != nil {
		if p := bc.profiles.Get(name); p != nil {
			return p
		}
	}
	// Fallback: legacy agents.
	if bc.agents != nil {
		if a := bc.agents.Get(name); a != nil {
			return profiles.FromAgent(a)
		}
	}
	// Legacy: builtin mode check.
	if normalized, err := users.NormalizeMode(name); err == nil && normalized != "" {
		return bc.getBuiltinProfile(normalized)
	}
	if strings.EqualFold(name, "general") {
		return bc.getBuiltinProfile("general")
	}
	return nil
}

func (bc *BotController) getVisibleProfile(name string, isOwner bool) *profiles.PromptProfile {
	profile := bc.getProfile(name)
	if !profiles.ProfileVisible(profile, isOwner) {
		return nil
	}
	return profile
}

// getBuiltinProfile returns a PromptProfile from the resolver's builtins.
// Used as fallback in getProfile when no resolver is available.
func (bc *BotController) getBuiltinProfile(name string) *profiles.PromptProfile {
	if bc.profiles != nil {
		return bc.profiles.Get(name)
	}
	return nil
}

// --- Timezone-aware cron parsing ---

// cronParseSystemPromptForLocation builds the LLM cron parse prompt with
// location-aware timezone guidance instead of the old hardcoded BRT.
func cronParseSystemPromptForLocation(now time.Time, tzName string) string {
	_, offset := now.Zone()
	offsetText := formatUTCOffset(offset)
	if tzName == "" {
		tzName = "UTC"
	}
	return fmt.Sprintf(`You are a scheduling assistant. Extract scheduling parameters from the user's message.

Respond with ONLY a JSON object (no markdown, no explanation):

For recurring schedules:
{"type":"cron","cron_expr":"<cron expression>","prompt":"<what to do>"}

For one-time schedules:
{"type":"once","run_at":"<ISO 8601 timestamp>","prompt":"<what to do>"}

Rules:
- User timezone: %s (%s)
- Current user-local time: %s
- cron_expr uses standard 5-field cron in the user's local timezone: minute hour day month weekday
- run_at must be ISO 8601 with timezone offset %s unless the user explicitly specifies another timezone
- prompt is the ACTION to perform, not the scheduling part
- If the user says "amanhã" or relative dates, calculate from current user-local time
- If no time specified, default to 09:00 user-local time

Examples:
"agenda todo dia às 9h revisar emails" → {"type":"cron","cron_expr":"0 9 * * *","prompt":"revisar emails"}
"me lembra amanhã às 15h de fazer deploy" → {"type":"once","run_at":"2026-03-27T15:00:00%s","prompt":"fazer deploy"}
"agendar toda segunda e quarta às 10h standup" → {"type":"cron","cron_expr":"0 10 * * 1,3","prompt":"standup"}`, tzName, offsetText, now.Format(time.RFC3339), offsetText, offsetText)
}

// formatUTCOffset returns a UTC offset string like "+00:00" or "-03:00".
func formatUTCOffset(offsetSeconds int) string {
	sign := "+"
	if offsetSeconds < 0 {
		sign = "-"
		offsetSeconds = -offsetSeconds
	}
	h := offsetSeconds / 3600
	m := (offsetSeconds % 3600) / 60
	return fmt.Sprintf("%s%02d:%02d", sign, h, m)
}

// parseCronWithLLM is the slow-path cron parser used when the regex fast-path
// in cronFastParse doesn't recognize the message. now and tzName configure
// the LLM prompt with user-local time semantics.
func (bc *BotController) parseCronWithLLM(text string, now time.Time, tzName string) (*cronCreateParsed, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cronParseTimeout)
	defer cancel()
	options := bridge.RequestOptions{
		SystemPrompt: cronParseSystemPromptForLocation(now, tzName),
	}
	bc.applyConfiguredModelOptions(&options)

	result, err := bc.bridge.ExecuteSync(ctx, bridge.Request{
		Command: "query",
		Prompt:  text,
		Options: options,
	})
	if err != nil {
		return nil, err
	}

	parsed, parseErr := parseCronCreateResponse(result.Content)
	if parseErr != nil {
		log.Printf("command: cron_create LLM parse error: %v", parseErr)
		return nil, nil
	}
	return parsed, nil
}

// Pre-compiled redaction regexes for status checkpoint output.
var (
	statusAPIKeyRE     = regexp.MustCompile(`\bsk-[A-Za-z0-9]{20,}`)
	statusGHTokenRE    = regexp.MustCompile(`\bghp_[A-Za-z0-9]{36}`)
	statusAWSKeyRE     = regexp.MustCompile(`\bAKIA[A-Z0-9]{16}`)
	statusJWTRE        = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]+`)
	statusBearerRE     = regexp.MustCompile(`(?i)(Authorization:\s*(?:Bearer|Basic)\s+)\S+`)
	statusPasswordRE   = regexp.MustCompile(`(?i)(password|secret|client_secret|api_key|access_token|refresh_token)\s*[=:]\s*\S+`)
	statusPrivateKeyRE = regexp.MustCompile(`(?s)-----BEGIN (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----.*?-----END (?:OPENSSH |RSA |DSA |EC |PGP )?PRIVATE KEY-----`)
)

// statusRedactSecrets applies credential redaction for status display.
// Patterns mirror the pipeline redactor but are simpler (only for checkpoint
// excerpts in /status output, not full prompt content).
func statusRedactSecrets(s string) string {
	result := s
	result = statusAPIKeyRE.ReplaceAllString(result, "[API_KEY_REDACTED]")
	result = statusGHTokenRE.ReplaceAllString(result, "[GH_TOKEN_REDACTED]")
	result = statusAWSKeyRE.ReplaceAllString(result, "[AWS_KEY_REDACTED]")
	result = statusJWTRE.ReplaceAllString(result, "[JWT_REDACTED]")
	result = statusBearerRE.ReplaceAllString(result, "$1[REDACTED]")
	result = statusPasswordRE.ReplaceAllString(result, "[CREDENTIAL_REDACTED]")
	result = statusPrivateKeyRE.ReplaceAllString(result, "[PRIVATE_KEY_BLOCK_REDACTED]")
	return result
}

// isOwner checks whether the sender is the designated owner user.
func (bc *BotController) isOwner(c telebot.Context) bool {
	senderID := safeSenderID(c.Sender())
	if bc.config != nil && senderID == bc.config.DefaultOwnerUserIDOrFallback() {
		return true
	}
	if bc.userStore == nil {
		return false
	}
	profile, err := bc.userStore.Get(senderID)
	return err == nil && profile != nil && profile.IsOwner
}

// cmdUsers lists all authorized users (owner only).
func (bc *BotController) cmdUsers(c telebot.Context) (string, error) {
	if !bc.isOwner(c) {
		return "Permissão negada.", nil
	}
	profiles, err := bc.userStore.List()
	if err != nil {
		return "", err
	}
	var lines []string
	lines = append(lines, "**Usuários autorizados**\n")
	for _, p := range profiles {
		status := "onboarded"
		if p.OnboardedAt.IsZero() {
			status = "pending"
		}
		lines = append(lines, fmt.Sprintf("- %s (id: %d, lang: %s, %s)", p.Name, p.UserID, p.Language, status))
	}
	return strings.Join(lines, "\n"), nil
}

// cmdForgetMe deletes the sender's data and sends a confirmation via inline buttons.
func (bc *BotController) cmdForgetMe(c telebot.Context) (string, error) {
	senderID := safeSenderID(c.Sender())
	if senderID == 0 {
		return "", fmt.Errorf("invalid sender")
	}

	// Check if user is the only one in whitelist
	if len(bc.config.TelegramAllowedUserIDs) <= 1 {
		return "Você é o único user configurado. Use o comando CLI para resetar.", nil
	}

	// Show confirmation inline buttons
	markup := &telebot.ReplyMarkup{}
	btnConfirm := markup.Data("Confirmar", "forget_me_confirm", fmt.Sprintf("%d", senderID))
	btnCancel := markup.Data("Cancelar", "forget_me_cancel", fmt.Sprintf("%d", senderID))
	markup.Inline(markup.Row(btnConfirm, btnCancel))

	_, err := bc.bot.Send(c.Chat(), "Tem certeza que deseja apagar todos os seus dados?", &telebot.SendOptions{
		ReplyMarkup: markup,
		ThreadID:    c.Message().ThreadID,
	})
	return "", err
}

// saveDefaultModel persists the default provider and model to the config file.
func (bc *BotController) saveDefaultModel(provider, model string) error {
	if bc.config == nil {
		return fmt.Errorf("config is nil")
	}

	// Read current app.json, update, write back
	resolver, err := runtime.New()
	if err != nil {
		return fmt.Errorf("resolve instance: %w", err)
	}
	cfgPath := resolver.AppConfig()

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var cfgMap map[string]any
	if err := json.Unmarshal(data, &cfgMap); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	cfgMap["default_provider"] = provider
	cfgMap["default_model"] = model

	updated, err := json.MarshalIndent(cfgMap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	// Atomic write: temp file in the same directory, then rename. Prevents
	// truncated configs (and the loss of API keys) if the process dies mid-write.
	tmp := cfgPath + ".tmp"
	if err := os.WriteFile(tmp, updated, 0600); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	if err := os.Rename(tmp, cfgPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename config: %w", err)
	}

	bc.config.DefaultProvider = provider
	bc.config.DefaultModel = model
	return nil
}

// ---------------------------------------------------------------------------
// Debug commands (owner only)
// ---------------------------------------------------------------------------

// cmdDebugLast returns a formatted summary of the latest run for a chat/thread.
func (bc *BotController) cmdDebugLast(chatID int64, threadID int) (string, error) {
	if bc.runLog == nil {
		return "Observabilidade desativada (sem runlog).", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	r, err := bc.runLog.Latest(ctx, chatID, threadID)
	if err != nil {
		return "", fmt.Errorf("buscar última execução: %w", err)
	}
	if r == nil {
		return "Nenhuma execução encontrada para este chat.", nil
	}

	return formatTelegramRunSummary(r), nil
}

// cmdDebugRun returns a compact timeline for a specific run (by RunID prefix or full ID).
func (bc *BotController) cmdDebugRun(chatID int64, threadID int, text string) (string, error) {
	if bc.runLog == nil {
		return "Observabilidade desativada (sem runlog).", nil
	}

	// Extract run_id from command text: "/debug run 01HX..." or "execucao 01HX..."
	runID := extractRunID(text)
	if runID == "" {
		return "Uso: /debug run <run_id>\n\nUse /debug last para ver o último run_id.", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	r, err := bc.runLog.GetRun(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("buscar execução: %w", err)
	}
	if r == nil {
		return fmt.Sprintf("Execução %q não encontrada.", runID), nil
	}

	events, err := bc.runLog.ListEvents(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("listar eventos da execução %s: %w", runID, err)
	}
	return formatTelegramRunDetail(r, events), nil
}

// cmdDebugErrors returns recent failed/timed-out runs for this chat.
func (bc *BotController) cmdDebugErrors() (string, error) {
	if bc.runLog == nil {
		return "Observabilidade desativada (sem runlog).", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	runs, err := bc.runLog.ListRuns(ctx, 0, 20)
	if err != nil {
		return "", fmt.Errorf("listar execuções: %w", err)
	}

	var failed []runlog.RunRecord
	for _, r := range runs {
		if r.Status == runlog.RunFailed || r.Status == runlog.RunTimedOut {
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 {
		return "Nenhuma execução com erro encontrada.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "❌ Últimos %d erros:\n\n", len(failed))
	for _, r := range failed {
		errMsg := r.Error
		if r.TimeoutOrigin != "" {
			errMsg = r.TimeoutOrigin
		}
		if errMsg == "" {
			errMsg = "(sem detalhe)"
		}
		dur := time.Duration(r.DurationMs) * time.Millisecond
		fmt.Fprintf(&sb, "• run=%s status=%s dur=%s\n  erro: %s\n",
			shortRunID(r.RunID), r.Status, dur.Round(time.Second), truncateTelegram(errMsg, 200))
	}
	return sb.String(), nil
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

func shortRunID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func truncateTelegram(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

// extractRunID extracts a run_id from a command text like "/debug run 01HX...".
// Returns empty string if no valid-looking ID is found.
func extractRunID(text string) string {
	// Remove command prefix, keep the rest.
	fields := strings.Fields(text)
	for _, f := range fields {
		if strings.HasPrefix(f, "run-") || len(f) >= 10 {
			return f
		}
	}
	// Fallback: return the last field only if it looks like a run ID.
	if len(fields) > 0 {
		last := fields[len(fields)-1]
		// Skip known command words that are not run IDs.
		switch last {
		case "/debug", "debug", "run", "last", "errors":
			return ""
		}
		return last
	}
	return ""
}

// formatTelegramRunSummary builds a concise /debug last output.
func formatTelegramRunSummary(r *runlog.RunRecord) string {
	var sb strings.Builder
	sb.WriteString("🔎 Última execução\n")

	fmt.Fprintf(&sb, "run: %s · status: %s", shortRunID(r.RunID), r.Status)
	if r.DurationMs > 0 {
		dur := time.Duration(r.DurationMs) * time.Millisecond
		fmt.Fprintf(&sb, " · %s", dur.Round(time.Second))
	}
	sb.WriteString("\n")

	if r.UserID > 0 || r.ChatID > 0 {
		fmt.Fprintf(&sb, "user: %d · chat: %d\n", r.UserID, r.ChatID)
	}
	if r.Provider != "" || r.Model != "" {
		fmt.Fprintf(&sb, "model: %s/%s\n", r.Provider, r.Model)
	}
	if r.CWD != "" {
		fmt.Fprintf(&sb, "cwd: %s\n", r.CWD)
	}
	if r.CostUSD > 0 || r.InputTokens > 0 {
		fmt.Fprintf(&sb, "cost: $%.4f · tokens: %d in / %d out", r.CostUSD, r.InputTokens, r.OutputTokens)
		if r.UsedFallback {
			sb.WriteString(" ⚠️ fallback")
		}
		sb.WriteString("\n")
	}
	if r.Error != "" {
		fmt.Fprintf(&sb, "error: %s\n", truncateTelegram(r.Error, 150))
	}
	if r.TimeoutOrigin != "" {
		fmt.Fprintf(&sb, "timeout: %s\n", r.TimeoutOrigin)
	}
	if r.EntryPoint != "" {
		fmt.Fprintf(&sb, "entrypoint: %s\n", r.EntryPoint)
	}

	return sb.String()
}

// formatTelegramRunDetail builds a detailed run view with timeline.
func formatTelegramRunDetail(r *runlog.RunRecord, events []runlog.RunEvent) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "📋 Execução %s\n\n", r.RunID)
	fmt.Fprintf(&sb, "Status: %s\n", r.Status)
	if r.EntryPoint != "" {
		fmt.Fprintf(&sb, "Entrypoint: %s\n", r.EntryPoint)
	}
	if r.AgentName != "" {
		fmt.Fprintf(&sb, "Agent: %s\n", r.AgentName)
	}
	fmt.Fprintf(&sb, "Provider: %s\n", r.Provider)
	fmt.Fprintf(&sb, "Model: %s\n", r.Model)
	if r.DurationMs > 0 {
		dur := time.Duration(r.DurationMs) * time.Millisecond
		fmt.Fprintf(&sb, "Duração: %s\n", dur.Round(time.Millisecond))
	}
	if r.InputTokens > 0 || r.OutputTokens > 0 {
		fmt.Fprintf(&sb, "Tokens: %d in / %d out\n", r.InputTokens, r.OutputTokens)
	}
	if r.CostUSD > 0 {
		fmt.Fprintf(&sb, "Custo: $%.4f\n", r.CostUSD)
	}
	if r.Error != "" {
		fmt.Fprintf(&sb, "Erro: %s\n", truncateTelegram(r.Error, 200))
	}
	if r.TimeoutOrigin != "" {
		fmt.Fprintf(&sb, "Timeout: %s\n", r.TimeoutOrigin)
	}
	if r.UsedFallback {
		sb.WriteString("Fallback: sim\n")
	}

	if len(events) > 0 {
		sb.WriteString("\nTimeline:\n")
		for _, ev := range events {
			t := time.Unix(ev.Timestamp, 0)
			marker := "  "
			switch ev.Level {
			case "error":
				marker = "❌"
			case "warn":
				marker = "⚠️"
			}
			fmt.Fprintf(&sb, "%s %s %s", t.Format("15:04:05"), marker, ev.Phase)
			if ev.Message != "" {
				fmt.Fprintf(&sb, " %s", truncateTelegram(ev.Message, 100))
			}
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

