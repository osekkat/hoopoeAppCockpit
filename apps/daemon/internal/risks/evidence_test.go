package risks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSection14CatalogIsReleaseSmokeReady(t *testing.T) {
	catalog := Section14Catalog()
	if err := ValidateCatalog(catalog); err != nil {
		t.Fatalf("validate catalog: %v", err)
	}
	for idx, risk := range catalog {
		wantID := idx + 1
		if risk.ID != wantID {
			t.Fatalf("catalog order id = %d at index %d, want %d", risk.ID, idx, wantID)
		}
	}
}

func TestSection14EvidencePathsExist(t *testing.T) {
	root := repoRoot(t)
	for _, risk := range Section14Catalog() {
		for _, ref := range risk.AcceptanceRefs {
			path := filepath.Join(root, filepath.FromSlash(ref.Path))
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("risk %02d evidence path %s: %v", risk.ID, ref.Path, err)
			}
		}
	}
}

func TestDocsRisksMirrorsSection14Catalog(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "docs", "risks.md"))
	if err != nil {
		t.Fatalf("read docs/risks.md: %v", err)
	}
	text := string(data)
	for _, risk := range Section14Catalog() {
		marker := riskMarker(risk.ID)
		for _, want := range []string{marker, risk.Title, risk.Owner} {
			if !strings.Contains(text, want) {
				t.Fatalf("docs/risks.md missing %q for risk %02d", want, risk.ID)
			}
		}
		for _, bead := range risk.Beads {
			if !strings.Contains(text, bead) {
				t.Fatalf("docs/risks.md missing bead %q for risk %02d", bead, risk.ID)
			}
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func riskMarker(id int) string {
	return fmt.Sprintf("RISK-%02d", id)
}
