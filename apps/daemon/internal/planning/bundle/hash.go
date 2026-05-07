// hash.go — ContentHash computation for the existing-codebase context
// bundle (hp-rsly third slice).
//
// The bundle's `ContentHash` field is the SHA-256 of a canonicalized
// JSON encoding so that:
//
//   1. Two builds that produce semantically identical bundles produce
//      byte-identical hashes (drives the content-addressable cache and
//      refinement-round bundle-reuse invariant).
//   2. The hash is independent of `GeneratedAt` and `ContentHash`
//      itself — those are populated AFTER hashing.
//   3. The hash is deterministic across Go compiler versions: every
//      key is sorted, every slice is in source order, and there are no
//      pointer-formatting surprises.
//
// The actual assembly subsystem (hp-rsly residual follow-ups) calls
// `ComputeContentHash` after collecting the discovery walk + adapter
// integration outputs and writes the result into the bundle's
// `ContentHash` field. Diagnostics endpoints can re-hash a returned
// bundle and assert equality with the stored field as a tamper /
// drift check.

package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// ComputeContentHash returns the canonical SHA-256 of the bundle, as
// a 64-char lowercase-hex string. The bundle's own `GeneratedAt` and
// `ContentHash` fields are excluded from the hash domain so a freshly-
// stamped bundle still hashes to the same value as its content-only
// twin.
//
// Returns an error only when JSON encoding fails (which should not
// happen for the schema-pinned shape — the encoder erroring is a real
// bug to surface, not silently swallow).
func ComputeContentHash(b schemas.ExistingCodebaseContextBundle) (string, error) {
	// Strip volatile fields before hashing. Operating on a copy means
	// the caller's bundle is untouched.
	stripped := b
	stripped.GeneratedAt = time.Time{}
	stripped.ContentHash = ""

	// Encode with a stable JSON encoder. encoding/json sorts map keys
	// alphabetically by default, but it serializes struct fields in
	// declaration order. We want declaration order (the openapi.yaml
	// schema is the source of truth for field order); switching to a
	// map would lose the field-name nesting hint that downstream tools
	// rely on.
	encoded, err := json.Marshal(stripped)
	if err != nil {
		return "", fmt.Errorf("planning/bundle: marshal for hash: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// MustComputeContentHash is the panic-on-error convenience wrapper for
// call sites where a marshal failure would mean the schema struct
// itself drifted (effectively unreachable in correct code). Tests use
// it; production code should call ComputeContentHash and surface the
// error.
func MustComputeContentHash(b schemas.ExistingCodebaseContextBundle) string {
	hash, err := ComputeContentHash(b)
	if err != nil {
		panic(err)
	}
	return hash
}
