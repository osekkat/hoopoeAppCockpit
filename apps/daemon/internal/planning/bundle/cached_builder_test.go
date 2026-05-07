package bundle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupCachedBuilderProject(t *testing.T) string {
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
	must("package.json", []byte(`{"name":"demo"}`))
	return root
}

func TestNewCachedBuilderRejectsNilOrchestrator(t *testing.T) {
	_, err := NewCachedBuilder(nil, NewBundleCache())
	if !errors.Is(err, ErrNilOrchestrator) {
		t.Errorf("err = %v, want ErrNilOrchestrator", err)
	}
}

func TestNewCachedBuilderRejectsNilCache(t *testing.T) {
	_, err := NewCachedBuilder(NewAssemblyOrchestrator(), nil)
	if err == nil {
		t.Fatal("nil cache should error")
	}
}

func TestCachedBuilderFirstCallMissThenStore(t *testing.T) {
	root := setupCachedBuilderProject(t)
	cache := NewBundleCache()
	cb, err := NewCachedBuilder(NewAssemblyOrchestrator(), cache)
	if err != nil {
		t.Fatalf("NewCachedBuilder: %v", err)
	}

	opts := BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: root,
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 100_000,
	}

	bundle, err := cb.Build(context.Background(), opts, AssemblyInput{})
	if err != nil {
		t.Fatalf("Build first: %v", err)
	}
	if bundle == nil {
		t.Fatal("bundle nil")
	}
	stats := cache.Stats()
	if stats.Misses != 1 {
		t.Errorf("Misses = %d, want 1 (first call should miss)", stats.Misses)
	}
	if stats.Stores != 1 {
		t.Errorf("Stores = %d, want 1 (first call should store)", stats.Stores)
	}
}

func TestCachedBuilderSecondCallHits(t *testing.T) {
	root := setupCachedBuilderProject(t)
	cache := NewBundleCache()
	cb, _ := NewCachedBuilder(NewAssemblyOrchestrator(), cache)

	opts := BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: root,
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 100_000,
	}

	first, err := cb.Build(context.Background(), opts, AssemblyInput{})
	if err != nil {
		t.Fatalf("Build first: %v", err)
	}
	second, err := cb.Build(context.Background(), opts, AssemblyInput{})
	if err != nil {
		t.Fatalf("Build second: %v", err)
	}
	if first != second {
		t.Errorf("second Build returned different pointer; cache miss: %p vs %p", first, second)
	}
	stats := cache.Stats()
	if stats.Hits != 1 {
		t.Errorf("Hits = %d, want 1", stats.Hits)
	}
}

func TestCachedBuilderDifferentCommitProducesMiss(t *testing.T) {
	root := setupCachedBuilderProject(t)
	cache := NewBundleCache()
	cb, _ := NewCachedBuilder(NewAssemblyOrchestrator(), cache)

	opts := BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: root,
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 100_000,
	}

	_, err := cb.Build(context.Background(), opts, AssemblyInput{})
	if err != nil {
		t.Fatalf("Build first: %v", err)
	}
	opts.CommitSHA = strings.Repeat("b", 40)
	_, err = cb.Build(context.Background(), opts, AssemblyInput{})
	if err != nil {
		t.Fatalf("Build second: %v", err)
	}
	stats := cache.Stats()
	if stats.Hits != 0 {
		t.Errorf("Hits = %d, want 0 (different CommitSHA should miss)", stats.Hits)
	}
	if stats.Stores != 2 {
		t.Errorf("Stores = %d, want 2", stats.Stores)
	}
}

func TestCachedBuilderDifferentBeadsProducesMiss(t *testing.T) {
	root := setupCachedBuilderProject(t)
	cache := NewBundleCache()
	cb, _ := NewCachedBuilder(NewAssemblyOrchestrator(), cache)

	opts := BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: root,
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 100_000,
	}

	cb.Build(context.Background(), opts, AssemblyInput{Beads: []RawBead{{Id: "hp-a", IssueType: "task"}}})
	cb.Build(context.Background(), opts, AssemblyInput{Beads: []RawBead{{Id: "hp-b", IssueType: "task"}}})
	stats := cache.Stats()
	if stats.Hits != 0 {
		t.Errorf("Hits = %d, want 0 (different beads should miss)", stats.Hits)
	}
}

func TestCachedBuilderSameBeadsDifferentOrderHits(t *testing.T) {
	// preBuildContentHash sorts bead IDs, so the same bead set in
	// different order must hit.
	root := setupCachedBuilderProject(t)
	cache := NewBundleCache()
	cb, _ := NewCachedBuilder(NewAssemblyOrchestrator(), cache)

	opts := BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: root,
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 100_000,
	}

	cb.Build(context.Background(), opts, AssemblyInput{
		Beads: []RawBead{
			{Id: "hp-a", IssueType: "task"},
			{Id: "hp-b", IssueType: "task"},
		},
	})
	cb.Build(context.Background(), opts, AssemblyInput{
		Beads: []RawBead{
			{Id: "hp-b", IssueType: "task"},
			{Id: "hp-a", IssueType: "task"},
		},
	})
	stats := cache.Stats()
	if stats.Hits != 1 {
		t.Errorf("Hits = %d, want 1 (sorted-id hash should treat the two as equal)", stats.Hits)
	}
}

func TestCachedBuilderInvalidOptsRejected(t *testing.T) {
	cb, _ := NewCachedBuilder(NewAssemblyOrchestrator(), NewBundleCache())
	_, err := cb.Build(context.Background(), BuildOpts{}, AssemblyInput{})
	if !errors.Is(err, ErrInvalidOpts) {
		t.Errorf("err = %v, want ErrInvalidOpts", err)
	}
}

func TestCachedBuilderInvalidCommitSHARejected(t *testing.T) {
	cb, _ := NewCachedBuilder(NewAssemblyOrchestrator(), NewBundleCache())
	opts := BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: t.TempDir(),
		CommitSHA:   "short",
		TokenBudget: 100_000,
	}
	_, err := cb.Build(context.Background(), opts, AssemblyInput{})
	if !errors.Is(err, ErrInvalidOpts) {
		t.Errorf("err = %v, want ErrInvalidOpts", err)
	}
}

func TestCachedBuilderCustomPolicyDifferentFingerprintMisses(t *testing.T) {
	root := setupCachedBuilderProject(t)
	cache := NewBundleCache()
	cb, _ := NewCachedBuilder(NewAssemblyOrchestrator(), cache)

	opts := BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: root,
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 100_000,
	}

	policyA := DefaultPolicy()
	policyB := DefaultPolicy()
	policyB.ExcludePatterns = append(policyB.ExcludePatterns, "*.proto")

	cb.Build(context.Background(), opts, AssemblyInput{Policy: &policyA})
	cb.Build(context.Background(), opts, AssemblyInput{Policy: &policyB})
	stats := cache.Stats()
	if stats.Hits != 0 {
		t.Errorf("Hits = %d, want 0 (different policy fingerprint should miss)", stats.Hits)
	}
}

func TestCachedBuilderHitDoesNotInvokeOrchestrator(t *testing.T) {
	// Verify hit path skips the orchestrator: stub the cache with a
	// pre-existing entry under the expected key, then call Build.
	root := setupCachedBuilderProject(t)
	cache := NewBundleCache()

	opts := BuildOpts{
		ProjectID:   "demo",
		ProjectRoot: root,
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 100_000,
	}
	preHash := preBuildContentHash(opts, AssemblyInput{})
	key, _ := ComputeCacheKey(opts, preHash)

	preStored := validBundle()
	preStored.ProjectId = "from-cache" // distinguishable
	if err := cache.Put(key, preStored); err != nil {
		t.Fatalf("Put: %v", err)
	}

	cb, _ := NewCachedBuilder(NewAssemblyOrchestrator(), cache)
	got, err := cb.Build(context.Background(), opts, AssemblyInput{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got.ProjectId != "from-cache" {
		t.Errorf("ProjectId = %q, want from-cache (orchestrator should not have been called)", got.ProjectId)
	}
}

func TestPreBuildContentHashDeterministic(t *testing.T) {
	opts := BuildOpts{
		ProjectID:   "x",
		ProjectRoot: "/tmp/x",
		CommitSHA:   strings.Repeat("a", 40),
		TokenBudget: 8_000,
	}
	a := preBuildContentHash(opts, AssemblyInput{})
	b := preBuildContentHash(opts, AssemblyInput{})
	if a != b {
		t.Errorf("preBuildContentHash not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("hash length = %d, want 64", len(a))
	}
}

func TestPolicyFingerprintIgnoresOrder(t *testing.T) {
	a := Policy{
		ExcludePatterns:    []string{"a", "b", "c"},
		ExcludeDirPrefixes: []string{"x/", "y/"},
	}
	b := Policy{
		ExcludePatterns:    []string{"c", "a", "b"},
		ExcludeDirPrefixes: []string{"y/", "x/"},
	}
	if policyFingerprint(&a) != policyFingerprint(&b) {
		t.Error("policy fingerprint should ignore insertion order")
	}
}
