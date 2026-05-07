package bundle

import (
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func sampleBundle() schemas.ExistingCodebaseContextBundle {
	return schemas.ExistingCodebaseContextBundle{
		SchemaVersion: 1,
		ProjectId:     "demo",
		CommitSha:     "0123456789abcdef0123456789abcdef01234567",
		GeneratedAt:   time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
		ContentHash:   "placeholder-not-real-hash",
		Readme: &schemas.FileSnapshot{
			Path:       "README.md",
			Sha256:     "0000000000000000000000000000000000000000000000000000000000000000",
			SizeBytes:  0,
			ContentB64: "",
		},
		ArchitectureDocs: []schemas.FileSnapshot{},
		PackageManifests: []schemas.ManifestSnapshot{},
		ExistingBeads:    []schemas.BeadSummary{},
		HealthHotspots:   []schemas.HotspotSummary{},
		Excluded:         []string{},
		Redactions:       []schemas.RedactionEntry{},
		TokenEstimate:    0,
		TokenBudget:      4096,
	}
}

func TestComputeContentHashReturns64HexChars(t *testing.T) {
	hash, err := ComputeContentHash(sampleBundle())
	if err != nil {
		t.Fatalf("ComputeContentHash: %v", err)
	}
	if len(hash) != 64 {
		t.Fatalf("hash len = %d, want 64", len(hash))
	}
	for i, r := range hash {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			t.Fatalf("hash[%d] = %q, not lowercase hex", i, r)
		}
	}
}

func TestComputeContentHashIsDeterministic(t *testing.T) {
	a, err := ComputeContentHash(sampleBundle())
	if err != nil {
		t.Fatalf("first ComputeContentHash: %v", err)
	}
	b, err := ComputeContentHash(sampleBundle())
	if err != nil {
		t.Fatalf("second ComputeContentHash: %v", err)
	}
	if a != b {
		t.Fatalf("hash differs between calls: %s vs %s", a, b)
	}
}

func TestComputeContentHashIgnoresGeneratedAt(t *testing.T) {
	a := sampleBundle()
	a.GeneratedAt = time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	b := sampleBundle()
	b.GeneratedAt = time.Date(2030, 12, 31, 23, 59, 59, 0, time.UTC)
	hashA, err := ComputeContentHash(a)
	if err != nil {
		t.Fatalf("ComputeContentHash(a): %v", err)
	}
	hashB, err := ComputeContentHash(b)
	if err != nil {
		t.Fatalf("ComputeContentHash(b): %v", err)
	}
	if hashA != hashB {
		t.Fatalf("hash sensitive to GeneratedAt: %s vs %s", hashA, hashB)
	}
}

func TestComputeContentHashIgnoresPriorContentHash(t *testing.T) {
	a := sampleBundle()
	a.ContentHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	b := sampleBundle()
	b.ContentHash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	hashA, err := ComputeContentHash(a)
	if err != nil {
		t.Fatalf("ComputeContentHash(a): %v", err)
	}
	hashB, err := ComputeContentHash(b)
	if err != nil {
		t.Fatalf("ComputeContentHash(b): %v", err)
	}
	if hashA != hashB {
		t.Fatalf("hash sensitive to its own ContentHash: %s vs %s", hashA, hashB)
	}
}

func TestComputeContentHashChangesWhenContentChanges(t *testing.T) {
	base := sampleBundle()
	hash1, err := ComputeContentHash(base)
	if err != nil {
		t.Fatalf("baseline hash: %v", err)
	}

	mutated := sampleBundle()
	mutated.ProjectId = "different-demo"
	hash2, err := ComputeContentHash(mutated)
	if err != nil {
		t.Fatalf("mutated hash: %v", err)
	}

	if hash1 == hash2 {
		t.Fatalf("hash unchanged after ProjectId mutation: %s", hash1)
	}

	mutatedSha := sampleBundle()
	mutatedSha.CommitSha = "fedcba9876543210fedcba9876543210fedcba98"
	hash3, err := ComputeContentHash(mutatedSha)
	if err != nil {
		t.Fatalf("commit-sha hash: %v", err)
	}
	if hash1 == hash3 {
		t.Fatalf("hash unchanged after CommitSha mutation: %s", hash1)
	}
}

func TestComputeContentHashHandlesNestedFileSnapshot(t *testing.T) {
	a := sampleBundle()
	if a.Readme == nil {
		t.Fatal("sampleBundle missing Readme")
	}
	original := *a.Readme
	hash1, err := ComputeContentHash(a)
	if err != nil {
		t.Fatalf("baseline hash: %v", err)
	}

	b := sampleBundle()
	mutated := original
	mutated.Path = "README.rst"
	b.Readme = &mutated
	hash2, err := ComputeContentHash(b)
	if err != nil {
		t.Fatalf("mutated hash: %v", err)
	}
	if hash1 == hash2 {
		t.Fatalf("hash unchanged after nested Readme.Path mutation")
	}
}

func TestMustComputeContentHashReturnsConsistentValue(t *testing.T) {
	hash := MustComputeContentHash(sampleBundle())
	if len(hash) != 64 {
		t.Fatalf("hash len = %d, want 64", len(hash))
	}
	if !strings.ContainsAny(hash, "0123456789abcdef") {
		t.Fatalf("hash %q has no hex chars", hash)
	}
}
