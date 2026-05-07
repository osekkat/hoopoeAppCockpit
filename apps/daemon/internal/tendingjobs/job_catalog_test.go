package tendingjobs

import (
	"strings"
	"testing"
	"time"
)

func TestCatalogContainsAllSevenSection84Jobs(t *testing.T) {
	t.Parallel()
	want := []JobID{
		JobTendSwarm,
		JobWatchSafetyThresholds,
		JobPushStaleCommits,
		JobSnapshotHealth,
		JobDriftCheck,
		JobReviewReadinessCheck,
		JobOrchestratorChat,
	}
	got := Catalog()
	if len(got) != len(want) {
		t.Fatalf("catalog length = %d, want %d (the §8.4 default tending job set)", len(got), len(want))
	}
	for i, spec := range got {
		if spec.ID != want[i] {
			t.Errorf("catalog[%d].ID = %q, want %q (order must match plan.md §8.4)", i, spec.ID, want[i])
		}
	}
}

func TestEveryJobHasNonEmptyDescriptionAndDelivery(t *testing.T) {
	t.Parallel()
	for _, spec := range Catalog() {
		if spec.Description == "" {
			t.Errorf("%s: Description is empty", spec.ID)
		}
		if spec.Delivery == "" {
			t.Errorf("%s: Delivery is empty (every job must declare a delivery surface)", spec.ID)
		}
	}
}

func TestEveryJobAuditsAlways(t *testing.T) {
	t.Parallel()
	for _, spec := range Catalog() {
		if !spec.AuditAlways {
			t.Errorf("%s: AuditAlways must be true (Guardrail 10: audit fires on every tick)", spec.ID)
		}
	}
}

func TestEveryJobHasAtLeastOneToolset(t *testing.T) {
	t.Parallel()
	for _, spec := range Catalog() {
		if len(spec.Toolsets) == 0 {
			t.Errorf("%s: Toolsets is empty (every job must declare its capability surface)", spec.ID)
		}
	}
}

func TestJobIDsAreKebabCase(t *testing.T) {
	t.Parallel()
	for _, spec := range Catalog() {
		s := string(spec.ID)
		if strings.Contains(s, "_") || strings.ToLower(s) != s {
			t.Errorf("%s: JobID must be lower-kebab-case", spec.ID)
		}
	}
}

func TestJobIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[JobID]bool, 7)
	for _, spec := range Catalog() {
		if seen[spec.ID] {
			t.Errorf("duplicate ID in catalog: %s", spec.ID)
		}
		seen[spec.ID] = true
	}
}

func TestTendSwarmIsFourMinuteInterval(t *testing.T) {
	t.Parallel()
	spec, ok := Lookup(JobTendSwarm)
	if !ok {
		t.Fatal("tend-swarm missing")
	}
	if spec.Trigger != TriggerInterval {
		t.Errorf("Trigger = %s, want interval", spec.Trigger)
	}
	if spec.Interval != 4*time.Minute {
		t.Errorf("Interval = %s, want 4m (per §8.4)", spec.Interval)
	}
	if spec.AlwaysDeterministic {
		t.Errorf("tend-swarm CAN wake the agent (its job is to detect what deterministic layer can't fix)")
	}
}

func TestWatchSafetyThresholdsIsAlwaysDeterministicAndUrgent(t *testing.T) {
	t.Parallel()
	spec, ok := Lookup(JobWatchSafetyThresholds)
	if !ok {
		t.Fatal("watch-safety-thresholds missing")
	}
	if !spec.AlwaysDeterministic {
		t.Errorf("watch-safety-thresholds must be AlwaysDeterministic (no judgment needed per §8.4)")
	}
	if spec.Delivery != DeliveryActivityUrgent {
		t.Errorf("Delivery = %s, want hoopoe_activity_urgent", spec.Delivery)
	}
	if spec.Interval != 30*time.Second {
		t.Errorf("Interval = %s, want 30s", spec.Interval)
	}
}

func TestPushStaleCommitsIsAlwaysDeterministicWithGitWrite(t *testing.T) {
	t.Parallel()
	spec, ok := Lookup(JobPushStaleCommits)
	if !ok {
		t.Fatal("push-stale-commits missing")
	}
	if !spec.AlwaysDeterministic {
		t.Errorf("push-stale-commits must be AlwaysDeterministic (push policy is mechanical per §7.3)")
	}
	if !containsToolset(spec.Toolsets, ToolsetGitWrite) {
		t.Errorf("push-stale-commits must declare git_write toolset")
	}
	if len(spec.Skills) != 0 {
		t.Errorf("push-stale-commits must have no skills (no agent wake)")
	}
}

func TestSnapshotHealthIsCombinedTrigger(t *testing.T) {
	t.Parallel()
	spec, ok := Lookup(JobSnapshotHealth)
	if !ok {
		t.Fatal("snapshot-health missing")
	}
	if spec.Trigger != TriggerCombined {
		t.Errorf("Trigger = %s, want combined (interval + event)", spec.Trigger)
	}
	if spec.Interval != 10*time.Minute {
		t.Errorf("Interval = %s, want 10m", spec.Interval)
	}
	if !containsString(spec.EventTriggers, "vps_push_completed") {
		t.Errorf("EventTriggers missing vps_push_completed: %v", spec.EventTriggers)
	}
	if !spec.AlwaysDeterministic {
		t.Errorf("snapshot-health must be AlwaysDeterministic (measurement is mechanical)")
	}
}

func TestOrchestratorChatIsEventTriggered(t *testing.T) {
	t.Parallel()
	spec, ok := Lookup(JobOrchestratorChat)
	if !ok {
		t.Fatal("orchestrator-chat missing")
	}
	if spec.Trigger != TriggerEvent {
		t.Errorf("Trigger = %s, want event (only fires on user_message_in_activity_panel)", spec.Trigger)
	}
	if !containsString(spec.EventTriggers, "user_message_in_activity_panel") {
		t.Errorf("EventTriggers missing user_message_in_activity_panel: %v", spec.EventTriggers)
	}
	if spec.Interval != 0 {
		t.Errorf("Interval = %s, want 0 (pure event-driven)", spec.Interval)
	}
	if spec.AlwaysDeterministic {
		t.Errorf("orchestrator-chat MUST be able to wake (it's a literal chat agent)")
	}
}

func TestJobsWithSkillFiltersByVibingWithNTM(t *testing.T) {
	t.Parallel()
	jobs := JobsWithSkill(SkillVibingWithNTM)
	wantIDs := []JobID{
		JobTendSwarm,
		JobDriftCheck,
		JobReviewReadinessCheck,
		JobOrchestratorChat,
	}
	if len(jobs) != len(wantIDs) {
		t.Fatalf("expected %d jobs with vibing-with-ntm, got %d", len(wantIDs), len(jobs))
	}
	for _, want := range wantIDs {
		found := false
		for _, spec := range jobs {
			if spec.ID == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected %s in JobsWithSkill(vibing-with-ntm)", want)
		}
	}
}

func TestAlwaysDeterministicJobsListMatchesSpec(t *testing.T) {
	t.Parallel()
	jobs := AlwaysDeterministicJobs()
	want := map[JobID]bool{
		JobWatchSafetyThresholds: true,
		JobPushStaleCommits:      true,
		JobSnapshotHealth:        true,
	}
	if len(jobs) != len(want) {
		t.Fatalf("expected %d AlwaysDeterministic jobs, got %d", len(want), len(jobs))
	}
	for _, spec := range jobs {
		if !want[spec.ID] {
			t.Errorf("unexpected AlwaysDeterministic job: %s", spec.ID)
		}
	}
}

func TestLookupReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := Lookup(JobTendSwarm); !ok {
		t.Errorf("Lookup must return true for a known ID")
	}
	if _, ok := Lookup(JobID("does_not_exist")); ok {
		t.Errorf("Lookup must return false for an unknown ID")
	}
}

func TestEveryIntervalJobHasPositiveInterval(t *testing.T) {
	t.Parallel()
	for _, spec := range Catalog() {
		if spec.Trigger == TriggerEvent {
			continue
		}
		if spec.Interval <= 0 {
			t.Errorf("%s: Interval = %s, must be > 0 for trigger %s", spec.ID, spec.Interval, spec.Trigger)
		}
	}
}

func TestEveryEventOrCombinedJobHasEventTriggers(t *testing.T) {
	t.Parallel()
	for _, spec := range Catalog() {
		if spec.Trigger != TriggerEvent && spec.Trigger != TriggerCombined {
			continue
		}
		if len(spec.EventTriggers) == 0 {
			t.Errorf("%s: trigger %s requires at least one EventTriggers entry", spec.ID, spec.Trigger)
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

func containsToolset(haystack []Toolset, needle Toolset) bool {
	for _, t := range haystack {
		if t == needle {
			return true
		}
	}
	return false
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
