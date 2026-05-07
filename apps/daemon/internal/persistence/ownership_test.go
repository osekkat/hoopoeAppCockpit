package persistence

import (
	"strings"
	"testing"
)

// TestRegisteredStoresValidate asserts every entry in the canonical
// persistence manifest carries a valid Class, a non-empty Name+Package
// pair, a CanonicalOwner where Class semantics require one, and a
// Rationale defending the classification. This is the load-bearing
// contract test for hp-w2zr — adding a new store without a valid
// classification will fail here.
func TestRegisteredStoresValidate(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("persistence manifest fails validation: %v", err)
	}
}

// TestRegisteredStoresCoverCanonicalOwnedSurfaces pins the manifest
// against drift: at minimum, every owned-class persistent store the
// daemon ships today must appear by name. Adding a new owned store
// without registering it here will fail this test (the developer
// either adds the registration or argues why the new store is
// Cache/ExternalToolArtifact instead).
func TestRegisteredStoresCoverCanonicalOwnedSurfaces(t *testing.T) {
	required := []string{
		"auth.pairing",
		"auth.session",
		"auth.server_secret",
		"approvals.file_store",
		"scheduler.state",
		"jobs.registry",
		"jobs.log_store",
		"onboarding.checkpoints",
		"projects.registry",
		"audit.writer",
	}
	names := make(map[string]struct{}, len(RegisteredStores))
	for _, entry := range RegisteredStores {
		names[entry.Name] = struct{}{}
	}
	for _, name := range required {
		if _, ok := names[name]; !ok {
			t.Errorf("required persistence store %q missing from RegisteredStores manifest", name)
		}
	}
}

// TestReadModelAndExternalArtifactDeclareCanonicalOwner pins plan.md
// §1.1: any persistence row that is NOT Hoopoe-owned has to name the
// upstream canonical owner. If a future contributor classifies a Git
// clone as ReadModel without declaring CanonicalOwner="git (origin)"
// or similar, the source-of-truth boundary becomes ambiguous and the
// drift this manifest exists to prevent re-emerges.
func TestReadModelAndExternalArtifactDeclareCanonicalOwner(t *testing.T) {
	for _, entry := range RegisteredStores {
		if entry.Class != ClassReadModel && entry.Class != ClassExternalToolArtifact {
			continue
		}
		if strings.TrimSpace(entry.CanonicalOwner) == "" {
			t.Errorf("entry %q (Class=%s) is missing CanonicalOwner", entry.Name, entry.Class)
		}
	}
}

// TestByClassFiltersAndSortsDeterministically asserts the helper's
// downstream contract: ByClass returns entries deterministically
// sorted so future drift tests / generators can compare manifests
// across runs without ordering flakes.
func TestByClassFiltersAndSortsDeterministically(t *testing.T) {
	owned := ByClass(ClassOwned)
	if len(owned) == 0 {
		t.Fatal("ByClass(ClassOwned) returned 0 entries; manifest must register at least one owned store")
	}
	for i := 1; i < len(owned); i++ {
		if owned[i-1].Name >= owned[i].Name {
			t.Errorf("ByClass result not sorted: %q before %q", owned[i-1].Name, owned[i].Name)
		}
		if owned[i].Class != ClassOwned {
			t.Errorf("ByClass(ClassOwned) returned entry %q with Class=%s", owned[i].Name, owned[i].Class)
		}
	}
}

// TestUnknownClassRejectedByValidate guards the gate: corrupting the
// manifest with a typo'd Class string (e.g. "owend") must fail
// Validate so drift never sneaks in via copy-paste edits.
func TestUnknownClassRejectedByValidate(t *testing.T) {
	original := RegisteredStores
	defer func() { RegisteredStores = original }()
	RegisteredStores = append([]StoreEntry{}, original...)
	RegisteredStores = append(RegisteredStores, StoreEntry{
		Name:      "synthetic.bad-class",
		Package:   "apps/daemon/internal/synthetic",
		Class:     Class("owend"),
		Rationale: "synthetic test entry — this should fail",
	})
	if err := Validate(); err == nil {
		t.Fatal("Validate() accepted a manifest entry with Class=\"owend\"; must reject typos")
	}
}
