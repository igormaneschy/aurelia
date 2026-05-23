package users

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// testMessageSender implements MessageSender for testing.
type testMessageSender struct{}

func (t *testMessageSender) SendText(chatID int64, threadID int, text string) (any, error) {
	return nil, nil
}

func newTestOnboarder(t *testing.T) (*Onboarder, *Store, *OnboardingStore) {
	t.Helper()
	root := t.TempDir()
	r := NewResolver(root)
	store := NewStore(r)

	dbPath := filepath.Join(t.TempDir(), "ob.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	obStore := NewOnboardingStore(db)
	if err := obStore.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}

	sender := &testMessageSender{}
	onboarder := NewOnboarder(store, obStore, sender)
	return onboarder, store, obStore
}

func TestOnboarder_FullFlow(t *testing.T) {
	onboarder, store, _ := newTestOnboarder(t)

	// Step 1: Begin
	greeting, err := onboarder.Begin(1, 100, 0, "olá")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if !strings.Contains(greeting, "Aurelia") {
		t.Errorf("greeting should mention Aurelia, got %q", greeting)
	}
	if !onboarder.Active(1) {
		t.Fatal("Active() should be true after Begin")
	}

	// Step 2: Provide name
	reply, done, err := onboarder.Step(1, "Alice")
	if err != nil {
		t.Fatalf("Step(name) error = %v", err)
	}
	if !strings.Contains(reply, "Alice") {
		t.Errorf("reply should mention name, got %q", reply)
	}
	if done {
		t.Fatal("Step(name) should not be done yet")
	}
	if !onboarder.Active(1) {
		t.Fatal("Active() should be true after name step")
	}

	// Step 3: Provide bio
	reply, done, err = onboarder.Step(1, "Sou desenvolvedora")
	if err != nil {
		t.Fatalf("Step(bio) error = %v", err)
	}
	if !done {
		t.Fatal("Step(bio) should mark onboarding as done")
	}
	if !strings.Contains(reply, "Alice") {
		t.Errorf("final reply should mention name, got %q", reply)
	}

	// Profile should exist
	profile, err := store.Get(1)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if profile == nil {
		t.Fatal("profile should exist after onboarding")
	}
	if profile.Name != "Alice" {
		t.Errorf("Name = %q, want %q", profile.Name, "Alice")
	}
	if profile.Language != "pt" {
		t.Errorf("Language = %q, want %q", profile.Language, "pt")
	}
	if profile.OnboardedAt.IsZero() {
		t.Fatal("OnboardedAt should be set")
	}

	// USER.md should exist with bio
	userMdPath := store.resolver.UserMdPath(1)
	data, err := os.ReadFile(userMdPath)
	if err != nil {
		t.Fatalf("read USER.md: %v", err)
	}
	if !strings.Contains(string(data), "Sou desenvolvedora") {
		t.Errorf("USER.md should contain bio, got %q", string(data))
	}

	// Active should be false after completion
	if onboarder.Active(1) {
		t.Fatal("Active() should be false after onboarding completes")
	}
}

func TestOnboarder_SkipBio(t *testing.T) {
	onboarder, store, _ := newTestOnboarder(t)

	// Begin
	_, err := onboarder.Begin(2, 100, 0, "hello")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}

	// Give name
	_, done, err := onboarder.Step(2, "Bob")
	if err != nil {
		t.Fatalf("Step(name) error = %v", err)
	}
	if done {
		t.Fatal("Step(name) should not be done")
	}

	// Skip bio with "pular"
	reply, done := "", false
	reply, done, err = onboarder.Step(2, "pular")
	if err != nil {
		t.Fatalf("Step(skip) error = %v", err)
	}
	if !done {
		t.Fatal("Step(skip) should mark onboarding as done")
	}

	// Language should be detected as "en"
	profile, err := store.Get(2)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if profile == nil {
		t.Fatal("profile should exist")
	}
	if profile.Name != "Bob" {
		t.Errorf("Name = %q, want %q", profile.Name, "Bob")
	}
	if profile.Language != "en" {
		t.Errorf("Language = %q, want %q", profile.Language, "en")
	}

	// Reply should be in English
	if !strings.Contains(reply, "Bob") {
		t.Errorf("reply should mention Bob, got %q", reply)
	}
}

func TestOnboarder_DetectLanguage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"olá tudo bem", "pt"},
		{"hello how are you", "en"},
		{"hi", "en"},
		{"obrigado", "pt"},
		{"bem", "pt"},
		{"", "pt"},      // empty → default pt
		{"12345", "pt"}, // no tokens → default pt
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := detectLanguage(tt.input)
			if got != tt.want {
				t.Errorf("detectLanguage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestOnboarder_Active(t *testing.T) {
	onboarder, _, _ := newTestOnboarder(t)

	// Before Begin
	if onboarder.Active(99) {
		t.Fatal("Active() should be false for non-existent user")
	}

	// After Begin
	_, err := onboarder.Begin(3, 100, 0, "olá")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if !onboarder.Active(3) {
		t.Fatal("Active() should be true after Begin")
	}

	// After name step
	_, _, err = onboarder.Step(3, "Charlie")
	if err != nil {
		t.Fatalf("Step(name) error = %v", err)
	}
	if !onboarder.Active(3) {
		t.Fatal("Active() should be true mid-onboarding")
	}

	// After completion
	_, _, err = onboarder.Step(3, "Bio text")
	if err != nil {
		t.Fatalf("Step(bio) error = %v", err)
	}
	if onboarder.Active(3) {
		t.Fatal("Active() should be false after onboarding completes")
	}
}

func TestOnboarder_Step_NoState(t *testing.T) {
	onboarder, _, _ := newTestOnboarder(t)

	_, _, err := onboarder.Step(999, "hello")
	if err == nil {
		t.Fatal("expected error for non-existent onboarding state")
	}
}

func TestOnboarder_EnglishFlow(t *testing.T) {
	onboarder, store, _ := newTestOnboarder(t)

	// Begin with English text
	greeting, err := onboarder.Begin(4, 100, 0, "hello")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if !strings.Contains(greeting, "Hello") {
		t.Errorf("greeting should be in English, got %q", greeting)
	}

	// Give name
	reply, done, err := onboarder.Step(4, "Diana")
	if err != nil {
		t.Fatalf("Step(name) error = %v", err)
	}
	if !strings.Contains(reply, "Nice to meet you") {
		t.Errorf("reply should be in English, got %q", reply)
	}
	if done {
		t.Fatal("Step(name) should not be done")
	}

	// Bio
	reply, done, err = onboarder.Step(4, "skip")
	if err != nil {
		t.Fatalf("Step(skip) error = %v", err)
	}
	if !done {
		t.Fatal("should be done")
	}
	if !strings.Contains(reply, "Thanks") {
		t.Errorf("final reply should be in English, got %q", reply)
	}

	profile, err := store.Get(4)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if profile.Language != "en" {
		t.Errorf("Language = %q, want %q", profile.Language, "en")
	}
}

// TestOnboarder_UserMdFilePermissions verifies USER.md has restricted perms.
func TestOnboarder_UserMdFilePermissions(t *testing.T) {
	onboarder, store, _ := newTestOnboarder(t)

	_, err := onboarder.Begin(5, 100, 0, "hello")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	_, _, err = onboarder.Step(5, "Eve")
	if err != nil {
		t.Fatalf("Step(name) error = %v", err)
	}
	_, _, err = onboarder.Step(5, "bio")
	if err != nil {
		t.Fatalf("Step(bio) error = %v", err)
	}

	path := store.resolver.UserMdPath(5)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat USER.md: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("USER.md permissions = %o, want 600", info.Mode().Perm())
	}
}

// TestOnboarder_SkipBioUsingWordSkip tests that the English word "skip" skips bio.
func TestOnboarder_SkipBioUsingWordSkip(t *testing.T) {
	onboarder, store, _ := newTestOnboarder(t)

	_, err := onboarder.Begin(6, 100, 0, "hi there")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	_, _, err = onboarder.Step(6, "Frank")
	if err != nil {
		t.Fatalf("Step(name) error = %v", err)
	}
	_, _, err = onboarder.Step(6, "skip")
	if err != nil {
		t.Fatalf("Step(skip) error = %v", err)
	}

	profile, err := store.Get(6)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if profile.Name != "Frank" {
		t.Errorf("Name = %q, want %q", profile.Name, "Frank")
	}
	if profile.OnboardedAt.IsZero() {
		t.Fatal("OnboardedAt should be set")
	}
}

// TestOnboarder_ProfileDirCreated checks that the user directory structure is created.
func TestOnboarder_ProfileDirCreated(t *testing.T) {
	root := t.TempDir()
	r := NewResolver(root)
	store := NewStore(r)

	dbPath := filepath.Join(t.TempDir(), "prof.db")
	dsn := dbPath + "?_journal_mode=WAL&_busy_timeout=5000"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	obStore := NewOnboardingStore(db)
	if err := obStore.EnsureSchema(); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	onboarder2 := NewOnboarder(store, obStore, &testMessageSender{})

	_, err = onboarder2.Begin(7, 100, 0, "hello")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	_, _, err = onboarder2.Step(7, "Grace")
	if err != nil {
		t.Fatalf("Step(name) error = %v", err)
	}
	_, _, err = onboarder2.Step(7, "I am a developer")
	if err != nil {
		t.Fatalf("Step(bio) error = %v", err)
	}

	// Check that the user dir and subdirs were created
	userDir := r.UserRoot(7)
	if info, err := os.Stat(userDir); err != nil {
		t.Fatalf("user dir should exist: %v", err)
	} else if !info.IsDir() {
		t.Fatal("user dir is not a directory")
	}

	// Check the profile file
	profilePath := r.ProfilePath(7)
	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("profile should exist: %v", err)
	}
}
