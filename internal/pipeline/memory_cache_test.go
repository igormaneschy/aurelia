package pipeline

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMemoryCache_HitAfterPut(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	dir := t.TempDir()

	// Create a memory file
	writeFile(t, filepath.Join(dir, "note.md"), "hello world")

	// Cache miss on first call
	_, ok := cache.get(dir)
	if ok {
		t.Fatal("expected cache miss before put")
	}

	cache.put(dir, "cached content", nil)

	// Cache hit after put
	got, ok := cache.get(dir)
	if !ok {
		t.Fatal("expected cache hit after put")
	}
	if got != "cached content" {
		t.Fatalf("expected 'cached content', got %q", got)
	}
}

func TestMemoryCache_MissAfterMtimeChange(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	cache.ttl = 0 // disable TTL so mtime validation runs every get()
	dir := t.TempDir()

	filePath := filepath.Join(dir, "data.md")
	writeFile(t, filePath, "version 1")

	cache.put(dir, "version 1", nil)

	// Modify the file
	time.Sleep(10 * time.Millisecond) // ensure different mtime
	writeFile(t, filePath, "version 2")

	// Cache should miss because mtime changed
	_, ok := cache.get(dir)
	if ok {
		t.Fatal("expected cache miss after file modification")
	}
}

func TestMemoryCache_InvalidateRemovesEntry(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "data.md"), "content")

	cache.put(dir, "content", nil)

	// Confirm cache hit
	if _, ok := cache.get(dir); !ok {
		t.Fatal("expected cache hit before invalidation")
	}

	cache.Invalidate(dir)

	// Cache miss after invalidation
	if _, ok := cache.get(dir); ok {
		t.Fatal("expected cache miss after invalidation")
	}
}

func TestMemoryCache_SelectiveInvalidationPreservesOtherDirs(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	dirA := t.TempDir()
	dirB := t.TempDir()

	writeFile(t, filepath.Join(dirA, "a.md"), "a")
	writeFile(t, filepath.Join(dirB, "b.md"), "b")
	cache.put(dirA, "content A", nil)
	cache.put(dirB, "content B", nil)

	cache.Invalidate(dirA)

	if _, ok := cache.get(dirA); ok {
		t.Fatal("expected cache miss for invalidated dirA")
	}
	got, ok := cache.get(dirB)
	if !ok || got != "content B" {
		t.Fatalf("expected cache hit for dirB, got %q ok=%v", got, ok)
	}
}

func TestMemoryCache_IgnoresNonMdFiles(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	dir := t.TempDir()

	// Only .md files should be tracked for mtime
	writeFile(t, filepath.Join(dir, "note.md"), "content")
	writeFile(t, filepath.Join(dir, "data.txt"), "ignored")

	cache.put(dir, "content", nil)

	// Modify the .txt file (should NOT invalidate cache)
	time.Sleep(10 * time.Millisecond)
	writeFile(t, filepath.Join(dir, "data.txt"), "changed")

	_, ok := cache.get(dir)
	if !ok {
		t.Fatal("expected cache hit when only non-md file changed")
	}
}

func TestMemoryCache_EmptyDirProducesCacheHit(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	dir := t.TempDir()

	// Put empty content for empty dir
	cache.put(dir, "", nil)

	got, ok := cache.get(dir)
	if !ok {
		t.Fatal("expected cache hit for empty dir")
	}
	if got != "" {
		t.Fatalf("expected empty content, got %q", got)
	}
}

func TestMemoryCache_MissAfterNewFileAdded(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	cache.ttl = 0
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "a.md"), "file a")
	cache.put(dir, "file a only", nil)

	// Add a new .md file
	writeFile(t, filepath.Join(dir, "b.md"), "file b")

	// Cache should miss because .md file count changed
	_, ok := cache.get(dir)
	if ok {
		t.Fatal("expected cache miss after new file added")
	}
}

func TestMemoryCache_MissAfterFileDeleted(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	cache.ttl = 0
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "a.md"), "file a")
	writeFile(t, filepath.Join(dir, "b.md"), "file b")
	cache.put(dir, "two files", nil)

	// Delete one .md file
	if err := os.Remove(filepath.Join(dir, "b.md")); err != nil {
		t.Fatal(err)
	}

	// Cache should miss because .md file count changed
	_, ok := cache.get(dir)
	if ok {
		t.Fatal("expected cache miss after file deleted")
	}
}

func TestMemoryCache_TTLFreshness(t *testing.T) {
	t.Parallel()

	cache := NewMemoryCache()
	cache.ttl = 50 * time.Millisecond
	dir := t.TempDir()

	filePath := filepath.Join(dir, "data.md")
	writeFile(t, filePath, "v1")
	cache.put(dir, "v1", nil)

	// Within TTL: change should NOT be detected (cache trusts itself).
	time.Sleep(5 * time.Millisecond)
	writeFile(t, filePath, "v2")
	if _, ok := cache.get(dir); !ok {
		t.Fatal("expected cache hit within TTL even after mtime change")
	}

	// After TTL: validation runs and detects the change.
	time.Sleep(60 * time.Millisecond)
	if _, ok := cache.get(dir); ok {
		t.Fatal("expected cache miss after TTL with mtime change")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
