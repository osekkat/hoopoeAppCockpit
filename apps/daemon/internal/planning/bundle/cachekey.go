// cachekey.go — content-addressable cache-key derivation for the
// existing-codebase context bundle (hp-rsly seventh slice).
//
// The bundle assembly subsystem is content-addressable: given the same
// inputs, repeated calls to `Builder.Build` must hit the same cache
// entry instead of re-walking the project root. `ComputeCacheKey`
// derives the stable string the cache layer keys on; the future LRU
// (30-day eviction) cache slice plugs into this function without
// re-computing the key from scratch.
//
// The key is constructed from inputs that materially affect the bundle
// shape:
//
//   - SchemaVersion — ensures a schema bump invalidates every prior
//     cached entry (bundle shape is no longer compatible).
//   - ProjectID    — different Hoopoe projects must not collide even
//     when they share a CommitSHA (forks, mirrors).
//   - CommitSHA    — bundles are pinned to a Git commit; advancing the
//     working tree must produce a fresh entry.
//   - TokenBudget  — different budgets truncate sections differently
//     (§5.5 model-context policy), so the assembled bundle differs.
//   - ContentHash  — guards against a buggy upstream slice producing
//     two semantically distinct bundles for otherwise-identical opts.
//
// Volatile fields (`GeneratedAt`) are excluded — the bundle's
// `ContentHash` already strips them (see hash.go).

package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// CacheKeyPrefix is the fixed sentinel every key starts with. Lets a
// cache layer reject keys from a different namespace early instead of
// passing the SHA through to a downstream lookup.
const CacheKeyPrefix = "bundle-cache-"

// cacheKeyHashLen is the hex-encoded SHA-256 length. Exposed as a
// helper so tests + diagnostics can assert the post-prefix portion is
// the expected width without re-deriving from the digest size.
const cacheKeyHashLen = sha256.Size * 2 // 32 bytes → 64 hex chars

// ErrInvalidContentHash is returned when ComputeCacheKey is given an
// empty content hash. Distinguished from ErrInvalidOpts so callers can
// pinpoint the root cause: "your opts are fine; you forgot to compute
// the content hash first."
var ErrInvalidContentHash = errors.New("planning/bundle: contentHash is required")

// ErrInvalidCacheKey is returned by ParseCacheKey when the key doesn't
// match the format ComputeCacheKey produces. Catches the "someone
// passed a stray random string into the cache lookup" bug class.
var ErrInvalidCacheKey = errors.New("planning/bundle: invalid cache key")

// ComputeCacheKey returns the cache key the assembly subsystem stores
// a bundle under. Format: `bundle-cache-<64-hex-chars>`. The hash
// covers SchemaVersion + ProjectID + CommitSHA + TokenBudget +
// ContentHash (delimited by NUL so no field-boundary ambiguity is
// possible).
//
// Errors:
//   - ErrInvalidOpts when BuildOpts misses a required field.
//   - ErrInvalidContentHash when contentHash is empty.
func ComputeCacheKey(opts BuildOpts, contentHash string) (string, error) {
	if err := opts.validate(); err != nil {
		return "", err
	}
	if contentHash == "" {
		return "", ErrInvalidContentHash
	}

	// Build the canonical payload. NUL-separated so a field that
	// happens to contain `=` or `-` can't shift the layout. Order
	// matches the doc-comment list above; never reorder without
	// bumping SchemaVersion (existing cache entries would orphan).
	parts := []string{
		fmt.Sprintf("v%d", SchemaVersion),
		opts.ProjectID,
		opts.CommitSHA,
		fmt.Sprintf("budget=%d", opts.TokenBudget),
		contentHash,
	}
	payload := strings.Join(parts, "\x00")

	digest := sha256.Sum256([]byte(payload))
	return CacheKeyPrefix + hex.EncodeToString(digest[:]), nil
}

// MustComputeCacheKey is the panic-on-error convenience wrapper for
// call sites where a derivation failure would mean upstream slices
// passed in invalid opts (effectively a bug, not a runtime condition).
// Tests + diagnostics use it; the production cache-fetch path calls
// ComputeCacheKey and surfaces the error.
func MustComputeCacheKey(opts BuildOpts, contentHash string) string {
	key, err := ComputeCacheKey(opts, contentHash)
	if err != nil {
		panic(err)
	}
	return key
}

// ParseCacheKey verifies the prefix + hex-width of `key` and returns
// the hex hash portion. Returns ErrInvalidCacheKey on any mismatch
// (wrong prefix, wrong hex length, non-hex characters).
//
// The cache layer uses this when adapting between Hoopoe-namespaced
// keys and a downstream key→value store that expects a clean hex
// digest (e.g., `~/.hoopoe/cache/bundle/<hash>.json`).
func ParseCacheKey(key string) (string, error) {
	if !strings.HasPrefix(key, CacheKeyPrefix) {
		return "", fmt.Errorf("%w: missing %q prefix", ErrInvalidCacheKey, CacheKeyPrefix)
	}
	hash := strings.TrimPrefix(key, CacheKeyPrefix)
	if len(hash) != cacheKeyHashLen {
		return "", fmt.Errorf("%w: hash portion is %d chars, want %d", ErrInvalidCacheKey, len(hash), cacheKeyHashLen)
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return "", fmt.Errorf("%w: hash portion is not hex: %v", ErrInvalidCacheKey, err)
	}
	return hash, nil
}
