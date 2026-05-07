package releasesmoke

import (
	"strings"
	"testing"
)

func TestCatalogContainsAllTenSection184Checks(t *testing.T) {
	t.Parallel()
	want := []CheckID{
		CheckSignedAppLaunches,
		CheckAutoUpdateMockServer,
		CheckSettingsKeybindingsMigrate,
		CheckDaemonUpgradeBacksUpDB,
		CheckLocalCloneClearAndRebuild,
		CheckEventReplayAfterSleep,
		CheckProcessManagerCancelNoOrphan,
		CheckHealthWorktreeCleanup,
		CheckAuditRedactionAllSecrets,
		CheckExportRestoreRoundTrip,
	}
	got := Catalog()
	if len(got) != len(want) {
		t.Fatalf("catalog length = %d, want %d (the §18.4 table has 10 rows)", len(got), len(want))
	}
	for i, check := range got {
		if check.ID != want[i] {
			t.Errorf("catalog[%d].ID = %q, want %q (order must match plan.md §18.4)", i, check.ID, want[i])
		}
	}
}

func TestEveryCheckHasNonEmptyTitleDescriptionEvidence(t *testing.T) {
	t.Parallel()
	for _, check := range Catalog() {
		if check.Title == "" {
			t.Errorf("%s: Title is empty", check.ID)
		}
		if check.Description == "" {
			t.Errorf("%s: Description is empty", check.ID)
		}
		if len(check.EvidenceArtifacts) == 0 {
			t.Errorf("%s: EvidenceArtifacts is empty (per §18.4 every check must produce evidence)", check.ID)
		}
	}
}

func TestEveryCheckIsReleaseNamespaced(t *testing.T) {
	t.Parallel()
	for _, check := range Catalog() {
		if !strings.HasPrefix(string(check.ID), "release.") {
			t.Errorf("%s: ID is missing the `release.` namespace prefix", check.ID)
		}
	}
}

func TestNumbersAreSequentialFromOne(t *testing.T) {
	t.Parallel()
	for i, check := range Catalog() {
		if check.Number != i+1 {
			t.Errorf("catalog[%d].Number = %d, want %d (§18.4 ordinals must match list order)", i, check.Number, i+1)
		}
	}
}

func TestEveryCheckIsBlockingPerSection184(t *testing.T) {
	t.Parallel()
	for _, check := range Catalog() {
		if check.Severity != SeverityBlocking {
			t.Errorf("%s: severity = %s, want blocking (§18.4 says any single failure blocks release)", check.ID, check.Severity)
		}
	}
}

func TestEventReplayCarriesP95SLO(t *testing.T) {
	t.Parallel()
	check, ok := Lookup(CheckEventReplayAfterSleep)
	if !ok {
		t.Fatal("event-replay check missing from catalog")
	}
	if check.SLO.Metric != "events.replay.10min.p95" {
		t.Errorf("SLO.Metric = %q, want events.replay.10min.p95 (per §10.5)", check.SLO.Metric)
	}
	if check.SLO.MaxValue != 5.0 {
		t.Errorf("SLO.MaxValue = %f, want 5.0 (per §18.4 description)", check.SLO.MaxValue)
	}
	if check.SLO.Unit != "seconds" {
		t.Errorf("SLO.Unit = %q, want seconds", check.SLO.Unit)
	}
}

func TestProcessManagerNoOrphansCarriesSLO(t *testing.T) {
	t.Parallel()
	check, ok := Lookup(CheckProcessManagerCancelNoOrphan)
	if !ok {
		t.Fatal("process-manager check missing from catalog")
	}
	if check.SLO.Metric != "job.cancellation.no-orphans" {
		t.Errorf("SLO.Metric = %q, want job.cancellation.no-orphans", check.SLO.Metric)
	}
}

func TestExportRestoreReferencesO42Substrate(t *testing.T) {
	t.Parallel()
	check, ok := Lookup(CheckExportRestoreRoundTrip)
	if !ok {
		t.Fatal("export-restore check missing from catalog")
	}
	found := false
	for _, sub := range check.SubstrateBeads {
		if sub == "hp-o42" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("export-restore must list hp-o42 as substrate bead (it owns export/restore semantics)")
	}
}

func TestSignedAppCheckListsCodesignArtifacts(t *testing.T) {
	t.Parallel()
	check, ok := Lookup(CheckSignedAppLaunches)
	if !ok {
		t.Fatal("signed-app check missing from catalog")
	}
	hasCodesign := false
	hasNotarization := false
	for _, artifact := range check.EvidenceArtifacts {
		if strings.Contains(artifact, "codesign") {
			hasCodesign = true
		}
		if strings.Contains(artifact, "notarization") {
			hasNotarization = true
		}
	}
	if !hasCodesign {
		t.Errorf("signed-app evidence must include a codesign-verify artifact")
	}
	if !hasNotarization {
		t.Errorf("signed-app evidence must include a notarization-status artifact")
	}
}

func TestAuditRedactionLogsScanFourArtifactClasses(t *testing.T) {
	t.Parallel()
	check, ok := Lookup(CheckAuditRedactionAllSecrets)
	if !ok {
		t.Fatal("audit-redaction check missing from catalog")
	}
	want := []string{
		"audit-log-scan",
		"structured-log-scan",
		"crash-report-scan",
		"plan-job-artifact-scan",
	}
	for _, prefix := range want {
		matched := false
		for _, artifact := range check.EvidenceArtifacts {
			if strings.HasPrefix(artifact, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("audit-redaction evidence missing artifact prefixed with %q (per §18.4 secret-class coverage)", prefix)
		}
	}
}

func TestLookupReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := Lookup(CheckSignedAppLaunches); !ok {
		t.Errorf("Lookup must return true for a known ID")
	}
	if _, ok := Lookup(CheckID("release.does_not_exist")); ok {
		t.Errorf("Lookup must return false for an unknown ID")
	}
}

func TestCheckIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[CheckID]bool, 10)
	for _, check := range Catalog() {
		if seen[check.ID] {
			t.Errorf("duplicate ID in catalog: %s", check.ID)
		}
		seen[check.ID] = true
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
