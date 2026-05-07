// budget.go — token-budget enforcement for the bundle assembly
// (hp-rsly eleventh slice).
//
// `EnforceBudget` takes a bundle plus an estimated token ceiling and
// truncates lower-priority sections in a documented order until the
// estimated token count fits. The enforcement is conservative: it
// favors preserving the §7.1-named "high-signal" surfaces (README,
// AGENTS.md, test layout, manifests) over more-easily-reproduced
// surfaces (bead summaries, hotspot rankings — both can be re-fetched
// cheaply).
//
// Truncation order (documented in the hp-rsly DOD):
//
//	existing beads → architecture docs → manifests → AGENTS.md → README
//
// At each step EnforceBudget drops the entire section if removing it
// would close enough of the gap; otherwise it falls through to the
// next priority. Token estimation uses a simple bytes-per-token
// heuristic (TokensPerByteFraction = 0.25, i.e. ~4 bytes/token); the
// per-model serialization slice (still hp-rsly residual) is the layer
// that swaps in a tokenizer-accurate count.
//
// Outputs:
//   - The bundle is mutated in-place: dropped sections become empty
//     slices (never nil — the schema requires the field).
//   - Dropped paths are appended to the caller-provided `excluded`
//     list (the bundle's `Excluded` field). Bead/hotspot drops are
//     synthesized as `beads:<count>` / `hotspots:<count>` markers
//     since those don't have file paths.

package bundle

import (
	"errors"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// TokensPerByteFraction is the heuristic ratio used to estimate token
// counts from byte counts. 0.25 ≈ 4 bytes/token; matches the rough
// convention CLI tools use when an actual tokenizer isn't available.
// The per-model serialization slice swaps this for a tokenizer.
const TokensPerByteFraction = 0.25

// ErrInvalidBudget is returned when EnforceBudget is given a negative
// budget. A budget of 0 means "use the bundle's TokenBudget field"
// (caller convenience).
var ErrInvalidBudget = errors.New("planning/bundle: invalid token budget")

// EstimateTokens returns the bytes-per-token-derived estimate for the
// bundle's content. The estimate covers every variable-size surface
// the assembly subsystem populates: README, AGENTS.md, architecture
// docs, manifests (raw bytes), bead summaries (title bytes), hotspot
// summaries (path bytes). Fixed-size header fields (CommitSha, Ids,
// schemaVersion) are ignored — they're constant overhead not
// affected by budget enforcement.
func EstimateTokens(b *schemas.ExistingCodebaseContextBundle) int {
	if b == nil {
		return 0
	}
	bytes := 0
	if b.Readme != nil {
		bytes += b.Readme.SizeBytes
	}
	if b.AgentsMd != nil {
		bytes += b.AgentsMd.SizeBytes
	}
	for _, d := range b.ArchitectureDocs {
		bytes += d.SizeBytes
	}
	for _, m := range b.PackageManifests {
		bytes += len(m.Raw)
	}
	for _, bead := range b.ExistingBeads {
		bytes += len(bead.Title) + len(bead.Id) + len(bead.IssueType) + len(bead.Status)
	}
	for _, h := range b.HealthHotspots {
		bytes += len(h.Path)
		if h.Language != nil {
			bytes += len(*h.Language)
		}
	}
	return int(float64(bytes) * TokensPerByteFraction)
}

// truncationStep names a documented drop priority. Exposed for tests
// + diagnostics so the order can be asserted without reaching into
// the EnforceBudget switch.
type truncationStep int

const (
	stepDropBeads truncationStep = iota
	stepDropArchitectureDocs
	stepDropManifests
	stepDropAgentsMd
	stepDropReadme
)

// truncationOrder is the documented drop priority. Mutating this
// list is a §7.1 contract change — the openapi schema's
// `tokenEstimate` doc-string mentions the exact sequence, so future
// reorders must update the doc-string + bump SchemaVersion.
var truncationOrder = []truncationStep{
	stepDropBeads,
	stepDropArchitectureDocs,
	stepDropManifests,
	stepDropAgentsMd,
	stepDropReadme,
}

// EnforceBudget truncates `b` in-place until EstimateTokens(b) fits
// `tokenBudget`. Returns the resulting estimate.
//
// `tokenBudget == 0` falls back to the bundle's own TokenBudget
// field. `tokenBudget < 0` is rejected.
//
// The returned `dropped` slice lists synthetic markers for every
// section the truncation discarded. The caller folds these into the
// bundle's `Excluded` field so the §7.1 UI artifact rail can show
// the user what got cut.
//
// The bundle's `TokenEstimate` field is updated so refinement-round
// prompts can render the post-truncation count without re-deriving.
func EnforceBudget(b *schemas.ExistingCodebaseContextBundle, tokenBudget int) (estimate int, dropped []string, err error) {
	if b == nil {
		return 0, nil, errors.New("planning/bundle: nil bundle")
	}
	if tokenBudget < 0 {
		return 0, nil, ErrInvalidBudget
	}
	if tokenBudget == 0 {
		tokenBudget = b.TokenBudget
	}

	dropped = []string{}
	estimate = EstimateTokens(b)

	if tokenBudget <= 0 || estimate <= tokenBudget {
		// No budget, or already fits. Stamp the estimate and exit.
		b.TokenEstimate = estimate
		return estimate, dropped, nil
	}

	for _, step := range truncationOrder {
		switch step {
		case stepDropBeads:
			if len(b.ExistingBeads) > 0 {
				dropped = append(dropped, formatDropMarker("beads", len(b.ExistingBeads)))
				b.ExistingBeads = []schemas.BeadSummary{}
			}
		case stepDropArchitectureDocs:
			for _, d := range b.ArchitectureDocs {
				dropped = append(dropped, d.Path)
			}
			b.ArchitectureDocs = []schemas.FileSnapshot{}
		case stepDropManifests:
			for _, m := range b.PackageManifests {
				dropped = append(dropped, m.Path)
			}
			b.PackageManifests = []schemas.ManifestSnapshot{}
		case stepDropAgentsMd:
			if b.AgentsMd != nil {
				dropped = append(dropped, b.AgentsMd.Path)
				b.AgentsMd = nil
			}
		case stepDropReadme:
			if b.Readme != nil {
				dropped = append(dropped, b.Readme.Path)
				b.Readme = nil
			}
		}
		estimate = EstimateTokens(b)
		if estimate <= tokenBudget {
			break
		}
	}

	b.TokenEstimate = estimate
	return estimate, dropped, nil
}

// formatDropMarker synthesizes a stable string for sections that
// don't have file paths. Exposed for tests so the marker format
// can be asserted without re-deriving from the constant.
func formatDropMarker(section string, count int) string {
	// Format: `<section>:<count>` — short enough to fit in the
	// `Excluded` strings without wrapping in the UI rail, and
	// machine-parseable when the renderer wants to render a
	// "30 beads dropped due to budget" pill instead of a path.
	return section + ":" + intToA(count)
}

// intToA is a tiny base-10 itoa to avoid pulling strconv into this
// file (the package keeps imports minimal — see hash.go,
// cachekey.go for the same convention).
func intToA(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	if neg {
		digits = append([]byte{'-'}, digits...)
	}
	return string(digits)
}
