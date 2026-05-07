// beads.go — bead-summary derivation for the bundle assembly
// (hp-rsly ninth slice).
//
// `SummarizeBeads` transforms raw bead records (the kind a BrAdapter
// returns from `br list --json --limit 0`) into the schema's
// BeadSummary shape, applying:
//
//   - Title truncation to MaxBeadTitleLen.
//   - Total-count cap to MaxBeadSummaries (lower-priority beads
//     dropped first; ties broken by ID for determinism).
//   - DependencyCount derived from blocks + soft links the caller
//     pre-aggregates (the schema's BeadSummary doesn't carry the
//     dep graph itself — just the count for prioritization signals).
//
// This slice does NOT call `br` directly. The caller (BrAdapter
// integration in `apps/daemon/internal/beadflow/` or the bundle
// assembly orchestrator) feeds `RawBead` records derived from
// adapter output. Keeping the layer adapter-agnostic means tests
// don't need a working `br` binary, and the cache layer can replay
// historical bundles without re-querying.

package bundle

import (
	"errors"
	"sort"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// MaxBeadTitleLen mirrors the `BeadSummary.title` schema doc-string:
// "Bead title (truncated to 200 chars)." Truncation applies a `…`
// suffix so the renderer can detect cut-off without a side channel.
const MaxBeadTitleLen = 200

// truncationSuffix is the visual indicator the assembly layer adds
// when a title hits the cap. The 200-char ceiling counts the suffix —
// the underlying body is sliced to (cap - len(suffix)) bytes.
const truncationSuffix = "…"

// MaxBeadSummaries is the default total-count cap. The §7.1 candidate
// prompt allocates a bounded section for "existing beads"; once the
// project has more, the lowest-priority beads drop first. The
// operative reason is token budget — see hp-rsly DOD's "token-budget
// hard cap with documented truncation order" residual.
const MaxBeadSummaries = 100

// ErrInvalidBead is returned when SummarizeBeads is given a record
// missing required fields (Id, IssueType). Status defaults to "open"
// and Priority defaults to 4 (P4) when absent — those are recoverable
// gaps, while a missing Id or IssueType is a real upstream bug.
var ErrInvalidBead = errors.New("planning/bundle: invalid bead record")

// RawBead is the minimal shape SummarizeBeads accepts. The
// BrAdapter (or the bundle assembly orchestrator) maps adapter
// output into this shape; SummarizeBeads converts it to the schema's
// BeadSummary.
//
// DependencyCount is precomputed: the BrAdapter has the full graph
// and can sum (blocks + soft) cheaply; the bundle layer doesn't
// re-walk it.
type RawBead struct {
	Id              string
	Title           string
	IssueType       string
	Priority        int
	Status          string
	DependencyCount int
}

// SummarizeBeads converts raw bead records into []BeadSummary,
// applying truncation + cap rules per the documented contract.
// `maxCount` controls the count cap; pass 0 for the default
// MaxBeadSummaries.
//
// Returns an empty (not nil) slice when `inputs` is empty so the
// bundle's `existingBeads` field always serializes as `[]` rather
// than `null` (the openapi schema requires the field).
//
// Errors:
//   - ErrInvalidBead when any record is missing Id or IssueType.
//
// Determinism: the output order is `priority asc, id asc`. The
// content-addressable cache + ContentHash both rely on stable order.
func SummarizeBeads(inputs []RawBead, maxCount int) ([]schemas.BeadSummary, error) {
	if maxCount <= 0 {
		maxCount = MaxBeadSummaries
	}

	for _, b := range inputs {
		if b.Id == "" || b.IssueType == "" {
			return nil, ErrInvalidBead
		}
	}

	// Sort by (priority asc, id asc). Lower priority numbers are
	// higher impact (P0 > P1 > ...).
	sorted := make([]RawBead, len(inputs))
	copy(sorted, inputs)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		return sorted[i].Id < sorted[j].Id
	})

	if len(sorted) > maxCount {
		sorted = sorted[:maxCount]
	}

	out := make([]schemas.BeadSummary, 0, len(sorted))
	for _, b := range sorted {
		out = append(out, schemas.BeadSummary{
			Id:              b.Id,
			Title:           truncateTitle(b.Title),
			IssueType:       b.IssueType,
			Priority:        defaultPriority(b.Priority),
			Status:          defaultStatus(b.Status),
			DependencyCount: b.DependencyCount,
		})
	}
	return out, nil
}

// truncateTitle clamps `title` to MaxBeadTitleLen, including the
// truncation suffix. Empty titles pass through unchanged so the
// caller can distinguish "no title given" from "title was elided."
func truncateTitle(title string) string {
	if title == "" {
		return ""
	}
	// Operate on rune count, not byte count, so multi-byte chars
	// don't get sliced mid-codepoint.
	runes := []rune(title)
	if len(runes) <= MaxBeadTitleLen {
		return title
	}
	body := string(runes[:MaxBeadTitleLen-len([]rune(truncationSuffix))])
	return body + truncationSuffix
}

func defaultPriority(p int) int {
	if p < 0 || p > 4 {
		// Out-of-range priority is treated as P4 (backlog) — same
		// rule br applies when it ingests an unknown integer.
		return 4
	}
	return p
}

func defaultStatus(s string) string {
	if s == "" {
		return "open"
	}
	return s
}

// IsBeadTitleTruncated reports whether a BeadSummary's title carries
// the truncation suffix. Useful for the §7.1 UI artifact rail's
// "showing first 200 chars of N" indicator without re-deriving the
// suffix string at the call site.
func IsBeadTitleTruncated(summary schemas.BeadSummary) bool {
	if summary.Title == "" {
		return false
	}
	runes := []rune(summary.Title)
	if len(runes) < len([]rune(truncationSuffix)) {
		return false
	}
	tail := string(runes[len(runes)-len([]rune(truncationSuffix)):])
	return tail == truncationSuffix
}
