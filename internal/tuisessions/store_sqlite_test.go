package tuisessions

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "tui_sessions.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestSQLiteStore_CreateAndGet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	sess, err := store.Create(ctx, -9000002, "work")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sess.ChatID != -9000002 || sess.Name != "work" {
		t.Errorf("Create returned %+v, want ChatID=-9000002 Name=work", sess)
	}

	got, err := store.Get(ctx, -9000002)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "work" {
		t.Errorf("Get.Name = %q, want %q", got.Name, "work")
	}
}

func TestSQLiteStore_CreateDuplicate(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, -9000003, "research"); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := store.Create(ctx, -9000003, "research")
	if err != ErrSessionExists {
		t.Errorf("duplicate Create err = %v, want ErrSessionExists", err)
	}
}

func TestSQLiteStore_GetNotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Get(context.Background(), -9000005)
	if err != ErrSessionNotFound {
		t.Errorf("Get(missing) err = %v, want ErrSessionNotFound", err)
	}
}

func TestSQLiteStore_List(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Create in order; List should return by last_used_at DESC.
	for _, tc := range []struct {
		chatID int64
		name   string
	}{
		{-9000001, "dm"},
		{-9000002, "work"},
		{-9000003, "research"},
	} {
		if _, err := store.Create(ctx, tc.chatID, tc.name); err != nil {
			t.Fatalf("Create %s: %v", tc.name, err)
		}
	}

	// Touch the "work" session so it becomes most recently used.
	if err := store.Touch(ctx, -9000002); err != nil {
		t.Fatalf("Touch: %v", err)
	}

	sessions, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("List returned %d sessions, want 3", len(sessions))
	}
	if sessions[0].Name != "work" {
		t.Errorf("List[0].Name = %q, want %q (most recently used)", sessions[0].Name, "work")
	}
}

func TestSQLiteStore_TouchNotFound(t *testing.T) {
	store := newTestStore(t)
	err := store.Touch(context.Background(), -9000009)
	if err != ErrSessionNotFound {
		t.Errorf("Touch(missing) err = %v, want ErrSessionNotFound", err)
	}
}

func TestSQLiteStore_Delete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.Create(ctx, -9000004, "temp"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := store.Delete(ctx, -9000004); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(ctx, -9000004); err != ErrSessionNotFound {
		t.Errorf("Get after Delete err = %v, want ErrSessionNotFound", err)
	}
}

func TestSQLiteStore_DeleteMissingIsNoOp(t *testing.T) {
	store := newTestStore(t)
	// Deleting a non-existent row should not error.
	if err := store.Delete(context.Background(), -9000009); err != nil {
		t.Errorf("Delete(missing) err = %v, want nil", err)
	}
}

func TestSQLiteStore_ListEmpty(t *testing.T) {
	store := newTestStore(t)
	sessions, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("List on empty store returned %d, want 0", len(sessions))
	}
}
