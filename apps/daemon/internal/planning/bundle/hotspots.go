// hotspots.go — health-hotspot derivation for the bundle assembly
// (hp-rsly tenth slice).
//
// `RankHotspots` transforms raw hotspot records (the kind a future
// HealthAdapter returns from §7.4.1's health snapshot pipeline) into
// the schema's HotspotSummary shape, applying:
//
//   - Top-N cap at MaxHotspotSummaries (25 per the §7.1 contract).
//   - Stable ranking by compositeScore desc, then path asc for tie
//     determinism.
//   - Path normalization to POSIX so capture-on-Linux / replay-on-
//     macOS produce identical hashes.
//
// This slice does NOT call a real HealthAdapter. The caller (the
// bundle assembly orchestrator) feeds RawHotspot records derived
// from adapter output. Keeping the layer adapter-agnostic mirrors
// the BeadSummary pattern (beads.go) so cache replay works without
// a working `bv`/`coverage`/`semgrep` toolchain.

package bundle

import (
	"errors"
	"sort"
	"strings"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// strictPosixPath unconditionally replaces backslashes with forward
// slashes, regardless of the host OS. The bundle's path contract is
// POSIX everywhere — a fixture authored on Windows must hash to the
// same value as one authored on macOS or Linux. The capture-layer
// `toPosixPath` only normalizes off-Linux because it's protecting a
// just-captured filesystem path; the hotspot layer takes its input
// from upstream adapters that may have been built anywhere.
func strictPosixPath(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// MaxHotspotSummaries is the §7.1 Top-25 cap. The candidate prompt
// allocates a bounded section for code-health hotspots; once the
// project's health pipeline produces more, the lowest-composite-
// score entries drop. The §7.1 doc-string is explicit: "Top 25
// hotspots by composite score from the §7.4.1 health pipeline.
// Capped at 25 by the assembly contract."
const MaxHotspotSummaries = 25

// ErrInvalidHotspot is returned when RankHotspots is given a record
// missing the required Path field. CompositeScore < 0 is also
// rejected because a negative composite suggests the upstream
// pipeline produced garbage; Hoopoe should not silently rank a
// "broken hotspot" higher than valid entries.
var ErrInvalidHotspot = errors.New("planning/bundle: invalid hotspot record")

// RawHotspot is the minimal shape RankHotspots accepts. The
// HealthAdapter (or the bundle assembly orchestrator) maps adapter
// output into this shape; RankHotspots produces the schema's
// HotspotSummary.
//
// Optional fields (Language, ChurnScore, ComplexityScore) mirror the
// schema — the §7.4.1 health pipeline isn't required to produce
// every signal for every file (a Markdown file has no complexity
// metric, for instance).
type RawHotspot struct {
	// Path is the project-root-relative path of the hotspot file.
	// Required.
	Path string

	// CompositeScore is the §7.4.1 composite ranking. Required;
	// must be >= 0.
	CompositeScore float32

	// Language is the detected language token (e.g., "typescript").
	// Optional — empty string maps to a nil pointer in the schema.
	Language string

	// ChurnScore is the recent-churn signal. Optional — IsNaN-style
	// "absent" is encoded as a nil pointer.
	ChurnScore *float32

	// ComplexityScore is the cyclomatic-complexity (or language-
	// analogous) signal. Optional.
	ComplexityScore *float32
}

// RankHotspots converts raw hotspot records into []HotspotSummary,
// applying the Top-25 cap + stable order. `maxCount` controls the
// cap; pass 0 for the default MaxHotspotSummaries.
//
// Returns an empty (not nil) slice when `inputs` is empty so the
// bundle's `healthHotspots` field always serializes as `[]` rather
// than `null` (the openapi schema requires the field).
//
// Errors:
//   - ErrInvalidHotspot when any record has an empty Path or
//     negative CompositeScore.
//
// Determinism: the output order is `compositeScore desc, path asc`.
// The content-addressable cache + ContentHash both rely on stable
// order — re-running RankHotspots on the same inputs must produce
// byte-identical bundles.
func RankHotspots(inputs []RawHotspot, maxCount int) ([]schemas.HotspotSummary, error) {
	if maxCount <= 0 {
		maxCount = MaxHotspotSummaries
	}

	for _, h := range inputs {
		if h.Path == "" {
			return nil, ErrInvalidHotspot
		}
		if h.CompositeScore < 0 {
			return nil, ErrInvalidHotspot
		}
	}

	sorted := make([]RawHotspot, len(inputs))
	copy(sorted, inputs)
	sort.SliceStable(sorted, func(i, j int) bool {
		// Higher composite score first.
		if sorted[i].CompositeScore != sorted[j].CompositeScore {
			return sorted[i].CompositeScore > sorted[j].CompositeScore
		}
		// Tie-break by path ascending (POSIX-normalized for
		// cross-platform stability).
		return strictPosixPath(sorted[i].Path) < strictPosixPath(sorted[j].Path)
	})

	if len(sorted) > maxCount {
		sorted = sorted[:maxCount]
	}

	out := make([]schemas.HotspotSummary, 0, len(sorted))
	for _, h := range sorted {
		summary := schemas.HotspotSummary{
			Path:           strictPosixPath(h.Path),
			CompositeScore: h.CompositeScore,
		}
		if h.Language != "" {
			lang := strings.ToLower(h.Language)
			summary.Language = &lang
		}
		if h.ChurnScore != nil {
			score := *h.ChurnScore
			summary.ChurnScore = &score
		}
		if h.ComplexityScore != nil {
			score := *h.ComplexityScore
			summary.ComplexityScore = &score
		}
		out = append(out, summary)
	}
	return out, nil
}

// HasHotspotLanguage reports whether the summary has a non-empty
// language tag. Useful for the §7.1 UI artifact rail's per-language
// filter without re-parsing the schema field's pointer at every
// render.
func HasHotspotLanguage(summary schemas.HotspotSummary) bool {
	return summary.Language != nil && *summary.Language != ""
}
