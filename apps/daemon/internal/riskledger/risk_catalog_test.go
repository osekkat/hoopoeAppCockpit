package riskledger

import (
	"strings"
	"testing"
)

func TestCatalogContainsAllFourteenSection14Risks(t *testing.T) {
	t.Parallel()
	want := []RiskID{
		RiskPTYStreamingFidelity,
		RiskToolOutputDriftBreaksAdapters,
		RiskHoopoeCacheDivergesFromCanonical,
		RiskFirstInstallBrittle,
		RiskSubscriptionRateLimitsExhaust,
		RiskAgentsCompeteForBuildsTests,
		RiskStaleAgentsHoldHostage,
		RiskUnsafeCommandsExposed,
		RiskPlanningQualityWeak,
		RiskUsersTrustSubjectiveScores,
		RiskLaptopSleepReliability,
		RiskCodexShapedAssumptions,
		RiskUpstreamT3CodeDrift,
		RiskPubSubUnboundedLeaks,
	}
	got := Catalog()
	if len(got) != len(want) {
		t.Fatalf("catalog length = %d, want %d (the §14 list has 14 named risks)", len(got), len(want))
	}
	for i, risk := range got {
		if risk.ID != want[i] {
			t.Errorf("catalog[%d].ID = %q, want %q (order must match plan.md §14)", i, risk.ID, want[i])
		}
	}
}

func TestEveryRiskHasNonEmptyTitleDescriptionMitigation(t *testing.T) {
	t.Parallel()
	for _, risk := range Catalog() {
		if risk.Title == "" {
			t.Errorf("%s: Title is empty", risk.ID)
		}
		if risk.Description == "" {
			t.Errorf("%s: Description is empty", risk.ID)
		}
		if risk.Mitigation == "" {
			t.Errorf("%s: Mitigation is empty (every §14 risk must have a stated mitigation)", risk.ID)
		}
	}
}

func TestEveryRiskIDIsRiskNamespaced(t *testing.T) {
	t.Parallel()
	for _, risk := range Catalog() {
		if !strings.HasPrefix(string(risk.ID), "risk.") {
			t.Errorf("%s: ID is missing the `risk.` namespace prefix", risk.ID)
		}
	}
}

func TestNumbersAreSequentialFromOne(t *testing.T) {
	t.Parallel()
	for i, risk := range Catalog() {
		if risk.Number != i+1 {
			t.Errorf("catalog[%d].Number = %d, want %d (§14 ordinals must match list order)", i, risk.Number, i+1)
		}
	}
}

func TestEveryNonProcessReviewRiskHasVerificationFixture(t *testing.T) {
	t.Parallel()
	for _, risk := range Catalog() {
		if risk.VerificationKind == VerificationProcessReview {
			continue
		}
		if risk.VerificationKind == VerificationPhaseAcceptance && risk.VerificationFixture == "" {
			// PhaseAcceptance can defer the fixture to the phase
			// epic's acceptance criteria; it's not required at the
			// ledger row level. Skip without complaint.
			continue
		}
		if risk.VerificationFixture == "" {
			t.Errorf("%s: VerificationFixture is empty for non-process-review risk (kind=%s)",
				risk.ID, risk.VerificationKind)
		}
	}
}

func TestEveryRiskHasOwnerBead(t *testing.T) {
	t.Parallel()
	for _, risk := range Catalog() {
		if risk.OwnerBead == "" {
			t.Errorf("%s: OwnerBead is empty (CI failure messages must route triage)", risk.ID)
		}
	}
}

func TestEveryRiskOwnerBeadIsHpPrefixed(t *testing.T) {
	t.Parallel()
	for _, risk := range Catalog() {
		if !strings.HasPrefix(risk.OwnerBead, "hp-") {
			t.Errorf("%s: OwnerBead %q is not hp- prefixed", risk.ID, risk.OwnerBead)
		}
	}
}

func TestRiskIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[RiskID]bool, 14)
	for _, risk := range Catalog() {
		if seen[risk.ID] {
			t.Errorf("duplicate ID in catalog: %s", risk.ID)
		}
		seen[risk.ID] = true
	}
}

func TestSubscriptionRateLimitsRiskReferencesV6cqAggregator(t *testing.T) {
	t.Parallel()
	risk, ok := Lookup(RiskSubscriptionRateLimitsExhaust)
	if !ok {
		t.Fatal("subscription-rate-limits risk missing")
	}
	if risk.OwnerBead != "hp-v6cq" {
		t.Errorf("OwnerBead = %q, want hp-v6cq (the rate-limit aggregator)", risk.OwnerBead)
	}
}

func TestPubSubUnboundedRiskReferencesIswvSubstrate(t *testing.T) {
	t.Parallel()
	risk, ok := Lookup(RiskPubSubUnboundedLeaks)
	if !ok {
		t.Fatal("pubsub-unbounded risk missing")
	}
	hasIswv := false
	for _, sub := range risk.SubstrateBeads {
		if sub == "hp-iswv" {
			hasIswv = true
		}
	}
	if !hasIswv {
		t.Errorf("pubsub-unbounded risk must reference hp-iswv (antipattern-compliance) in SubstrateBeads")
	}
}

func TestCodexShapedAssumptionsIsLintRuleVerified(t *testing.T) {
	t.Parallel()
	risk, ok := Lookup(RiskCodexShapedAssumptions)
	if !ok {
		t.Fatal("codex-shaped-assumptions risk missing")
	}
	if risk.VerificationKind != VerificationLintRule {
		t.Errorf("VerificationKind = %s, want lint_rule (codex-shape-scrub is the assertion mechanism)", risk.VerificationKind)
	}
}

func TestUpstreamT3CodeDriftIsProcessReview(t *testing.T) {
	t.Parallel()
	risk, ok := Lookup(RiskUpstreamT3CodeDrift)
	if !ok {
		t.Fatal("upstream-t3code-drift risk missing")
	}
	if risk.VerificationKind != VerificationProcessReview {
		t.Errorf("VerificationKind = %s, want process_review (quarterly review is the mitigation)", risk.VerificationKind)
	}
}

func TestUsersTrustSubjectiveScoresIsUIAssertion(t *testing.T) {
	t.Parallel()
	risk, ok := Lookup(RiskUsersTrustSubjectiveScores)
	if !ok {
		t.Fatal("subjective-scores risk missing")
	}
	if risk.VerificationKind != VerificationUIAssertion {
		t.Errorf("VerificationKind = %s, want ui_assertion (every quality score must link evidence + override)",
			risk.VerificationKind)
	}
}

func TestLookupReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := Lookup(RiskFirstInstallBrittle); !ok {
		t.Errorf("Lookup must return true for a known ID")
	}
	if _, ok := Lookup(RiskID("risk.99.does_not_exist")); ok {
		t.Errorf("Lookup must return false for an unknown ID")
	}
}

func TestByVerificationKindFiltersCorrectly(t *testing.T) {
	t.Parallel()
	chaos := ByVerificationKind(VerificationChaos)
	if len(chaos) == 0 {
		t.Fatalf("expected at least one chaos-verified risk")
	}
	for _, risk := range chaos {
		if risk.VerificationKind != VerificationChaos {
			t.Errorf("ByVerificationKind(chaos) returned non-chaos risk: %s (kind=%s)",
				risk.ID, risk.VerificationKind)
		}
	}

	processReview := ByVerificationKind(VerificationProcessReview)
	if len(processReview) != 1 {
		t.Errorf("expected exactly 1 process-review risk (upstream-drift), got %d", len(processReview))
	}
}

func TestCatalogIsImmutableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := Catalog()
	b := Catalog()
	if len(a) != len(b) {
		t.Fatalf("catalog length differs across calls: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Errorf("catalog[%d] differs across calls: %q vs %q", i, a[i].ID, b[i].ID)
		}
	}
}

func TestEveryRiskCarriesDiscoveredIn(t *testing.T) {
	t.Parallel()
	for _, risk := range Catalog() {
		if risk.DiscoveredIn == "" {
			t.Errorf("%s: DiscoveredIn is empty (every catalog row must declare its origin so growth is inspectable)", risk.ID)
		}
	}
}
