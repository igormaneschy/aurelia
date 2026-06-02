package users

import (
	"fmt"
	"os"
	"path/filepath"
)

// Resolver returns absolute paths for a given user_id.
type Resolver struct {
	root string // ~/.aurelia
}

// NewResolver creates a Resolver with the given root directory.
func NewResolver(root string) *Resolver {
	return &Resolver{root: root}
}

// Root returns the resolver's root directory.
func (r *Resolver) Root() string {
	return r.root
}

// UserRoot returns the base directory for a user.
func (r *Resolver) UserRoot(userID int64) string {
	return filepath.Join(r.root, "users", fmt.Sprintf("%d", userID))
}

// MemoryDir returns the memory directory for a user.
func (r *Resolver) MemoryDir(userID int64) string {
	return filepath.Join(r.UserRoot(userID), "memory")
}

// PersonasDir returns the personas directory for a user.
func (r *Resolver) PersonasDir(userID int64) string {
	return filepath.Join(r.UserRoot(userID), "personas")
}

// UserMdPath returns the path to a user's USER.md file.
func (r *Resolver) UserMdPath(userID int64) string {
	return filepath.Join(r.PersonasDir(userID), "USER.md")
}

// UserModePath returns the path to a user's mode overlay file.
// Mode must already be normalized (call NormalizeMode first).
func (r *Resolver) UserModePath(userID int64, mode string) string {
	return filepath.Join(r.PersonasDir(userID), "mode_"+mode+".md")
}

// ProfilePath returns the path to a user's profile.json file.
func (r *Resolver) ProfilePath(userID int64) string {
	return filepath.Join(r.UserRoot(userID), "profile.json")
}

// SkillsDir returns the skills directory for a user.
func (r *Resolver) SkillsDir(userID int64) string {
	return filepath.Join(r.UserRoot(userID), "skills")
}

// EnsureUserDir creates all required user subdirectories.
func (r *Resolver) EnsureUserDir(userID int64) error {
	dirs := []string{
		r.MemoryDir(userID),
		r.PersonasDir(userID),
		filepath.Join(r.UserRoot(userID), "projects"),
		r.SkillsDir(userID),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	return nil
}

// TopicsDir returns the global topic memory directory (shared across users).
// Topics are shared because they represent cross-user knowledge base entries
// (e.g., forum topics visible to multiple participants), not user-private memory.
func (r *Resolver) TopicsDir() string {
	return filepath.Join(r.root, "topics")
}
