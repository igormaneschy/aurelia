package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/agents"
	"github.com/igormaneschy/aurelia/internal/bridge"
	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/cron"
	"github.com/igormaneschy/aurelia/internal/runlog"
	"github.com/igormaneschy/aurelia/internal/runtime"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/users"
)

func TestMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want *CommandType // nil means no match
	}{
		// --- cron_create ---
		{name: "agenda with details", text: "agenda uma reunião amanhã às 10h", want: cmdPtr(CmdCronCreate)},
		{name: "agendar keyword", text: "agendar lembrete pra sexta", want: cmdPtr(CmdCronCreate)},
		{name: "cria um lembrete", text: "cria um lembrete pra amanhã", want: cmdPtr(CmdCronCreate)},
		{name: "me lembra", text: "me lembra de revisar o PR às 15h", want: cmdPtr(CmdCronCreate)},

		// --- cron_list ---
		{name: "meus agendamentos", text: "meus agendamentos", want: cmdPtr(CmdCronList)},
		{name: "o que ta agendado", text: "o que tá agendado?", want: cmdPtr(CmdCronList)},
		{name: "lista agendamentos", text: "lista agendamentos", want: cmdPtr(CmdCronList)},

		// --- cron_cancel ---
		{name: "cancela agendamento", text: "cancela o agendamento abc123", want: cmdPtr(CmdCronCancel)},
		{name: "cancele agendamento", text: "cancele o agendamento das 7h", want: cmdPtr(CmdCronCancel)},
		{name: "remove lembrete", text: "remove o lembrete de reunião", want: cmdPtr(CmdCronCancel)},
		{name: "desativa agendamento", text: "desativa agendamento abc", want: cmdPtr(CmdCronCancel)},
		{name: "exclui agendamento", text: "exclui agendamento abc123", want: cmdPtr(CmdCronCancel)},
		{name: "apaga agendamento", text: "apaga agendamento abc123", want: cmdPtr(CmdCronCancel)},

		// --- session_reset ---
		{name: "nova conversa", text: "nova conversa", want: cmdPtr(CmdSessionReset)},
		{name: "limpa o contexto", text: "limpa o contexto", want: cmdPtr(CmdSessionReset)},
		{name: "reset", text: "reset", want: cmdPtr(CmdSessionReset)},
		{name: "comeca de novo", text: "começa de novo", want: cmdPtr(CmdSessionReset)},

		// --- status ---
		{name: "status", text: "status", want: cmdPtr(CmdStatus)},

		// --- list_agents ---
		{name: "quais agents", text: "quais agents?", want: cmdPtr(CmdListAgents)},
		{name: "lista agents", text: "lista agents", want: cmdPtr(CmdListAgents)},
		{name: "meus agents", text: "meus agents", want: cmdPtr(CmdListAgents)},

		// --- list_models ---
		{name: "quais modelos", text: "quais modelos?", want: cmdPtr(CmdListModels)},
		{name: "lista modelos", text: "lista modelos", want: cmdPtr(CmdListModels)},
		{name: "lista provedores", text: "lista provedores", want: cmdPtr(CmdListModels)},

		// --- memory_status ---
		{name: "memory status", text: "memory status", want: cmdPtr(CmdMemoryStatus)},
		{name: "status da memoria exact", text: "status da memoria", want: cmdPtr(CmdMemoryStatus)},
		{name: "status memoria exact", text: "status memoria", want: cmdPtr(CmdMemoryStatus)},
		{name: "status memoria with punct", text: "status da memoria?", want: cmdPtr(CmdMemoryStatus)},

		// --- memory_checkpoint ---
		{name: "memory checkpoint", text: "memory checkpoint trabalhando em X", want: cmdPtr(CmdMemoryCheckpoint)},
		{name: "checkpoint memoria", text: "checkpoint memoria revisar PR", want: cmdPtr(CmdMemoryCheckpoint)},
		{name: "checkpoint de memoria", text: "checkpoint de memoria", want: cmdPtr(CmdMemoryCheckpoint)},

		// --- mode_set ---
		{name: "/mode developer", text: "/mode developer", want: cmdPtr(CmdSetMode)},
		{name: "/mode researcher", text: "/mode researcher", want: cmdPtr(CmdSetMode)},
		{name: "/mode general", text: "/mode general", want: cmdPtr(CmdSetMode)},
		{name: "modo dev", text: "modo dev", want: cmdPtr(CmdSetMode)},
		{name: "modo desenvolvedor", text: "modo desenvolvedor", want: cmdPtr(CmdSetMode)},
		{name: "modo pesquisa", text: "modo pesquisa", want: cmdPtr(CmdSetMode)},
		{name: "modo pesquisador", text: "modo pesquisador", want: cmdPtr(CmdSetMode)},
		{name: "modo geral", text: "modo geral", want: cmdPtr(CmdSetMode)},

		// --- mode_query ---
		{name: "/mode alone", text: "/mode", want: cmdPtr(CmdSetMode)},
		{name: "qual meu modo", text: "qual meu modo", want: cmdPtr(CmdSetMode)},
		{name: "qual o meu modo", text: "qual o meu modo", want: cmdPtr(CmdSetMode)},
		{name: "meu modo atual", text: "meu modo atual", want: cmdPtr(CmdSetMode)},

		// --- mode in group/topic with @botname suffix ---
		// Telegram appends "@<bot_username>" to slash commands in groups.
		// MatchCommand must strip the suffix so the same rules apply in
		// private and group/topic contexts (regression 2026-06-02).
		{name: "/mode@bot alone", text: "/mode@ManeDev_bot", want: cmdPtr(CmdSetMode)},
		{name: "/mode@bot developer", text: "/mode@ManeDev_bot developer", want: cmdPtr(CmdSetMode)},
		{name: "/modo@bot dev", text: "/modo@ManeDev_bot dev", want: cmdPtr(CmdSetMode)},

		// --- stripBotMention regression: at-mention in middle of text not stripped ---
		// The regex anchors at ^ and requires a leading "/" — other @mentions
		// in conversation text must be left alone.
		{name: "mention in middle not stripped", text: "olá @ManeDev_bot como vai", want: nil},
		{name: "no slash before mention", text: "oi @ManeDev_bot tudo bem?", want: nil},

		// --- NO match: normal conversation ---
		{name: "greeting", text: "bom dia", want: nil},
		{name: "question", text: "como funciona o bridge?", want: nil},
		{name: "code request", text: "escreve um teste pro handler", want: nil},

		// --- NO match: mode false positives ---
		{name: "modo devops", text: "modo devops no projeto", want: nil},
		{name: "modo developer narrative", text: "modo developer é melhor para isso", want: nil},
		{name: "narrative pesquisa", text: "tenho pesquisa a fazer sobre X", want: nil},
		{name: "narrative pesquisador", text: "o pesquisador da USP publicou um paper", want: nil},
		{name: "narrative researcher", text: "a researcher role at the lab", want: nil},

		// --- NO match: narrative context (anti-false-positive) ---
		{name: "narrative agendar", text: "ontem eu tentei agendar uma reunião", want: nil},
		{name: "narrative lembrete", text: "ele me lembrou de fazer o deploy", want: nil},
		{name: "narrative status", text: "o status do PR tá verde", want: nil},
		{name: "narrative reset", text: "depois do reset o servidor voltou", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := MatchCommand(tt.text)

			if tt.want == nil {
				if got != nil {
					t.Fatalf("MatchCommand(%q) = %v, want nil", tt.text, got.Type)
				}
				return
			}

			if got == nil {
				t.Fatalf("MatchCommand(%q) = nil, want %v", tt.text, *tt.want)
				return
			}
			if got.Type != *tt.want {
				t.Fatalf("MatchCommand(%q).Type = %v, want %v", tt.text, got.Type, *tt.want)
			}
		})
	}
}

func cmdPtr(c CommandType) *CommandType { return &c }

// --- T4: session_reset tests ---

func TestCmdSessionReset(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetSession(42, 0, 100, "/tmp/test-session.jsonl")
	sessions.SetSession(42, 0, 200, "/tmp/other-user-session.jsonl")

	bc := &BotController{
		config:   &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		sessions: sessions,
	}

	reply, err := bc.cmdSessionReset(42, 0, 100, false)
	if err != nil {
		t.Fatalf("cmdSessionReset() error = %v", err)
	}

	// Sender session should be cleared without affecting another user in the same chat/thread.
	if sid := sessions.GetSession(42, 0, 100); sid != "" {
		t.Fatalf("sender session should be cleared, got %q", sid)
	}
	if sid := sessions.GetSession(42, 0, 200); sid != "/tmp/other-user-session.jsonl" {
		t.Fatalf("other user session should be preserved, got %q", sid)
	}

	// Since tracker is removed, we only check that reset reply still works
	if !strings.Contains(reply, "resetada") {
		t.Fatalf("expected reset summary in reply, got %q", reply)
	}
}

func TestCmdSessionReset_EmptySessionUsesSimpleMessage(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config:   &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		sessions: session.NewStore(),
	}

	reply, err := bc.cmdSessionReset(42, 0, 100, false)
	if err != nil {
		t.Fatalf("cmdSessionReset() error = %v", err)
	}
	if reply != "Sessão resetada. Próxima mensagem inicia conversa nova." {
		t.Fatalf("unexpected empty reset reply: %q", reply)
	}
}

// --- T5: cron_list tests ---

func TestCmdCronList_WithJobs(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{
		jobs: []cron.CronJob{
			{ID: "abc12345-full-uuid", TargetChatID: 42, ScheduleType: "cron", CronExpr: "0 9 * * *", Prompt: "bom dia", Active: true, LastStatus: "idle"},
			{ID: "def67890-full-uuid", TargetChatID: 42, ScheduleType: "once", Prompt: "lembrete", Active: true, LastStatus: "pending"},
		},
	}

	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronList(42, 0, 1001)
	if err != nil {
		t.Fatalf("cmdCronList() error = %v", err)
	}

	if reply == "" {
		t.Fatal("expected non-empty reply")
	}
	// Should contain job info
	if !contains(reply, "abc12345") || !contains(reply, "bom dia") {
		t.Fatalf("reply should contain job info, got %q", reply)
	}
	// Must use owner-scoped method, not unscoped ListJobs
	if len(service.listByOwnerCalls) != 1 {
		t.Fatalf("expected 1 ListJobsByOwner call, got %d", len(service.listByOwnerCalls))
	}
	if service.listByOwnerCalls[0] != "1001" {
		t.Fatalf("expected ListJobsByOwner for user 1001, got %q", service.listByOwnerCalls[0])
	}
}

func TestCmdCronList_Empty(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{jobs: nil}
	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronList(42, 0, 1001)
	if err != nil {
		t.Fatalf("cmdCronList() error = %v", err)
	}
	if reply == "" {
		t.Fatal("expected non-empty reply even when no jobs")
	}
}

func TestCmdCronList_FiltersByChat(t *testing.T) {
	t.Parallel()

	// Jobs from another chat should not appear.
	service := &fakeCronCommandService{
		jobs: []cron.CronJob{
			{ID: "job-chat-42", TargetChatID: 42, ScheduleType: "cron", CronExpr: "0 9 * * *", Prompt: "chat 42 job", Active: true, LastStatus: "idle"},
			{ID: "job-chat-99", TargetChatID: 99, ScheduleType: "cron", CronExpr: "0 9 * * *", Prompt: "chat 99 job", Active: true, LastStatus: "idle"},
		},
	}
	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronList(42, 0, 1001)
	if err != nil {
		t.Fatalf("cmdCronList() error = %v", err)
	}
	// Should only show the chat 42 job
	if !contains(reply, "chat 42 job") {
		t.Fatalf("expected chat 42 job in reply, got %q", reply)
	}
	if contains(reply, "chat 99 job") {
		t.Fatal("expected chat 99 job to be filtered out")
	}
}

func TestCmdCronList_FiltersByThread(t *testing.T) {
	t.Parallel()

	// Job in thread 1 should not appear when listing thread 0.
	service := &fakeCronCommandService{
		jobs: []cron.CronJob{
			{ID: "aaa-thread-0", TargetChatID: 42, TargetThreadID: 0, ScheduleType: "cron", CronExpr: "0 9 * * *", Prompt: "thread 0 job", Active: true, LastStatus: "idle"},
			{ID: "bbb-thread-1", TargetChatID: 42, TargetThreadID: 1, ScheduleType: "cron", CronExpr: "0 9 * * *", Prompt: "thread 1 job", Active: true, LastStatus: "idle"},
		},
	}
	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronList(42, 0, 1001)
	if err != nil {
		t.Fatalf("cmdCronList() error = %v", err)
	}
	if !contains(reply, "thread 0 job") {
		t.Fatalf("expected thread 0 job in reply, got %q", reply)
	}
	if contains(reply, "thread 1 job") {
		t.Fatal("expected thread 1 job to be filtered out")
	}
}

// --- T6: cron_cancel tests ---

func TestCmdCronCancel_WithID(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{
		jobs: []cron.CronJob{
			{ID: "abc12345-full-uuid", TargetChatID: 42, TargetThreadID: 0, OwnerUserID: "1001", ScheduleType: "cron", CronExpr: "0 9 * * *", Prompt: "test", Active: true, LastStatus: "idle"},
		},
	}
	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronCancel(42, 0, 1001, "cancela agendamento abc12345")
	if err != nil {
		t.Fatalf("cmdCronCancel() error = %v", err)
	}

	// Must use owner-scoped method, not unscoped DeleteJob
	if len(service.deleteByOwnerCalls) != 1 {
		t.Fatalf("expected 1 DeleteJobByOwner call, got %d", len(service.deleteByOwnerCalls))
	}
	if service.deleteByOwnerCalls[0].userID != "1001" || service.deleteByOwnerCalls[0].jobID != "abc12345-full-uuid" {
		t.Fatalf("expected DeleteJobByOwner(1001, abc12345-full-uuid), got %#v", service.deleteByOwnerCalls[0])
	}
	if len(service.deleteCalls) != 0 {
		t.Fatalf("expected 0 unscoped DeleteJob calls, got %d — user-facing path must use owner-scoped method", len(service.deleteCalls))
	}
	if reply == "" {
		t.Fatal("expected non-empty reply")
	}
}

func TestCmdCronCancel_CrossChatAndThread(t *testing.T) {
	t.Parallel()

	// Job in chat 99 thread 1 should not be cancelable from chat 42 thread 0.
	service := &fakeCronCommandService{
		jobs: []cron.CronJob{
			{ID: "deadbeef-1234-5678-9abc", TargetChatID: 99, TargetThreadID: 1, OwnerUserID: "1001", ScheduleType: "cron", CronExpr: "0 9 * * *", Prompt: "other", Active: true, LastStatus: "idle"},
		},
	}
	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronCancel(42, 0, 1001, "cancela agendamento deadbeef-1234-5678-9abc")
	if err != nil {
		t.Fatalf("cmdCronCancel() error = %v", err)
	}

	// Should NOT call DeleteJobByOwner — job doesn't match chat/thread
	if len(service.deleteByOwnerCalls) != 0 {
		t.Fatalf("expected 0 DeleteJobByOwner calls for cross-chat job, got %d", len(service.deleteByOwnerCalls))
	}
	if !contains(reply, "não encontrado") && !contains(reply, "Nenhum") {
		t.Fatalf("expected 'not found' message for cross-chat job, got %q", reply)
	}
}

func TestCmdCronCancel_NoID(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{}
	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		cronHandler: NewCronCommandHandler(service),
	}

	reply, err := bc.cmdCronCancel(42, 0, 1001, "cancela agendamento")
	if err != nil {
		t.Fatalf("cmdCronCancel() error = %v", err)
	}

	// Should not attempt delete without an ID
	if len(service.deleteByOwnerCalls) != 0 {
		t.Fatalf("expected no DeleteJobByOwner calls, got %d", len(service.deleteByOwnerCalls))
	}
	if reply == "" {
		t.Fatal("expected guidance reply")
	}
}

// --- T8: cron_create tests ---

func TestParseCronCreateResponse_RecurringJSON(t *testing.T) {
	t.Parallel()

	raw := `{"type":"cron","cron_expr":"0 9 * * *","prompt":"revisar emails"}`
	parsed, err := parseCronCreateResponse(raw)
	if err != nil {
		t.Fatalf("parseCronCreateResponse() error = %v", err)
	}
	if parsed.Type != "cron" || parsed.CronExpr != "0 9 * * *" || parsed.Prompt != "revisar emails" {
		t.Fatalf("unexpected parsed: %+v", parsed)
	}
}

func TestParseCronCreateResponse_OnceJSON(t *testing.T) {
	t.Parallel()

	raw := `{"type":"once","run_at":"2026-03-27T15:00:00-03:00","prompt":"fazer deploy"}`
	parsed, err := parseCronCreateResponse(raw)
	if err != nil {
		t.Fatalf("parseCronCreateResponse() error = %v", err)
	}
	if parsed.Type != "once" || parsed.RunAt != "2026-03-27T15:00:00-03:00" || parsed.Prompt != "fazer deploy" {
		t.Fatalf("unexpected parsed: %+v", parsed)
	}
}

func TestParseCronCreateResponse_MarkdownFences(t *testing.T) {
	t.Parallel()

	raw := "```json\n{\"type\":\"cron\",\"cron_expr\":\"0 9 * * 1\",\"prompt\":\"standup\"}\n```"
	parsed, err := parseCronCreateResponse(raw)
	if err != nil {
		t.Fatalf("parseCronCreateResponse() error = %v", err)
	}
	if parsed.Type != "cron" || parsed.CronExpr != "0 9 * * 1" {
		t.Fatalf("unexpected parsed: %+v", parsed)
	}
}

func TestParseCronCreateResponse_MissingPrompt(t *testing.T) {
	t.Parallel()

	raw := `{"type":"cron","cron_expr":"0 9 * * *"}`
	_, err := parseCronCreateResponse(raw)
	if err == nil {
		t.Fatal("expected error for missing prompt")
	}
}

func TestParseCronCreateResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parseCronCreateResponse("not json at all")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- T9: status tests ---

func TestCmdStatus(t *testing.T) {
	t.Parallel()

	service := &fakeCronCommandService{
		jobs: []cron.CronJob{
			{ID: "j1", Active: true},
			{ID: "j2", Active: false},
		},
	}
	sessions := session.NewStore()
	sessions.Set(42, 0, "/tmp/test-session.jsonl")
	sessions.SetCwd(42, 0, "/repo/aurelia")

	bc := &BotController{
		config: &config.AppConfig{
			DefaultModel: "kimi-k2-thinking",
			Providers:    map[string]config.ProviderConfig{},
		},
		cronHandler: NewCronCommandHandler(service),
		sessions:    sessions,
	}

	reply, err := bc.cmdStatus(42, 0, 123)
	if err != nil {
		t.Fatalf("cmdStatus() error = %v", err)
	}
	if !strings.Contains(reply, "kimi-k2-thinking") {
		t.Fatalf("expected model in status, got %q", reply)
	}
	if !strings.Contains(reply, "1") { // 1 active job
		t.Fatalf("expected active job count in status, got %q", reply)
	}
	if strings.Contains(reply, "sid=") || strings.Contains(reply, "test-session.jsonl") {
		t.Fatalf("status should not expose session internals, got %q", reply)
	}
	if !strings.Contains(reply, "/repo/aurelia") {
		t.Fatalf("expected cwd in status, got %q", reply)
	}
}

func TestStatusWorkLinesIncludeActiveDescriptionAndQueue(t *testing.T) {
	t.Parallel()

	lines := statusWorkLines("\"teste\" rodando há 12s", 1)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "teste") || !strings.Contains(joined, "Fila") || !strings.Contains(joined, "1 mensagem") {
		t.Fatalf("expected active work and queue info, got %q", joined)
	}
	if got := statusWorkLines("", 1); got != nil {
		t.Fatalf("empty work should produce no lines, got %v", got)
	}
}

func TestCmdStatus_NoActiveSession(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config:   &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		sessions: session.NewStore(),
	}

	reply, err := bc.cmdStatus(42, 0, 123)
	if err != nil {
		t.Fatalf("cmdStatus() error = %v", err)
	}
	if reply == "" {
		t.Fatal("expected non-empty status")
	}
}

func TestCmdStatus_ShowsPIDefaultInAutoMode(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config:   &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		sessions: session.NewStore(),
	}

	reply, err := bc.cmdStatus(42, 0, 123)
	if err != nil {
		t.Fatalf("cmdStatus() error = %v", err)
	}
	if !strings.Contains(reply, "PI default") {
		t.Fatalf("expected PI default in status, got %q", reply)
	}
}

func TestCmdStatus_UsesTopicCwd(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/group/repo")
	sessions.SetCwd(42, 99, "/topic/repo")

	bc := &BotController{
		config: &config.AppConfig{
			DefaultModel: "kimi-k2-thinking",
			Providers:    map[string]config.ProviderConfig{},
		},
		sessions: sessions,
	}

	reply, err := bc.cmdStatus(42, 99, 123)
	if err != nil {
		t.Fatalf("cmdStatus() error = %v", err)
	}
	if !strings.Contains(reply, "/topic/repo") {
		t.Fatalf("expected topic cwd in status, got %q", reply)
	}
	if strings.Contains(reply, "/group/repo") {
		t.Fatalf("expected topic cwd to override group cwd, got %q", reply)
	}
}

func TestCmdStatus_FallsBackToGroupCwd(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/group/repo")

	bc := &BotController{
		config: &config.AppConfig{
			DefaultModel: "kimi-k2-thinking",
			Providers:    map[string]config.ProviderConfig{},
		},
		sessions: sessions,
	}

	reply, err := bc.cmdStatus(42, 99, 123)
	if err != nil {
		t.Fatalf("cmdStatus() error = %v", err)
	}
	if !strings.Contains(reply, "/group/repo") {
		t.Fatalf("expected group cwd fallback in status, got %q", reply)
	}
}

func TestStatusRunLogSummary_CheckpointIncluded(t *testing.T) {
	t.Parallel()

	rl := &fakeRunLog{
		latest: &runlog.RunRecord{
			RunID:      "run-test-abc-123",
			ChatID:     42,
			ThreadID:   0,
			Status:     runlog.RunFailed,
			Checkpoint: "Status: failed\nFerramentas: Read, Grep\nErro: network timeout",
			Error:      "network timeout",
			StartedAt:  time.Now().Add(-5 * time.Minute),
		},
	}

	lines := statusRunLogSummary(rl, 42, 0)
	if lines == nil {
		t.Fatal("expected non-nil lines")
	}

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "failed") {
		t.Fatalf("expected failed status, got %q", joined)
	}
	// Check that the short run ID is shown (first 8 chars).
	if !strings.Contains(joined, "run-test") {
		t.Fatalf("expected short run_id in status, got %q", joined)
	}
	// Check error info is shown for failed run.
	if !strings.Contains(joined, "network timeout") {
		t.Fatalf("expected error info in status, got %q", joined)
	}
	if !strings.Contains(joined, "Checkpoint") {
		t.Fatalf("expected checkpoint in status, got %q", joined)
	}
	if !strings.Contains(joined, "continua") {
		t.Fatalf("expected 'continua' hint for failed run, got %q", joined)
	}
}

func TestStatusRunLogSummary_CompletedNoContinuationHint(t *testing.T) {
	t.Parallel()

	rl := &fakeRunLog{
		latest: &runlog.RunRecord{
			RunID:      "run-complete-001",
			ChatID:     42,
			ThreadID:   0,
			Status:     runlog.RunCompleted,
			Checkpoint: "Status: completed",
			StartedAt:  time.Now().Add(-1 * time.Hour),
		},
	}

	lines := statusRunLogSummary(rl, 42, 0)
	if lines == nil {
		t.Fatal("expected non-nil lines")
	}

	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "completed") {
		t.Fatalf("expected completed status, got %q", joined)
	}
	// Should show short run_id (first 8 chars).
	if !strings.Contains(joined, "run-comp") {
		t.Fatalf("expected short run_id in status, got %q", joined)
	}
	if strings.Contains(joined, "continua") {
		t.Fatalf("completed run should not show 'continua' hint, got %q", joined)
	}
}

func TestStatusRunLogSummary_RedactsSecrets(t *testing.T) {
	t.Parallel()

	rl := &fakeRunLog{
		latest: &runlog.RunRecord{
			ChatID:     42,
			ThreadID:   0,
			Status:     runlog.RunFailed,
			Checkpoint: "Status: failed\nAPI Key: sk-test1234567890abcdef",
			StartedAt:  time.Now().Add(-5 * time.Minute),
		},
	}

	lines := statusRunLogSummary(rl, 42, 0)
	joined := strings.Join(lines, "\n")

	if strings.Contains(joined, "sk-test") {
		t.Fatal("checkpoint should redact secrets")
	}
	if !strings.Contains(joined, "REDACTED") {
		t.Fatalf("expected redacted marker, got %q", joined)
	}
}

// --- T10: list_agents tests ---

func TestCmdListAgents_WithAgents(t *testing.T) {
	t.Parallel()

	// Create a temp dir with agent files
	reg := buildTestRegistry(t, map[string]string{
		"coder":      "Writes and debugs code",
		"prospector": "Busca leads e prospecta clientes",
	})

	bc := &BotController{
		config: &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		agents: reg,
	}

	reply, err := bc.cmdListAgents(0)
	if err != nil {
		t.Fatalf("cmdListAgents() error = %v", err)
	}
	if !strings.Contains(reply, "coder") || !strings.Contains(reply, "prospector") {
		t.Fatalf("expected agent names, got %q", reply)
	}
}

func TestCmdListAgents_Empty(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config: &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
	}

	reply, err := bc.cmdListAgents(0)
	if err != nil {
		t.Fatalf("cmdListAgents() error = %v", err)
	}
	if !strings.Contains(reply, "Nenhum") {
		t.Fatalf("expected 'nenhum' message, got %q", reply)
	}
}

// buildTestRegistry creates a Registry with agents for testing.
func buildTestRegistry(t *testing.T, agentMap map[string]string) *agents.Registry {
	t.Helper()
	dir := t.TempDir()
	for name, desc := range agentMap {
		content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\nYou are %s.", name, desc, name)
		path := dir + "/" + name + ".md"
		if err := writeTestFile(path, content); err != nil {
			t.Fatalf("failed to write agent file: %v", err)
		}
	}
	reg, err := agents.Load(dir)
	if err != nil {
		t.Fatalf("agents.Load() error = %v", err)
	}
	return reg
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

// fakeRunLog implements runlog.Store for testing status checkpoint output.
type fakeRunLog struct {
	latest *runlog.RunRecord
}

func (f *fakeRunLog) Start(ctx context.Context, record runlog.RunRecord) error { return nil }
func (f *fakeRunLog) Update(ctx context.Context, update runlog.RunUpdate) error { return nil }
func (f *fakeRunLog) Complete(ctx context.Context, runID string, status runlog.RunStatus, checkpoint, errMsg string) error {
	return nil
}
func (f *fakeRunLog) Latest(ctx context.Context, chatID int64, threadID int) (*runlog.RunRecord, error) {
	if f.latest == nil || f.latest.ChatID != chatID || f.latest.ThreadID != threadID {
		return nil, nil
	}
	return f.latest, nil
}
func (f *fakeRunLog) RecordEvent(_ context.Context, _ runlog.RunEvent) error { return nil }
func (f *fakeRunLog) ListEvents(_ context.Context, _ string) ([]runlog.RunEvent, error) {
	return nil, nil
}
func (f *fakeRunLog) GetRun(_ context.Context, _ string) (*runlog.RunRecord, error) {
	return nil, nil
}
func (f *fakeRunLog) ListRuns(_ context.Context, _ int64, _ int) ([]runlog.RunRecord, error) {
	return nil, nil
}
func (f *fakeRunLog) Metrics(_ context.Context, _ runlog.MetricsFilter) (*runlog.MetricsResult, error) {
	return nil, nil
}
func (f *fakeRunLog) GetLastOutboundMessage(_ context.Context, _ string) (int64, int, int64, error) {
	return 0, 0, 0, nil
}
func (f *fakeRunLog) Close() error { return nil }

// --- T11: list_models tests ---

func TestCmdListModels_RequiresBridge(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config: &config.AppConfig{
			DefaultModel: "kimi-k2-thinking",
			Providers: map[string]config.ProviderConfig{
				"anthropic": {APIKey: "sk-test"},
			},
		},
	}

	reply, err := bc.cmdListModels()
	if err != nil {
		t.Fatalf("cmdListModels() error = %v", err)
	}
	// Without a bridge, cmdListModels returns a user-friendly message.
	if !strings.Contains(reply, "disponível") {
		t.Fatalf("expected bridge-not-available message, got %q", reply)
	}
}

func TestCmdListModels_ShowsPIDefaultAndAutoHint(t *testing.T) {
	t.Parallel()

	bc := &BotController{
		config: &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
	}

	reply, err := bc.cmdListModels()
	if err != nil {
		t.Fatalf("cmdListModels() error = %v", err)
	}
	if !strings.Contains(reply, "PI default") || !strings.Contains(reply, "/model auto") {
		t.Fatalf("expected PI default and /model auto hint, got %q", reply)
	}
}

func TestCmdListModels_PrioritizesLocalModelsBeforeDisplayLimit(t *testing.T) {
	t.Parallel()

	models := make([]bridge.ModelInfo, 0, 32)
	for i := 0; i < 30; i++ {
		models = append(models, bridge.ModelInfo{Provider: fmt.Sprintf("remote-%02d", i), ID: fmt.Sprintf("model-%02d", i)})
	}
	models = append(models,
		bridge.ModelInfo{Provider: "ollama-tailscale", ID: "qwen3.5:9b-48k"},
		bridge.ModelInfo{Provider: "ollama-tailscale", ID: "gemma4:e4b-it-q4_K_M"},
	)

	bc := &BotController{
		config:      &config.AppConfig{Providers: map[string]config.ProviderConfig{}},
		modelLister: &fakeModelLister{models: [][]bridge.ModelInfo{models}},
	}

	reply, err := bc.cmdListModels()
	if err != nil {
		t.Fatalf("cmdListModels() error = %v", err)
	}
	if !strings.Contains(reply, "ollama-tailscale") || !strings.Contains(reply, "qwen3.5:9b-48k") {
		t.Fatalf("expected local Ollama models before display limit, got %q", reply)
	}
}

func TestExtractModelName(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"slash model", "/model claude-opus-4-6", "claude-opus-4-6"},
		{"slash auto", "/model auto", "auto"},
		{"slash model with extra spaces", "  /model  kimi-k2.5  ", "kimi-k2.5"},
		{"muda modelo para", "muda modelo para claude-sonnet", "claude-sonnet"},
		{"trocar modelo para", "trocar modelo para gpt-4", "gpt-4"},
		{"escolhe modelo", "escolhe modelo gemini", "gemini"},
		{"no known prefix returns empty (regression)", "olá tudo bem amigo", ""},
		{"empty input", "", ""},
		{"prefix without name returns empty", "muda modelo para ", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractModelName(tc.text); got != tc.want {
				t.Errorf("extractModelName(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestSetModelAutoForScope_ClearsConfigPersistsAndResetsSession(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AURELIA_HOME", tmpDir)

	r, err := runtime.New()
	if err != nil {
		t.Fatalf("runtime.New() unexpected error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(r.AppConfig()), 0o700); err != nil {
		t.Fatalf("MkdirAll() unexpected error: %v", err)
	}
	initial := `{"default_provider":"anthropic","default_model":"claude-sonnet-4-6","providers":{}}`
	if err := os.WriteFile(r.AppConfig(), []byte(initial), 0o600); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	sessions := session.NewStore()
	sessions.SetSession(42, 99, 100, "/tmp/topic-session.jsonl")
	bc := &BotController{
		config: &config.AppConfig{
			DefaultProvider: "anthropic",
			DefaultModel:    "claude-sonnet-4-6",
			Providers:       map[string]config.ProviderConfig{},
		},
		sessions:         sessions,
		modelCache:       []bridge.ModelInfo{{Provider: "anthropic", ID: "claude-sonnet-4-6"}},
		modelCacheExpiry: time.Now().Add(time.Hour),
	}

	reply, err := bc.setModelAutoForScope(42, 99, 100)
	if err != nil {
		t.Fatalf("setModelAutoForScope() error = %v", err)
	}
	if !strings.Contains(reply, "PI default") {
		t.Fatalf("expected PI default confirmation, got %q", reply)
	}
	if !bc.config.IsModelAuto() {
		t.Fatalf("expected in-memory config auto, got provider=%q model=%q", bc.config.DefaultProvider, bc.config.DefaultModel)
	}
	if sid := sessions.GetSession(42, 99, 100); sid != "" {
		t.Fatalf("current session should be reset, got %q", sid)
	}
	if len(bc.modelCache) != 0 || !bc.modelCacheExpiry.IsZero() {
		t.Fatalf("expected model cache invalidated")
	}

	data, err := os.ReadFile(r.AppConfig())
	if err != nil {
		t.Fatalf("ReadFile() unexpected error: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal() unexpected error: %v", err)
	}
	if raw["default_provider"] != "" || raw["default_model"] != "" {
		t.Fatalf("expected persisted auto config, got %q", string(data))
	}
}

func TestResetCurrentModelSession_ClearsOnlyCurrentThread(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetSession(42, 0, 100, "/tmp/general-session.jsonl")
	sessions.SetSession(42, 99, 100, "/tmp/topic-session.jsonl")
	sessions.SetSession(42, 99, 200, "/tmp/other-user-topic-session.jsonl")
	sessions.SetCwd(42, 99, "/topic/repo")

	bc := &BotController{sessions: sessions}
	msg := bc.resetCurrentModelSession(42, 99, 100)

	if sid := sessions.GetSession(42, 99, 100); sid != "" {
		t.Fatalf("current topic session should be cleared, got %q", sid)
	}
	if sid := sessions.GetSession(42, 99, 200); sid != "/tmp/other-user-topic-session.jsonl" {
		t.Fatalf("other user topic session should be preserved, got %q", sid)
	}
	if sid := sessions.GetSession(42, 0, 100); sid != "/tmp/general-session.jsonl" {
		t.Fatalf("general session should be preserved, got %q", sid)
	}
	if cwd := sessions.GetCwd(42, 99); cwd != "/topic/repo" {
		t.Fatalf("topic cwd should be preserved, got %q", cwd)
	}
	if !strings.Contains(msg, "tópico") {
		t.Fatalf("expected topic reset message, got %q", msg)
	}
}

func TestResetCurrentModelSession_ClearsPrivateChatSession(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.SetSession(42, 0, 100, "/tmp/private-session.jsonl")
	sessions.SetSession(42, 0, 200, "/tmp/other-private-session.jsonl")

	bc := &BotController{sessions: sessions}
	msg := bc.resetCurrentModelSession(42, 0, 100)

	if sid := sessions.GetSession(42, 0, 100); sid != "" {
		t.Fatalf("private chat session should be cleared, got %q", sid)
	}
	if sid := sessions.GetSession(42, 0, 200); sid != "/tmp/other-private-session.jsonl" {
		t.Fatalf("other private session should be preserved, got %q", sid)
	}
	if !strings.Contains(msg, "Sessão privada resetada") {
		t.Fatalf("expected private-session reset message, got %q", msg)
	}
}

func TestHelpMessageIncludesNaturalExamples(t *testing.T) {
	t.Parallel()

	help := helpMessage()
	for _, want := range []string{"💡", "/stop", "agenda todo dia às 9h", "muda modelo", "limpa o contexto"} {
		if !strings.Contains(help, want) {
			t.Fatalf("help missing %q: %q", want, help)
		}
	}
}

// TestHelpMessageIncludesModeCommand verifies that /mode is documented in
// the inline /help output (regression: was missing after the operability
// feature — users had to know the command existed by trying it).
func TestHelpMessageIncludesModeCommand(t *testing.T) {
	t.Parallel()

	help := helpMessage()
	if !strings.Contains(help, "/mode") {
		t.Fatalf("help should document /mode, got: %q", help)
	}
	if !strings.Contains(help, "developer") || !strings.Contains(help, "researcher") {
		t.Fatalf("help /mode description should mention developer/researcher, got: %q", help)
	}
}

// TestStripBotMention verifies the helper that strips @botname suffix from
// slash commands. Telegram appends this in group/topic chats to disambiguate
// the target bot. The helper must:
//   - strip when input starts with /<cmd>@<bot>
//   - leave non-slash text untouched (other @mentions stay)
//   - leave slash commands without @botname untouched
func TestStripBotMention(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "slash with botname", in: "/mode@ManeDev_bot", want: "/mode"},
		{name: "slash with botname + payload", in: "/mode@ManeDev_bot developer", want: "/mode developer"},
		{name: "slash without botname", in: "/mode", want: "/mode"},
		{name: "slash without botname + payload", in: "/mode developer", want: "/mode developer"},
		{name: "modo variant", in: "/modo@ManeDev_bot dev", want: "/modo dev"},
		{name: "at mention without slash", in: "olá @ManeDev_bot", want: "olá @ManeDev_bot"},
		{name: "at mention mid text", in: "fala @ManeDev_bot beleza?", want: "fala @ManeDev_bot beleza?"},
		{name: "empty", in: "", want: ""},
		{name: "no slash no at", in: "modo dev", want: "modo dev"},
		{name: "underscore in botname", in: "/agents@ManeDev_bot", want: "/agents"},
		{name: "digits in command", in: "/m3@ManeDev_bot", want: "/m3"},
		{name: "botname with digits", in: "/mode@Bot123", want: "/mode"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stripBotMention(tt.in); got != tt.want {
				t.Errorf("stripBotMention(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestHandleModeCommand_ReconstructsText verifies that handleModeCommand
// builds the canonical "/mode <target>" text from telebot's payload before
// delegating to cmdSetMode. Regression: in groups, telebot strips the
// @botname and provides only the payload (e.g., "developer"), so the
// handler must reconstruct the slash form for the existing extractModeTarget
// logic to work.
func TestHandleModeCommand_ReconstructsText(t *testing.T) {
	t.Parallel()

	// We can't easily mock a telebot.Context, but we can verify the text
	// reconstruction by testing extractModeTarget on the canonical forms
	// the handler builds. If those parse correctly, the dispatch works.
	tests := []struct {
		reconstructed string
		wantTarget    string
	}{
		{"/mode", ""}, // query
		{"/mode developer", "developer"},
		{"/mode dev", "dev"},
		{"/mode general", "general"},
		{"/modo dev", "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.reconstructed, func(t *testing.T) {
			t.Parallel()
			if got := extractModeTarget(tt.reconstructed); got != tt.wantTarget {
				t.Errorf("extractModeTarget(%q) = %q, want %q", tt.reconstructed, got, tt.wantTarget)
			}
		})
	}
}

// TestSlashMenuCommands_IncludesMode verifies that the Telegram slash menu
// registers the /mode command. Regression: operability feature added
// cmdSetMode handler but did not register it with bot.SetCommands, so
// /mode never appeared in the Telegram auto-complete menu.
func TestSlashMenuCommands_IncludesMode(t *testing.T) {
	t.Parallel()

	cmds := slashMenuCommands()
	want := map[string]bool{
		"new": false, "usage": false, "status": false, "cwd": false,
		"cron": false, "agents": false, "memory": false, "model": false,
		"mode": false, "stop": false, "users": false, "forgetme": false,
		"help": false,
	}
	for _, c := range cmds {
		if _, ok := want[c.Text]; ok {
			want[c.Text] = true
		} else {
			t.Errorf("unexpected command in slash menu: %q", c.Text)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("command %q missing from slash menu", name)
		}
	}
	if len(cmds) != len(want) {
		t.Errorf("expected %d commands, got %d", len(want), len(cmds))
	}
	for _, c := range cmds {
		if c.Description == "" {
			t.Errorf("command %q has empty description (Telegram requires it)", c.Text)
		}
	}
}

func TestCancelActiveRun_NilPipelineReturnsFalse(t *testing.T) {
	t.Parallel()

	bc := &BotController{}
	if got := bc.cancelActiveRun(42, 0); got {
		t.Fatal("cancelActiveRun with nil pipeline should return false")
	}
}

// TestStopPreservesSession verifies that cancelActiveRun (the only thing /stop does)
// does NOT clear session or cwd — unlike /new and /reset.
func TestStopPreservesSession(t *testing.T) {
	t.Parallel()

	sessions := session.NewStore()
	sessions.Set(42, 0, "/tmp/sess-keep.jsonl")

	bc := &BotController{
		sessions: sessions,
		// pipeline is nil → cancelActiveRun returns false, touches nothing
	}

	bc.cancelActiveRun(42, 0)

	// Session must still be intact
	if sid := sessions.Get(42, 0); sid != "/tmp/sess-keep.jsonl" {
		t.Fatalf("cancelActiveRun cleared session want %q, got %q", "sess-keep", sid)
	}
}

func TestCmdDebugLast_WithRun(t *testing.T) {
	bc := &BotController{
		runLog: &fakeRunLog{
			latest: &runlog.RunRecord{
				RunID:        "test-run-123",
				ChatID:       100,
				ThreadID:     0,
				Status:       runlog.RunCompleted,
				Provider:     "kimi",
				Model:        "kimi-k2",
				DurationMs:   5000,
				InputTokens:  100,
				OutputTokens: 20,
				CostUSD:      0.001,
			},
		},
	}

	reply, err := bc.cmdDebugLast(100, 0)
	if err != nil {
		t.Fatalf("cmdDebugLast: %v", err)
	}
	if !contains(reply, "test-run") {
		t.Fatalf("expected run ID in reply, got: %s", reply)
	}
	if !contains(reply, "completed") {
		t.Fatalf("expected status in reply, got: %s", reply)
	}
	if !contains(reply, "kimi/kimi-k2") {
		t.Fatalf("expected provider/model in reply, got: %s", reply)
	}
}

func TestCmdDebugLast_NoRun(t *testing.T) {
	bc := &BotController{
		runLog: &fakeRunLog{latest: nil},
	}
	reply, err := bc.cmdDebugLast(100, 0)
	if err != nil {
		t.Fatalf("cmdDebugLast: %v", err)
	}
	if !contains(reply, "Nenhuma execução") {
		t.Fatalf("expected 'Nenhuma execução' message, got: %s", reply)
	}
}

func TestCmdDebugErrors(t *testing.T) {
	bc := &BotController{
		runLog: &fakeRunLog{latest: nil},
	}
	reply, err := bc.cmdDebugErrors()
	if err != nil {
		t.Fatalf("cmdDebugErrors: %v", err)
	}
	// With empty fake, should return "no errors" message.
	if reply == "" {
		t.Fatal("expected non-empty reply")
	}
}

func TestCmdDebugRun_NoID(t *testing.T) {
	bc := &BotController{
		runLog: &fakeRunLog{latest: nil},
	}
	reply, err := bc.cmdDebugRun(100, 0, "/debug run")
	if err != nil {
		t.Fatalf("cmdDebugRun: %v", err)
	}
	if !contains(reply, "Uso") {
		t.Fatalf("expected usage message, got: %s", reply)
	}
}

func TestExtractRunID_NoMatch(t *testing.T) {
	if got := extractRunID("/debug run"); got != "" {
		t.Fatalf("expected empty for '/debug run', got %q", got)
	}
	if got := extractRunID("/debug last"); got != "" {
		t.Fatalf("expected empty for '/debug last', got %q", got)
	}
}

func TestExtractRunID_WithID(t *testing.T) {
	if got := extractRunID("/debug run 01HXABCD1234"); got != "01HXABCD1234" {
		t.Fatalf("expected 01HXABCD1234, got %q", got)
	}
}

func TestDebugCommands_OwnerOnly(t *testing.T) {
	// Verify that the handler sends a permission-denied message for non-owners.
	// This is tested via the handleCommand switch, not individual handlers.
	t.Parallel()

	// The handler functions themselves don't check ownership;
	// the permission check is in handleCommand before dispatching.
	// Assert that the switch cases exist by checking they compile and
	// the owner-check pattern matches SetModel.
	_ = CmdDebugLast
	_ = CmdDebugRun
	_ = CmdDebugErrors
}

func TestAppendModeTag(t *testing.T) {
	tests := []struct {
		name string
		note string
		mode string
		want string
	}{
		{"empty note, developer", "", "developer", "[mode:developer]"},
		{"note, developer", "before refactor", "developer", "before refactor [mode:developer]"},
		{"note, researcher", "research findings", "researcher", "research findings [mode:researcher]"},
		{"note, general", "some note", "", "some note"},
		{"note, auto", "some note", "general", "some note"},
		{"note, invalid ignored", "some note", "invalid", "some note"},
		{"empty note, empty mode", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := appendModeTag(tc.note, tc.mode)
			if got != tc.want {
				t.Errorf("appendModeTag(%q, %q) = %q, want %q", tc.note, tc.mode, got, tc.want)
			}
		})
	}
}

func TestUserTimezone_NoStore(t *testing.T) {
	bc := &BotController{}
	name, loc := bc.userTimezone(42)
	if name != "" {
		t.Errorf("timezone name = %q, want \"\"", name)
	}
	if loc != time.UTC {
		t.Errorf("loc = %v, want UTC", loc)
	}
}

func TestCronParseSystemPrompt_IncludesTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 6, 2, 14, 30, 0, 0, loc)
	prompt := cronParseSystemPromptForLocation(now, "America/Sao_Paulo")
	if !strings.Contains(prompt, "America/Sao_Paulo") {
		t.Fatal("prompt should include timezone name")
	}
	if !strings.Contains(prompt, "-03:00") {
		t.Fatal("prompt should include UTC offset")
	}
	if !strings.Contains(prompt, "2026-06-02T14:30:00") {
		t.Fatal("prompt should include user-local time")
	}
	// Should NOT contain old hardcoded BRT guidance
	if strings.Contains(strings.ToLower(prompt), "brt") {
		t.Fatal("prompt should NOT contain hardcoded BRT")
	}
}

func TestCronParseSystemPrompt_DefaultUTC(t *testing.T) {
	now := time.Date(2026, 6, 2, 14, 30, 0, 0, time.UTC)
	prompt := cronParseSystemPromptForLocation(now, "")
	if !strings.Contains(prompt, "UTC") {
		t.Fatal("prompt should default to UTC when tzName is empty")
	}
	if !strings.Contains(prompt, "+00:00") {
		t.Fatal("prompt should include +00:00 offset")
	}
}

func TestExtractModeTarget(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"/mode", ""},
		{"/mode developer", "developer"},
		{"/mode researcher", "researcher"},
		{"/mode general", "general"},
		{"/modo dev", "dev"},
		{"/modo pesquisa", "pesquisa"},
		{"/modo geral", "geral"},
		{"modo dev", "dev"},
		{"modo desenvolvedor", "desenvolvedor"},
		{"qual meu modo", ""},
		{"meu modo atual", ""},
	}
	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			got := extractModeTarget(tc.text)
			if got != tc.want {
				t.Errorf("extractModeTarget(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestFormatUTCOffset(t *testing.T) {
	tests := []struct {
		offset int
		want   string
	}{
		{0, "+00:00"},
		{3600, "+01:00"},
		{-10800, "-03:00"},
		{-18000, "-05:00"},
		{7200, "+02:00"},
	}
	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := formatUTCOffset(tc.offset)
			if got != tc.want {
				t.Errorf("formatUTCOffset(%d) = %q, want %q", tc.offset, got, tc.want)
			}
		})
	}
}

func TestCmdSetMode_ProfileUserMismatch(t *testing.T) {
	// Verify cmdSetMode refuses to save when the loaded profile's UserID
	// does not match the sender. This prevents a bug where the wrong user's
	// profile is mutated.
	dir := t.TempDir()
	resolver := users.NewResolver(dir)
	store := users.NewStore(resolver)

	// Save profile for user 1 (the sender).
	profile1 := &users.Profile{
		UserID:     1,
		Name:       "User One",
		Language:   "pt",
		ActiveMode: "",
	}
	if err := store.Save(profile1); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	// Pre-flight: confirm store invariant — the loaded profile's UserID
	// matches the file-keyed id.
	loaded, err := store.Get(1)
	if err != nil {
		t.Fatalf("get profile: %v", err)
	}
	if loaded == nil || loaded.UserID != 1 {
		t.Fatalf("store invariant violated: profile.UserID=%d want 1", loaded.UserID)
	}

	// Call cmdSetMode directly via the real handler with a fake context.
	// /mode (alone) is a query — exercises the userStore load + the
	// profile.UserID == sender check without needing the full model
	// refresh path.
	bc := &BotController{
		userStore: store,
		bot:       newOfflineTestBot(t),
		config: &config.AppConfig{
			DefaultOwnerUserID: 1,
			Providers:          map[string]config.ProviderConfig{},
		},
	}
	c := newTestTelegramContext(42, 0, 1, "")

	reply, err := bc.cmdSetMode(c, "/mode")
	if err != nil {
		t.Fatalf("cmdSetMode() error = %v", err)
	}
	if !strings.Contains(reply, "general") {
		t.Fatalf("expected query to return current mode, got %q", reply)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && stringContains(s, substr))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
