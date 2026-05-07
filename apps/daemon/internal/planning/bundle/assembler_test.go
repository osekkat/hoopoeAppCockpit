package bundle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupSyntheticProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	must := func(rel string, content []byte) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, content, 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	must("README.md", []byte("# Demo"))
	must("AGENTS.md", []byte("# Agents"))
	must("docs/architecture/01.md", []byte("arch overview"))
	must("package.json", []byte(`{"name":"demo"}`))
	must("vitest.config.ts", []byte(""))
	// Secret-suggestive path that ApplyPolicy must reject.
	must(".env", []byte("SECRET=xxx"))
	return root
}

func fixedClockOrchestrator(at time.Time) *AssemblyOrchestrator {
	return &AssemblyOrchestrator{clock: func() time.Time { return at }}
}

func validBuildOpts(root string) BuildOpts {
	return BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: root,
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 100_000,
	}
}

func TestAssemblyOrchestratorBuildEndToEnd(t *testing.T) {
	root := setupSyntheticProject(t)
	o := fixedClockOrchestrator(time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC))

	bundle, err := o.Build(context.Background(), validBuildOpts(root), AssemblyInput{
		Beads: []RawBead{
			{Id: "hp-a", Title: "demo bead", IssueType: "task", Priority: 1},
		},
		Hotspots: []RawHotspot{
			{Path: "src/foo.ts", CompositeScore: 50, Language: "TypeScript"},
		},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if bundle.Readme == nil || !strings.HasSuffix(bundle.Readme.Path, "README.md") {
		t.Errorf("Readme not captured: %v", bundle.Readme)
	}
	if bundle.AgentsMd == nil {
		t.Error("AgentsMd not captured")
	}
	if len(bundle.ArchitectureDocs) != 1 {
		t.Errorf("ArchitectureDocs len = %d, want 1", len(bundle.ArchitectureDocs))
	}
	if len(bundle.PackageManifests) != 1 {
		t.Errorf("PackageManifests len = %d, want 1", len(bundle.PackageManifests))
	}
	if bundle.TestLayout == nil || bundle.TestLayout.Runner != "vitest" {
		t.Errorf("TestLayout.Runner mismatch: %v", bundle.TestLayout)
	}
	if len(bundle.ExistingBeads) != 1 {
		t.Errorf("ExistingBeads len = %d, want 1", len(bundle.ExistingBeads))
	}
	if len(bundle.HealthHotspots) != 1 {
		t.Errorf("HealthHotspots len = %d, want 1", len(bundle.HealthHotspots))
	}
	if bundle.ContentHash == "" {
		t.Error("ContentHash not stamped")
	}
	if bundle.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", bundle.SchemaVersion)
	}
}

func TestAssemblyOrchestratorBuildPolicyDoesNotCaptureDotEnv(t *testing.T) {
	root := setupSyntheticProject(t)
	o := NewAssemblyOrchestrator()
	bundle, err := o.Build(context.Background(), validBuildOpts(root), AssemblyInput{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// .env was written but is not a manifest (it's not in the
	// manifest list), so it shouldn't appear in PackageManifests.
	// Verify nothing in the captured bundle references it.
	if bundle.Readme != nil && strings.Contains(bundle.Readme.Path, ".env") {
		t.Error(".env smuggled into Readme")
	}
	for _, m := range bundle.PackageManifests {
		if strings.Contains(m.Path, ".env") {
			t.Errorf(".env smuggled into manifest: %v", m)
		}
	}
}

func TestAssemblyOrchestratorBuildContentHashDeterministic(t *testing.T) {
	root := setupSyntheticProject(t)
	o := fixedClockOrchestrator(time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC))

	a, err := o.Build(context.Background(), validBuildOpts(root), AssemblyInput{})
	if err != nil {
		t.Fatalf("Build first: %v", err)
	}
	b, err := o.Build(context.Background(), validBuildOpts(root), AssemblyInput{})
	if err != nil {
		t.Fatalf("Build second: %v", err)
	}
	if a.ContentHash == "" || a.ContentHash != b.ContentHash {
		t.Errorf("ContentHash drift: %q vs %q", a.ContentHash, b.ContentHash)
	}
}

func TestAssemblyOrchestratorBuildContextCancelledRejected(t *testing.T) {
	root := setupSyntheticProject(t)
	o := NewAssemblyOrchestrator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := o.Build(ctx, validBuildOpts(root), AssemblyInput{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestAssemblyOrchestratorBuildInvalidOptsRejected(t *testing.T) {
	o := NewAssemblyOrchestrator()
	_, err := o.Build(context.Background(), BuildOpts{}, AssemblyInput{})
	if !errors.Is(err, ErrInvalidOpts) {
		t.Errorf("err = %v, want ErrInvalidOpts", err)
	}
}

func TestAssemblyOrchestratorBuildBudgetTriggersTruncation(t *testing.T) {
	root := setupSyntheticProject(t)
	o := NewAssemblyOrchestrator()

	opts := validBuildOpts(root)
	opts.TokenBudget = 1 // tiny — forces truncation

	beads := []RawBead{}
	for i := 0; i < 50; i++ {
		beads = append(beads, RawBead{Id: "hp-z", IssueType: "task"})
	}

	bundle, err := o.Build(context.Background(), opts, AssemblyInput{Beads: beads})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if bundle.TokenEstimate > 1 && len(bundle.Excluded) == 0 {
		t.Errorf("budget=1 should have produced excluded markers; got TokenEstimate=%d, Excluded=%v", bundle.TokenEstimate, bundle.Excluded)
	}
}

func TestAssemblyOrchestratorBuildEmptyProject(t *testing.T) {
	root := t.TempDir()
	o := NewAssemblyOrchestrator()
	bundle, err := o.Build(context.Background(), validBuildOpts(root), AssemblyInput{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if bundle.Readme != nil {
		t.Errorf("Readme = %v, want nil for empty project", bundle.Readme)
	}
	if bundle.ContentHash == "" {
		t.Error("ContentHash should still be set for empty bundle")
	}
}

func TestAssemblyOrchestratorBuildPropagatesGeneratedAt(t *testing.T) {
	root := setupSyntheticProject(t)
	at := time.Date(2026, 6, 1, 10, 30, 0, 0, time.UTC)
	o := fixedClockOrchestrator(at)
	bundle, err := o.Build(context.Background(), validBuildOpts(root), AssemblyInput{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !bundle.GeneratedAt.Equal(at) {
		t.Errorf("GeneratedAt = %v, want %v", bundle.GeneratedAt, at)
	}
}

func TestAssemblyOrchestratorBuildCustomPolicyApplied(t *testing.T) {
	root := setupSyntheticProject(t)
	o := NewAssemblyOrchestrator()

	// Custom policy that excludes everything matching `*.md`.
	policy := DefaultPolicy()
	policy.ExcludePatterns = append(policy.ExcludePatterns, "*.md")

	bundle, err := o.Build(context.Background(), validBuildOpts(root), AssemblyInput{
		Policy: &policy,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if bundle.Readme != nil {
		t.Errorf("Readme should be excluded under custom *.md policy: %v", bundle.Readme)
	}
	if bundle.AgentsMd != nil {
		t.Errorf("AgentsMd should be excluded under custom *.md policy")
	}
	if len(bundle.ArchitectureDocs) != 0 {
		t.Errorf("ArchitectureDocs should be excluded under custom *.md policy: %d", len(bundle.ArchitectureDocs))
	}
	// Verify the excluded list captured the rejection.
	hasReadmeExclusion := false
	for _, e := range bundle.Excluded {
		if strings.Contains(e, "README.md") && strings.Contains(e, "pattern:*.md") {
			hasReadmeExclusion = true
			break
		}
	}
	if !hasReadmeExclusion {
		t.Errorf("README.md exclusion not recorded in Excluded: %v", bundle.Excluded)
	}
}

func TestAssemblyOrchestratorBuildEmptyAdapterInputs(t *testing.T) {
	root := setupSyntheticProject(t)
	o := NewAssemblyOrchestrator()
	bundle, err := o.Build(context.Background(), validBuildOpts(root), AssemblyInput{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if bundle.ExistingBeads == nil {
		t.Error("ExistingBeads should be empty slice, not nil")
	}
	if bundle.HealthHotspots == nil {
		t.Error("HealthHotspots should be empty slice, not nil")
	}
}
