package bundle

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func validBundle() *schemas.ExistingCodebaseContextBundle {
	return &schemas.ExistingCodebaseContextBundle{
		ProjectId:        "demo",
		CommitSha:        strings.Repeat("a", 40),
		SchemaVersion:    schemas.ExistingCodebaseContextBundleSchemaVersion(SchemaVersion),
		ContentHash:      "test-hash",
		ArchitectureDocs: []schemas.FileSnapshot{},
		PackageManifests: []schemas.ManifestSnapshot{},
		ExistingBeads:    []schemas.BeadSummary{},
		HealthHotspots:   []schemas.HotspotSummary{},
		Excluded:         []string{},
		Redactions:       []schemas.RedactionEntry{},
	}
}

func TestBundleCachePutGet(t *testing.T) {
	c := NewBundleCache()
	b := validBundle()
	if err := c.Put("key-1", b); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := c.Get("key-1")
	if !ok {
		t.Fatal("Get returned ok=false after Put")
	}
	if got != b {
		t.Errorf("Get returned different pointer: got %p want %p", got, b)
	}
}

func TestBundleCacheGetMiss(t *testing.T) {
	c := NewBundleCache()
	got, ok := c.Get("missing")
	if ok || got != nil {
		t.Errorf("Get on missing key returned %v, %v; want nil, false", got, ok)
	}
}

func TestBundleCachePutNilBundleRejected(t *testing.T) {
	c := NewBundleCache()
	err := c.Put("k", nil)
	if !errors.Is(err, ErrInvalidCacheValue) {
		t.Errorf("err = %v, want ErrInvalidCacheValue", err)
	}
}

func TestBundleCachePutUnhashedBundleRejected(t *testing.T) {
	c := NewBundleCache()
	b := validBundle()
	b.ContentHash = ""
	err := c.Put("k", b)
	if !errors.Is(err, ErrInvalidCacheValue) {
		t.Errorf("err = %v, want ErrInvalidCacheValue (empty ContentHash)", err)
	}
}

func TestBundleCachePutSchemaVersionMismatchRejected(t *testing.T) {
	c := NewBundleCache()
	b := validBundle()
	b.SchemaVersion = schemas.ExistingCodebaseContextBundleSchemaVersion(SchemaVersion + 1)
	err := c.Put("k", b)
	if !errors.Is(err, ErrSchemaVersionMismatch) {
		t.Errorf("err = %v, want ErrSchemaVersionMismatch", err)
	}
}

func TestBundleCachePutEmptyKeyRejected(t *testing.T) {
	c := NewBundleCache()
	err := c.Put("", validBundle())
	if err == nil {
		t.Fatal("empty key should error")
	}
}

func TestBundleCacheTTLExpiration(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	cur := now
	c := NewBundleCacheWith(10, time.Hour, func() time.Time { return cur })
	if err := c.Put("k", validBundle()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	// Advance past TTL.
	cur = now.Add(time.Hour + time.Second)
	if _, ok := c.Get("k"); ok {
		t.Error("Get returned ok=true after TTL expiry")
	}
	stats := c.Stats()
	if stats.Evictions == 0 {
		t.Error("expired entry should be evicted on Get")
	}
}

func TestBundleCacheLRUEviction(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	cur := now
	c := NewBundleCacheWith(2, time.Hour, func() time.Time { return cur })

	// Put 3 entries (over capacity 2).
	for _, k := range []string{"a", "b", "c"} {
		cur = cur.Add(time.Second)
		if err := c.Put(k, validBundle()); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}
	// "a" is the oldest by lastAccess; should have been evicted.
	stats := c.Stats()
	if stats.Size != 2 {
		t.Errorf("Size = %d, want 2", stats.Size)
	}
	if _, ok := c.Get("a"); ok {
		t.Error("LRU eviction did not remove oldest entry 'a'")
	}
	if _, ok := c.Get("b"); !ok {
		t.Error("'b' should still be present")
	}
}

func TestBundleCacheGetUpdatesLRU(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	cur := now
	c := NewBundleCacheWith(2, time.Hour, func() time.Time { return cur })

	cur = cur.Add(time.Second)
	c.Put("a", validBundle())
	cur = cur.Add(time.Second)
	c.Put("b", validBundle())
	cur = cur.Add(time.Second)
	// Access "a" → it's now the more-recently-used.
	c.Get("a")
	cur = cur.Add(time.Second)
	c.Put("c", validBundle())

	// "b" should be evicted now (a was accessed; c is newest).
	if _, ok := c.Get("b"); ok {
		t.Error("'b' should have been evicted by LRU after 'a' was Get")
	}
	if _, ok := c.Get("a"); !ok {
		t.Error("'a' should still be present after access bumped its LRU position")
	}
}

func TestBundleCacheStats(t *testing.T) {
	c := NewBundleCache()
	c.Put("a", validBundle())
	c.Get("a")          // hit
	c.Get("missing")    // miss
	stats := c.Stats()
	if stats.Hits != 1 {
		t.Errorf("Hits = %d, want 1", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Misses = %d, want 1", stats.Misses)
	}
	if stats.Stores != 1 {
		t.Errorf("Stores = %d, want 1", stats.Stores)
	}
	if stats.Size != 1 {
		t.Errorf("Size = %d, want 1", stats.Size)
	}
	if stats.Capacity != MaxCacheEntries {
		t.Errorf("Capacity = %d, want %d", stats.Capacity, MaxCacheEntries)
	}
}

func TestBundleCacheDelete(t *testing.T) {
	c := NewBundleCache()
	c.Put("a", validBundle())

	if !c.Delete("a") {
		t.Error("Delete should return true for existing entry")
	}
	if c.Delete("a") {
		t.Error("Delete should return false on second call")
	}
	if _, ok := c.Get("a"); ok {
		t.Error("Get should miss after Delete")
	}
}

func TestBundleCacheReset(t *testing.T) {
	c := NewBundleCache()
	c.Put("a", validBundle())
	c.Put("b", validBundle())
	c.Get("a")
	c.Reset()
	stats := c.Stats()
	if stats.Size != 0 {
		t.Errorf("Size after Reset = %d, want 0", stats.Size)
	}
	if stats.Hits != 0 || stats.Misses != 0 || stats.Stores != 0 {
		t.Errorf("counters not reset: %+v", stats)
	}
	if _, ok := c.Get("a"); ok {
		t.Error("Get after Reset returned hit")
	}
}

func TestBundleCacheConcurrentSafety(t *testing.T) {
	c := NewBundleCache()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := strings.Repeat(string(rune('a'+n%26)), 3)
			_ = c.Put(key, validBundle())
			_, _ = c.Get(key)
		}(i)
	}
	wg.Wait()
	// Just confirm no race / deadlock and stats are sensible.
	stats := c.Stats()
	if stats.Stores == 0 {
		t.Error("no stores recorded under concurrent load")
	}
}

func TestNewBundleCacheWithZeroCapacityFallsBack(t *testing.T) {
	c := NewBundleCacheWith(0, 0, nil)
	if c.capacity != MaxCacheEntries {
		t.Errorf("capacity = %d, want fallback %d", c.capacity, MaxCacheEntries)
	}
	if c.ttl != DefaultCacheTTL {
		t.Errorf("ttl = %v, want fallback %v", c.ttl, DefaultCacheTTL)
	}
	if c.clock == nil {
		t.Error("clock fallback nil")
	}
}
