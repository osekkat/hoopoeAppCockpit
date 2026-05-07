package review

import (
	"errors"
	"strings"
	"testing"
)

func TestPromptCatalogContainsAllTenRounds(t *testing.T) {
	t.Parallel()
	got := PromptCatalog()
	if len(got) != 10 {
		t.Fatalf("prompt catalog length = %d, want 10 (rounds 0..9)", len(got))
	}
	for i, template := range got {
		if template.RoundIndex != i {
			t.Errorf("catalog[%d].RoundIndex = %d, want %d", i, template.RoundIndex, i)
		}
	}
}

func TestEveryTemplateHasNonEmptyBody(t *testing.T) {
	t.Parallel()
	for _, template := range PromptCatalog() {
		if strings.TrimSpace(template.Body) == "" {
			t.Errorf("%s: Body is empty", template.RoundID)
		}
	}
}

func TestEveryTemplateHasVersionAndCurrentSchemaVersion(t *testing.T) {
	t.Parallel()
	for _, template := range PromptCatalog() {
		if template.Version == "" {
			t.Errorf("%s: Version is empty", template.RoundID)
		}
		if template.SchemaVersion != PromptCatalogSchemaVersion {
			t.Errorf("%s: SchemaVersion = %d, want %d", template.RoundID, template.SchemaVersion, PromptCatalogSchemaVersion)
		}
	}
}

func TestRoundIDsAreUniqueAndCanonicallyShaped(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool, 10)
	for _, template := range PromptCatalog() {
		if seen[template.RoundID] {
			t.Errorf("duplicate RoundID: %s", template.RoundID)
		}
		seen[template.RoundID] = true
		if !strings.HasPrefix(template.RoundID, "round-") {
			t.Errorf("%s: RoundID must start with `round-`", template.RoundID)
		}
	}
}

func TestPromptByRoundIDReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := PromptByRoundID("round-0"); !ok {
		t.Errorf("PromptByRoundID(round-0) must return true")
	}
	if _, ok := PromptByRoundID("round-99"); ok {
		t.Errorf("PromptByRoundID(round-99) must return false")
	}
}

func TestPromptByRoundIndexReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := PromptByRoundIndex(0); !ok {
		t.Errorf("PromptByRoundIndex(0) must return true")
	}
	if _, ok := PromptByRoundIndex(99); ok {
		t.Errorf("PromptByRoundIndex(99) must return false")
	}
}

func TestRenderPromptSubstitutesVariables(t *testing.T) {
	t.Parallel()
	template := PromptTemplate{
		RoundID: "round-test",
		Body:    "Project: {{.ProjectName}} — Bead: {{.BeadID}}",
		RequiredVars: []PromptVariable{
			PromptVarProjectName, PromptVarBeadID,
		},
	}
	got, err := RenderPrompt(template, map[PromptVariable]string{
		PromptVarProjectName: "Hoopoe",
		PromptVarBeadID:      "hp-test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "Project: Hoopoe — Bead: hp-test" {
		t.Errorf("rendered = %q", got)
	}
}

func TestRenderPromptMissingRequiredVarReturnsError(t *testing.T) {
	t.Parallel()
	template := PromptTemplate{
		RoundID:      "round-test",
		Body:         "Bead: {{.BeadID}}",
		RequiredVars: []PromptVariable{PromptVarBeadID},
	}
	_, err := RenderPrompt(template, map[PromptVariable]string{})
	if !errors.Is(err, ErrMissingPromptVariable) {
		t.Errorf("expected ErrMissingPromptVariable, got %v", err)
	}
}

func TestRenderPromptEmptyRequiredVarReturnsError(t *testing.T) {
	t.Parallel()
	template := PromptTemplate{
		RoundID:      "round-test",
		Body:         "Bead: {{.BeadID}}",
		RequiredVars: []PromptVariable{PromptVarBeadID},
	}
	_, err := RenderPrompt(template, map[PromptVariable]string{
		PromptVarBeadID: "   ",
	})
	if !errors.Is(err, ErrMissingPromptVariable) {
		t.Errorf("whitespace-only variable must trigger ErrMissingPromptVariable, got %v", err)
	}
}

func TestRenderPromptIgnoresUnknownVars(t *testing.T) {
	t.Parallel()
	template := PromptTemplate{
		RoundID: "round-test",
		Body:    "Bead: {{.BeadID}}",
	}
	got, err := RenderPrompt(template, map[PromptVariable]string{
		PromptVarBeadID:    "hp-test",
		"FutureVarNotUsed": "will not appear",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "hp-test") {
		t.Errorf("rendered missing BeadID: %q", got)
	}
}

func TestRound0CallsForUBS(t *testing.T) {
	t.Parallel()
	template, ok := PromptByRoundID("round-0")
	if !ok {
		t.Fatal("round-0 missing")
	}
	if !strings.Contains(template.Body, "UBS") {
		t.Errorf("round-0 body must reference UBS: %q", template.Body)
	}
}

func TestRound3ReferencesAgentsMD(t *testing.T) {
	t.Parallel()
	template, ok := PromptByRoundID("round-3")
	if !ok {
		t.Fatal("round-3 missing")
	}
	if !strings.Contains(template.Body, "AGENTS.md") {
		t.Errorf("round-3 body must reference AGENTS.md (fresh-eyes review reads it fresh): %q", template.Body)
	}
}

func TestRound9LandingChecklistEnumeratesItems(t *testing.T) {
	t.Parallel()
	template, ok := PromptByRoundID("round-9")
	if !ok {
		t.Fatal("round-9 missing")
	}
	for i := 1; i <= 5; i++ {
		marker := strings.Index(template.Body, "")
		_ = marker
	}
	for _, want := range []string{"tests", "Git", "br", "Audit log"} {
		if !strings.Contains(template.Body, want) {
			t.Errorf("round-9 landing checklist must mention %q: %q", want, template.Body)
		}
	}
}

func TestRound5ReferencesHealthHotspots(t *testing.T) {
	t.Parallel()
	template, ok := PromptByRoundID("round-5")
	if !ok {
		t.Fatal("round-5 missing")
	}
	if !strings.Contains(template.Body, "{{.HealthHotspots}}") {
		t.Errorf("round-5 body must reference HealthHotspots variable: %q", template.Body)
	}
	wantContains := []PromptVariable{PromptVarHealthHotspots}
	for _, v := range wantContains {
		found := false
		for _, r := range template.RequiredVars {
			if r == v {
				found = true
			}
		}
		if !found {
			t.Errorf("round-5 RequiredVars missing %s", v)
		}
	}
}

func TestPromptDigestIsStableHex(t *testing.T) {
	t.Parallel()
	digest := PromptDigest("anything")
	if len(digest) != 64 {
		t.Errorf("PromptDigest length = %d, want 64 (sha256 hex)", len(digest))
	}
	for _, ch := range digest {
		isHex := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')
		if !isHex {
			t.Errorf("PromptDigest contains non-hex char %q", ch)
		}
	}
	// Stability across calls.
	if PromptDigest("anything") != digest {
		t.Errorf("PromptDigest must be deterministic")
	}
	// Different input → different digest.
	if PromptDigest("different") == digest {
		t.Errorf("PromptDigest must change when input changes")
	}
}

func TestPromptCatalogIsImmutableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := PromptCatalog()
	b := PromptCatalog()
	if len(a) != len(b) {
		t.Fatalf("catalog length differs across calls")
	}
	for i := range a {
		if a[i].RoundID != b[i].RoundID {
			t.Errorf("catalog[%d] differs across calls", i)
		}
	}
}

func TestPromptCatalogJoinsToReviewRoundCatalog(t *testing.T) {
	t.Parallel()
	// hp-8xm DOD: per-round prompt template aligns with the
	// review-package's RoundCatalog. Every prompt template's
	// RoundID must match a RoundSpec.RoundID.
	rounds := RoundCatalog()
	roundIDs := make(map[string]bool, len(rounds))
	for _, r := range rounds {
		roundIDs[r.RoundID] = true
	}
	for _, tpl := range PromptCatalog() {
		if !roundIDs[tpl.RoundID] {
			t.Errorf("prompt template %q has no matching RoundSpec in RoundCatalog", tpl.RoundID)
		}
	}
}

func TestSnapshotEveryRenderedPromptIsNonEmpty(t *testing.T) {
	t.Parallel()
	// Regression-test the rendered prompts using the bead's
	// REQUIRED variable set as a fixture; a missing-variable
	// regression would surface here.
	fixture := map[PromptVariable]string{
		PromptVarProjectName:       "Hoopoe",
		PromptVarBeadID:            "hp-test",
		PromptVarBeadTitle:         "Test bead",
		PromptVarReviewSubject:     "apps/daemon/internal/review",
		PromptVarRecentDiffSummary: "(diff summary placeholder)",
		PromptVarHealthHotspots:    "(hotspot list placeholder)",
		PromptVarPriorFindings:     "(prior findings placeholder)",
		PromptVarAgentsMD:          "(agents.md excerpt placeholder)",
		PromptVarRoundIndex:        "0",
	}
	for _, tpl := range PromptCatalog() {
		got, err := RenderPrompt(tpl, fixture)
		if err != nil {
			t.Fatalf("%s: RenderPrompt failed: %v", tpl.RoundID, err)
		}
		if strings.TrimSpace(got) == "" {
			t.Errorf("%s: rendered prompt is empty", tpl.RoundID)
		}
		// No remaining `{{.X}}` placeholders in the rendered
		// output; if any are left, a required variable is
		// missing from RequiredVars.
		if strings.Contains(got, "{{.") {
			t.Errorf("%s: rendered output contains unsubstituted placeholder: %q", tpl.RoundID, got)
		}
	}
}
