package review

import (
	"fmt"
	"sort"
	"strings"
)

// hp-v9hc parity checker: a pure function that takes two Findings —
// one from ModeSubscriptionCLI / ModeDeterministicTool ("direct LLM"
// in §7.4.2 parlance, plus deterministic-tool which is a degenerate
// case of the same path) and one from ModeDelegatedAgent — and
// asserts they describe the same underlying issue with schema-
// identical content.
//
// The §7.4.2 parity claim is load-bearing: "Both modes write findings
// into the same finding ledger and use the same prompt templates.
// The mode is an implementation detail; the user sees a consistent
// Review tab." If the two execution paths drift in their finding
// shape, the unified ledger / convergence detector / finding-to-bead
// converter all break silently.
//
// This file is the shape-parity validator the hp-v9hc test bead
// dispatches over. The actual end-to-end fixture (subject under
// review → both modes run → compare) is a follow-up cut.

// ParityViolationKind classifies a single drift between two
// findings.
type ParityViolationKind string

const (
	// ParityFingerprintMismatch: the canonical-form fingerprints
	// differ — the two modes produced findings on different
	// underlying issues. This is the MOST severe drift: it means
	// dedup will fail and the convergence detector will see two
	// rows when it should see one.
	ParityFingerprintMismatch ParityViolationKind = "fingerprint_mismatch"

	// ParitySeverityMismatch: the two modes assigned different
	// severity tiers to the same canonical issue.
	ParitySeverityMismatch ParityViolationKind = "severity_mismatch"

	// ParityCategoryMismatch: the two modes assigned different
	// categories.
	ParityCategoryMismatch ParityViolationKind = "category_mismatch"

	// ParityRuleIDMismatch: the two modes attribute the issue to
	// different rule IDs.
	ParityRuleIDMismatch ParityViolationKind = "rule_id_mismatch"

	// ParityFileAnchorMismatch: the two modes anchor the issue at
	// different file/line locations.
	ParityFileAnchorMismatch ParityViolationKind = "file_anchor_mismatch"

	// ParityMessageDivergence: the message strings are
	// substantively different (beyond cosmetic whitespace) — the
	// findings would not deduplicate on text similarity.
	ParityMessageDivergence ParityViolationKind = "message_divergence"

	// ParityModeMissing: one of the findings has an empty Mode
	// field, so the parity check cannot be performed at all.
	ParityModeMissing ParityViolationKind = "mode_missing"

	// ParitySameMode: both findings carry the same Mode — the
	// parity check requires one finding from each side of the
	// §7.4.2 partition.
	ParitySameMode ParityViolationKind = "same_mode"

	// ParityEvidenceClassMismatch: the kinds of evidence attached
	// (e.g., "log_excerpt" vs "test_failure") differ.
	ParityEvidenceClassMismatch ParityViolationKind = "evidence_class_mismatch"

	// ParitySchemaVersionMismatch: the schemaVersion fields differ,
	// which means the two paths are running on different ledger
	// migrations (a §10.3 schema-version event has not been
	// completed cleanly).
	ParitySchemaVersionMismatch ParityViolationKind = "schema_version_mismatch"
)

// ParityViolation is a single drift the validator found between
// two findings.
type ParityViolation struct {
	Kind     ParityViolationKind `json:"kind"`
	Field    string              `json:"field,omitempty"`
	Detail   string              `json:"detail,omitempty"`
	LeftValue  string            `json:"leftValue,omitempty"`
	RightValue string            `json:"rightValue,omitempty"`
}

// ParityResult is the typed verdict the validator returns. Empty
// Violations means "the two findings are schema-identical for
// parity purposes".
type ParityResult struct {
	OK         bool              `json:"ok"`
	Violations []ParityViolation `json:"violations,omitempty"`
}

// CheckParity compares two findings — `direct` from a direct-LLM
// or deterministic-tool execution, `delegated` from a delegated-
// agent execution — and returns a ParityResult listing every
// drift between them.
//
// The check is order-independent on the input pair: callers may
// pass them in any order; the function inspects each finding's
// Mode field and routes accordingly. A SameMode pair is itself a
// parity violation (the test must compare across the §7.4.2
// partition, not within it).
//
// Mode-specific fields (Mode, the source-tool prefix on Source,
// the actor that recorded the transition) are explicitly excluded
// from the comparison; they are the §7.4.2 "implementation detail"
// the user does not see.
func CheckParity(left, right Finding) ParityResult {
	result := ParityResult{OK: true}

	if left.Mode == "" || right.Mode == "" {
		result.OK = false
		result.Violations = append(result.Violations, ParityViolation{
			Kind:   ParityModeMissing,
			Detail: "both findings must declare ExecutionMode",
		})
		return result
	}
	if left.Mode == right.Mode {
		result.OK = false
		result.Violations = append(result.Violations, ParityViolation{
			Kind:       ParitySameMode,
			Detail:     "parity check requires one finding from each side of §7.4.2",
			LeftValue:  string(left.Mode),
			RightValue: string(right.Mode),
		})
		return result
	}

	if left.SchemaVersion != right.SchemaVersion {
		result.Violations = append(result.Violations, ParityViolation{
			Kind:       ParitySchemaVersionMismatch,
			Field:      "schemaVersion",
			LeftValue:  fmt.Sprintf("%d", left.SchemaVersion),
			RightValue: fmt.Sprintf("%d", right.SchemaVersion),
		})
	}

	if left.Fingerprint != right.Fingerprint {
		result.Violations = append(result.Violations, ParityViolation{
			Kind:       ParityFingerprintMismatch,
			Field:      "fingerprint",
			Detail:     "different fingerprints means the two modes produced findings on different underlying issues",
			LeftValue:  left.Fingerprint,
			RightValue: right.Fingerprint,
		})
	}

	if left.Severity != right.Severity {
		result.Violations = append(result.Violations, ParityViolation{
			Kind:       ParitySeverityMismatch,
			Field:      "severity",
			LeftValue:  string(left.Severity),
			RightValue: string(right.Severity),
		})
	}

	if left.Category != right.Category {
		result.Violations = append(result.Violations, ParityViolation{
			Kind:       ParityCategoryMismatch,
			Field:      "category",
			LeftValue:  left.Category,
			RightValue: right.Category,
		})
	}

	if left.RuleID != right.RuleID {
		result.Violations = append(result.Violations, ParityViolation{
			Kind:       ParityRuleIDMismatch,
			Field:      "ruleId",
			LeftValue:  left.RuleID,
			RightValue: right.RuleID,
		})
	}

	if !sameAnchor(left, right) {
		result.Violations = append(result.Violations, ParityViolation{
			Kind:       ParityFileAnchorMismatch,
			Field:      "filePath/startLine/endLine",
			LeftValue:  anchorString(left),
			RightValue: anchorString(right),
		})
	}

	if !sameMessageSubstance(left.Message, right.Message) {
		result.Violations = append(result.Violations, ParityViolation{
			Kind:       ParityMessageDivergence,
			Field:      "message",
			LeftValue:  left.Message,
			RightValue: right.Message,
		})
	}

	if !sameEvidenceClasses(left.Evidence, right.Evidence) {
		result.Violations = append(result.Violations, ParityViolation{
			Kind:       ParityEvidenceClassMismatch,
			Field:      "evidence[].kind",
			LeftValue:  evidenceClassesString(left.Evidence),
			RightValue: evidenceClassesString(right.Evidence),
		})
	}

	if len(result.Violations) > 0 {
		result.OK = false
	}
	return result
}

func sameAnchor(left, right Finding) bool {
	return left.FilePath == right.FilePath &&
		left.StartLine == right.StartLine &&
		left.EndLine == right.EndLine
}

func anchorString(f Finding) string {
	return fmt.Sprintf("%s:%d-%d", f.FilePath, f.StartLine, f.EndLine)
}

// sameMessageSubstance compares two messages for substantive
// equality. Cosmetic whitespace differences and trailing
// punctuation are tolerated; word-level set differences are not.
//
// We deliberately tolerate small differences (the two modes will
// produce slightly different prose for the same finding) but
// require the message word-set to be a near-superset of the same
// content — different findings on different issues will diverge
// at this layer.
func sameMessageSubstance(a, b string) bool {
	leftWords := normalizeWords(a)
	rightWords := normalizeWords(b)
	if len(leftWords) == 0 && len(rightWords) == 0 {
		return true
	}
	overlap := setOverlap(leftWords, rightWords)
	if overlap == 0 {
		return false
	}
	smaller := len(leftWords)
	if len(rightWords) < smaller {
		smaller = len(rightWords)
	}
	if smaller == 0 {
		return false
	}
	// At least 60% of the smaller message's words must appear in
	// the larger one — captures cosmetic re-wording while flagging
	// genuine semantic divergence.
	return float64(overlap)/float64(smaller) >= 0.6
}

func normalizeWords(s string) map[string]struct{} {
	words := strings.Fields(strings.ToLower(s))
	out := make(map[string]struct{}, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'()[]{}")
		if w == "" {
			continue
		}
		out[w] = struct{}{}
	}
	return out
}

func setOverlap(a, b map[string]struct{}) int {
	count := 0
	for k := range a {
		if _, ok := b[k]; ok {
			count++
		}
	}
	return count
}

// sameEvidenceClasses compares the multiset of `Kind` values
// attached to two findings. Order is irrelevant; multiplicities
// matter.
func sameEvidenceClasses(left, right []EvidenceRef) bool {
	if len(left) != len(right) {
		return false
	}
	leftKinds := evidenceKinds(left)
	rightKinds := evidenceKinds(right)
	if len(leftKinds) != len(rightKinds) {
		return false
	}
	for i := range leftKinds {
		if leftKinds[i] != rightKinds[i] {
			return false
		}
	}
	return true
}

func evidenceKinds(refs []EvidenceRef) []string {
	out := make([]string, len(refs))
	for i, r := range refs {
		out[i] = r.Kind
	}
	sort.Strings(out)
	return out
}

func evidenceClassesString(refs []EvidenceRef) string {
	kinds := evidenceKinds(refs)
	return strings.Join(kinds, ",")
}
