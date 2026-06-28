package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// defaultMemoryCacheTTL is the period during which a cached entry is trusted
// without re-validating mtimes. Validation walks every .md file via os.Stat;
// in fast chat turns these scans dominate. 30s balances freshness with I/O
// reduction; explicit invalidate() calls (after writes) keep the cache correct.
const defaultMemoryCacheTTL = 30 * time.Second

// MemoryCache caches pre-rendered memory directory content by mtime.
// A directory's content is re-read when any .md file's mtime changes, a new
// .md file appears, or an existing one is deleted.
// Exported so the daemon can share one instance across Telegram and TUI
// pipeline instances (TUI creates a pipeline per send; without a shared
// cache every turn re-reads all memory .md files from disk).
type MemoryCache struct {
	mu      sync.RWMutex
	entries map[string]memoryCacheEntry
	ttl     time.Duration
}

type memoryCacheEntry struct {
	content       string
	mtimes        map[string]time.Time // filename → mtime (set of files at put time)
	filenames     map[string]struct{}  // set of .md filenames for new/deleted detection
	lastValidated time.Time            // skip mtime check while within TTL
}

func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		entries: make(map[string]memoryCacheEntry, 16),
		ttl:     defaultMemoryCacheTTL,
	}
}

// get returns cached content for dir if the directory's .md files haven't
// changed (mtime, additions, or deletions). Returns false on first access
// or when any change is detected. Validation is skipped entirely while the
// entry is within memoryCacheTTL — explicit invalidate() calls after writes
// keep things consistent across that window.
func (c *MemoryCache) get(dir string) (string, bool) {
	c.mu.RLock()
	entry, ok := c.entries[dir]
	c.mu.RUnlock()
	if !ok {
		return "", false
	}

	if c.ttl > 0 && time.Since(entry.lastValidated) < c.ttl {
		return entry.content, true
	}

	// Read current dir to detect new or deleted .md files.
	// The mtimes map is never mutated after put() publishes it (immutable),
	// so it's safe to read outside the lock.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	// Quick count check: if the number of .md files differs, something changed.
	var mdCount int
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			mdCount++
		}
	}
	if mdCount != len(entry.filenames) {
		return "", false
	}

	// Verify every known file still has the same mtime.
	for name, cachedMtime := range entry.mtimes {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !fi.ModTime().Equal(cachedMtime) {
			return "", false
		}
	}

	// Refresh validation timestamp so the next few calls hit the fast path.
	c.mu.Lock()
	if e, ok := c.entries[dir]; ok {
		e.lastValidated = time.Now()
		c.entries[dir] = e
	} else {
		c.mu.Unlock()
		return "", false
	}
	c.mu.Unlock()

	return entry.content, true
}

// put stores pre-rendered content for dir, recording mtimes of all .md files.
// mtimes is a map of filename → ModTime already collected by the caller to
// avoid a redundant ReadDir. If nil, put will read the dir itself.
func (c *MemoryCache) put(dir string, content string, mtimes map[string]time.Time) {
	if mtimes == nil {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		mtimes = make(map[string]time.Time, len(entries))
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			fi, err := e.Info()
			if err != nil {
				continue
			}
			mtimes[e.Name()] = fi.ModTime()
		}
	}

	filenames := make(map[string]struct{}, len(mtimes))
	for name := range mtimes {
		filenames[name] = struct{}{}
	}

	c.mu.Lock()
	c.entries[dir] = memoryCacheEntry{
		content:       content,
		mtimes:        mtimes,
		filenames:     filenames,
		lastValidated: time.Now(),
	}
	c.mu.Unlock()
}

// invalidate removes the cached entry for dir, forcing a re-read on next access.
func (c *MemoryCache) invalidate(dir string) {
	c.mu.Lock()
	delete(c.entries, dir)
	c.mu.Unlock()
}

// InvalidateMemoryDirs clears cached entries for all memory directories that may
// have been modified. Called after nudge/dream writes and CWD changes.
func (bc *Service) InvalidateMemoryDirs(chatID int64, threadID int, userID int64, cwd string) {
	if bc.memoryCache == nil {
		return
	}

	// Invalidate user memory dir via canonical PathResolver
	if bc.resolver != nil && userID != 0 {
		if userDir := bc.resolver.UserMemoryDir(userID); userDir != "" {
			bc.memoryCache.invalidate(userDir)
		}
	} else if bc.memoryDir != "" {
		bc.memoryCache.invalidate(bc.memoryDir)
	}

	// Invalidate topic memory dir via canonical PathResolver
	if bc.resolver != nil {
		if topicDir := bc.resolver.TopicMemoryDir(chatID, threadID); topicDir != "" {
			bc.memoryCache.invalidate(topicDir)
		}
	}

	if cwd != "" && bc.resolver != nil {
		// CWD overlay — scoped by (chat_id, thread_id) already
		if overlayDir := bc.resolver.TopicCwdOverlayDir(chatID, threadID); overlayDir != "" {
			bc.memoryCache.invalidate(overlayDir)
		}
		// project_team: removed — redundant with cwd_overlay
	}
}

