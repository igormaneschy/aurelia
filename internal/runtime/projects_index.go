package runtime

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ProjectIndex caches project name → path lookups to avoid scanning the
// filesystem on every message. Results are persisted to disk and rebuilt
// periodically in the background.
type ProjectIndex struct {
	mu                 sync.RWMutex
	projects           map[string]string // project name → absolute path
	lastBuilt          time.Time
	lastRebuildAttempt time.Time
	rebuilding         bool
	jsonPath           string // path to persistent cache file
	roots              []string

	// rebuildFn is a test seam. When non-nil, ScheduleRebuild calls this
	// instead of Rebuild. Used to inject panics for recovery testing.
	rebuildFn func(context.Context) error
}

// NewProjectIndex creates an index that scans the given roots and persists
// to jsonPath. If jsonPath exists, it is loaded on construction.
func NewProjectIndex(roots []string, jsonPath string) *ProjectIndex {
	idx := &ProjectIndex{
		projects: make(map[string]string),
		jsonPath: jsonPath,
		roots:    roots,
	}
	idx.load()
	return idx
}

// persistPath returns the default path for the project index cache file.
func PersistPath(aureliaDir string) string {
	return filepath.Join(aureliaDir, "projects.json")
}

// Lookup returns the absolute path for a project name. Returns "" if not found.
func (p *ProjectIndex) Lookup(name string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.projects[strings.ToLower(name)]
}

// ScheduleRebuild starts a background rebuild unless another rebuild is
// already running or the debounce interval has not elapsed.
func (p *ProjectIndex) ScheduleRebuild(debounce time.Duration) bool {
	p.mu.Lock()
	now := time.Now()
	if p.rebuilding || (!p.lastRebuildAttempt.IsZero() && now.Sub(p.lastRebuildAttempt) < debounce) {
		p.mu.Unlock()
		return false
	}
	p.rebuilding = true
	p.lastRebuildAttempt = now
	p.mu.Unlock()

	go func() {
		// Reset rebuilding in a defer so it always runs, even on panic.
		// Must be registered FIRST (runs LAST in LIFO defer order) so the
		// recovery defer runs first and the reset runs after recovery.
		defer func() {
			p.mu.Lock()
			p.rebuilding = false
			p.mu.Unlock()
		}()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("project index: panic in scheduled rebuild: %v", r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		var err error
		if p.rebuildFn != nil {
			err = p.rebuildFn(ctx)
		} else {
			err = p.Rebuild(ctx)
		}
		if err != nil {
			log.Printf("project index: scheduled rebuild error: %v", err)
		}
	}()
	return true
}

// Rebuild scans roots and rebuilds the index. Safe to call concurrently.
// On error, the old index is preserved.
func (p *ProjectIndex) Rebuild(ctx context.Context) error {
	projects := make(map[string]string)

	for _, root := range rootsToScan(p.roots) {
		if err := ctx.Err(); err != nil {
			return err
		}
		scanRoot(root, projects)
	}

	p.mu.Lock()
	p.projects = projects
	p.lastBuilt = time.Now()
	p.mu.Unlock()

	// Persist to disk (best-effort)
	if p.jsonPath != "" {
		if err := p.save(); err != nil {
			log.Printf("project index: failed to persist: %v", err)
		}
	}

	return nil
}

// rootsToScan returns the actual root directories to scan, falling back to
// user home + mount points if roots is empty (for backward compatibility).
func rootsToScan(roots []string) []string {
	if len(roots) > 0 {
		return roots
	}
	home, _ := os.UserHomeDir()
	var r []string
	if home != "" {
		r = append(r, home)
	}
	for _, media := range []string{"/media", "/mnt"} {
		if userDirs, err := os.ReadDir(media); err == nil {
			for _, u := range userDirs {
				if u.IsDir() {
					r = append(r, filepath.Join(media, u.Name()))
				}
			}
		}
	}
	return r
}

// scanRoot walks a root directory up to depth 4, collecting project directories.
func scanRoot(root string, projects map[string]string) {
	rootDepth := strings.Count(root, string(filepath.Separator))
	const maxDepth = 4

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			if err != nil && d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()

		if path != root {
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return filepath.SkipDir
			}
		}

		depth := strings.Count(path, string(filepath.Separator)) - rootDepth
		if depth > maxDepth {
			return filepath.SkipDir
		}
		if depth < 2 {
			return nil
		}

		home, _ := os.UserHomeDir()
		if home != "" && path == home {
			return nil
		}

		// Check if it's a real project (has at least some files)
		entries, readErr := os.ReadDir(path)
		if readErr != nil || len(entries) < 2 {
			return nil
		}

		lowerName := strings.ToLower(name)
		// Only index names that look like project names (avoid generic dirs)
		if strings.ContainsAny(lowerName, "-_.0123456789") || hasCamelCase(name) {
			projects[lowerName] = path
		}
		return nil
	})
}

func hasCamelCase(w string) bool {
	for i := 1; i < len(w); i++ {
		if w[i-1] >= 'a' && w[i-1] <= 'z' && w[i] >= 'A' && w[i] <= 'Z' {
			return true
		}
	}
	return false
}

// skipDirs contains directory names to skip during scanning.
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, ".cache": true, ".local": true,
	".npm": true, ".cargo": true, ".rustup": true, "vendor": true,
	".vscode": true, ".idea": true, "__pycache__": true, ".tox": true,
	"dist": true, "build": true, ".next": true, ".nuxt": true,
	".gradle": true, ".m2": true, "target": true, ".docker": true,
	".virtualenvs": true, ".pyenv": true, ".nvm": true, ".sdkman": true,
}

// load reads the index from disk.
func (p *ProjectIndex) load() {
	data, err := os.ReadFile(p.jsonPath)
	if err != nil {
		return // First run — no cache yet
	}
	var entries []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Printf("project index: invalid cache file %s: %v", p.jsonPath, err)
		return
	}
	p.mu.Lock()
	for _, e := range entries {
		p.projects[strings.ToLower(e.Name)] = e.Path
	}
	p.mu.Unlock()
}

// save persists the index to disk as a sorted JSON array.
func (p *ProjectIndex) save() error {
	p.mu.RLock()
	entries := make([]struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}, 0, len(p.projects))
	for name, path := range p.projects {
		entries = append(entries, struct {
			Name string `json:"name"`
			Path string `json:"path"`
		}{Name: name, Path: path})
	}
	p.mu.RUnlock()

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.jsonPath, data, 0644)
}
