package capabilities

import (
	"path/filepath"
	"testing"
)

const repoRoot = "../../../.." // apps/daemon/internal/capabilities → repo root

func fixtureScenario(name string) string {
	return filepath.Join(repoRoot, "packages/fixtures/scenarios", name)
}

func TestLoadHealthyHourFixture(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.LoadFixtureBacked(fixtureScenario("healthy-hour")); err != nil {
		t.Fatalf("load: %v", err)
	}
	snap := r.Snapshot()

	// Phase 0 fixture is tagged phase0-2026-05-02. The bead's DOD requires
	// fixturesVersion >= phase0-2026-05-02.
	if snap.FixturesVersion != "phase0-2026-05-02" {
		t.Errorf("fixturesVersion=%q (expected phase0-2026-05-02)", snap.FixturesVersion)
	}

	// Spot-check known capabilities from the fixture body.
	want := []struct {
		ref    string
		status CapabilityStatus
	}{
		{"git.status.read", StatusOK},
		{"git.push", StatusBlockedByPolicy},
		{"br.issues.read", StatusOK},
		{"bv.robot.triage", StatusOK},
		{"bv.tui", StatusBlockedByPolicy},
		{"agent_mail.messages.read", StatusOK},
		{"caam.account.switch", StatusBlockedByPolicy},
		{"jsm.skill.install", StatusOK},
		{"dcg.verdicts.subscribe", StatusUntested},
	}
	for _, tc := range want {
		got, ok := r.LookupCapabilityStatus(tc.ref)
		if !ok {
			t.Errorf("%s: not found in fixture", tc.ref)
			continue
		}
		if got != tc.status {
			t.Errorf("%s: got %s, want %s", tc.ref, got, tc.status)
		}
	}
}

func TestLoadFixtureCapabilitySemanticsForRateLimitedNoCAAM(t *testing.T) {
	// rate-limited-no-caam scenario must report caam differently from the
	// healthy-hour scenario (capabilities_optional missing for CAAM-driven
	// recovery). The fixture corpus encodes this; verify it loads.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.LoadFixtureBacked(fixtureScenario("rate-limited-no-caam")); err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := r.LookupCapabilityStatus("git.status.read"); !ok {
		t.Errorf("rate-limited-no-caam: missing git.status.read entry")
	}
}

func TestLoadAllPhase0Scenarios(t *testing.T) {
	scenarios := []string{
		"healthy-hour",
		"idle-but-not-stuck",
		"wedged-pane",
		"rate-limited-with-caam",
		"rate-limited-no-caam",
	}
	for _, name := range scenarios {
		t.Run(name, func(t *testing.T) {
			r := newTestRegistry(t, "2026-05-02T23:29:34Z")
			if err := r.LoadFixtureBacked(fixtureScenario(name)); err != nil {
				t.Fatalf("scenario %s: %v", name, err)
			}
			snap := r.Snapshot()
			if err := snap.Validate(); err != nil {
				t.Errorf("scenario %s: snapshot validate: %v", name, err)
			}
			// At minimum every scenario fixture must include git, br, bv,
			// agent_mail, ntm — the core canonical-state tools.
			for _, tool := range []ToolID{ToolGit, ToolBR, ToolBV, ToolAgentMail, ToolNTM} {
				if snap.Tools[tool] == nil {
					t.Errorf("scenario %s: tool %s missing from fixture", name, tool)
				}
			}
		})
	}
}

// TestParserSuccessIsNotCapabilitySuccess is the load-bearing §2.8 contract:
// a fixture that "parses" must not silently mark a feature as available if
// the underlying capability is missing/blocked/degraded. This test pins it
// against a feature with strict requirements.
func TestParserSuccessIsNotCapabilitySuccess(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.LoadFixtureBacked(fixtureScenario("healthy-hour")); err != nil {
		t.Fatal(err)
	}

	// A feature that requires git.push will be blocked-by-policy in the
	// healthy-hour fixture (snapshot scripts never push). Despite the
	// fixture parsing fine and git.status.read being OK, this feature must
	// resolve to BlockedByPolicy.
	if err := r.RegisterFeature(&FeatureCapabilityRequirement{
		FeatureID:            "swarm.bead.push-branch",
		CapabilitiesRequired: []string{"git.status.read", "git.push"},
		DegradedMode: DegradedModeContract{
			IfMissingRequired: BlockJob,
			IfMissingOptional: ContinueWithWarning,
			ActivityBehavior:  ActivityPanelWarning,
		},
	}); err != nil {
		t.Fatal(err)
	}
	dec, err := r.Determine("swarm.bead.push-branch")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Render != RenderBlockedByPolicy {
		t.Fatalf("FIXTURE PARSED BUT FEATURE WAS WRONGLY MARKED AVAILABLE: render=%s", dec.Render)
	}
}
