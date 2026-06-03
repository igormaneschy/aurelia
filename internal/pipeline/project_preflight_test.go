package pipeline

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/igormaneschy/aurelia/internal/config"
	"github.com/igormaneschy/aurelia/internal/orchestrator"
	"github.com/igormaneschy/aurelia/internal/profiles"
	"github.com/igormaneschy/aurelia/internal/projectbinding"
	"github.com/igormaneschy/aurelia/internal/session"
)

// testOutputRecorder records output calls for testing preflight behavior.
type testOutputRecorder struct {
	sentMessages  []string
	messageSent   bool
	confirmCalled bool
}

func (r *testOutputRecorder) StartTyping(int64, int) func() { return func() {} }
func (r *testOutputRecorder) NewProgress(int64, int) ProgressReporter { return nil }
func (r *testOutputRecorder) SendError(int64, int, string) error { return nil }
func (r *testOutputRecorder) SendReply(int64, int, string) (int64, error) { return 0, nil }

func (r *testOutputRecorder) SendText(_ int64, _ int, text string) (any, error) {
	r.messageSent = true
	r.sentMessages = append(r.sentMessages, text)
	return nil, nil
}

func (r *testOutputRecorder) DeleteMessage(any)                                        {}
func (r *testOutputRecorder) ConfirmMessage(int64, int)                               { r.confirmCalled = true }
func (r *testOutputRecorder) ExecuteApprovedPlan(int64, int, int, string, int64, *orchestrator.Plan) {}

func TestCheckProjectPreflight_NoCwdWithKnownProjects(t *testing.T) {
	// Setup known projects for user 42
	bindings, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bindings.Close()

	ctx := t.Context()
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 100, ThreadID: 0},
		CWD:         "/repo/aurelia",
		ProjectSlug: "-repo-aurelia",
		Source:      projectbinding.BindingManual,
		CreatedBy:   42,
	}); err != nil {
		t.Fatal(err)
	}
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 200, ThreadID: 0},
		CWD:         "/repo/other-project",
		ProjectSlug: "-repo-other",
		Source:      projectbinding.BindingManual,
		CreatedBy:   42,
	}); err != nil {
		t.Fatal(err)
	}

	output := &testOutputRecorder{}
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		output:   output,
		bindings: bindings,
	}

	input := pipelineInput{
		chatID:    42,
		threadID:  0,
		messageID: 1,
		userID:    42,
		text:      "leia o código do projeto",
	}

	handled := svc.checkProjectPreflight(input, nil, "leia o código do projeto")
	if !handled {
		t.Fatal("expected preflight to handle codebase read without cwd")
	}
	if !output.messageSent {
		t.Fatal("expected preflight to send a message")
	}
	if !output.confirmCalled {
		t.Fatal("expected preflight to confirm the message")
	}

	msg := output.sentMessages[0]
	if !strings.Contains(msg, "/repo/aurelia") {
		t.Fatal("expected known project /repo/aurelia in preflight message")
	}
	if !strings.Contains(msg, "/repo/other-project") {
		t.Fatal("expected known project /repo/other-project in preflight message")
	}
	if !strings.Contains(msg, "sugestões") {
		t.Fatal("expected 'sugestões' label for known projects")
	}
	if !strings.Contains(msg, "/cwd") {
		t.Fatal("expected /cwd command in preflight message")
	}
}

func TestCheckProjectPreflight_NoCwdNoKnownProjects(t *testing.T) {
	output := &testOutputRecorder{}
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		output:   output,
	}

	input := pipelineInput{
		chatID:    42,
		threadID:  0,
		messageID: 1,
		userID:    42,
		text:      "read the codebase",
	}

	handled := svc.checkProjectPreflight(input, nil, "read the codebase")
	if !handled {
		t.Fatal("expected preflight to handle codebase read without cwd")
	}
	if !output.messageSent {
		t.Fatal("expected preflight to send a message")
	}

	msg := output.sentMessages[0]
	if !strings.Contains(msg, "não tem projeto fixado") {
		t.Fatal("expected 'não tem projeto fixado' in preflight message")
	}
	if !strings.Contains(msg, "/cwd") {
		t.Fatal("expected /cwd command in preflight message")
	}
	if strings.Contains(msg, "sugestões") {
		t.Fatal("should NOT show known project suggestions when none exist")
	}
}

func TestCheckProjectPreflight_CwdExistsDoesNotIntercept(t *testing.T) {
	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/repo/aurelia")

	output := &testOutputRecorder{}
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: sessions,
		output:   output,
	}

	input := pipelineInput{
		chatID:    42,
		threadID:  0,
		messageID: 1,
		userID:    42,
		text:      "leia o código do projeto",
	}

	handled := svc.checkProjectPreflight(input, nil, "leia o código do projeto")
	if handled {
		t.Fatal("expected preflight to NOT intercept when cwd is set")
	}
	if output.messageSent {
		t.Fatal("expected no message when cwd is set")
	}
}

func TestCheckProjectPreflight_CasualChatDoesNotIntercept(t *testing.T) {
	output := &testOutputRecorder{}
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		output:   output,
	}

	input := pipelineInput{
		chatID:    42,
		threadID:  0,
		messageID: 1,
		userID:    42,
		text:      "bom dia",
	}

	handled := svc.checkProjectPreflight(input, nil, "bom dia")
	if handled {
		t.Fatal("expected preflight to NOT intercept casual chat")
	}
	if output.messageSent {
		t.Fatal("expected no message for casual chat")
	}
}

func TestBuildProjectPreflightMessage_WithKnownPaths(t *testing.T) {
	msg := buildProjectPreflightMessage([]string{"/repo/aurelia", "/repo/other"})

	if !strings.Contains(msg, "não tem projeto fixado") {
		t.Error("should mention no project is fixed")
	}
	if !strings.Contains(msg, "/cwd /repo/aurelia") {
		t.Error("should include first known path")
	}
	if !strings.Contains(msg, "/cwd /repo/other") {
		t.Error("should include second known path")
	}
	if !strings.Contains(msg, "sugestões") {
		t.Error("should label paths as suggestions")
	}
	if !strings.Contains(msg, "não são o CWD ativo") {
		t.Error("should clarify these are not active cwd")
	}
}

func TestBuildProjectPreflightMessage_WithoutKnownPaths(t *testing.T) {
	msg := buildProjectPreflightMessage(nil)
	msg2 := buildProjectPreflightMessage([]string{})

	for _, m := range []string{msg, msg2} {
		if !strings.Contains(m, "não tem projeto fixado") {
			t.Error("should mention no project is fixed")
		}
		if !strings.Contains(m, "/cwd") {
			t.Error("should include /cwd command")
		}
		if strings.Contains(m, "sugestões") {
			t.Error("should NOT label paths as suggestions when none exist")
		}
	}
}

func TestCheckProjectPreflight_NoCrossChatForUserIDZero(t *testing.T) {
	bindings, err := projectbinding.NewSQLiteStore(filepath.Join(t.TempDir(), "bindings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer bindings.Close()

	ctx := t.Context()
	// Another user has bindings
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 100, ThreadID: 0},
		CWD:         "/repo/secret",
		ProjectSlug: "-repo-secret",
		Source:      projectbinding.BindingManual,
		CreatedBy:   99,
	}); err != nil {
		t.Fatal(err)
	}
	// User 42 also has bindings
	if err := bindings.Set(ctx, projectbinding.ProjectBinding{
		Key:         projectbinding.ConversationKey{ChatID: 200, ThreadID: 0},
		CWD:         "/repo/user42-project",
		ProjectSlug: "-repo-user42",
		Source:      projectbinding.BindingManual,
		CreatedBy:   42,
	}); err != nil {
		t.Fatal(err)
	}

	output := &testOutputRecorder{}
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		output:   output,
		bindings: bindings,
	}

	// userID=0 should see generic message, not known projects
	input := pipelineInput{
		chatID:    42,
		threadID:  0,
		messageID: 1,
		userID:    0,
		text:      "leia o código do projeto",
	}

	handled := svc.checkProjectPreflight(input, nil, "leia o código do projeto")
	if !handled {
		t.Fatal("expected preflight to handle codebase read without cwd even for userID=0")
	}
	if !output.messageSent {
		t.Fatal("expected preflight to send a message")
	}

	msg := output.sentMessages[0]
	if strings.Contains(msg, "/repo/secret") {
		t.Fatal("should NOT show another user's project")
	}
	if strings.Contains(msg, "/repo/user42-project") {
		t.Fatal("should NOT show any known projects for userID=0")
	}
	if !strings.Contains(msg, "/cwd <caminho>") {
		t.Fatal("should show generic /cwd <caminho> guidance")
	}
}

func TestCheckProjectPreflight_AgentCwdOverride(t *testing.T) {
	// Agent with a CWD set should bypass preflight even without session/binding cwd
	pp := &profiles.PromptProfile{
		Name: "test-agent",
		Cwd:  "/agent/path",
	}

	output := &testOutputRecorder{}
	svc := &Service{
		config:   &config.AppConfig{},
		sessions: session.NewStore(),
		output:   output,
	}

	input := pipelineInput{
		chatID:    42,
		threadID:  0,
		messageID: 1,
		userID:    42,
		text:      "leia o código do projeto",
	}

	handled := svc.checkProjectPreflight(input, pp, "leia o código do projeto")
	if handled {
		t.Fatal("expected preflight to NOT intercept when agent has CWD set")
	}
	if output.messageSent {
		t.Fatal("expected no message when agent CWD is set")
	}
}
