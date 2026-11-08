package memorycontext

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	DefaultMaxChars    = 4000
	DefaultCacheTTL    = 30 * time.Second
	DefaultRecentPages = 3
)

// WorkingSet is the bounded project context injected into the prompt.
type WorkingSet struct {
	Content         string
	Chars           int
	Sources         []string
	Degraded        bool
	DegradedReason  string
	PendingHandoffs int
	GeneratedAt     time.Time
}

// Status is the read-only memory status used by project state and /memory.
type Status struct {
	Available         bool
	Degraded          bool
	Pages             int
	Observations      int
	Sessions          int
	PendingHandoffs   int
	LastObservationAt string
	Sources           []string
	Error             string
}

// Provider assembles project memory context without owning durable state.
type Provider interface {
	WorkingSet(ctx context.Context, scope Scope) (WorkingSet, error)
	Status(ctx context.Context, scope Scope) (Status, error)
}

// HandoffCanceler is implemented by providers that can expire pending handoffs
// when the user resets a conversation.
type HandoffCanceler interface {
	CancelHandoffs(ctx context.Context, scope Scope) error
}

type cacheEntry struct {
	workingSet WorkingSet
	expiresAt  time.Time
}

// Cache stores working sets by scope. It is only a read-through cache; it is
// never used as a durable memory source.
type Cache struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	entries map[string]cacheEntry
}

func NewCache(ttl time.Duration) *Cache {
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	return &Cache{ttl: ttl, now: time.Now, entries: make(map[string]cacheEntry)}
}

func (c *Cache) Get(key string) (WorkingSet, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || c.now().After(entry.expiresAt) {
		return WorkingSet{}, false
	}
	return cloneWorkingSet(entry.workingSet), true
}

func (c *Cache) GetStale(key string) (WorkingSet, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return WorkingSet{}, false
	}
	ws := cloneWorkingSet(entry.workingSet)
	ws.Degraded = true
	return ws, true
}

func (c *Cache) Set(key string, ws WorkingSet) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ws = cloneWorkingSet(ws)
	c.entries[key] = cacheEntry{workingSet: ws, expiresAt: c.now().Add(c.ttl)}
}

func (c *Cache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func (c *Cache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]cacheEntry)
}

func cloneWorkingSet(ws WorkingSet) WorkingSet {
	ws.Sources = append([]string(nil), ws.Sources...)
	return ws
}

// CachedProvider adds read-through caching and a read-only fallback. An
// ai-memory error never blocks a turn: the last cached working set is returned
// with Degraded=true, or an empty degraded set when no cache exists.
type CachedProvider struct {
	provider Provider
	cache    *Cache
	now      func() time.Time
}

func NewCachedProvider(provider Provider, cache *Cache) *CachedProvider {
	if cache == nil {
		cache = NewCache(DefaultCacheTTL)
	}
	return &CachedProvider{provider: provider, cache: cache, now: time.Now}
}

func (p *CachedProvider) WorkingSet(ctx context.Context, scope Scope) (WorkingSet, error) {
	if !scope.HasProject() {
		return WorkingSet{GeneratedAt: p.now()}, nil
	}
	key := scope.Key()
	if ws, ok := p.cache.Get(key); ok {
		return ws, nil
	}
	ws, err := p.provider.WorkingSet(ctx, scope)
	if err == nil {
		p.cache.Set(key, ws)
		return ws, nil
	}
	if cached, ok := p.cache.GetStale(key); ok {
		cached.DegradedReason = sanitizeDegradedReason(err)
		cached.GeneratedAt = p.now()
		return cached, nil
	}
	return WorkingSet{
		Degraded:       true,
		DegradedReason: sanitizeDegradedReason(err),
		GeneratedAt:    p.now(),
	}, nil
}

func (p *CachedProvider) Status(ctx context.Context, scope Scope) (Status, error) {
	if !scope.HasProject() {
		return Status{Available: false, Degraded: true, Error: "no ai-memory project for cwd"}, nil
	}
	status, err := p.provider.Status(ctx, scope)
	if err != nil {
		return Status{
			Available: false,
			Degraded:  true,
			Error:     sanitizeDegradedReason(err),
		}, nil
	}
	return status, nil
}

func (p *CachedProvider) Invalidate(scope Scope) {
	p.cache.Invalidate(scope.Key())
}

// CancelHandoffs delegates handoff cancellation to the underlying provider
// when it supports it.
func (p *CachedProvider) CancelHandoffs(ctx context.Context, scope Scope) error {
	if canceler, ok := p.provider.(HandoffCanceler); ok {
		return canceler.CancelHandoffs(ctx, scope)
	}
	return nil
}

func sanitizeDegradedReason(err error) string {
	if err == nil {
		return "ai-memory unavailable"
	}
	reason := strings.TrimSpace(err.Error())
	if len(reason) > 240 {
		reason = truncateRunes(reason, 240)
	}
	return reason
}

func truncateRunes(value string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	return string(runes[:max])
}
