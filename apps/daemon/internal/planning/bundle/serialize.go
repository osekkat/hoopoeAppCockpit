// serialize.go — per-model bundle serialization
// (hp-rsly thirteenth slice).
//
// `SerializeMarkdown` renders an `ExistingCodebaseContextBundle` into
// a stable markdown document the candidate-prompt runners hand to
// each CLI / Oracle invocation. The format prioritizes:
//
//   1. Stable section order — refinement rounds reuse the same
//      bundle; the renderer must not reorder sections between
//      rounds (that would invalidate the upstream content-hash).
//   2. Explicit truncation banners — every section the assembly
//      truncated (TruncatedFromBytes set, ExistingBeads dropped,
//      etc.) renders an inline "Truncated from N bytes" so the
//      model sees the elision.
//   3. Provenance footer — CommitSha + ContentHash + TokenEstimate
//      so the model can quote them when asked "what state did you
//      see?"
//
// Section order (frozen by §7.1 contract):
//
//   1. Project metadata (id, commit sha, schema version)
//   2. README
//   3. AGENTS.md
//   4. Architecture docs (alphabetical)
//   5. Package manifests (kind asc, path asc tie-break)
//   6. Test layout
//   7. Existing beads (priority asc, id asc)
//   8. Health hotspots (composite desc)
//   9. Excluded surface (path:reason markers)
//  10. Provenance footer
//
// What this slice does NOT do (hp-rsly residual):
//
//   - Per-model variants beyond markdown (Oracle browser-mode may
//     prefer a more compact form; CLI runners share the markdown
//     baseline). The CLI/Oracle adapter slice plugs in alternate
//     emit paths if/when measurements show the markdown shape
//     wastes tokens.
//   - Tokenizer-accurate truncation feedback (still using the
//     bytes-per-token heuristic from EnforceBudget).

package bundle

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// ErrNilBundle is returned when SerializeMarkdown is given a nil
// bundle. Distinguished from ErrInvalidOpts / ErrInvalidPolicy so
// the caller can pinpoint which layer detected the missing input.
var ErrNilBundle = errors.New("planning/bundle: nil bundle")

// SerializeMarkdown renders the bundle as a markdown document.
// Output is deterministic — same input → byte-identical bytes —
// because every section uses sorted iteration and decoded base64
// content is rendered verbatim.
func SerializeMarkdown(b *schemas.ExistingCodebaseContextBundle) (string, error) {
	if b == nil {
		return "", ErrNilBundle
	}
	var sb strings.Builder

	writeMetadata(&sb, b)
	writeFileSnapshot(&sb, "## README", b.Readme)
	writeFileSnapshot(&sb, "## AGENTS.md", b.AgentsMd)
	writeArchitectureDocs(&sb, b.ArchitectureDocs)
	writeManifests(&sb, b.PackageManifests)
	writeTestLayout(&sb, b.TestLayout)
	writeBeads(&sb, b.ExistingBeads)
	writeHotspots(&sb, b.HealthHotspots)
	writeExcluded(&sb, b.Excluded)
	writeProvenance(&sb, b)

	return sb.String(), nil
}

func writeMetadata(sb *strings.Builder, b *schemas.ExistingCodebaseContextBundle) {
	sb.WriteString("# Existing Codebase Context Bundle\n\n")
	fmt.Fprintf(sb, "- **Project ID:** `%s`\n", b.ProjectId)
	fmt.Fprintf(sb, "- **Commit SHA:** `%s`\n", b.CommitSha)
	fmt.Fprintf(sb, "- **Schema version:** `%d`\n", int(b.SchemaVersion))
	sb.WriteString("\n")
}

func writeFileSnapshot(sb *strings.Builder, heading string, snap *schemas.FileSnapshot) {
	if snap == nil {
		return
	}
	sb.WriteString(heading)
	sb.WriteString("\n\n")
	if snap.TruncatedFromBytes != nil {
		fmt.Fprintf(sb, "> _Truncated: showing first %d bytes of %d (sha256: `%s`)._\n\n", snap.SizeBytes, *snap.TruncatedFromBytes, snap.Sha256)
	}
	body, err := decodeFileSnapshotContent(snap)
	if err != nil {
		// Render the error inline rather than fail — a corrupt
		// snapshot upstream shouldn't take the whole serialization
		// down, and the model needs to see something is wrong.
		fmt.Fprintf(sb, "_(decode error: %s)_\n\n", err)
		return
	}
	sb.WriteString("```\n")
	sb.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		sb.WriteString("\n")
	}
	sb.WriteString("```\n\n")
}

func writeArchitectureDocs(sb *strings.Builder, docs []schemas.FileSnapshot) {
	if len(docs) == 0 {
		return
	}
	sb.WriteString("## Architecture docs\n\n")
	for i := range docs {
		writeFileSnapshot(sb, "### `"+docs[i].Path+"`", &docs[i])
	}
}

func writeManifests(sb *strings.Builder, manifests []schemas.ManifestSnapshot) {
	if len(manifests) == 0 {
		return
	}
	sb.WriteString("## Package manifests\n\n")
	for _, m := range manifests {
		fmt.Fprintf(sb, "### `%s` (%s)\n\n", m.Path, string(m.Kind))
		sb.WriteString("```\n")
		sb.WriteString(m.Raw)
		if !strings.HasSuffix(m.Raw, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n\n")
	}
}

func writeTestLayout(sb *strings.Builder, layout *schemas.TestLayoutSummary) {
	if layout == nil {
		return
	}
	sb.WriteString("## Test layout\n\n")
	fmt.Fprintf(sb, "- **Runner:** `%s`\n", layout.Runner)
	if cov := layout.CoverageConfig; cov != nil && *cov != "" {
		fmt.Fprintf(sb, "- **Coverage config:** `%s`\n", *cov)
	}
	if len(layout.TestFilePatterns) > 0 {
		sb.WriteString("- **Test file patterns:**\n")
		for _, p := range layout.TestFilePatterns {
			fmt.Fprintf(sb, "  - `%s`\n", p)
		}
	}
	if len(layout.FixtureConventions) > 0 {
		sb.WriteString("- **Fixture conventions:**\n")
		for _, p := range layout.FixtureConventions {
			fmt.Fprintf(sb, "  - `%s`\n", p)
		}
	}
	sb.WriteString("\n")
}

func writeBeads(sb *strings.Builder, beads []schemas.BeadSummary) {
	if len(beads) == 0 {
		return
	}
	sb.WriteString("## Existing beads\n\n")
	sb.WriteString("| ID | P | Type | Status | Deps | Title |\n")
	sb.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, b := range beads {
		fmt.Fprintf(sb, "| `%s` | P%d | %s | %s | %d | %s |\n", b.Id, b.Priority, b.IssueType, b.Status, b.DependencyCount, escapeTableCell(b.Title))
	}
	sb.WriteString("\n")
}

func writeHotspots(sb *strings.Builder, hotspots []schemas.HotspotSummary) {
	if len(hotspots) == 0 {
		return
	}
	sb.WriteString("## Health hotspots (top by composite score)\n\n")
	sb.WriteString("| Path | Lang | Composite | Churn | Complexity |\n")
	sb.WriteString("| --- | --- | --- | --- | --- |\n")
	for _, h := range hotspots {
		lang := ""
		if h.Language != nil {
			lang = *h.Language
		}
		churn := ""
		if h.ChurnScore != nil {
			churn = fmt.Sprintf("%.2f", *h.ChurnScore)
		}
		complexity := ""
		if h.ComplexityScore != nil {
			complexity = fmt.Sprintf("%.2f", *h.ComplexityScore)
		}
		fmt.Fprintf(sb, "| `%s` | %s | %.2f | %s | %s |\n", h.Path, lang, h.CompositeScore, churn, complexity)
	}
	sb.WriteString("\n")
}

func writeExcluded(sb *strings.Builder, excluded []string) {
	if len(excluded) == 0 {
		return
	}
	sb.WriteString("## Excluded\n\n")
	sb.WriteString("> The following surfaces were excluded from the bundle by policy or budget. ")
	sb.WriteString("Visit the \"manage what models see\" link in the artifact rail to override.\n\n")
	for _, e := range excluded {
		fmt.Fprintf(sb, "- `%s`\n", e)
	}
	sb.WriteString("\n")
}

func writeProvenance(sb *strings.Builder, b *schemas.ExistingCodebaseContextBundle) {
	sb.WriteString("---\n\n")
	sb.WriteString("**Provenance.** ")
	fmt.Fprintf(sb, "ContentHash: `%s`. ", b.ContentHash)
	fmt.Fprintf(sb, "TokenEstimate: %d. ", b.TokenEstimate)
	if b.TokenBudget > 0 {
		fmt.Fprintf(sb, "TokenBudget: %d. ", b.TokenBudget)
	}
	if !b.GeneratedAt.IsZero() {
		fmt.Fprintf(sb, "GeneratedAt: `%s`.", b.GeneratedAt.Format("2006-01-02T15:04:05Z"))
	}
	sb.WriteString("\n")
}

// escapeTableCell removes pipes + newlines from cell content so the
// markdown table doesn't fragment. Title truncation already happens
// upstream (SummarizeBeads), so this only handles characters that
// would corrupt the table layout.
func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
