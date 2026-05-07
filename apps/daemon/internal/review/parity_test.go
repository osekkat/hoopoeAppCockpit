package review

import (
	"strings"
	"testing"
	"time"
)

func directFinding() Finding {
	return Finding{
		SchemaVersion: 1,
		ID:            "01J0000000000000000000DIR1",
		ProjectID:     "proj-x",
		RoundID:       "round-7",
		Source:        "model:anthropic:claude-opus-4-7",
		Mode:          ModeSubscriptionCLI,
		Severity:      SeverityHigh,
		Status:        FindingNew,
		Fingerprint:   "fp-deadbeef",
		Category:      "concurrency",
		RuleID:        "rule-42",
		Message:       "Mutex is unlocked twice in error path of HandleRequest",
		FilePath:      "apps/daemon/internal/api/router.go",
		StartLine:     142,
		EndLine:       155,
		Evidence: []EvidenceRef{
			{Kind: "log_excerpt", URI: "audit:1234"},
		},
		CreatedAt: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
	}
}

func delegatedFinding() Finding {
	return Finding{
		SchemaVersion: 1,
		ID:            "01J0000000000000000000DEL1",
		ProjectID:     "proj-x",
		RoundID:       "round-7",
		Source:        "agent:reviewer-3",
		Mode:          ModeDelegatedAgent,
		Severity:      SeverityHigh,
		Status:        FindingNew,
		Fingerprint:   "fp-deadbeef",
		Category:      "concurrency",
		RuleID:        "rule-42",
		Message:       "HandleRequest unlocks the mutex twice in its error path",
		FilePath:      "apps/daemon/internal/api/router.go",
		StartLine:     142,
		EndLine:       155,
		Evidence: []EvidenceRef{
			{Kind: "log_excerpt", URI: "audit:9999"},
		},
		CreatedAt: time.Date(2026, 5, 7, 12, 0, 5, 0, time.UTC),
		UpdatedAt: time.Date(2026, 5, 7, 12, 0, 5, 0, time.UTC),
	}
}

func TestCheckParityHappyPathAcceptsCosmeticReworded(t *testing.T) {
	t.Parallel()
	result := CheckParity(directFinding(), delegatedFinding())
	if !result.OK {
		t.Fatalf("expected parity OK on well-formed pair, got violations: %+v", result.Violations)
	}
	if len(result.Violations) != 0 {
		t.Fatalf("expected zero violations, got %d", len(result.Violations))
	}
}

func TestCheckParityIsOrderIndependent(t *testing.T) {
	t.Parallel()
	a := CheckParity(directFinding(), delegatedFinding())
	b := CheckParity(delegatedFinding(), directFinding())
	if a.OK != b.OK {
		t.Fatalf("parity OK flipped on order swap: %v vs %v", a.OK, b.OK)
	}
}

func TestCheckParityFailsWhenModeMissing(t *testing.T) {
	t.Parallel()
	left := directFinding()
	left.Mode = ""
	result := CheckParity(left, delegatedFinding())
	if result.OK {
		t.Fatal("missing mode must fail parity")
	}
	if !hasViolation(result, ParityModeMissing) {
		t.Errorf("expected ParityModeMissing violation, got %+v", result.Violations)
	}
}

func TestCheckParityFailsOnSameMode(t *testing.T) {
	t.Parallel()
	a := directFinding()
	b := directFinding()
	b.ID = "01J0000000000000000000DIR2"
	result := CheckParity(a, b)
	if result.OK {
		t.Fatal("two findings from the same mode must fail parity")
	}
	if !hasViolation(result, ParitySameMode) {
		t.Errorf("expected ParitySameMode violation, got %+v", result.Violations)
	}
}

func TestCheckParityFlagsFingerprintMismatch(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.Fingerprint = "fp-different"
	result := CheckParity(directFinding(), right)
	if result.OK {
		t.Fatal("fingerprint mismatch must fail parity")
	}
	if !hasViolation(result, ParityFingerprintMismatch) {
		t.Errorf("expected ParityFingerprintMismatch, got %+v", result.Violations)
	}
}

func TestCheckParityFlagsSeverityMismatch(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.Severity = SeverityLow
	result := CheckParity(directFinding(), right)
	if result.OK {
		t.Fatal("severity mismatch must fail parity")
	}
	if !hasViolation(result, ParitySeverityMismatch) {
		t.Errorf("expected ParitySeverityMismatch, got %+v", result.Violations)
	}
}

func TestCheckParityFlagsCategoryMismatch(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.Category = "performance"
	result := CheckParity(directFinding(), right)
	if !hasViolation(result, ParityCategoryMismatch) {
		t.Errorf("expected ParityCategoryMismatch, got %+v", result.Violations)
	}
}

func TestCheckParityFlagsRuleIDMismatch(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.RuleID = "rule-99"
	result := CheckParity(directFinding(), right)
	if !hasViolation(result, ParityRuleIDMismatch) {
		t.Errorf("expected ParityRuleIDMismatch, got %+v", result.Violations)
	}
}

func TestCheckParityFlagsFileAnchorMismatch(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.StartLine = 200
	right.EndLine = 215
	result := CheckParity(directFinding(), right)
	if !hasViolation(result, ParityFileAnchorMismatch) {
		t.Errorf("expected ParityFileAnchorMismatch, got %+v", result.Violations)
	}
}

func TestCheckParityFlagsMessageDivergence(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.Message = "completely unrelated message about another module"
	result := CheckParity(directFinding(), right)
	if !hasViolation(result, ParityMessageDivergence) {
		t.Errorf("expected ParityMessageDivergence, got %+v", result.Violations)
	}
}

func TestCheckParityFlagsEvidenceClassMismatch(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.Evidence = []EvidenceRef{{Kind: "test_failure"}}
	result := CheckParity(directFinding(), right)
	if !hasViolation(result, ParityEvidenceClassMismatch) {
		t.Errorf("expected ParityEvidenceClassMismatch, got %+v", result.Violations)
	}
}

func TestCheckParityFlagsSchemaVersionMismatch(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.SchemaVersion = 2
	result := CheckParity(directFinding(), right)
	if !hasViolation(result, ParitySchemaVersionMismatch) {
		t.Errorf("expected ParitySchemaVersionMismatch, got %+v", result.Violations)
	}
}

func TestCheckParityToleratesCosmeticWordOrder(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.Message = "Twice in its error path, HandleRequest unlocks the mutex"
	result := CheckParity(directFinding(), right)
	if !result.OK {
		t.Fatalf("cosmetic word reorder must NOT trigger MessageDivergence: %+v", result.Violations)
	}
}

func TestCheckParityToleratesEvidenceURIChange(t *testing.T) {
	t.Parallel()
	// Same evidence Kind but different URI — the two modes
	// produced different evidence pointers (different audit row).
	// That is expected; only the Kind must match.
	right := delegatedFinding()
	right.Evidence = []EvidenceRef{
		{Kind: "log_excerpt", URI: "audit:5555"},
	}
	result := CheckParity(directFinding(), right)
	if !result.OK {
		t.Fatalf("different evidence URIs (same Kind) must NOT trigger parity failure: %+v", result.Violations)
	}
}

func TestCheckParityIgnoresIDAndCreatedAtAndSourceAndMode(t *testing.T) {
	t.Parallel()
	// These fields are explicitly mode-specific and must NOT
	// contribute to a parity violation.
	right := delegatedFinding()
	right.ID = "01J0000000000000000000DIFF"
	right.Source = "different_source"
	right.CreatedAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	right.UpdatedAt = right.CreatedAt
	result := CheckParity(directFinding(), right)
	if !result.OK {
		t.Fatalf("ID/Source/CreatedAt/Mode differences must NOT contribute to parity violations: %+v",
			result.Violations)
	}
}

func TestCheckParityReportsAllViolationsNotJustFirst(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.Severity = SeverityLow
	right.Category = "performance"
	right.RuleID = "rule-99"
	result := CheckParity(directFinding(), right)
	if len(result.Violations) < 3 {
		t.Errorf("expected at least 3 violations (severity + category + ruleID), got %d: %+v",
			len(result.Violations), result.Violations)
	}
}

func TestParityResultOKReflectsViolations(t *testing.T) {
	t.Parallel()
	right := delegatedFinding()
	right.RuleID = "rule-99"
	result := CheckParity(directFinding(), right)
	if result.OK {
		t.Errorf("OK must be false when Violations is non-empty")
	}
}

// hasViolation checks for a specific ViolationKind in a result.
func hasViolation(result ParityResult, kind ParityViolationKind) bool {
	for _, v := range result.Violations {
		if v.Kind == kind {
			return true
		}
	}
	return false
}

// Sanity check that a few canonical mode strings round-trip
// through the parity check correctly.
func TestParityAcceptsAllModePairs(t *testing.T) {
	t.Parallel()
	pairs := []struct{ left, right ExecutionMode }{
		{ModeSubscriptionCLI, ModeDelegatedAgent},
		{ModeDeterministicTool, ModeDelegatedAgent},
	}
	for _, pair := range pairs {
		left := directFinding()
		left.Mode = pair.left
		right := delegatedFinding()
		right.Mode = pair.right
		result := CheckParity(left, right)
		if !result.OK {
			t.Errorf("pair (%s, %s) must accept: %+v",
				pair.left, pair.right, result.Violations)
		}
	}
	// And the rejected pair: same-mode.
	left := directFinding()
	right := directFinding()
	right.ID = "second"
	if CheckParity(left, right).OK {
		t.Errorf("same-mode pair must be rejected")
	}
}

// Quick smoke test on the violation-string serialization:
// kinds carry namespaced-looking strings the audit log can grep on.
func TestParityViolationKindsAreStable(t *testing.T) {
	t.Parallel()
	want := []ParityViolationKind{
		ParityFingerprintMismatch, ParitySeverityMismatch,
		ParityCategoryMismatch, ParityRuleIDMismatch,
		ParityFileAnchorMismatch, ParityMessageDivergence,
		ParityModeMissing, ParitySameMode,
		ParityEvidenceClassMismatch, ParitySchemaVersionMismatch,
	}
	for _, kind := range want {
		s := string(kind)
		if s == "" {
			t.Errorf("empty violation kind: %v", kind)
		}
		if strings.Contains(s, " ") {
			t.Errorf("violation kind %q contains whitespace", s)
		}
	}
}
