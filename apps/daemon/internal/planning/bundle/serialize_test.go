package bundle

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func makeRichBundle() *schemas.ExistingCodebaseContextBundle {
	readmeText := "# Demo project\n\nReadme body."
	readmeB64 := base64.StdEncoding.EncodeToString([]byte(readmeText))
	agentsText := "# AGENTS.md body"
	agentsB64 := base64.StdEncoding.EncodeToString([]byte(agentsText))
	docText := "doc body"
	docB64 := base64.StdEncoding.EncodeToString([]byte(docText))
	covCfg := "vitest.config.ts"

	return &schemas.ExistingCodebaseContextBundle{
		ProjectId:     "demo",
		CommitSha:     strings.Repeat("a", 40),
		SchemaVersion: schemas.ExistingCodebaseContextBundleSchemaVersion(SchemaVersion),
		ContentHash:   "test-hash",
		GeneratedAt:   time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		TokenBudget:   8000,
		TokenEstimate: 4500,
		Readme: &schemas.FileSnapshot{
			Path: "README.md", SizeBytes: len(readmeText), ContentB64: readmeB64,
		},
		AgentsMd: &schemas.FileSnapshot{
			Path: "AGENTS.md", SizeBytes: len(agentsText), ContentB64: agentsB64,
		},
		ArchitectureDocs: []schemas.FileSnapshot{
			{Path: "docs/architecture/01.md", SizeBytes: len(docText), ContentB64: docB64},
		},
		PackageManifests: []schemas.ManifestSnapshot{
			{Path: "package.json", Kind: schemas.PackageJson, Raw: `{"name":"demo"}`},
		},
		TestLayout: &schemas.TestLayoutSummary{
			Runner:             "vitest",
			TestFilePatterns:   []string{"**/*.test.ts"},
			FixtureConventions: []string{"test/fixtures/"},
			CoverageConfig:     &covCfg,
		},
		ExistingBeads: []schemas.BeadSummary{
			{Id: "hp-a", Title: "demo", IssueType: "task", Priority: 1, Status: "open", DependencyCount: 2},
		},
		HealthHotspots: []schemas.HotspotSummary{
			{Path: "src/foo.ts", CompositeScore: 87.5},
		},
		Excluded:   []string{".env [pattern:.env]"},
		Redactions: []schemas.RedactionEntry{},
	}
}

func TestSerializeMarkdownNilBundle(t *testing.T) {
	_, err := SerializeMarkdown(nil)
	if !errors.Is(err, ErrNilBundle) {
		t.Errorf("err = %v, want ErrNilBundle", err)
	}
}

func TestSerializeMarkdownAllSections(t *testing.T) {
	b := makeRichBundle()
	out, err := SerializeMarkdown(b)
	if err != nil {
		t.Fatalf("SerializeMarkdown: %v", err)
	}

	wantSections := []string{
		"# Existing Codebase Context Bundle",
		"## README",
		"## AGENTS.md",
		"## Architecture docs",
		"## Package manifests",
		"## Test layout",
		"## Existing beads",
		"## Health hotspots",
		"## Excluded",
		"**Provenance.**",
	}
	for _, want := range wantSections {
		if !strings.Contains(out, want) {
			t.Errorf("output missing section %q\n--- output ---\n%s", want, out[:min(2000, len(out))])
		}
	}
}

func TestSerializeMarkdownSectionOrder(t *testing.T) {
	b := makeRichBundle()
	out, _ := SerializeMarkdown(b)

	// Section order must follow §7.1 contract.
	wantOrder := []string{
		"# Existing Codebase Context Bundle",
		"## README",
		"## AGENTS.md",
		"## Architecture docs",
		"## Package manifests",
		"## Test layout",
		"## Existing beads",
		"## Health hotspots",
		"## Excluded",
		"**Provenance.**",
	}
	prev := -1
	for _, w := range wantOrder {
		idx := strings.Index(out, w)
		if idx < 0 {
			t.Errorf("section %q not found", w)
			continue
		}
		if idx < prev {
			t.Errorf("section %q at %d came before previous section at %d", w, idx, prev)
		}
		prev = idx
	}
}

func TestSerializeMarkdownDeterministic(t *testing.T) {
	b := makeRichBundle()
	a, _ := SerializeMarkdown(b)
	c, _ := SerializeMarkdown(b)
	if a != c {
		t.Error("SerializeMarkdown not deterministic on same input")
	}
}

func TestSerializeMarkdownDecodesBase64(t *testing.T) {
	b := makeRichBundle()
	out, _ := SerializeMarkdown(b)
	// README body should appear (decoded).
	if !strings.Contains(out, "# Demo project") {
		t.Errorf("README body not decoded into output")
	}
	// AGENTS body should appear.
	if !strings.Contains(out, "# AGENTS.md body") {
		t.Errorf("AGENTS body not decoded into output")
	}
}

func TestSerializeMarkdownEmptyBundleNoSpuriousSections(t *testing.T) {
	b := &schemas.ExistingCodebaseContextBundle{
		ProjectId:        "x",
		CommitSha:        strings.Repeat("a", 40),
		SchemaVersion:    schemas.ExistingCodebaseContextBundleSchemaVersion(SchemaVersion),
		ContentHash:      "h",
		ArchitectureDocs: []schemas.FileSnapshot{},
		PackageManifests: []schemas.ManifestSnapshot{},
		ExistingBeads:    []schemas.BeadSummary{},
		HealthHotspots:   []schemas.HotspotSummary{},
		Excluded:         []string{},
		Redactions:       []schemas.RedactionEntry{},
	}
	out, err := SerializeMarkdown(b)
	if err != nil {
		t.Fatalf("SerializeMarkdown: %v", err)
	}
	// Empty sections should not render.
	missingFor := []string{
		"## README",
		"## AGENTS.md",
		"## Architecture docs",
		"## Package manifests",
		"## Existing beads",
		"## Health hotspots",
		"## Excluded",
	}
	for _, m := range missingFor {
		if strings.Contains(out, m) {
			t.Errorf("empty bundle rendered %q section", m)
		}
	}
	// Metadata + provenance should always render.
	if !strings.Contains(out, "# Existing Codebase Context Bundle") {
		t.Error("metadata section absent")
	}
	if !strings.Contains(out, "**Provenance.**") {
		t.Error("provenance section absent")
	}
}

func TestSerializeMarkdownTruncationBanner(t *testing.T) {
	b := makeRichBundle()
	originalSize := 5_000
	b.Readme.TruncatedFromBytes = &originalSize
	out, _ := SerializeMarkdown(b)
	if !strings.Contains(out, "Truncated:") {
		t.Errorf("truncation banner missing for truncated README")
	}
}

func TestSerializeMarkdownTablePipeEscape(t *testing.T) {
	b := makeRichBundle()
	b.ExistingBeads[0].Title = "title with | pipe"
	out, _ := SerializeMarkdown(b)
	// Pipes inside a cell must be escaped.
	if !strings.Contains(out, "title with \\| pipe") {
		t.Errorf("title pipe not escaped: %q in\n%s", "title with | pipe", out)
	}
}

func TestSerializeMarkdownProvenanceFields(t *testing.T) {
	b := makeRichBundle()
	out, _ := SerializeMarkdown(b)
	wantProv := []string{
		"ContentHash: `test-hash`",
		"TokenEstimate: 4500",
		"TokenBudget: 8000",
		"GeneratedAt: `2026-05-07T12:00:00Z`",
	}
	for _, w := range wantProv {
		if !strings.Contains(out, w) {
			t.Errorf("provenance missing %q", w)
		}
	}
}

func TestSerializeMarkdownProvenanceOmitsZeroBudget(t *testing.T) {
	b := makeRichBundle()
	b.TokenBudget = 0
	out, _ := SerializeMarkdown(b)
	if strings.Contains(out, "TokenBudget: 0") {
		t.Error("zero TokenBudget should be omitted from provenance")
	}
}

func TestSerializeMarkdownProvenanceOmitsZeroGeneratedAt(t *testing.T) {
	b := makeRichBundle()
	b.GeneratedAt = time.Time{}
	out, _ := SerializeMarkdown(b)
	if strings.Contains(out, "GeneratedAt:") {
		t.Error("zero GeneratedAt should be omitted from provenance")
	}
}

func TestSerializeMarkdownDecodeErrorRendersInline(t *testing.T) {
	b := makeRichBundle()
	b.Readme.ContentB64 = "@@@not-base64@@@"
	out, _ := SerializeMarkdown(b)
	if !strings.Contains(out, "decode error") {
		t.Error("decode error not surfaced inline")
	}
	// Shouldn't crash; should still emit other sections.
	if !strings.Contains(out, "## AGENTS.md") {
		t.Error("decode error in README should not abort serialization")
	}
}

func TestSerializeMarkdownTestLayoutAbsent(t *testing.T) {
	b := makeRichBundle()
	b.TestLayout = nil
	out, _ := SerializeMarkdown(b)
	if strings.Contains(out, "## Test layout") {
		t.Error("nil TestLayout should not render the section")
	}
}

func TestEscapeTableCell(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"with | pipe", "with \\| pipe"},
		{"with\nnewline", "with newline"},
		{"both | and\nboth", "both \\| and both"},
	}
	for _, c := range cases {
		if got := escapeTableCell(c.in); got != c.want {
			t.Errorf("escapeTableCell(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
