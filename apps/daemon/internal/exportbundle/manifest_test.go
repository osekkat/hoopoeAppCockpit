package exportbundle

import (
	"strings"
	"testing"
)

func TestSectionCatalogContainsExpectedSections(t *testing.T) {
	t.Parallel()
	want := []SectionID{
		SectionDaemonProjectMetadata,
		SectionAuditLogSlice,
		SectionEventReplayCheckpoints,
		SectionPlanArtifacts,
		SectionBeadConversionTraces,
		SectionTraceabilityMaps,
		SectionHealthSnapshots,
		SectionReviewFindings,
		SectionLandingQueueHistory,
		SectionArtifactRefs,
		SectionCapabilityToolInventory,
		SectionSkillLockFile,
		SectionRedactedDiagnostics,
	}
	got := SectionCatalog()
	if len(got) != len(want) {
		t.Fatalf("section catalog length = %d, want %d (every §10.4 bundle entry)", len(got), len(want))
	}
	for i, section := range got {
		if section.ID != want[i] {
			t.Errorf("catalog[%d].ID = %q, want %q", i, section.ID, want[i])
		}
	}
}

func TestEverySectionHasNonEmptyDescriptionAndPositiveSchemaVersion(t *testing.T) {
	t.Parallel()
	for _, section := range SectionCatalog() {
		if section.Description == "" {
			t.Errorf("%s: Description is empty", section.ID)
		}
		if section.SchemaVersion < 1 {
			t.Errorf("%s: SchemaVersion = %d, want ≥ 1", section.ID, section.SchemaVersion)
		}
	}
}

func TestSectionIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[SectionID]bool, 16)
	for _, section := range SectionCatalog() {
		if seen[section.ID] {
			t.Errorf("duplicate ID in catalog: %s", section.ID)
		}
		seen[section.ID] = true
	}
}

func TestRequiredSectionsExcludesOptional(t *testing.T) {
	t.Parallel()
	required := RequiredSections()
	if len(required) == 0 {
		t.Fatal("expected at least one required section")
	}
	for _, section := range required {
		if section.Optional {
			t.Errorf("%s: Optional sections must not appear in RequiredSections()", section.ID)
		}
	}
}

func TestRequiredSectionsIncludeMetadataAndAuditLog(t *testing.T) {
	t.Parallel()
	required := RequiredSections()
	requiredIDs := make(map[SectionID]bool, len(required))
	for _, section := range required {
		requiredIDs[section.ID] = true
	}
	mustHave := []SectionID{
		SectionDaemonProjectMetadata,
		SectionAuditLogSlice,
		SectionEventReplayCheckpoints,
		SectionCapabilityToolInventory,
		SectionSkillLockFile,
	}
	for _, id := range mustHave {
		if !requiredIDs[id] {
			t.Errorf("%s must be required for restore (cannot rehydrate without it)", id)
		}
	}
}

func TestPlanArtifactsAndReviewFindingsAreOptional(t *testing.T) {
	t.Parallel()
	// A project that has not run Stage 01 yet has no plans;
	// a project that has not run Phase 12 yet has no review
	// findings. Both must be optional.
	for _, id := range []SectionID{SectionPlanArtifacts, SectionReviewFindings} {
		section, ok := LookupSection(id)
		if !ok {
			t.Fatalf("%s missing from catalog", id)
		}
		if !section.Optional {
			t.Errorf("%s must be Optional (project may not have run the substrate stage)", id)
		}
	}
}

func TestSecretsHandlingCoversKnownClasses(t *testing.T) {
	t.Parallel()
	known := map[SecretsHandling]bool{
		SecretsRedact:        true,
		SecretsExclude:       true,
		SecretsNotApplicable: true,
	}
	for _, section := range SectionCatalog() {
		if !known[section.Secrets] {
			t.Errorf("%s: Secrets = %q, not in the known set", section.ID, section.Secrets)
		}
	}
}

func TestAuditLogSecretsAreRedactedNotExcluded(t *testing.T) {
	t.Parallel()
	// Audit log redaction is the default per §10.4 + Guardrail 10:
	// audit always fires; redaction scrubs but never elides rows
	// whole. So Secrets must be Redact, not Exclude.
	section, ok := LookupSection(SectionAuditLogSlice)
	if !ok {
		t.Fatal("audit_log_slice missing")
	}
	if section.Secrets != SecretsRedact {
		t.Errorf("audit_log_slice Secrets = %q, want redact (Guardrail 10 + §10.4)", section.Secrets)
	}
}

func TestPlanArtifactsAreCanonicalSourced(t *testing.T) {
	t.Parallel()
	// Per §1.1: plan markdown files are canonical-owned by the
	// repo's .hoopoe/plans/<plan-id>/ directory; not a daemon
	// cache. The bundle writer must read from canonical.
	section, ok := LookupSection(SectionPlanArtifacts)
	if !ok {
		t.Fatal("plan_artifacts missing")
	}
	if section.Source != SourceCanonical {
		t.Errorf("plan_artifacts Source = %q, want canonical (§1.1)", section.Source)
	}
}

func TestSkillLockFileIsCanonicalAndRequired(t *testing.T) {
	t.Parallel()
	section, ok := LookupSection(SectionSkillLockFile)
	if !ok {
		t.Fatal("skill_lock_file missing")
	}
	if section.Source != SourceCanonical {
		t.Errorf("skill_lock_file Source = %q, want canonical", section.Source)
	}
	if section.Optional {
		t.Errorf("skill_lock_file must be required (restore needs it to reproduce the skill set)")
	}
}

func TestLookupSectionReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := LookupSection(SectionAuditLogSlice); !ok {
		t.Errorf("LookupSection must return true for a known ID")
	}
	if _, ok := LookupSection(SectionID("does_not_exist")); ok {
		t.Errorf("LookupSection must return false for an unknown ID")
	}
}

func TestSectionCatalogIsImmutableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := SectionCatalog()
	b := SectionCatalog()
	if len(a) != len(b) {
		t.Fatalf("catalog length differs across calls: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Errorf("catalog[%d] differs across calls: %q vs %q", i, a[i].ID, b[i].ID)
		}
	}
}

func TestBundleManifestSchemaVersionIsCurrent(t *testing.T) {
	t.Parallel()
	if BundleManifestSchemaVersion < 1 {
		t.Errorf("BundleManifestSchemaVersion = %d, want ≥ 1", BundleManifestSchemaVersion)
	}
}

func TestEverySectionIDIsLowerSnakeCase(t *testing.T) {
	t.Parallel()
	for _, section := range SectionCatalog() {
		s := string(section.ID)
		if strings.ToLower(s) != s {
			t.Errorf("%s: ID must be lower_snake_case", section.ID)
		}
		if strings.ContainsAny(s, " -.") {
			t.Errorf("%s: ID contains forbidden chars (only [a-z0-9_] allowed)", section.ID)
		}
	}
}
