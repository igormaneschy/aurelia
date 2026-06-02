package users

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolver_Paths(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name string
		fn   func(r *Resolver) string
		want string
	}{
		{
			name: "Root",
			fn:   func(r *Resolver) string { return r.Root() },
			want: root,
		},
		{
			name: "UserRoot",
			fn:   func(r *Resolver) string { return r.UserRoot(42) },
			want: filepath.Join(root, "users", "42"),
		},
		{
			name: "MemoryDir",
			fn:   func(r *Resolver) string { return r.MemoryDir(42) },
			want: filepath.Join(root, "users", "42", "memory"),
		},
		{
			name: "PersonasDir",
			fn:   func(r *Resolver) string { return r.PersonasDir(42) },
			want: filepath.Join(root, "users", "42", "personas"),
		},
		{
			name: "UserMdPath",
			fn:   func(r *Resolver) string { return r.UserMdPath(42) },
			want: filepath.Join(root, "users", "42", "personas", "USER.md"),
		},
		{
			name: "ProfilePath",
			fn:   func(r *Resolver) string { return r.ProfilePath(42) },
			want: filepath.Join(root, "users", "42", "profile.json"),
		},
		{
			name: "SkillsDir",
			fn:   func(r *Resolver) string { return r.SkillsDir(42) },
			want: filepath.Join(root, "users", "42", "skills"),
		},
		{
			name: "TopicsDir",
			fn:   func(r *Resolver) string { return r.TopicsDir() },
			want: filepath.Join(root, "topics"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewResolver(root)
			got := tt.fn(r)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolver_EnsureUserDir_CreatesAll(t *testing.T) {
	root := t.TempDir()
	r := NewResolver(root)

	if err := r.EnsureUserDir(42); err != nil {
		t.Fatalf("EnsureUserDir() error = %v", err)
	}

	dirs := []string{
		r.MemoryDir(42),
		r.PersonasDir(42),
		filepath.Join(r.UserRoot(42), "projects"),
		r.SkillsDir(42),
	}
	for _, d := range dirs {
		if info, err := os.Stat(d); err != nil {
			t.Errorf("expected dir %q to exist: %v", d, err)
		} else if !info.IsDir() {
			t.Errorf("%q is not a directory", d)
		}
	}
}

func TestResolver_EnsureUserDir_Idempotent(t *testing.T) {
	root := t.TempDir()
	r := NewResolver(root)

	if err := r.EnsureUserDir(42); err != nil {
		t.Fatalf("first call error = %v", err)
	}
	if err := r.EnsureUserDir(42); err != nil {
		t.Fatalf("second call error = %v", err)
	}
}
