package tendingeval

import (
	"strings"
	"testing"
)

func TestCatalogContainsAllTwelveSection88Fixtures(t *testing.T) {
	t.Parallel()
	want := []FixtureID{
		FixtureHealthyHour,
		FixtureIdleButNotStuck,
		FixtureGenuinelyWedgedPane,
		FixtureRateLimitedWithCAAM,
		FixtureRateLimitedNoCAAM,
		FixtureStaleReservation,
		FixtureCommitBurst,
		FixtureBudgetBreach,
		FixtureSkillDrift,
		FixtureMissingTool,
		FixturePostconditionFailure,
		FixtureActionArbitration,
	}
	got := Catalog()
	if len(got) != len(want) {
		t.Fatalf("catalog length = %d, want %d (the §8.8 fixture list has 12 entries)", len(got), len(want))
	}
	for i, fixture := range got {
		if fixture.ID != want[i] {
			t.Errorf("catalog[%d].ID = %q, want %q (order must match plan.md §8.8)", i, fixture.ID, want[i])
		}
	}
}

func TestEveryFixtureHasNonEmptyTitleDescription(t *testing.T) {
	t.Parallel()
	for _, fixture := range Catalog() {
		if fixture.Title == "" {
			t.Errorf("%s: Title is empty", fixture.ID)
		}
		if fixture.Description == "" {
			t.Errorf("%s: Description is empty", fixture.ID)
		}
	}
}

func TestEveryFixtureIDIsTendingEvalNamespaced(t *testing.T) {
	t.Parallel()
	for _, fixture := range Catalog() {
		if !strings.HasPrefix(string(fixture.ID), "tending_eval.") {
			t.Errorf("%s: ID is missing the `tending_eval.` namespace prefix", fixture.ID)
		}
	}
}

func TestNumbersAreSequentialFromOne(t *testing.T) {
	t.Parallel()
	for i, fixture := range Catalog() {
		if fixture.Number != i+1 {
			t.Errorf("catalog[%d].Number = %d, want %d (§8.8 ordinals must match list order)", i, fixture.Number, i+1)
		}
	}
}

func TestFixtureIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[FixtureID]bool, 12)
	for _, fixture := range Catalog() {
		if seen[fixture.ID] {
			t.Errorf("duplicate ID in catalog: %s", fixture.ID)
		}
		seen[fixture.ID] = true
	}
}

func TestHealthyHourEnforcesZeroCostInvariant(t *testing.T) {
	t.Parallel()
	fixture, ok := Lookup(FixtureHealthyHour)
	if !ok {
		t.Fatal("healthy_hour fixture missing")
	}
	if fixture.HealthClass != HealthClassHealthy {
		t.Errorf("HealthClass = %s, want healthy", fixture.HealthClass)
	}
	if fixture.ExpectedWake != WakeNeverFires {
		t.Errorf("ExpectedWake = %s, want never_fires (§8.6 zero-LLM-cost invariant)", fixture.ExpectedWake)
	}
	if fixture.MaxTokenBudget != 0 {
		t.Errorf("MaxTokenBudget = %d, want 0 (healthy hour must produce no LLM cost)", fixture.MaxTokenBudget)
	}
}

func TestIdleButNotStuckCapsAtSoftClassification(t *testing.T) {
	t.Parallel()
	fixture, ok := Lookup(FixtureIdleButNotStuck)
	if !ok {
		t.Fatal("idle_but_not_stuck fixture missing")
	}
	// Long-no-output alone must NOT escalate (false-positive
	// protection per §1.4 / §8.7).
	if fixture.ExpectedWake != WakeNeverFires {
		t.Errorf("ExpectedWake = %s, want never_fires (long-no-output alone must not escalate)", fixture.ExpectedWake)
	}
	if fixture.MaxTokenBudget != 0 {
		t.Errorf("MaxTokenBudget = %d, want 0", fixture.MaxTokenBudget)
	}
}

func TestRateLimitedWithCAAMProducesSwitchAccount(t *testing.T) {
	t.Parallel()
	fixture, ok := Lookup(FixtureRateLimitedWithCAAM)
	if !ok {
		t.Fatal("rate_limited_with_caam fixture missing")
	}
	if !contains(fixture.ExpectedActionKinds, "caam.switch_account") {
		t.Errorf("expected caam.switch_account in ExpectedActionKinds, got %v", fixture.ExpectedActionKinds)
	}
}

func TestRateLimitedNoCAAMProducesFallbackPath(t *testing.T) {
	t.Parallel()
	fixture, ok := Lookup(FixtureRateLimitedNoCAAM)
	if !ok {
		t.Fatal("rate_limited_no_caam fixture missing")
	}
	if fixture.HealthClass != HealthClassDegraded {
		t.Errorf("HealthClass = %s, want degraded", fixture.HealthClass)
	}
	if !contains(fixture.ExpectedActionKinds, "casr.resume_session") {
		t.Errorf("expected casr.resume_session in fallback path, got %v", fixture.ExpectedActionKinds)
	}
	if !contains(fixture.ExpectedActionKinds, "swarm.pause_agent") {
		t.Errorf("expected swarm.pause_agent in fallback path, got %v", fixture.ExpectedActionKinds)
	}
}

func TestStaleReservationFiresAgentMailNotice(t *testing.T) {
	t.Parallel()
	fixture, ok := Lookup(FixtureStaleReservation)
	if !ok {
		t.Fatal("stale_reservation fixture missing")
	}
	if !contains(fixture.ExpectedActionKinds, "agent_mail.force_release_reservation") {
		t.Errorf("expected agent_mail.force_release_reservation in actions, got %v", fixture.ExpectedActionKinds)
	}
	if !fixture.ExpectedApprovals {
		t.Errorf("force-release-reservation must require approval (destructive_shared per hp-6d7)")
	}
}

func TestActionArbitrationCoversTwoConflictingPlans(t *testing.T) {
	t.Parallel()
	fixture, ok := Lookup(FixtureActionArbitration)
	if !ok {
		t.Fatal("action_arbitration fixture missing")
	}
	if fixture.HealthClass != HealthClassEdge {
		t.Errorf("HealthClass = %s, want edge", fixture.HealthClass)
	}
	if len(fixture.ExpectedActionKinds) < 2 {
		t.Errorf("arbitration fixture must declare ≥2 conflicting actions, got %v", fixture.ExpectedActionKinds)
	}
}

func TestCommitBurstStaysHealthy(t *testing.T) {
	t.Parallel()
	fixture, ok := Lookup(FixtureCommitBurst)
	if !ok {
		t.Fatal("commit_burst fixture missing")
	}
	if fixture.HealthClass != HealthClassHealthy {
		t.Errorf("HealthClass = %s, want healthy (push-stale-commits is deterministic, no LLM wake)", fixture.HealthClass)
	}
	if fixture.MaxTokenBudget != 0 {
		t.Errorf("MaxTokenBudget = %d, want 0 (no LLM wake on commit burst)", fixture.MaxTokenBudget)
	}
}

func TestMissingToolStaysWakeNeverFires(t *testing.T) {
	t.Parallel()
	fixture, ok := Lookup(FixtureMissingTool)
	if !ok {
		t.Fatal("missing_tool fixture missing")
	}
	// Per §8.7 / capability degradation: missing tool surfaces a
	// degraded-capability warning; agent does NOT wake.
	if fixture.ExpectedWake != WakeNeverFires {
		t.Errorf("ExpectedWake = %s, want never_fires (capability degradation does not wake the agent)", fixture.ExpectedWake)
	}
}

func TestByHealthClassFiltersCorrectly(t *testing.T) {
	t.Parallel()
	healthy := ByHealthClass(HealthClassHealthy)
	if len(healthy) == 0 {
		t.Fatalf("expected ≥1 healthy fixture (the §8.6 invariant gate)")
	}
	for _, fixture := range healthy {
		if fixture.HealthClass != HealthClassHealthy {
			t.Errorf("ByHealthClass(healthy) returned %s with class %s",
				fixture.ID, fixture.HealthClass)
		}
	}
	unhealthy := ByHealthClass(HealthClassUnhealthy)
	if len(unhealthy) == 0 {
		t.Fatalf("expected ≥1 unhealthy fixture (action paths must have coverage)")
	}
	edge := ByHealthClass(HealthClassEdge)
	if len(edge) != 1 {
		t.Errorf("expected exactly 1 edge fixture (action_arbitration), got %d", len(edge))
	}
}

func TestLookupReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := Lookup(FixtureHealthyHour); !ok {
		t.Errorf("Lookup must return true for a known ID")
	}
	if _, ok := Lookup(FixtureID("tending_eval.99.does_not_exist")); ok {
		t.Errorf("Lookup must return false for an unknown ID")
	}
}

func TestEveryFixtureMaxTokenBudgetIsNonNegative(t *testing.T) {
	t.Parallel()
	for _, fixture := range Catalog() {
		if fixture.MaxTokenBudget < 0 {
			t.Errorf("%s: MaxTokenBudget = %d, must be ≥ 0", fixture.ID, fixture.MaxTokenBudget)
		}
	}
}

func TestEveryFixtureMaxActivityEntriesIsNonNegative(t *testing.T) {
	t.Parallel()
	for _, fixture := range Catalog() {
		if fixture.MaxActivityEntries < 0 {
			t.Errorf("%s: MaxActivityEntries = %d, must be ≥ 0", fixture.ID, fixture.MaxActivityEntries)
		}
	}
}

func TestHealthyFixturesCarrySmallActivityCaps(t *testing.T) {
	t.Parallel()
	// Healthy fixtures should produce minimal Activity-panel
	// noise (§8.6 invariant). A healthy fixture allowing > 5
	// Activity entries is suspicious.
	for _, fixture := range Catalog() {
		if fixture.HealthClass == HealthClassHealthy && fixture.MaxActivityEntries > 5 {
			t.Errorf("%s: MaxActivityEntries = %d, healthy fixtures should cap at ≤5 (§8.6 noise invariant)",
				fixture.ID, fixture.MaxActivityEntries)
		}
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

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
