package bundle

import (
	"errors"
	"strings"
	"testing"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func makeBundle() *schemas.ExistingCodebaseContextBundle {
	readmeBytes := strings.Repeat("R", 4_000)
	agentsBytes := strings.Repeat("A", 4_000)
	docBytes := strings.Repeat("D", 8_000)
	manifestBytes := strings.Repeat("M", 2_000)
	return &schemas.ExistingCodebaseContextBundle{
		Readme: &schemas.FileSnapshot{
			Path: "README.md", SizeBytes: len(readmeBytes), ContentB64: readmeBytes,
		},
		AgentsMd: &schemas.FileSnapshot{
			Path: "AGENTS.md", SizeBytes: len(agentsBytes), ContentB64: agentsBytes,
		},
		ArchitectureDocs: []schemas.FileSnapshot{
			{Path: "docs/architecture/01.md", SizeBytes: len(docBytes), ContentB64: docBytes},
			{Path: "docs/architecture/02.md", SizeBytes: len(docBytes), ContentB64: docBytes},
		},
		PackageManifests: []schemas.ManifestSnapshot{
			{Path: "package.json", Kind: schemas.PackageJson, Raw: manifestBytes},
		},
		ExistingBeads: []schemas.BeadSummary{
			{Id: "hp-a", Title: "demo bead a", IssueType: "task", Priority: 1, Status: "open"},
			{Id: "hp-b", Title: "demo bead b", IssueType: "task", Priority: 2, Status: "open"},
		},
		HealthHotspots: []schemas.HotspotSummary{
			{Path: "src/foo.ts", CompositeScore: 50},
		},
		ProjectId:     "demo",
		CommitSha:     strings.Repeat("a", 40),
		SchemaVersion: schemas.ExistingCodebaseContextBundleSchemaVersion(SchemaVersion),
		TokenBudget:   8000,
		Excluded:      []string{},
		Redactions:    []schemas.RedactionEntry{},
	}
}

func TestEstimateTokensBasic(t *testing.T) {
	b := makeBundle()
	est := EstimateTokens(b)
	// Total bytes ~= 4000 + 4000 + 8000 + 8000 + 2000 + small extras
	// ~ 26050; * 0.25 ≈ 6500.
	if est < 5_000 || est > 8_000 {
		t.Errorf("estimate = %d, want roughly 6500 (5000-8000 band)", est)
	}
}

func TestEstimateTokensNilSafe(t *testing.T) {
	if est := EstimateTokens(nil); est != 0 {
		t.Errorf("nil bundle estimate = %d, want 0", est)
	}
}

func TestEnforceBudgetNilBundleErrors(t *testing.T) {
	if _, _, err := EnforceBudget(nil, 100); err == nil {
		t.Error("EnforceBudget(nil) should error")
	}
}

func TestEnforceBudgetNegativeBudgetErrors(t *testing.T) {
	b := makeBundle()
	_, _, err := EnforceBudget(b, -1)
	if !errors.Is(err, ErrInvalidBudget) {
		t.Errorf("err = %v, want ErrInvalidBudget", err)
	}
}

func TestEnforceBudgetUnderCapNoOp(t *testing.T) {
	b := makeBundle()
	originalReadme := b.Readme
	originalDocs := len(b.ArchitectureDocs)
	originalBeads := len(b.ExistingBeads)

	est, dropped, err := EnforceBudget(b, 100_000)
	if err != nil {
		t.Fatalf("EnforceBudget: %v", err)
	}
	if len(dropped) != 0 {
		t.Errorf("dropped under-cap: %v", dropped)
	}
	if b.Readme != originalReadme || len(b.ArchitectureDocs) != originalDocs || len(b.ExistingBeads) != originalBeads {
		t.Errorf("bundle mutated under cap: readme=%v docs=%d beads=%d", b.Readme, len(b.ArchitectureDocs), len(b.ExistingBeads))
	}
	if b.TokenEstimate != est {
		t.Errorf("TokenEstimate = %d, want %d", b.TokenEstimate, est)
	}
}

func TestEnforceBudgetDropsBeadsFirst(t *testing.T) {
	// Set a budget that fits everything except the beads.
	b := makeBundle()
	full := EstimateTokens(b)
	beadsBytes := 0
	for _, bead := range b.ExistingBeads {
		beadsBytes += len(bead.Title) + len(bead.Id) + len(bead.IssueType) + len(bead.Status)
	}
	// Budget = full - 1 (just under the full estimate).
	_, dropped, err := EnforceBudget(b, full-1)
	if err != nil {
		t.Fatalf("EnforceBudget: %v", err)
	}
	if len(b.ExistingBeads) != 0 {
		t.Errorf("beads not dropped first: %d remain", len(b.ExistingBeads))
	}
	// Architecture docs, manifests, readme, agents should all survive.
	if b.Readme == nil {
		t.Error("Readme dropped despite beads-first rule")
	}
	if len(b.ArchitectureDocs) == 0 {
		t.Error("ArchitectureDocs dropped despite beads-first rule")
	}
	// Dropped marker should mention beads.
	foundBeadsMarker := false
	for _, d := range dropped {
		if strings.HasPrefix(d, "beads:") {
			foundBeadsMarker = true
			break
		}
	}
	if !foundBeadsMarker {
		t.Errorf("dropped does not include beads marker: %v", dropped)
	}
}

func TestEnforceBudgetDropOrder(t *testing.T) {
	// Budget = 0 means use the bundle's TokenBudget; pass an explicit
	// tiny budget to force every step.
	b := makeBundle()
	_, dropped, err := EnforceBudget(b, 1)
	if err != nil {
		t.Fatalf("EnforceBudget: %v", err)
	}
	// At budget=1, every section should drop. Verify the bundle is
	// fully truncated.
	if len(b.ExistingBeads) != 0 {
		t.Errorf("ExistingBeads not dropped at budget=1: %d", len(b.ExistingBeads))
	}
	if len(b.ArchitectureDocs) != 0 {
		t.Errorf("ArchitectureDocs not dropped at budget=1: %d", len(b.ArchitectureDocs))
	}
	if len(b.PackageManifests) != 0 {
		t.Errorf("PackageManifests not dropped at budget=1: %d", len(b.PackageManifests))
	}
	if b.AgentsMd != nil {
		t.Error("AgentsMd not dropped at budget=1")
	}
	if b.Readme != nil {
		t.Error("Readme not dropped at budget=1")
	}
	if len(dropped) == 0 {
		t.Error("dropped list is empty after full truncation")
	}
}

func TestEnforceBudgetMutationStampsTokenEstimate(t *testing.T) {
	b := makeBundle()
	est, _, err := EnforceBudget(b, 1000)
	if err != nil {
		t.Fatalf("EnforceBudget: %v", err)
	}
	if b.TokenEstimate != est {
		t.Errorf("TokenEstimate = %d, want %d", b.TokenEstimate, est)
	}
}

func TestEnforceBudgetSlicesNeverNil(t *testing.T) {
	// Even after full truncation, the slice fields must be empty,
	// not nil — the openapi schema requires them to serialize as
	// `[]` rather than `null`.
	b := makeBundle()
	if _, _, err := EnforceBudget(b, 1); err != nil {
		t.Fatalf("EnforceBudget: %v", err)
	}
	if b.ExistingBeads == nil {
		t.Error("ExistingBeads is nil after truncation")
	}
	if b.ArchitectureDocs == nil {
		t.Error("ArchitectureDocs is nil after truncation")
	}
	if b.PackageManifests == nil {
		t.Error("PackageManifests is nil after truncation")
	}
}

func TestEnforceBudgetZeroBudgetUsesField(t *testing.T) {
	// budget=0 falls back to the bundle's own TokenBudget field.
	b := makeBundle()
	b.TokenBudget = 1 // tiny
	_, dropped, err := EnforceBudget(b, 0)
	if err != nil {
		t.Fatalf("EnforceBudget: %v", err)
	}
	if len(dropped) == 0 {
		t.Error("dropped is empty despite tiny TokenBudget field")
	}
}

func TestEnforceBudgetZeroBudgetNoFieldNoOp(t *testing.T) {
	// budget=0 + TokenBudget=0 means "no budget, leave bundle alone."
	b := makeBundle()
	b.TokenBudget = 0
	_, dropped, err := EnforceBudget(b, 0)
	if err != nil {
		t.Fatalf("EnforceBudget: %v", err)
	}
	if len(dropped) != 0 {
		t.Errorf("dropped despite no budget: %v", dropped)
	}
	if b.Readme == nil {
		t.Error("Readme dropped despite no budget")
	}
}

func TestFormatDropMarker(t *testing.T) {
	got := formatDropMarker("beads", 30)
	want := "beads:30"
	if got != want {
		t.Errorf("formatDropMarker = %q, want %q", got, want)
	}
}

func TestIntToAEdgeCases(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{-7, "-7"},
		{100, "100"},
	}
	for _, c := range cases {
		if got := intToA(c.in); got != c.want {
			t.Errorf("intToA(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEnforceBudgetDroppedPathsAreRecorded(t *testing.T) {
	b := makeBundle()
	_, dropped, err := EnforceBudget(b, 1)
	if err != nil {
		t.Fatalf("EnforceBudget: %v", err)
	}
	// At budget=1, every section drops; expect README + AGENTS +
	// 2 arch docs + 1 manifest + beads marker (=6 entries minimum).
	if len(dropped) < 5 {
		t.Errorf("dropped len = %d, want >= 5 (README, AGENTS, 2 docs, 1 manifest, beads marker)", len(dropped))
	}
	// Verify README appears.
	foundReadme := false
	for _, d := range dropped {
		if d == "README.md" {
			foundReadme = true
			break
		}
	}
	if !foundReadme {
		t.Errorf("README.md not in dropped: %v", dropped)
	}
}
