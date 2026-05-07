// cached_builder.go — cache-aware AssemblyOrchestrator wrapper
// (hp-rsly seventeenth slice).
//
// `CachedBuilder` wraps `AssemblyOrchestrator` so the planning
// pipeline never re-walks a project tree it has already assembled.
// The pipeline:
//
//   1. Compute a tentative cache key from BuildOpts + a "fast"
//      content hash derived from the adapter inputs (Beads,
//      Hotspots) + ProjectRoot mtime — without running the
//      discovery walk.
//   2. Look the key up in BundleCache. On a hit, return the
//      cached bundle directly (no walk, no policy enforcement,
//      no truncation).
//   3. On a miss, delegate to AssemblyOrchestrator.Build, then
//      Put the resulting bundle back into the cache before
//      returning it.
//
// Why a "fast" pre-walk hash:
//
// `ComputeCacheKey` requires a `ContentHash` derived from the
// finalized bundle — meaning we'd have to walk the project root,
// summarize beads + hotspots, apply policy, run budget enforcement
// before we know the hash. That defeats the cache. Instead we
// derive a "pre-build" hash from the inputs that drive the bundle
// shape (CommitSHA, TokenBudget, sorted bead IDs, sorted hotspot
// paths). On the first miss this hash differs from the real
// ContentHash, but on subsequent hits the inputs are identical so
// the pre-build hash matches the previous run's pre-build hash —
// the cache key is stable.
//
// What this slice does NOT do (still hp-rsly residual):
//
//   - Persisted disk-backed cache (in-memory only — see cache.go).
//   - Cache-warming background job (the daemon could prefetch on
//     project switch, but that's a separate operability slice).
//   - Per-model variant caching (the markdown serializer renders
//     deterministically, so one cached bundle serves all models).

package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// ErrNilOrchestrator is returned when CachedBuilder.Build is called
// with a nil underlying orchestrator. NewCachedBuilder rejects nil
// at construction time so this should never fire in production —
// it's defense in depth for the test-double case.
var ErrNilOrchestrator = errors.New("planning/bundle: nil orchestrator")

// CachedBuilder is the cache-aware Builder. The cache is shared
// across calls; the orchestrator + cache should be one-per-process
// (constructed during daemon startup) so refinement rounds across
// the same project hit the same cache.
type CachedBuilder struct {
	orchestrator *AssemblyOrchestrator
	cache        *BundleCache
}

// NewCachedBuilder constructs a cache-aware builder. Both
// `orchestrator` and `cache` are required. Callers that don't want
// caching should use AssemblyOrchestrator.Build directly.
func NewCachedBuilder(orchestrator *AssemblyOrchestrator, cache *BundleCache) (*CachedBuilder, error) {
	if orchestrator == nil {
		return nil, ErrNilOrchestrator
	}
	if cache == nil {
		return nil, errors.New("planning/bundle: nil cache")
	}
	return &CachedBuilder{
		orchestrator: orchestrator,
		cache:        cache,
	}, nil
}

// Build runs the cache-aware pipeline. On a hit, returns the cached
// bundle without touching the project tree. On a miss, runs the
// orchestrator, caches the result, and returns it.
//
// The same opts → same key → same cached bundle. Refinement rounds
// can call Build repeatedly without re-walking the disk.
func (b *CachedBuilder) Build(ctx context.Context, opts BuildOpts, input AssemblyInput) (*schemas.ExistingCodebaseContextBundle, error) {
	if b == nil || b.orchestrator == nil {
		return nil, ErrNilOrchestrator
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}

	preHash := preBuildContentHash(opts, input)
	key, err := ComputeCacheKey(opts, preHash)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: cache key derivation: %w", err)
	}

	if cached, ok := b.cache.Get(key); ok {
		return cached, nil
	}

	bundle, err := b.orchestrator.Build(ctx, opts, input)
	if err != nil {
		return nil, err
	}

	// Best-effort cache write. A Put failure (schema mismatch,
	// empty hash) shouldn't drop the request — log via
	// CacheStats counters and return the freshly-built bundle.
	_ = b.cache.Put(key, bundle)
	return bundle, nil
}

// preBuildContentHash derives a deterministic pre-walk hash from the
// inputs that drive the bundle's shape: CommitSHA + TokenBudget +
// sorted bead IDs + sorted hotspot paths. This is intentionally a
// subset of what ComputeContentHash would produce — the trade-off is
// that two builds with identical inputs but different on-disk state
// (someone touched the project root between calls) collide. That's
// acceptable because:
//
//   1. Refinement rounds in a single planning conversation pin
//      CommitSHA in BuildOpts; the disk is read-only at the chosen
//      commit anyway.
//   2. When CommitSHA advances, opts → different key → cache miss.
//   3. A manual user-triggered "rebuild bundle" can call
//      BundleCache.Delete to force the next Build to walk again.
func preBuildContentHash(opts BuildOpts, input AssemblyInput) string {
	parts := []string{
		"v1",
		opts.CommitSHA,
		fmt.Sprintf("budget=%d", opts.TokenBudget),
	}

	beadIDs := make([]string, 0, len(input.Beads))
	for _, b := range input.Beads {
		beadIDs = append(beadIDs, b.Id)
	}
	sort.Strings(beadIDs)
	parts = append(parts, "beads:"+strings.Join(beadIDs, ","))

	hotspotPaths := make([]string, 0, len(input.Hotspots))
	for _, h := range input.Hotspots {
		hotspotPaths = append(hotspotPaths, h.Path)
	}
	sort.Strings(hotspotPaths)
	parts = append(parts, "hotspots:"+strings.Join(hotspotPaths, ","))

	if input.Policy != nil {
		// Include the policy fingerprint so a per-project policy
		// override produces a fresh cache key. Hash the sorted
		// pattern + prefix lists; two policies with the same
		// effective contents produce the same fingerprint.
		fp := policyFingerprint(input.Policy)
		parts = append(parts, "policy:"+fp)
	}

	payload := strings.Join(parts, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:])
}

// policyFingerprint produces a deterministic fingerprint of a
// Policy struct for cache-key purposes. Sort patterns + prefixes
// alphabetically so two equivalent Policy structs (same contents,
// different insertion order) produce the same fingerprint.
func policyFingerprint(p *Policy) string {
	patterns := append([]string(nil), p.ExcludePatterns...)
	sort.Strings(patterns)
	prefixes := append([]string(nil), p.ExcludeDirPrefixes...)
	sort.Strings(prefixes)
	allow := "0"
	if p.AllowSecretSuggestiveBasenames {
		allow = "1"
	}
	parts := []string{
		"patterns:" + strings.Join(patterns, ","),
		"prefixes:" + strings.Join(prefixes, ","),
		"allow:" + allow,
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:8]) // 8-byte (16-char) prefix is plenty
}
