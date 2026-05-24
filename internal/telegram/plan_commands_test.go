package telegram

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/igormaneschy/aurelia/internal/planning"
	"github.com/igormaneschy/aurelia/internal/session"
	"github.com/igormaneschy/aurelia/internal/users"
	_ "modernc.org/sqlite"
)

// fakePlanningStore implements planning.Store for testing plan command handlers.
type fakePlanningStore struct {
	mu     sync.Mutex
	states map[session.SessionKey]*planning.State

	getErr    error
	saveErr   error
	deleteErr error
	listErr   error
}

func newFakePlanningStore() *fakePlanningStore {
	return &fakePlanningStore{states: make(map[session.SessionKey]*planning.State)}
}

func (f *fakePlanningStore) Get(_ context.Context, key session.SessionKey) (*planning.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.states[key], nil
}

func (f *fakePlanningStore) Save(_ context.Context, state *planning.State) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	state.Version++
	f.states[state.Key] = state
	return nil
}

func (f *fakePlanningStore) Delete(_ context.Context, key session.SessionKey) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.deleteErr != nil {
		return f.deleteErr
	}
	delete(f.states, key)
	return nil
}

func (f *fakePlanningStore) ListByUser(_ context.Context, userID int64) ([]planning.State, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	var result []planning.State
	for _, s := range f.states {
		if s.Key.UserID == userID {
			result = append(result, *s)
		}
	}
	return result, nil
}

func (f *fakePlanningStore) GC(_ context.Context, _ time.Duration) error { return nil }
func (f *fakePlanningStore) Close() error                                { return nil }

// captureTelegramContext records the last text sent via Send() for test assertions.
type captureTelegramContext struct {
	testTelegramContext
	lastSent string
	sendErr  error
}

func newCaptureContext(chatID int64, threadID int, senderID int64) *captureTelegramContext {
	return &captureTelegramContext{
		testTelegramContext: *newTestTelegramContext(chatID, threadID, senderID, ""),
	}
}

func (c *captureTelegramContext) Send(what interface{}, opts ...interface{}) error {
	if s, ok := what.(string); ok {
		c.lastSent = s
	}
	return c.sendErr
}

// --- Tests ---

func TestHandlePlan(t *testing.T) {
	store := newFakePlanningStore()
	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/repo/test")
	bc := &BotController{
		planningStore: store,
		sessions:      sessions,
	}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlan(c)
	if err != nil {
		t.Fatalf("handlePlan() error = %v", err)
	}

	// Verify state was saved
	state, _ := store.Get(context.Background(), session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 100})
	if state == nil {
		t.Fatal("expected state to be created")
	}
	if state.Status != planning.StatusActive {
		t.Fatalf("expected active status, got %s", state.Status)
	}
	if state.Phase != planning.PhaseSpecify {
		t.Fatalf("expected specify phase, got %s", state.Phase)
	}
	if !strings.Contains(c.lastSent, "Modo Plano ativado") {
		t.Fatalf("expected activation message, got: %s", c.lastSent)
	}
}

func TestHandlePlan_ExistingState(t *testing.T) {
	store := newFakePlanningStore()
	key := session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 100}
	store.states[key] = &planning.State{
		Key:    key,
		Status: planning.StatusActive,
		Phase:  planning.PhaseDesign,
	}
	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/repo/test")
	bc := &BotController{
		planningStore: store,
		sessions:      sessions,
	}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlan(c)
	if err != nil {
		t.Fatalf("handlePlan() error = %v", err)
	}

	if !strings.Contains(c.lastSent, "Já existe") {
		t.Fatalf("expected existing state message, got: %s", c.lastSent)
	}

	// Verify no new state was saved (still the same phase)
	state, _ := store.Get(context.Background(), key)
	if state.Phase != planning.PhaseDesign {
		t.Fatalf("expected phase to remain design, got %s", state.Phase)
	}
}

func TestHandlePlan_NoCWD(t *testing.T) {
	store := newFakePlanningStore()
	bc := &BotController{
		planningStore: store,
	}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlan(c)
	if err != nil {
		t.Fatalf("handlePlan() error = %v", err)
	}

	if !strings.Contains(c.lastSent, "/cwd") {
		t.Fatalf("expected /cwd hint, got: %s", c.lastSent)
	}
}

func TestHandlePlanStatus(t *testing.T) {
	store := newFakePlanningStore()
	now := time.Now()
	key := session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 100}
	store.states[key] = &planning.State{
		Key:       key,
		Status:    planning.StatusActive,
		Phase:     planning.PhaseSpecify,
		CWD:       "/repo/test",
		CreatedAt: now.Add(-10 * time.Minute),
		UpdatedAt: now,
		ProjectCtx: &planning.ProjectContext{
			Layouts: []string{"tlc", "planning"},
			Stacks:  []string{"go"},
		},
		Materialized: []planning.Artifact{
			{Path: "/repo/test/plan.md", Tool: "Write", Phase: planning.PhaseSpecify, CreatedAt: now},
		},
	}
	bc := &BotController{planningStore: store}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlanStatus(c)
	if err != nil {
		t.Fatalf("handlePlanStatus() error = %v", err)
	}

	if !strings.Contains(c.lastSent, "specify") {
		t.Fatalf("expected phase in status, got: %s", c.lastSent)
	}
	if !strings.Contains(c.lastSent, "/repo/test") {
		t.Fatalf("expected cwd in status, got: %s", c.lastSent)
	}
	if !strings.Contains(c.lastSent, "tlc") {
		t.Fatalf("expected layouts in status, got: %s", c.lastSent)
	}
	if !strings.Contains(c.lastSent, "Write") {
		t.Fatalf("expected artifact info in status, got: %s", c.lastSent)
	}
}

func TestHandlePlanStatus_NoState(t *testing.T) {
	store := newFakePlanningStore()
	bc := &BotController{planningStore: store}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlanStatus(c)
	if err != nil {
		t.Fatalf("handlePlanStatus() error = %v", err)
	}

	if !strings.Contains(c.lastSent, "Nenhum plano") {
		t.Fatalf("expected no-plan message, got: %s", c.lastSent)
	}
}

func TestHandlePlanList(t *testing.T) {
	store := newFakePlanningStore()
	store.states[session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 100}] = &planning.State{
		Key:    session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 100},
		Status: planning.StatusActive,
		Phase:  planning.PhaseSpecify,
	}
	store.states[session.SessionKey{ChatID: 42, ThreadID: 5, UserID: 100}] = &planning.State{
		Key:    session.SessionKey{ChatID: 42, ThreadID: 5, UserID: 100},
		Status: planning.StatusAwaitingExec,
		Phase:  planning.PhaseReview,
	}
	// Different user's state should not appear
	store.states[session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 200}] = &planning.State{
		Key:    session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 200},
		Status: planning.StatusActive,
		Phase:  planning.PhaseDesign,
	}

	bc := &BotController{planningStore: store}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlanList(c)
	if err != nil {
		t.Fatalf("handlePlanList() error = %v", err)
	}

	if !strings.Contains(c.lastSent, "2") {
		t.Fatalf("expected 2 plans in list, got: %s", c.lastSent)
	}
	if !strings.Contains(c.lastSent, "specify") {
		t.Fatalf("expected specify phase, got: %s", c.lastSent)
	}
	if !strings.Contains(c.lastSent, "review") {
		t.Fatalf("expected review phase, got: %s", c.lastSent)
	}
}

func TestHandlePlanList_Empty(t *testing.T) {
	store := newFakePlanningStore()
	bc := &BotController{planningStore: store}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlanList(c)
	if err != nil {
		t.Fatalf("handlePlanList() error = %v", err)
	}

	if !strings.Contains(c.lastSent, "Nenhum plano") {
		t.Fatalf("expected no-plans message, got: %s", c.lastSent)
	}
}

func TestHandlePlanCancel(t *testing.T) {
	store := newFakePlanningStore()
	key := session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 100}
	store.states[key] = &planning.State{
		Key:    key,
		Status: planning.StatusActive,
		Phase:  planning.PhaseDesign,
	}
	bc := &BotController{planningStore: store}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlanCancel(c)
	if err != nil {
		t.Fatalf("handlePlanCancel() error = %v", err)
	}

	// State should be deleted
	state, _ := store.Get(context.Background(), key)
	if state != nil {
		t.Fatal("expected state to be deleted")
	}

	if !strings.Contains(c.lastSent, "cancelado") {
		t.Fatalf("expected cancellation message, got: %s", c.lastSent)
	}
}

func TestHandlePlanCancel_NoState(t *testing.T) {
	store := newFakePlanningStore()
	bc := &BotController{planningStore: store}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlanCancel(c)
	if err != nil {
		t.Fatalf("handlePlanCancel() error = %v", err)
	}

	if !strings.Contains(c.lastSent, "Nenhum plano") {
		t.Fatalf("expected no-plan message, got: %s", c.lastSent)
	}
}

func TestHandlePlanReset(t *testing.T) {
	store := newFakePlanningStore()
	key := session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 100}
	store.states[key] = &planning.State{
		Key:    key,
		Status: planning.StatusActive,
		Phase:  planning.PhaseDesign,
	}
	sessions := session.NewStore()
	sessions.SetCwd(42, 0, "/repo/test")
	bc := &BotController{
		planningStore: store,
		sessions:      sessions,
	}
	c := newCaptureContext(42, 0, 100)

	err := bc.handlePlanReset(c)
	if err != nil {
		t.Fatalf("handlePlanReset() error = %v", err)
	}

	// Old state should be replaced with a new one at PhaseSpecify
	state, _ := store.Get(context.Background(), key)
	if state == nil {
		t.Fatal("expected new state after reset")
	}
	if state.Phase != planning.PhaseSpecify {
		t.Fatalf("expected specify phase after reset, got %s", state.Phase)
	}
	if !strings.Contains(c.lastSent, "resetado") {
		t.Fatalf("expected reset message, got: %s", c.lastSent)
	}
}

func TestHandleExecute(t *testing.T) {
	store := newFakePlanningStore()
	key := session.SessionKey{ChatID: 42, ThreadID: 0, UserID: 100}
	store.states[key] = &planning.State{
		Key:    key,
		Status: planning.StatusActive,
		Phase:  planning.PhaseReview,
	}
	bc := &BotController{planningStore: store}
	c := newCaptureContext(42, 0, 100)

	err := bc.handleExecute(c)
	if err != nil {
		t.Fatalf("handleExecute() error = %v", err)
	}

	// State should be marked as awaiting_exec
	state, _ := store.Get(context.Background(), key)
	if state == nil {
		t.Fatal("expected state to still exist")
	}
	if state.Status != planning.StatusAwaitingExec {
		t.Fatalf("expected awaiting_exec status, got %s", state.Status)
	}

	if !strings.Contains(c.lastSent, "execução") {
		t.Fatalf("expected execution message, got: %s", c.lastSent)
	}
}

func TestHandleExecute_NoState(t *testing.T) {
	store := newFakePlanningStore()
	bc := &BotController{planningStore: store}
	c := newCaptureContext(42, 0, 100)

	err := bc.handleExecute(c)
	if err != nil {
		t.Fatalf("handleExecute() error = %v", err)
	}

	if !strings.Contains(c.lastSent, "Nenhum plano") {
		t.Fatalf("expected no-plan message, got: %s", c.lastSent)
	}
}

func TestHandlePlan_UserGateBlocked(t *testing.T) {
	// Create real stores backed by temp directory — no profile exists,
	// so UserGate.Check returns NeedsOnboarding.
	root := t.TempDir()
	resolver := users.NewResolver(root)
	store := users.NewStore(resolver)

	dbPath := filepath.Join(t.TempDir(), "ob.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	obStore := users.NewOnboardingStore(db)
	if err := obStore.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	gate := NewUserGate(store, obStore, 0)

	bc := &BotController{
		planningStore: newFakePlanningStore(),
		userGate:      gate,
	}
	c := newCaptureContext(42, 0, 999)

	err = bc.handlePlan(c)
	if err != nil {
		t.Fatalf("handlePlan() error = %v", err)
	}

	if !strings.Contains(c.lastSent, "onboarding") {
		t.Fatalf("expected onboarding message, got: %s", c.lastSent)
	}
}

// Verify that the fake planning store implements the full interface.
var _ planning.Store = (*fakePlanningStore)(nil)
