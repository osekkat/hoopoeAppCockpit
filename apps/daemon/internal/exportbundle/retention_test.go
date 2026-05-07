package exportbundle

import (
	"testing"
	"time"
)

func TestRetentionCatalogContainsExpectedDomains(t *testing.T) {
	t.Parallel()
	want := []RetentionDomain{
		RetentionAuditLog,
		RetentionTerminalLogs,
		RetentionBuildLogs,
		RetentionBootstrapLogs,
		RetentionModelRawArtifacts,
		RetentionHealthSnapshots,
		RetentionEventReplayLog,
		RetentionSkillInstalls,
	}
	got := RetentionCatalog()
	if len(got) != len(want) {
		t.Fatalf("retention catalog length = %d, want %d (every §10.3 retention class)", len(got), len(want))
	}
	for i, policy := range got {
		if policy.Domain != want[i] {
			t.Errorf("catalog[%d].Domain = %q, want %q", i, policy.Domain, want[i])
		}
	}
}

func TestEveryPolicyHasNonEmptyDescription(t *testing.T) {
	t.Parallel()
	for _, policy := range RetentionCatalog() {
		if policy.Description == "" {
			t.Errorf("%s: Description is empty", policy.Domain)
		}
	}
}

func TestRetentionDomainsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[RetentionDomain]bool)
	for _, policy := range RetentionCatalog() {
		if seen[policy.Domain] {
			t.Errorf("duplicate domain in catalog: %s", policy.Domain)
		}
		seen[policy.Domain] = true
	}
}

func TestAuditLogIsIndefinite(t *testing.T) {
	t.Parallel()
	policy, ok := LookupPolicy(RetentionAuditLog)
	if !ok {
		t.Fatal("audit_log policy missing")
	}
	if policy.Strategy != CompactionIndefinite {
		t.Errorf("audit_log.Strategy = %q, want indefinite (per §10.3 'indefinite unless user exports + prunes')",
			policy.Strategy)
	}
	if policy.PerProjectConfigurable {
		t.Errorf("audit_log retention must NOT be per-project configurable (Guardrail 10 cross-cutting)")
	}
}

func TestTerminalAndBuildLogsHave30DayDefaultTTL(t *testing.T) {
	t.Parallel()
	for _, domain := range []RetentionDomain{RetentionTerminalLogs, RetentionBuildLogs, RetentionBootstrapLogs} {
		policy, ok := LookupPolicy(domain)
		if !ok {
			t.Errorf("%s: policy missing", domain)
			continue
		}
		if policy.Strategy != CompactionDropAfterTTL {
			t.Errorf("%s: Strategy = %q, want drop_after_ttl", domain, policy.Strategy)
		}
		if policy.DefaultTTL != 30*24*time.Hour {
			t.Errorf("%s: DefaultTTL = %s, want 720h (30 days per §10.3)", domain, policy.DefaultTTL)
		}
		if !policy.PerProjectConfigurable {
			t.Errorf("%s: must be per-project configurable per §10.3", domain)
		}
	}
}

func TestModelRawArtifactsAre30DayPrivate(t *testing.T) {
	t.Parallel()
	policy, ok := LookupPolicy(RetentionModelRawArtifacts)
	if !ok {
		t.Fatal("model_raw_artifacts policy missing")
	}
	if policy.DefaultTTL != 30*24*time.Hour {
		t.Errorf("DefaultTTL = %s, want 720h (30 days private retention default per §10.3)", policy.DefaultTTL)
	}
}

func TestHealthSnapshotsUseKeepLastN(t *testing.T) {
	t.Parallel()
	policy, ok := LookupPolicy(RetentionHealthSnapshots)
	if !ok {
		t.Fatal("health_snapshots policy missing")
	}
	if policy.Strategy != CompactionKeepLastN {
		t.Errorf("Strategy = %q, want keep_last_n (per §10.3 'keep last N + compacted trend history')", policy.Strategy)
	}
	if policy.DefaultKeepN < 1 {
		t.Errorf("DefaultKeepN = %d, must be ≥ 1", policy.DefaultKeepN)
	}
}

func TestEventReplayLogUsesRollingSnapshot(t *testing.T) {
	t.Parallel()
	policy, ok := LookupPolicy(RetentionEventReplayLog)
	if !ok {
		t.Fatal("event_replay_log policy missing")
	}
	if policy.Strategy != CompactionRollingSnapshot {
		t.Errorf("Strategy = %q, want rolling_snapshot (per §10.3 'enough to cover recent reconnects, then compact into snapshots')",
			policy.Strategy)
	}
}

func TestSkillInstallsArePinnedNotTTL(t *testing.T) {
	t.Parallel()
	policy, ok := LookupPolicy(RetentionSkillInstalls)
	if !ok {
		t.Fatal("skill_installs policy missing")
	}
	if policy.Strategy != CompactionPinned {
		t.Errorf("Strategy = %q, want pinned (per §10.3 'pinned via .hoopoe/skills.lock.json')",
			policy.Strategy)
	}
	if policy.DefaultTTL != 0 {
		t.Errorf("DefaultTTL = %s, want 0 (pinned strategy has no TTL)", policy.DefaultTTL)
	}
}

func TestLookupPolicyReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := LookupPolicy(RetentionAuditLog); !ok {
		t.Errorf("LookupPolicy must return true for a known domain")
	}
	if _, ok := LookupPolicy(RetentionDomain("does_not_exist")); ok {
		t.Errorf("LookupPolicy must return false for an unknown domain")
	}
}

func TestEveryDropAfterTTLPolicyHasPositiveTTL(t *testing.T) {
	t.Parallel()
	for _, policy := range RetentionCatalog() {
		if policy.Strategy != CompactionDropAfterTTL {
			continue
		}
		if policy.DefaultTTL <= 0 {
			t.Errorf("%s: drop_after_ttl strategy must have positive DefaultTTL, got %s",
				policy.Domain, policy.DefaultTTL)
		}
	}
}

func TestEveryKeepLastNPolicyHasPositiveN(t *testing.T) {
	t.Parallel()
	for _, policy := range RetentionCatalog() {
		if policy.Strategy != CompactionKeepLastN {
			continue
		}
		if policy.DefaultKeepN <= 0 {
			t.Errorf("%s: keep_last_n strategy must have positive DefaultKeepN, got %d",
				policy.Domain, policy.DefaultKeepN)
		}
	}
}

func TestRetentionCatalogIsImmutableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := RetentionCatalog()
	b := RetentionCatalog()
	if len(a) != len(b) {
		t.Fatalf("retention catalog length differs across calls: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Domain != b[i].Domain {
			t.Errorf("catalog[%d] differs across calls: %q vs %q", i, a[i].Domain, b[i].Domain)
		}
	}
}
