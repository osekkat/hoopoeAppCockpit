package diagnostics

import (
	"strings"
	"testing"
)

func TestCatalogContainsAllTwelveSection102Actions(t *testing.T) {
	t.Parallel()
	want := []RepairActionID{
		RepairRestartDaemon,
		RepairRestartNTMAgentMail,
		RepairRunACFSDoctor,
		RepairClearLocalClone,
		RepairPruneOrphanClones,
		RepairForceReleaseReservation,
		RepairReplayEvents,
		RepairRebuildBeadReadModel,
		RepairRerunHealthSnapshot,
		RepairVerifySkills,
		RepairShowRawPane,
		RepairRestartOracle,
	}
	got := Catalog()
	if len(got) != len(want) {
		t.Fatalf("catalog length = %d, want %d (the §10.2 table has 12 rows)", len(got), len(want))
	}
	for i, action := range got {
		if action.ID != want[i] {
			t.Errorf("catalog[%d].ID = %q, want %q (order must match plan.md table)", i, action.ID, want[i])
		}
	}
}

func TestEveryEntryHasNonEmptyLabelDescriptionAuditAction(t *testing.T) {
	t.Parallel()
	for _, action := range Catalog() {
		if action.Label == "" {
			t.Errorf("%s: Label is empty", action.ID)
		}
		if action.Description == "" {
			t.Errorf("%s: Description is empty", action.ID)
		}
		if action.AuditAction == "" {
			t.Errorf("%s: AuditAction is empty (Guardrail 10 requires audit on every dispatch)", action.ID)
		}
	}
}

func TestAuditActionMatchesRepairID(t *testing.T) {
	t.Parallel()
	for _, action := range Catalog() {
		if action.AuditAction != string(action.ID) {
			t.Errorf("%s: AuditAction = %q, want = ID = %q", action.ID, action.AuditAction, action.ID)
		}
	}
}

func TestEveryRepairIDIsDiagnosticsNamespaced(t *testing.T) {
	t.Parallel()
	for _, action := range Catalog() {
		if !strings.HasPrefix(string(action.ID), "diagnostics.") {
			t.Errorf("%s: ID is missing the `diagnostics.` namespace prefix", action.ID)
		}
	}
}

func TestForceReleaseReservationIsDestructiveSharedAndPostsMail(t *testing.T) {
	t.Parallel()
	action, ok := Lookup(RepairForceReleaseReservation)
	if !ok {
		t.Fatal("force-release-reservation missing from catalog")
	}
	if action.Safety != SafetyDestructiveShared {
		t.Errorf("safety = %s, want destructive_shared", action.Safety)
	}
	if !action.RequiresConfirmation || !action.RequiresApproval || !action.RequiresReason {
		t.Errorf("force-release-reservation must require confirmation + approval + reason")
	}
	if !action.PostsAgentMail {
		t.Errorf("force-release-reservation must post an Agent Mail notice (per §10.2)")
	}
}

func TestClearLocalCloneIsDestructiveLocalAndDoesNotTouchOrigin(t *testing.T) {
	t.Parallel()
	action, ok := Lookup(RepairClearLocalClone)
	if !ok {
		t.Fatal("clear-local-clone missing from catalog")
	}
	if action.Safety != SafetyDestructiveLocal {
		t.Errorf("safety = %s, want destructive_local (Guardrail 3 forbids touching origin)", action.Safety)
	}
	if action.RequiresApproval {
		t.Errorf("clear-local-clone is destructive_local (single-user impact); approval not required")
	}
	if !strings.Contains(action.Description, "untouched") && !strings.Contains(action.Description, "never touches") {
		t.Errorf("description must explicitly state origin is not touched (Guardrail 3): %q", action.Description)
	}
}

func TestRestartDaemonIsServiceLifecycleAndRequiresApproval(t *testing.T) {
	t.Parallel()
	action, ok := Lookup(RepairRestartDaemon)
	if !ok {
		t.Fatal("restart-daemon missing from catalog")
	}
	if action.Safety != SafetyServiceLifecycle {
		t.Errorf("safety = %s, want service_lifecycle", action.Safety)
	}
	if !action.RequiresApproval {
		t.Errorf("restart-daemon must require approval (multi-agent impact)")
	}
	if action.ImpactWarning == "" {
		t.Errorf("restart-daemon must surface an impact warning")
	}
}

func TestReadOnlyActionsDoNotRequireApproval(t *testing.T) {
	t.Parallel()
	for _, action := range Catalog() {
		if action.Safety != SafetyReadOnly {
			continue
		}
		if action.RequiresApproval {
			t.Errorf("%s: read_only action must not require approval", action.ID)
		}
	}
}

func TestCacheOnlyActionsTouchOnlyHoopoeCache(t *testing.T) {
	t.Parallel()
	cacheOnly := []RepairActionID{
		RepairRebuildBeadReadModel,
		RepairVerifySkills,
	}
	for _, id := range cacheOnly {
		action, ok := Lookup(id)
		if !ok {
			t.Errorf("%s: missing from catalog", id)
			continue
		}
		if action.Safety != SafetyCacheOnly {
			t.Errorf("%s: safety = %s, want cache_only", id, action.Safety)
		}
	}
}

func TestShowRawPaneIsAuditedAndOptIn(t *testing.T) {
	t.Parallel()
	action, ok := Lookup(RepairShowRawPane)
	if !ok {
		t.Fatal("show-raw-pane missing from catalog")
	}
	// Guardrail 12: PTY plumbing exists for forensics; the toggle must
	// be confirmed on every enable, and the audit action must fire.
	if !action.RequiresConfirmation {
		t.Errorf("show-raw-pane must require confirmation (Guardrail 12 audited toggle)")
	}
	if action.CapabilityRequired != "ntm.pane.attach" {
		t.Errorf("show-raw-pane must declare ntm.pane.attach capability, got %q", action.CapabilityRequired)
	}
}

func TestLookupReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := Lookup(RepairRestartDaemon); !ok {
		t.Errorf("Lookup must return true for a known ID")
	}
	if _, ok := Lookup(RepairActionID("diagnostics.does_not_exist")); ok {
		t.Errorf("Lookup must return false for an unknown ID")
	}
}

func TestCatalogIsImmutableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := Catalog()
	b := Catalog()
	if len(a) != len(b) {
		t.Fatalf("catalog length differs between calls: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Errorf("catalog[%d] differs across calls: %q vs %q", i, a[i].ID, b[i].ID)
		}
	}
}

func TestRepairIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[RepairActionID]bool, 12)
	for _, action := range Catalog() {
		if seen[action.ID] {
			t.Errorf("duplicate ID in catalog: %s", action.ID)
		}
		seen[action.ID] = true
	}
}
