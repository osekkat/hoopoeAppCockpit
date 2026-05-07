// cache.go — content-addressable cache for assembled bundles
// (hp-rsly sixteenth slice).
//
// `BundleCache` stores assembled `ExistingCodebaseContextBundle`
// values keyed by the cache key `ComputeCacheKey` produces. The
// planning pipeline calls `Get` before `Build`; on a hit it skips
// the discovery walk + assembly and returns the cached bundle
// directly. On a miss, the pipeline runs Build and stores the
// result via `Put`.
//
// Eviction policy:
//
//   - Per-entry TTL: DefaultCacheTTL = 30 days. Refinement rounds
//     across a Phase-5 conversation reuse the bundle for hours;
//     the 30-day window covers extended planning sessions while
//     still bounding cache size.
//   - Capacity-based LRU: when the cache exceeds MaxCacheEntries,
//     the least-recently-accessed entry is evicted first. Both
//     `Get` (hit) and `Put` count as "access" — a Get on a stored
//     entry refreshes its position.
//   - Schema-version invalidation: Put rejects bundles whose
//     SchemaVersion doesn't match the package SchemaVersion. The
//     §7.1 contract is "cache invalidates on schema bump"; a stored
//     v1 bundle becomes invisible after a v2 deploy because the
//     cache key includes the version (so the same opts hash to
//     a different key). This double-check guards against a
//     rolled-back daemon binary still serving an unflushed cache.
//
// Thread safety: BundleCache is safe for concurrent use. The
// daemon reaches it from the HTTP handler (one goroutine per
// request) and the planning-job worker (one goroutine per refinement
// round); both must be able to Get/Put without external locking.

package bundle

import (
	"errors"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// DefaultCacheTTL is the per-entry expiry. The §7.1 DOD calls out
// "LRU 30d eviction" — Hoopoe's planning pipeline holds a bundle
// across a refinement-round conversation that may span days; the
// TTL should comfortably cover that without running unbounded.
const DefaultCacheTTL = 30 * 24 * time.Hour

// MaxCacheEntries is the LRU capacity. Each entry is a serialized
// bundle (typically <= TokenBudget * 4 bytes ≈ 32-64 KB), so 256
// entries cap the cache footprint at ~16 MB upper bound — within
// the daemon's tracked-allocation envelope.
const MaxCacheEntries = 256

// ErrSchemaVersionMismatch is returned by Put when the bundle's
// SchemaVersion doesn't match the package's SchemaVersion. Forces
// the upstream pipeline to upgrade the bundle before caching.
var ErrSchemaVersionMismatch = errors.New("planning/bundle: bundle SchemaVersion mismatches package SchemaVersion")

// ErrInvalidCacheValue is returned by Put when the bundle is nil
// or has an empty ContentHash (Put requires a hashed bundle so the
// stored content is what subsequent Gets see).
var ErrInvalidCacheValue = errors.New("planning/bundle: invalid bundle for cache (nil or unhashed)")

// CacheStats reports observability counters for the cache. The
// HTTP /v1/system/specs handler can surface these (latency
// histograms live in a separate slice; this struct is the
// counter-only baseline).
type CacheStats struct {
	Hits      int
	Misses    int
	Stores    int
	Evictions int
	Size      int
	Capacity  int
}

// cacheEntry is the internal record. `expires` + `lastAccess` are
// stamped by the wall clock (or the test-injected clock) so TTL +
// LRU evaluation is straightforward.
type cacheEntry struct {
	value      *schemas.ExistingCodebaseContextBundle
	expires    time.Time
	lastAccess time.Time
}

// BundleCache is the cache implementation. The daemon constructs
// one per-process; tests construct one per-test with an injected
// clock for deterministic TTL assertions.
type BundleCache struct {
	mu         sync.Mutex
	entries    map[string]*cacheEntry
	capacity   int
	ttl        time.Duration
	clock      func() time.Time
	stats      CacheStats
}

// NewBundleCache returns a cache with the production defaults
// (DefaultCacheTTL + MaxCacheEntries + wall clock).
func NewBundleCache() *BundleCache {
	return &BundleCache{
		entries:  make(map[string]*cacheEntry),
		capacity: MaxCacheEntries,
		ttl:      DefaultCacheTTL,
		clock:    time.Now,
	}
}

// NewBundleCacheWith returns a cache with custom capacity + TTL +
// clock. Tests use this to assert eviction + TTL behavior with
// small caps + fixed clocks.
func NewBundleCacheWith(capacity int, ttl time.Duration, clock func() time.Time) *BundleCache {
	if capacity <= 0 {
		capacity = MaxCacheEntries
	}
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}
	if clock == nil {
		clock = time.Now
	}
	return &BundleCache{
		entries:  make(map[string]*cacheEntry),
		capacity: capacity,
		ttl:      ttl,
		clock:    clock,
	}
}

// Get returns the cached bundle for `key`, or nil + false when the
// key is absent / expired. A hit updates the entry's last-access
// timestamp so LRU eviction picks the genuinely-stale entries.
func (c *BundleCache) Get(key string) (*schemas.ExistingCodebaseContextBundle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		c.stats.Misses++
		return nil, false
	}

	now := c.clock()
	if now.After(entry.expires) {
		// Expired — evict synchronously and report miss. Avoids the
		// "stale TTL but still served" behavior some caches get
		// wrong.
		delete(c.entries, key)
		c.stats.Misses++
		c.stats.Evictions++
		return nil, false
	}

	entry.lastAccess = now
	c.stats.Hits++
	return entry.value, true
}

// Put stores `bundle` under `key`. The bundle's SchemaVersion is
// verified against the package SchemaVersion; an unhashed bundle
// (empty ContentHash) is rejected because the cache contract is
// "store finalized bundles only."
func (c *BundleCache) Put(key string, bundle *schemas.ExistingCodebaseContextBundle) error {
	if bundle == nil || bundle.ContentHash == "" {
		return ErrInvalidCacheValue
	}
	if int(bundle.SchemaVersion) != SchemaVersion {
		return ErrSchemaVersionMismatch
	}
	if key == "" {
		return errors.New("planning/bundle: empty cache key")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.clock()
	c.entries[key] = &cacheEntry{
		value:      bundle,
		expires:    now.Add(c.ttl),
		lastAccess: now,
	}
	c.stats.Stores++

	// Trim if over capacity. Evict by LRU (lowest lastAccess first).
	for len(c.entries) > c.capacity {
		c.evictLRU()
	}
	return nil
}

// Delete removes the entry for `key`. Returns true if an entry
// existed; the caller can use the bool to log "invalidated 1 entry"
// vs "no-op."
func (c *BundleCache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.entries[key]
	if ok {
		delete(c.entries, key)
		c.stats.Evictions++
	}
	return ok
}

// Stats returns a snapshot of the counters. Safe for concurrent
// use; the returned struct is a copy.
func (c *BundleCache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := c.stats
	stats.Size = len(c.entries)
	stats.Capacity = c.capacity
	return stats
}

// Reset evicts every entry and resets the counters. Used by the
// daemon's `/v1/system/cache/reset` debug endpoint and by tests.
func (c *BundleCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*cacheEntry)
	c.stats = CacheStats{}
}

// evictLRU removes the entry with the oldest `lastAccess`. Caller
// holds the mutex.
func (c *BundleCache) evictLRU() {
	var (
		oldestKey  string
		oldestSeen time.Time
		first      = true
	)
	for k, e := range c.entries {
		if first || e.lastAccess.Before(oldestSeen) {
			oldestKey = k
			oldestSeen = e.lastAccess
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
		c.stats.Evictions++
	}
}
