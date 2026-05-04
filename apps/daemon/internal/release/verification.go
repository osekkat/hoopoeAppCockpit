// Package release verifies daemon release artifacts before bootstrap or upgrade
// code is allowed to install them.
package release

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	SchemaVersion = 1

	SignatureAlgorithmEd25519 = "ed25519"

	DefaultMinimumSLSALevel = 3

	ActionVerificationOverride = "release.verification.override"
	ActionSBOMAcknowledged     = "release.sbom.acknowledged"

	InsecureOverrideBanner = "Daemon installed with provenance verification BYPASSED. Production use is unsupported."
	SBOMWarningBanner      = "Daemon installed after SBOM vulnerability acknowledgement."
)

var (
	ErrInvalidManifest      = errors.New("release: invalid manifest")
	ErrChecksumMismatch     = errors.New("release: checksum mismatch")
	ErrSignatureInvalid     = errors.New("release: signature invalid")
	ErrAttestationInvalid   = errors.New("release: attestation invalid")
	ErrSBOMInvalid          = errors.New("release: sbom invalid")
	ErrSBOMPolicy           = errors.New("release: sbom policy requires acknowledgement")
	ErrOverrideRequired     = errors.New("release: install refused without insecure override")
	ErrOverrideAuditFailure = errors.New("release: insecure override audit failed")
	ErrInventoryInvalid     = errors.New("release: invalid inventory")
)

type Status string

const (
	StatusVerified Status = "verified"
	StatusWarning  Status = "warning"
	StatusOverride Status = "override"
)

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type Manifest struct {
	SchemaVersion int              `json:"schemaVersion"`
	Version       string           `json:"version"`
	Channel       string           `json:"channel,omitempty"`
	Artifact      ArtifactManifest `json:"artifact"`
	SourceCommit  string           `json:"sourceCommit"`
	Signing       SigningManifest  `json:"signing"`
	Provenance    ProvenancePolicy `json:"provenance"`
	Compatibility Compatibility    `json:"compatibility"`
}

type ArtifactManifest struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type SigningManifest struct {
	KeyID     string `json:"keyId"`
	Identity  string `json:"identity"`
	Algorithm string `json:"algorithm"`
}

type ProvenancePolicy struct {
	BuilderID           string `json:"builderId"`
	WorkflowRef         string `json:"workflowRef"`
	WorkflowPath        string `json:"workflowPath"`
	MinSLSALevel        int    `json:"minSlsaLevel,omitempty"`
	RequireReproducible *bool  `json:"requireReproducible,omitempty"`
}

type Compatibility struct {
	MinDesktopVersion string `json:"minDesktopVersion,omitempty"`
	MinAPIVersion     string `json:"minApiVersion,omitempty"`
}

type TrustedKey struct {
	KeyID    string
	Identity string
	Public   ed25519.PublicKey
}

type Attestation struct {
	PredicateType string    `json:"predicateType"`
	Subjects      []Subject `json:"subjects"`
	SourceCommit  string    `json:"sourceCommit"`
	BuilderID     string    `json:"builderId"`
	WorkflowRef   string    `json:"workflowRef"`
	WorkflowPath  string    `json:"workflowPath"`
	SLSALevel     int       `json:"slsaLevel"`
	Reproducible  bool      `json:"reproducible"`
}

type Subject struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type SBOM struct {
	SchemaVersion   int             `json:"schemaVersion"`
	Format          string          `json:"format,omitempty"`
	Packages        []SBOMPackage   `json:"packages"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities,omitempty"`
}

type SBOMPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	PURL    string `json:"purl,omitempty"`
}

type Vulnerability struct {
	ID           string   `json:"id"`
	Package      string   `json:"package"`
	Version      string   `json:"version,omitempty"`
	Severity     Severity `json:"severity"`
	FixedVersion string   `json:"fixedVersion,omitempty"`
	Source       string   `json:"source,omitempty"`
}

type CVEDatabase struct {
	SchemaVersion int             `json:"schemaVersion"`
	GeneratedAt   string          `json:"generatedAt,omitempty"`
	Records       []Vulnerability `json:"records"`
}

type Override struct {
	Enabled bool
	Actor   string
	Reason  string
	At      time.Time
}

type SBOMAcknowledgement struct {
	Accepted bool
	Actor    string
	Reason   string
	At       time.Time
}

type VerifyRequest struct {
	Manifest            Manifest
	Binary              []byte
	Signature           []byte
	Attestation         []byte
	SBOM                []byte
	CVEDatabase         CVEDatabase
	Override            Override
	SBOMAcknowledgement SBOMAcknowledgement
}

type VerificationResult struct {
	Status            Status          `json:"status"`
	Inventory         InventoryRecord `json:"inventory"`
	Diagnostics       []Diagnostic    `json:"diagnostics,omitempty"`
	DiagnosticsBanner string          `json:"diagnosticsBanner,omitempty"`
}

type Diagnostic struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
}

type InventoryRecord struct {
	SchemaVersion     int             `json:"schemaVersion"`
	VerifiedAt        string          `json:"verifiedAt"`
	Status            Status          `json:"status"`
	Version           string          `json:"version"`
	Channel           string          `json:"channel,omitempty"`
	ArtifactName      string          `json:"artifactName"`
	Checksum          string          `json:"checksum"`
	SBOMDigest        string          `json:"sbomDigest"`
	AttestationDigest string          `json:"attestationDigest"`
	SourceCommit      string          `json:"sourceCommit"`
	BuilderID         string          `json:"builderId"`
	WorkflowRef       string          `json:"workflowRef"`
	WorkflowPath      string          `json:"workflowPath"`
	SLSALevel         int             `json:"slsaLevel"`
	Reproducible      bool            `json:"reproducible"`
	SigningIdentity   string          `json:"signingIdentity"`
	SigningKeyID      string          `json:"signingKeyId"`
	MinDesktopVersion string          `json:"minDesktopVersion,omitempty"`
	MinAPIVersion     string          `json:"minApiVersion,omitempty"`
	Override          *OverrideRecord `json:"override,omitempty"`
	SBOMPolicy        *SBOMPolicy     `json:"sbomPolicy,omitempty"`
	DiagnosticsBanner string          `json:"diagnosticsBanner,omitempty"`
}

type OverrideRecord struct {
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
	At     string `json:"at"`
}

type SBOMPolicy struct {
	AcknowledgedBy string          `json:"acknowledgedBy,omitempty"`
	AcknowledgedAt string          `json:"acknowledgedAt,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	Findings       []Vulnerability `json:"findings,omitempty"`
}

type AuditEvent struct {
	Action    string         `json:"action"`
	Actor     string         `json:"actor"`
	Reason    string         `json:"reason,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Result    string         `json:"result"`
	Data      map[string]any `json:"data,omitempty"`
}

type AuditSink interface {
	AppendReleaseAudit(ctx context.Context, event AuditEvent) error
}

type Verifier struct {
	TrustedKeys []TrustedKey
	Audit       AuditSink
	Now         func() time.Time
}

func (v Verifier) Verify(ctx context.Context, req VerifyRequest) (VerificationResult, error) {
	now := v.now()
	if err := req.Manifest.validate(); err != nil {
		return VerificationResult{}, err
	}
	expectedDigest, err := normalizeDigest(req.Manifest.Artifact.SHA256)
	if err != nil {
		return VerificationResult{}, fmt.Errorf("%w: artifact sha256: %v", ErrInvalidManifest, err)
	}
	actualDigest := DigestBytes(req.Binary)
	var diagnostics []Diagnostic
	var fatal error
	if actualDigest != expectedDigest {
		diagnostics = append(diagnostics, Diagnostic{
			Code:     "checksum_mismatch",
			Severity: SeverityCritical,
			Message:  "daemon binary checksum does not match release manifest",
		})
		fatal = ErrChecksumMismatch
	}
	if err := v.verifySignature(req.Manifest, expectedDigest, req.Signature); err != nil {
		diagnostics = append(diagnostics, Diagnostic{
			Code:     "signature_invalid",
			Severity: SeverityCritical,
			Message:  "daemon binary signature could not be verified against a trusted release key",
		})
		fatal = joinFatal(fatal, err)
	}
	attestation, err := parseAttestation(req.Attestation)
	if err != nil {
		diagnostics = append(diagnostics, Diagnostic{
			Code:     "attestation_invalid",
			Severity: SeverityCritical,
			Message:  "daemon provenance attestation is invalid or incomplete",
		})
		fatal = joinFatal(fatal, err)
	}
	if err == nil {
		if err := verifyAttestation(req.Manifest, attestation, expectedDigest); err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Code:     "attestation_invalid",
				Severity: SeverityCritical,
				Message:  err.Error(),
			})
			fatal = joinFatal(fatal, err)
		}
	}
	sbom, err := parseSBOM(req.SBOM)
	if err != nil {
		diagnostics = append(diagnostics, Diagnostic{
			Code:     "sbom_invalid",
			Severity: SeverityCritical,
			Message:  "daemon SBOM is invalid or lists no dependencies",
		})
		fatal = joinFatal(fatal, err)
	}

	if fatal != nil {
		if !req.Override.valid() {
			return VerificationResult{Diagnostics: diagnostics}, fmt.Errorf("%w: %v", ErrOverrideRequired, fatal)
		}
		if err := v.audit(ctx, AuditEvent{
			Action:    ActionVerificationOverride,
			Actor:     req.Override.Actor,
			Reason:    req.Override.Reason,
			Timestamp: eventTime(req.Override.At, now),
			Result:    "override",
			Data: map[string]any{
				"artifactName": req.Manifest.Artifact.Name,
				"checksum":     actualDigest,
				"failures":     diagnosticCodes(diagnostics),
			},
		}); err != nil {
			return VerificationResult{Diagnostics: diagnostics}, fmt.Errorf("%w: %v", ErrOverrideAuditFailure, err)
		}
		record := inventoryRecord(req.Manifest, attestation, expectedDigest, DigestBytes(req.SBOM), DigestBytes(req.Attestation), now, StatusOverride)
		record.Override = &OverrideRecord{
			Actor:  req.Override.Actor,
			Reason: req.Override.Reason,
			At:     eventTime(req.Override.At, now).UTC().Format(time.RFC3339Nano),
		}
		record.DiagnosticsBanner = InsecureOverrideBanner
		return VerificationResult{
			Status:            StatusOverride,
			Inventory:         record,
			Diagnostics:       append(diagnostics, Diagnostic{Code: "insecure_override", Severity: SeverityCritical, Message: InsecureOverrideBanner}),
			DiagnosticsBanner: InsecureOverrideBanner,
		}, nil
	}

	if err := req.CVEDatabase.validate(); err != nil {
		return VerificationResult{
			Diagnostics: append(diagnostics, Diagnostic{
				Code:     "cve_db_invalid",
				Severity: SeverityCritical,
				Message:  "last-known-good CVE database is invalid",
			}),
		}, err
	}

	findings := policyFindings(sbom, req.CVEDatabase)
	if len(findings) > 0 && !req.SBOMAcknowledgement.valid() {
		return VerificationResult{
			Diagnostics: append(diagnostics, Diagnostic{
				Code:     "sbom_ack_required",
				Severity: maxFindingSeverity(findings),
				Message:  "SBOM vulnerability policy requires explicit user acknowledgement before install",
			}),
		}, ErrSBOMPolicy
	}

	status := StatusVerified
	banner := ""
	if len(findings) > 0 {
		status = StatusWarning
		banner = SBOMWarningBanner
		if err := v.audit(ctx, AuditEvent{
			Action:    ActionSBOMAcknowledged,
			Actor:     req.SBOMAcknowledgement.Actor,
			Reason:    req.SBOMAcknowledgement.Reason,
			Timestamp: eventTime(req.SBOMAcknowledgement.At, now),
			Result:    "acknowledged",
			Data: map[string]any{
				"artifactName":    req.Manifest.Artifact.Name,
				"findingCount":    len(findings),
				"highestSeverity": maxFindingSeverity(findings),
			},
		}); err != nil {
			return VerificationResult{Diagnostics: diagnostics}, fmt.Errorf("release: sbom acknowledgement audit: %w", err)
		}
		diagnostics = append(diagnostics, Diagnostic{Code: "sbom_acknowledged", Severity: maxFindingSeverity(findings), Message: SBOMWarningBanner})
	}

	record := inventoryRecord(req.Manifest, attestation, expectedDigest, DigestBytes(req.SBOM), DigestBytes(req.Attestation), now, status)
	if len(findings) > 0 {
		record.SBOMPolicy = &SBOMPolicy{
			AcknowledgedBy: req.SBOMAcknowledgement.Actor,
			AcknowledgedAt: eventTime(req.SBOMAcknowledgement.At, now).UTC().Format(time.RFC3339Nano),
			Reason:         req.SBOMAcknowledgement.Reason,
			Findings:       findings,
		}
		record.DiagnosticsBanner = banner
	}
	return VerificationResult{
		Status:            status,
		Inventory:         record,
		Diagnostics:       diagnostics,
		DiagnosticsBanner: banner,
	}, nil
}

func WriteInventory(path string, record InventoryRecord) error {
	if err := record.validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("release: inventory mkdir: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("release: inventory encode: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("release: inventory write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("release: inventory rename: %w", err)
	}
	return nil
}

func LoadInventory(path string) (InventoryRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return InventoryRecord{}, fmt.Errorf("release: inventory read: %w", err)
	}
	var record InventoryRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return InventoryRecord{}, fmt.Errorf("release: inventory decode: %w", err)
	}
	if err := record.validate(); err != nil {
		return InventoryRecord{}, err
	}
	return record, nil
}

func DigestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func signaturePayload(manifest Manifest, digest string) []byte {
	return []byte(strings.Join([]string{
		"hoopoe-daemon-release-v1",
		manifest.Artifact.Name,
		digest,
		manifest.SourceCommit,
		manifest.Signing.Identity,
	}, "\n"))
}

func (m Manifest) validate() error {
	if m.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schemaVersion %d != %d", ErrInvalidManifest, m.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("%w: version is required", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Artifact.Name) == "" {
		return fmt.Errorf("%w: artifact name is required", ErrInvalidManifest)
	}
	if _, err := normalizeDigest(m.Artifact.SHA256); err != nil {
		return fmt.Errorf("%w: artifact sha256: %v", ErrInvalidManifest, err)
	}
	if strings.TrimSpace(m.SourceCommit) == "" {
		return fmt.Errorf("%w: source commit is required", ErrInvalidManifest)
	}
	if m.Signing.Algorithm != SignatureAlgorithmEd25519 {
		return fmt.Errorf("%w: unsupported signature algorithm %q", ErrInvalidManifest, m.Signing.Algorithm)
	}
	if strings.TrimSpace(m.Signing.KeyID) == "" || strings.TrimSpace(m.Signing.Identity) == "" {
		return fmt.Errorf("%w: signing key id and identity are required", ErrInvalidManifest)
	}
	if strings.TrimSpace(m.Provenance.BuilderID) == "" || strings.TrimSpace(m.Provenance.WorkflowRef) == "" || strings.TrimSpace(m.Provenance.WorkflowPath) == "" {
		return fmt.Errorf("%w: provenance builder and workflow are required", ErrInvalidManifest)
	}
	return nil
}

func (r InventoryRecord) validate() error {
	if r.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schemaVersion %d != %d", ErrInventoryInvalid, r.SchemaVersion, SchemaVersion)
	}
	if r.Status != StatusVerified && r.Status != StatusWarning && r.Status != StatusOverride {
		return fmt.Errorf("%w: invalid status %q", ErrInventoryInvalid, r.Status)
	}
	if strings.TrimSpace(r.VerifiedAt) == "" || strings.TrimSpace(r.Version) == "" || strings.TrimSpace(r.ArtifactName) == "" {
		return fmt.Errorf("%w: missing required release metadata", ErrInventoryInvalid)
	}
	if _, err := normalizeDigest(r.Checksum); err != nil {
		return fmt.Errorf("%w: checksum: %v", ErrInventoryInvalid, err)
	}
	if _, err := normalizeDigest(r.SBOMDigest); err != nil {
		return fmt.Errorf("%w: sbom digest: %v", ErrInventoryInvalid, err)
	}
	if _, err := normalizeDigest(r.AttestationDigest); err != nil {
		return fmt.Errorf("%w: attestation digest: %v", ErrInventoryInvalid, err)
	}
	return nil
}

func (v Verifier) verifySignature(manifest Manifest, digest string, signature []byte) error {
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("%w: signature size = %d", ErrSignatureInvalid, len(signature))
	}
	for _, key := range v.TrustedKeys {
		if key.KeyID != manifest.Signing.KeyID || key.Identity != manifest.Signing.Identity {
			continue
		}
		if len(key.Public) != ed25519.PublicKeySize {
			return fmt.Errorf("%w: trusted key %s has invalid public key", ErrSignatureInvalid, key.KeyID)
		}
		if ed25519.Verify(key.Public, signaturePayload(manifest, digest), signature) {
			return nil
		}
		return fmt.Errorf("%w: key %s did not verify payload", ErrSignatureInvalid, key.KeyID)
	}
	return fmt.Errorf("%w: trusted key %s for %q not found", ErrSignatureInvalid, manifest.Signing.KeyID, manifest.Signing.Identity)
}

func parseAttestation(data []byte) (Attestation, error) {
	var attestation Attestation
	if err := json.Unmarshal(data, &attestation); err != nil {
		return Attestation{}, fmt.Errorf("%w: decode: %v", ErrAttestationInvalid, err)
	}
	if strings.TrimSpace(attestation.PredicateType) == "" || len(attestation.Subjects) == 0 {
		return Attestation{}, fmt.Errorf("%w: predicate type and subjects are required", ErrAttestationInvalid)
	}
	return attestation, nil
}

func verifyAttestation(manifest Manifest, attestation Attestation, digest string) error {
	if attestation.SourceCommit != manifest.SourceCommit {
		return fmt.Errorf("%w: source commit %q != manifest %q", ErrAttestationInvalid, attestation.SourceCommit, manifest.SourceCommit)
	}
	if attestation.BuilderID != manifest.Provenance.BuilderID {
		return fmt.Errorf("%w: builder id %q != expected %q", ErrAttestationInvalid, attestation.BuilderID, manifest.Provenance.BuilderID)
	}
	if attestation.WorkflowRef != manifest.Provenance.WorkflowRef {
		return fmt.Errorf("%w: workflow ref %q != expected %q", ErrAttestationInvalid, attestation.WorkflowRef, manifest.Provenance.WorkflowRef)
	}
	if attestation.WorkflowPath != manifest.Provenance.WorkflowPath {
		return fmt.Errorf("%w: workflow path %q != expected %q", ErrAttestationInvalid, attestation.WorkflowPath, manifest.Provenance.WorkflowPath)
	}
	minLevel := manifest.Provenance.MinSLSALevel
	if minLevel == 0 {
		minLevel = DefaultMinimumSLSALevel
	}
	if attestation.SLSALevel < minLevel {
		return fmt.Errorf("%w: SLSA level %d < %d", ErrAttestationInvalid, attestation.SLSALevel, minLevel)
	}
	requireReproducible := true
	if manifest.Provenance.RequireReproducible != nil {
		requireReproducible = *manifest.Provenance.RequireReproducible
	}
	if requireReproducible && !attestation.Reproducible {
		return fmt.Errorf("%w: build was not marked reproducible", ErrAttestationInvalid)
	}
	for _, subject := range attestation.Subjects {
		subjectDigest, err := normalizeDigest(subject.SHA256)
		if err != nil {
			continue
		}
		if subject.Name == manifest.Artifact.Name && subjectDigest == digest {
			return nil
		}
	}
	return fmt.Errorf("%w: no subject matched %s %s", ErrAttestationInvalid, manifest.Artifact.Name, digest)
}

func parseSBOM(data []byte) (SBOM, error) {
	var sbom SBOM
	if err := json.Unmarshal(data, &sbom); err != nil {
		return SBOM{}, fmt.Errorf("%w: decode: %v", ErrSBOMInvalid, err)
	}
	if sbom.SchemaVersion != SchemaVersion {
		return SBOM{}, fmt.Errorf("%w: schemaVersion %d != %d", ErrSBOMInvalid, sbom.SchemaVersion, SchemaVersion)
	}
	if len(sbom.Packages) == 0 {
		return SBOM{}, fmt.Errorf("%w: package list is empty", ErrSBOMInvalid)
	}
	for _, pkg := range sbom.Packages {
		if strings.TrimSpace(pkg.Name) == "" || strings.TrimSpace(pkg.Version) == "" {
			return SBOM{}, fmt.Errorf("%w: package name and version are required", ErrSBOMInvalid)
		}
	}
	return sbom, nil
}

func (db CVEDatabase) validate() error {
	if db.SchemaVersion != 0 && db.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: cve database schemaVersion %d != %d", ErrSBOMInvalid, db.SchemaVersion, SchemaVersion)
	}
	for _, record := range db.Records {
		if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.Package) == "" || strings.TrimSpace(string(record.Severity)) == "" {
			return fmt.Errorf("%w: cve database records require id, package, and severity", ErrSBOMInvalid)
		}
	}
	return nil
}

func policyFindings(sbom SBOM, db CVEDatabase) []Vulnerability {
	findings := make([]Vulnerability, 0, len(sbom.Vulnerabilities)+len(db.Records))
	for _, vuln := range sbom.Vulnerabilities {
		if isBlockingSeverity(vuln.Severity) {
			if vuln.Source == "" {
				vuln.Source = "sbom"
			}
			findings = append(findings, vuln)
		}
	}
	for _, pkg := range sbom.Packages {
		for _, record := range db.Records {
			if !isBlockingSeverity(record.Severity) || record.Package != pkg.Name {
				continue
			}
			if record.Version != "" && record.Version != pkg.Version {
				continue
			}
			record.Version = firstNonEmpty(record.Version, pkg.Version)
			if record.Source == "" {
				record.Source = "last-known-good-cve-db"
			}
			findings = append(findings, record)
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i].Package + "\x00" + findings[i].ID
		right := findings[j].Package + "\x00" + findings[j].ID
		return left < right
	})
	return dedupeFindings(findings)
}

func dedupeFindings(findings []Vulnerability) []Vulnerability {
	out := findings[:0]
	seen := map[string]bool{}
	for _, finding := range findings {
		key := finding.Package + "\x00" + finding.Version + "\x00" + finding.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, finding)
	}
	return out
}

func inventoryRecord(manifest Manifest, attestation Attestation, checksum string, sbomDigest string, attestationDigest string, now time.Time, status Status) InventoryRecord {
	return InventoryRecord{
		SchemaVersion:     SchemaVersion,
		VerifiedAt:        now.UTC().Format(time.RFC3339Nano),
		Status:            status,
		Version:           manifest.Version,
		Channel:           manifest.Channel,
		ArtifactName:      manifest.Artifact.Name,
		Checksum:          checksum,
		SBOMDigest:        sbomDigest,
		AttestationDigest: attestationDigest,
		SourceCommit:      manifest.SourceCommit,
		BuilderID:         attestation.BuilderID,
		WorkflowRef:       attestation.WorkflowRef,
		WorkflowPath:      attestation.WorkflowPath,
		SLSALevel:         attestation.SLSALevel,
		Reproducible:      attestation.Reproducible,
		SigningIdentity:   manifest.Signing.Identity,
		SigningKeyID:      manifest.Signing.KeyID,
		MinDesktopVersion: manifest.Compatibility.MinDesktopVersion,
		MinAPIVersion:     manifest.Compatibility.MinAPIVersion,
	}
}

func (v Verifier) audit(ctx context.Context, event AuditEvent) error {
	if v.Audit == nil {
		return nil
	}
	return v.Audit.AppendReleaseAudit(ctx, event)
}

func (v Verifier) now() time.Time {
	if v.Now != nil {
		return v.Now().UTC()
	}
	return time.Now().UTC()
}

func (o Override) valid() bool {
	return o.Enabled && strings.TrimSpace(o.Actor) != "" && strings.TrimSpace(o.Reason) != ""
}

func (a SBOMAcknowledgement) valid() bool {
	return a.Accepted && strings.TrimSpace(a.Actor) != "" && strings.TrimSpace(a.Reason) != ""
}

func eventTime(value time.Time, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback.UTC()
	}
	return value.UTC()
}

func normalizeDigest(digest string) (string, error) {
	digest = strings.TrimSpace(strings.ToLower(digest))
	digest = strings.TrimPrefix(digest, "sha256:")
	if len(digest) != sha256.Size*2 {
		return "", fmt.Errorf("sha256 digest has %d hex chars", len(digest))
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil {
		return "", fmt.Errorf("invalid sha256 digest: %v", err)
	}
	return "sha256:" + hex.EncodeToString(decoded), nil
}

func isBlockingSeverity(severity Severity) bool {
	switch Severity(strings.ToLower(string(severity))) {
	case SeverityHigh, SeverityCritical:
		return true
	default:
		return false
	}
}

func maxFindingSeverity(findings []Vulnerability) Severity {
	out := SeverityInfo
	for _, finding := range findings {
		if severityRank(finding.Severity) > severityRank(out) {
			out = finding.Severity
		}
	}
	return out
}

func severityRank(severity Severity) int {
	switch Severity(strings.ToLower(string(severity))) {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	default:
		return 1
	}
}

func diagnosticCodes(diagnostics []Diagnostic) []string {
	out := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		out = append(out, diagnostic.Code)
	}
	sort.Strings(out)
	return out
}

func joinFatal(left error, right error) error {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return errors.Join(left, right)
}

func firstNonEmpty(left string, right string) string {
	if left != "" {
		return left
	}
	return right
}
