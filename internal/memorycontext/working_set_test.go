package memorycontext

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeProvider struct {
	workingSet WorkingSet
	status     Status
	err        error
	calls      int
}

func (f *fakeProvider) WorkingSet(context.Context, Scope) (WorkingSet, error) {
	f.calls++
	return f.workingSet, f.err
}

func (f *fakeProvider) Status(context.Context, Scope) (Status, error) {
	f.calls++
	return f.status, f.err
}

func TestCacheExpiryAndStaleRead(t *testing.T) {
	cache := NewCache(time.Minute)
	now := time.Unix(100, 0)
	cache.now = func() time.Time { return now }
	ws := WorkingSet{Content: "cached", Chars: 6}
	cache.Set("key", ws)

	if got, ok := cache.Get("key"); !ok || got.Content != "cached" {
		t.Fatalf("cache.Get = %+v, %v", got, ok)
	}
	now = now.Add(2 * time.Minute)
	if _, ok := cache.Get("key"); ok {
		t.Fatal("expired cache entry returned by Get")
	}
	stale, ok := cache.GetStale("key")
	if !ok || !stale.Degraded || stale.Content != "cached" {
		t.Fatalf("GetStale = %+v, %v", stale, ok)
	}
}

func TestCachedProviderFallsBackToStaleWorkingSet(t *testing.T) {
	cache := NewCache(time.Minute)
	now := time.Unix(200, 0)
	cache.now = func() time.Time { return now }
	provider := &fakeProvider{workingSet: WorkingSet{Content: "project facts", Chars: 14}}
	cached := NewCachedProvider(provider, cache)
	cached.now = func() time.Time { return now }
	scope := Scope{Workspace: "personal", Project: "aurelia", CWD: "/project"}

	first, err := cached.WorkingSet(context.Background(), scope)
	if err != nil || first.Content != "project facts" {
		t.Fatalf("first WorkingSet = %+v, %v", first, err)
	}
	provider.err = errors.New("server down")
	now = now.Add(2 * time.Minute)
	second, err := cached.WorkingSet(context.Background(), scope)
	if err != nil {
		t.Fatalf("fallback WorkingSet: %v", err)
	}
	if second.Content != "project facts" || !second.Degraded || second.DegradedReason == "" {
		t.Fatalf("fallback = %+v, want stale degraded content", second)
	}
}

func TestCachedProviderDegradesWithoutCache(t *testing.T) {
	provider := &fakeProvider{err: errors.New("server down")}
	cached := NewCachedProvider(provider, NewCache(time.Minute))
	scope := Scope{Workspace: "personal", Project: "aurelia", CWD: "/project"}
	ws, err := cached.WorkingSet(context.Background(), scope)
	if err != nil {
		t.Fatalf("WorkingSet: %v", err)
	}
	if !ws.Degraded || ws.Content != "" || ws.DegradedReason == "" {
		t.Fatalf("degraded working set = %+v", ws)
	}
	status, err := cached.Status(context.Background(), scope)
	if err != nil || !status.Degraded || status.Available {
		t.Fatalf("degraded status = %+v, %v", status, err)
	}
}

func TestCachedProviderSkipsProviderWithoutProject(t *testing.T) {
	provider := &fakeProvider{workingSet: WorkingSet{Content: "must not be used"}}
	cached := NewCachedProvider(provider, NewCache(time.Minute))
	ws, err := cached.WorkingSet(context.Background(), Scope{CWD: "/unmarked"})
	if err != nil {
		t.Fatalf("WorkingSet: %v", err)
	}
	if ws.Content != "" || provider.calls != 0 {
		t.Fatalf("unexpected provider call: ws=%+v calls=%d", ws, provider.calls)
	}
}

func TestTruncateRunesDoesNotSplitUTF8(t *testing.T) {
	if got := truncateRunes("ação", 3); got != "açã" {
		t.Fatalf("truncateRunes = %q, want açã", got)
	}
	if got := truncateRunes("ação", 0); got != "" {
		t.Fatalf("truncateRunes(0) = %q, want empty", got)
	}
}
