package release

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestVerifierAcceptsCompleteReleaseBundle(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)

	result, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if result.Status != StatusVerified || result.DiagnosticsBanner != "" {
		t.Fatalf("result = %+v", result)
	}
	record := result.Inventory
	if record.Status != StatusVerified || record.Checksum != fixture.digest || record.SBOMDigest != DigestBytes(fixture.request.SBOM) {
		t.Fatalf("inventory = %+v", record)
	}
	if record.SigningIdentity != fixture.request.Manifest.Signing.Identity || record.BuilderID != fixture.request.Manifest.Provenance.BuilderID {
		t.Fatalf("provenance metadata not recorded: %+v", record)
	}
	if len(fixture.audit.events) != 0 {
		t.Fatalf("unexpected audit events: %+v", fixture.audit.events)
	}
}

func TestVerifierRefusesChecksumMismatchUnlessOverrideIsAudited(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	fixture.request.Binary = []byte("tampered daemon")

	_, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if !errors.Is(err, ErrOverrideRequired) || !strings.Contains(err.Error(), ErrChecksumMismatch.Error()) {
		t.Fatalf("checksum mismatch err = %v", err)
	}

	overrideAt := time.Unix(200, 0).UTC()
	fixture.request.Override = Override{
		Enabled: true,
		Actor:   "alice@example.com",
		Reason:  "local fixture validation",
		At:      overrideAt,
	}
	result, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("Verify with override: %v", err)
	}
	if result.Status != StatusOverride || result.DiagnosticsBanner != InsecureOverrideBanner {
		t.Fatalf("override result = %+v", result)
	}
	if result.Inventory.Override == nil || result.Inventory.Override.Actor != "alice@example.com" {
		t.Fatalf("override metadata missing: %+v", result.Inventory)
	}
	if len(fixture.audit.events) != 1 || fixture.audit.events[0].Action != ActionVerificationOverride {
		t.Fatalf("audit events = %+v", fixture.audit.events)
	}
	if !fixture.audit.events[0].Timestamp.Equal(overrideAt) {
		t.Fatalf("audit timestamp = %s", fixture.audit.events[0].Timestamp)
	}
}

func TestVerifierRefusesOverrideWhenAuditFails(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	fixture.request.Binary = []byte("tampered daemon")
	fixture.request.Override = Override{Enabled: true, Actor: "dev", Reason: "test"}
	fixture.audit.err = errors.New("disk full")

	_, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if !errors.Is(err, ErrOverrideAuditFailure) {
		t.Fatalf("override audit err = %v", err)
	}
}

func TestVerifierRefusesInvalidSignature(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	fixture.request.Signature[0] ^= 0xff

	_, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if !errors.Is(err, ErrOverrideRequired) || !strings.Contains(err.Error(), ErrSignatureInvalid.Error()) {
		t.Fatalf("signature err = %v", err)
	}
}

func TestVerifierSupportsTrustedKeyRotationHistory(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	oldPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	fixture.verifier.TrustedKeys = append([]TrustedKey{{
		KeyID:    "old-release-key",
		Identity: fixture.request.Manifest.Signing.Identity,
		Public:   oldPub,
	}}, fixture.verifier.TrustedKeys...)

	if _, err := fixture.verifier.Verify(context.Background(), fixture.request); err != nil {
		t.Fatalf("Verify with key history: %v", err)
	}
}

func TestVerifierRefusesAttestationBuilderMismatch(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	var attestation Attestation
	mustJSON(t, fixture.request.Attestation, &attestation)
	attestation.BuilderID = "github-actions://attacker/workflow"
	fixture.request.Attestation = mustMarshal(t, attestation)

	_, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if !errors.Is(err, ErrOverrideRequired) || !strings.Contains(err.Error(), ErrAttestationInvalid.Error()) {
		t.Fatalf("attestation err = %v", err)
	}
}

func TestVerifierRefusesMalformedSBOM(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	fixture.request.SBOM = []byte(`{"schemaVersion":1,"packages":[]}`)

	_, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if !errors.Is(err, ErrOverrideRequired) || !strings.Contains(err.Error(), ErrSBOMInvalid.Error()) {
		t.Fatalf("sbom err = %v", err)
	}
}

func TestVerifierRefusesInvalidCVEDatabase(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	fixture.request.CVEDatabase = CVEDatabase{
		SchemaVersion: 99,
		Records: []Vulnerability{{
			ID:       "CVE-2026-0001",
			Package:  "modernc.org/sqlite",
			Severity: SeverityHigh,
		}},
	}

	_, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if !errors.Is(err, ErrSBOMInvalid) {
		t.Fatalf("cve db err = %v", err)
	}
}

func TestVerifierRequiresSBOMAcknowledgementForBlockingCVEs(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	fixture.request.CVEDatabase = CVEDatabase{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   "2026-05-04T00:00:00Z",
		Records: []Vulnerability{{
			ID:           "CVE-2026-0001",
			Package:      "modernc.org/sqlite",
			Version:      "v1.50.0",
			Severity:     SeverityCritical,
			FixedVersion: "v1.50.1",
		}},
	}

	_, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if !errors.Is(err, ErrSBOMPolicy) {
		t.Fatalf("sbom policy err = %v", err)
	}

	ackAt := time.Unix(300, 0).UTC()
	fixture.request.SBOMAcknowledgement = SBOMAcknowledgement{
		Accepted: true,
		Actor:    "operator",
		Reason:   "air-gapped fixture",
		At:       ackAt,
	}
	result, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("Verify with ack: %v", err)
	}
	if result.Status != StatusWarning || result.DiagnosticsBanner != SBOMWarningBanner {
		t.Fatalf("warning result = %+v", result)
	}
	if result.Inventory.SBOMPolicy == nil || len(result.Inventory.SBOMPolicy.Findings) != 1 {
		t.Fatalf("sbom policy not recorded: %+v", result.Inventory.SBOMPolicy)
	}
	if len(fixture.audit.events) != 1 || fixture.audit.events[0].Action != ActionSBOMAcknowledged {
		t.Fatalf("audit events = %+v", fixture.audit.events)
	}
	if !fixture.audit.events[0].Timestamp.Equal(ackAt) {
		t.Fatalf("ack timestamp = %s", fixture.audit.events[0].Timestamp)
	}
}

func TestVerifierDedupesEmbeddedAndDatabaseCVEs(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	var sbom SBOM
	mustJSON(t, fixture.request.SBOM, &sbom)
	vuln := Vulnerability{ID: "CVE-2026-0002", Package: "github.com/go-chi/chi/v5", Version: "v5.2.5", Severity: SeverityHigh}
	sbom.Vulnerabilities = append(sbom.Vulnerabilities, vuln)
	fixture.request.SBOM = mustMarshal(t, sbom)
	fixture.request.CVEDatabase = CVEDatabase{SchemaVersion: SchemaVersion, Records: []Vulnerability{vuln}}
	fixture.request.SBOMAcknowledgement = SBOMAcknowledgement{Accepted: true, Actor: "operator", Reason: "known issue"}

	result, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(result.Inventory.SBOMPolicy.Findings) != 1 {
		t.Fatalf("deduped findings = %+v", result.Inventory.SBOMPolicy.Findings)
	}
}

func TestWriteAndLoadInventory(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t)
	result, err := fixture.verifier.Verify(context.Background(), fixture.request)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	path := filepath.Join(t.TempDir(), ".hoopoe", "daemon-inventory.json")
	if err := WriteInventory(path, result.Inventory); err != nil {
		t.Fatalf("WriteInventory: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary inventory file still exists: %v", err)
	}
	got, err := LoadInventory(path)
	if err != nil {
		t.Fatalf("LoadInventory: %v", err)
	}
	if !reflect.DeepEqual(got, result.Inventory) {
		t.Fatalf("loaded inventory = %+v, want %+v", got, result.Inventory)
	}
}

type fixture struct {
	request  VerifyRequest
	verifier Verifier
	audit    *recordingAudit
	digest   string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	now := time.Unix(100, 0).UTC()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	binary := []byte("hoopoe daemon fixture binary")
	digest := DigestBytes(binary)
	requireReproducible := true
	manifest := Manifest{
		SchemaVersion: SchemaVersion,
		Version:       "0.2.0",
		Channel:       "stable",
		Artifact: ArtifactManifest{
			Name:   "hoopoed-darwin-arm64",
			SHA256: digest,
		},
		SourceCommit: "abc123def456",
		Signing: SigningManifest{
			KeyID:     "release-key-2026-05",
			Identity:  "Hoopoe daemon release key",
			Algorithm: SignatureAlgorithmEd25519,
		},
		Provenance: ProvenancePolicy{
			BuilderID:           "github-actions://hoopoe/release/.github/workflows/daemon-release.yml",
			WorkflowRef:         "refs/tags/v0.2.0",
			WorkflowPath:        ".github/workflows/daemon-release.yml",
			MinSLSALevel:        DefaultMinimumSLSALevel,
			RequireReproducible: &requireReproducible,
		},
		Compatibility: Compatibility{
			MinDesktopVersion: "0.2.0",
			MinAPIVersion:     "2026-05-04",
		},
	}
	attestation := Attestation{
		PredicateType: "https://slsa.dev/provenance/v1",
		Subjects: []Subject{{
			Name:   manifest.Artifact.Name,
			SHA256: digest,
		}},
		SourceCommit: manifest.SourceCommit,
		BuilderID:    manifest.Provenance.BuilderID,
		WorkflowRef:  manifest.Provenance.WorkflowRef,
		WorkflowPath: manifest.Provenance.WorkflowPath,
		SLSALevel:    DefaultMinimumSLSALevel,
		Reproducible: true,
	}
	sbom := SBOM{
		SchemaVersion: SchemaVersion,
		Format:        "hoopoe-sbom-v1",
		Packages: []SBOMPackage{
			{Name: "github.com/go-chi/chi/v5", Version: "v5.2.5"},
			{Name: "modernc.org/sqlite", Version: "v1.50.0"},
		},
	}
	audit := &recordingAudit{}
	req := VerifyRequest{
		Manifest:    manifest,
		Binary:      binary,
		Signature:   ed25519.Sign(private, signaturePayload(manifest, digest)),
		Attestation: mustMarshal(t, attestation),
		SBOM:        mustMarshal(t, sbom),
		CVEDatabase: CVEDatabase{SchemaVersion: SchemaVersion},
	}
	return fixture{
		request: req,
		verifier: Verifier{
			TrustedKeys: []TrustedKey{{
				KeyID:    manifest.Signing.KeyID,
				Identity: manifest.Signing.Identity,
				Public:   public,
			}},
			Audit: audit,
			Now:   func() time.Time { return now },
		},
		audit:  audit,
		digest: digest,
	}
}

type recordingAudit struct {
	events []AuditEvent
	err    error
}

func (a *recordingAudit) AppendReleaseAudit(_ context.Context, event AuditEvent) error {
	if a.err != nil {
		return a.err
	}
	a.events = append(a.events, event)
	return nil
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return data
}

func mustJSON(t *testing.T, data []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
}
