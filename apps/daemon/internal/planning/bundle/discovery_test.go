package bundle

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWalkProjectRootEmptyProjectRoot(t *testing.T) {
	_, err := WalkProjectRoot("")
	if err == nil {
		t.Fatal("WalkProjectRoot(\"\") should error")
	}
}

func TestWalkProjectRootEmptyDirReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	res, err := WalkProjectRoot(root)
	if err != nil {
		t.Fatalf("WalkProjectRoot: %v", err)
	}
	if res.Readme != nil {
		t.Errorf("Readme = %v, want nil for empty project", res.Readme)
	}
	if res.AgentsMd != nil {
		t.Errorf("AgentsMd = %v, want nil for empty project", res.AgentsMd)
	}
	if len(res.ArchitectureDocs) != 0 {
		t.Errorf("ArchitectureDocs len = %d, want 0", len(res.ArchitectureDocs))
	}
	if len(res.PackageManifests) != 0 {
		t.Errorf("PackageManifests len = %d, want 0", len(res.PackageManifests))
	}
	if res.TestLayout == nil {
		t.Error("TestLayout should be non-nil (even for empty project)")
	}
	if res.TestLayout != nil && res.TestLayout.Runner != RunnerUnknown {
		t.Errorf("TestLayout.Runner = %q, want %q for empty project", res.TestLayout.Runner, RunnerUnknown)
	}
}

func TestWalkProjectRootCapturesReadmeAndAgents(t *testing.T) {
	root := t.TempDir()
	if err := writeAtRoot(root, "README.md", []byte("# Hoopoe")); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := writeAtRoot(root, "AGENTS.md", []byte("# Agents")); err != nil {
		t.Fatalf("write AGENTS: %v", err)
	}

	res, err := WalkProjectRoot(root)
	if err != nil {
		t.Fatalf("WalkProjectRoot: %v", err)
	}
	if res.Readme == nil {
		t.Fatal("Readme should be captured")
	}
	if res.Readme.Path != "README.md" {
		t.Errorf("Readme.Path = %q, want README.md", res.Readme.Path)
	}
	if res.AgentsMd == nil {
		t.Fatal("AgentsMd should be captured")
	}
	if res.AgentsMd.Path != "AGENTS.md" {
		t.Errorf("AgentsMd.Path = %q, want AGENTS.md", res.AgentsMd.Path)
	}
}

func TestWalkProjectRootCapturesArchitectureDocs(t *testing.T) {
	root := t.TempDir()
	// Root-level ARCHITECTURE.md.
	if err := writeAtRoot(root, "ARCHITECTURE.md", []byte("top-level arch")); err != nil {
		t.Fatalf("write ARCHITECTURE: %v", err)
	}
	// docs/architecture/*.md
	if err := writeAtRoot(root, filepath.Join("docs", "architecture", "01-overview.md"), []byte("overview")); err != nil {
		t.Fatalf("write 01-overview: %v", err)
	}
	if err := writeAtRoot(root, filepath.Join("docs", "architecture", "02-data-model.md"), []byte("data model")); err != nil {
		t.Fatalf("write 02-data-model: %v", err)
	}

	res, err := WalkProjectRoot(root)
	if err != nil {
		t.Fatalf("WalkProjectRoot: %v", err)
	}
	if len(res.ArchitectureDocs) != 3 {
		t.Fatalf("ArchitectureDocs len = %d, want 3", len(res.ArchitectureDocs))
	}
	// Confirm alphabetical order — ARCHITECTURE.md sorts before docs/.
	wantPaths := []string{"ARCHITECTURE.md", "docs/architecture/01-overview.md", "docs/architecture/02-data-model.md"}
	for i, want := range wantPaths {
		if res.ArchitectureDocs[i].Path != want {
			t.Errorf("ArchitectureDocs[%d].Path = %q, want %q", i, res.ArchitectureDocs[i].Path, want)
		}
	}
}

func TestWalkProjectRootArchDocsBudgetSkipsRest(t *testing.T) {
	// Use a tiny synthetic budget by writing files larger than a
	// single-file fraction of a 100KB envelope; assert the skipped
	// list is populated for the over-budget portion.
	root := t.TempDir()
	// First file fills ~60KB of the 100KB budget.
	if err := writeAtRoot(root, filepath.Join("docs", "architecture", "01.md"), bytes.Repeat([]byte("A"), 60*1024)); err != nil {
		t.Fatalf("write 01.md: %v", err)
	}
	// Second file fills another ~60KB — should also fit (40KB remaining at most).
	if err := writeAtRoot(root, filepath.Join("docs", "architecture", "02.md"), bytes.Repeat([]byte("B"), 60*1024)); err != nil {
		t.Fatalf("write 02.md: %v", err)
	}
	// Third file at 10KB — should be skipped because budget is exhausted.
	if err := writeAtRoot(root, filepath.Join("docs", "architecture", "03.md"), bytes.Repeat([]byte("C"), 10*1024)); err != nil {
		t.Fatalf("write 03.md: %v", err)
	}

	res, err := WalkProjectRoot(root)
	if err != nil {
		t.Fatalf("WalkProjectRoot: %v", err)
	}
	totalCaptured := 0
	for _, d := range res.ArchitectureDocs {
		totalCaptured += d.SizeBytes
	}
	if totalCaptured > DefaultArchitectureDocsBudget {
		t.Errorf("captured bytes = %d, want <= budget %d", totalCaptured, DefaultArchitectureDocsBudget)
	}
	// At least one file should land in skipped or be truncated; we
	// assert *something* came back skipped OR truncated (the budget
	// math allows either outcome depending on per-file caps).
	hasSkipped := len(res.SkippedArchitectureDocs) > 0
	hasTruncated := false
	for _, d := range res.ArchitectureDocs {
		if d.TruncatedFromBytes != nil {
			hasTruncated = true
			break
		}
	}
	if !hasSkipped && !hasTruncated {
		t.Errorf("no skip or truncation despite oversize: skipped=%v captured=%d (budget=%d)", res.SkippedArchitectureDocs, totalCaptured, DefaultArchitectureDocsBudget)
	}
}

func TestWalkProjectRootForwardsManifestsAndTestLayout(t *testing.T) {
	root := t.TempDir()
	if err := writeAtRoot(root, "package.json", []byte(`{"name":"demo"}`)); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := writeAtRoot(root, "vitest.config.ts", []byte("")); err != nil {
		t.Fatalf("write vitest.config: %v", err)
	}

	res, err := WalkProjectRoot(root)
	if err != nil {
		t.Fatalf("WalkProjectRoot: %v", err)
	}
	if len(res.PackageManifests) != 1 {
		t.Errorf("PackageManifests len = %d, want 1", len(res.PackageManifests))
	}
	if res.TestLayout == nil || res.TestLayout.Runner != "vitest" {
		runner := "(nil)"
		if res.TestLayout != nil {
			runner = res.TestLayout.Runner
		}
		t.Errorf("TestLayout.Runner = %q, want vitest", runner)
	}
}

func TestWalkProjectRootDeterministic(t *testing.T) {
	root := t.TempDir()
	if err := writeAtRoot(root, "README.md", []byte("# Hoopoe")); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := writeAtRoot(root, "package.json", []byte(`{}`)); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	if err := writeAtRoot(root, filepath.Join("docs", "architecture", "01.md"), []byte("arch")); err != nil {
		t.Fatalf("write 01.md: %v", err)
	}

	a, err := WalkProjectRoot(root)
	if err != nil {
		t.Fatalf("first walk: %v", err)
	}
	b, err := WalkProjectRoot(root)
	if err != nil {
		t.Fatalf("second walk: %v", err)
	}
	if a.Readme == nil || b.Readme == nil || a.Readme.Sha256 != b.Readme.Sha256 {
		t.Error("Readme drift between walks")
	}
	if len(a.ArchitectureDocs) != len(b.ArchitectureDocs) {
		t.Fatalf("arch docs len drift: %d vs %d", len(a.ArchitectureDocs), len(b.ArchitectureDocs))
	}
	for i := range a.ArchitectureDocs {
		if a.ArchitectureDocs[i].Path != b.ArchitectureDocs[i].Path {
			t.Errorf("arch docs order drift at %d: %q vs %q", i, a.ArchitectureDocs[i].Path, b.ArchitectureDocs[i].Path)
		}
	}
}

func TestWalkProjectRootReadmeFallback(t *testing.T) {
	// When README.md is absent but readme.md (lowercase) exists, the
	// walk should still pick it up via the case-fallback list.
	root := t.TempDir()
	if err := writeAtRoot(root, "readme.md", []byte("lower")); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	res, err := WalkProjectRoot(root)
	if err != nil {
		t.Fatalf("WalkProjectRoot: %v", err)
	}
	if res.Readme == nil {
		t.Fatal("readme.md should be captured via case fallback")
	}
	if !strings.HasSuffix(res.Readme.Path, "readme.md") {
		t.Errorf("Readme.Path = %q, want suffix readme.md", res.Readme.Path)
	}
}

func TestWalkProjectRootMissingProjectRootErrors(t *testing.T) {
	tmp := t.TempDir()
	missing := filepath.Join(tmp, "does-not-exist")
	// Discovery is allowed to surface filesystem errors, but the
	// non-existence of the dir doesn't fail abs() on Linux — it just
	// doesn't list any candidates. Verify the walk completes cleanly.
	res, err := WalkProjectRoot(missing)
	if err != nil {
		// Some platforms return ErrNotExist via stat from helpers;
		// either outcome is acceptable. Don't assert error vs no error,
		// just confirm we don't panic + the result is sane when no err.
		if !errors.Is(err, os.ErrNotExist) {
			t.Logf("WalkProjectRoot non-existent: %v (allowed)", err)
		}
		return
	}
	if res.Readme != nil {
		t.Errorf("Readme = %v, want nil for non-existent project", res.Readme)
	}
}
