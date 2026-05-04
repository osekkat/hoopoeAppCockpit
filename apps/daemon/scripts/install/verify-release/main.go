// Command verify-release is the bootstrap-time wrapper around
// internal/release's checksum, signature, attestation, and SBOM policy checks.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/release"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("verify-release", flag.ContinueOnError)
	binaryPath := flags.String("binary", "", "daemon binary")
	manifestPath := flags.String("manifest", "", "release manifest JSON")
	signaturePath := flags.String("signature", "", "Ed25519 signature")
	attestationPath := flags.String("attestation", "", "provenance attestation JSON")
	sbomPath := flags.String("sbom", "", "SBOM JSON")
	cveDBPath := flags.String("cve-db", "", "last-known-good CVE database JSON")
	trustedKeyPath := flags.String("trusted-key", "", "trusted release key JSON")
	inventoryPath := flags.String("inventory", "", "inventory output path")
	auditPath := flags.String("audit", "", "release audit JSONL path")
	override := flags.Bool("override", false, "allow insecure override when verification fails")
	overrideActor := flags.String("override-actor", "operator", "override actor")
	overrideReason := flags.String("override-reason", "", "override reason")
	sbomAck := flags.Bool("sbom-ack", false, "acknowledge blocking SBOM findings")
	sbomAckActor := flags.String("sbom-ack-actor", "operator", "SBOM acknowledgement actor")
	sbomAckReason := flags.String("sbom-ack-reason", "", "SBOM acknowledgement reason")
	if err := flags.Parse(args); err != nil {
		return err
	}

	manifest := release.Manifest{}
	if err := readJSONFile(*manifestPath, &manifest); err != nil {
		return err
	}
	var cveDB release.CVEDatabase
	if *cveDBPath != "" {
		if err := readJSONFile(*cveDBPath, &cveDB); err != nil {
			return err
		}
	}
	var keys []release.TrustedKey
	if *trustedKeyPath != "" {
		key, err := readTrustedKey(*trustedKeyPath)
		if err != nil {
			return err
		}
		keys = append(keys, key)
	}
	var err error
	req := release.VerifyRequest{
		Manifest:    manifest,
		Binary:      nil,
		Signature:   nil,
		Attestation: nil,
		SBOM:        nil,
		CVEDatabase: cveDB,
		Override: release.Override{
			Enabled: *override,
			Actor:   *overrideActor,
			Reason:  *overrideReason,
			At:      time.Now().UTC(),
		},
		SBOMAcknowledgement: release.SBOMAcknowledgement{
			Accepted: *sbomAck,
			Actor:    *sbomAckActor,
			Reason:   *sbomAckReason,
			At:       time.Now().UTC(),
		},
	}
	if req.Binary, err = readRequiredFile(*binaryPath, "binary"); err != nil {
		return err
	}
	if req.Signature, err = readRequiredFile(*signaturePath, "signature"); err != nil {
		return err
	}
	if req.Attestation, err = readRequiredFile(*attestationPath, "attestation"); err != nil {
		return err
	}
	if req.SBOM, err = readRequiredFile(*sbomPath, "sbom"); err != nil {
		return err
	}
	verifier := release.Verifier{
		TrustedKeys: keys,
		Audit:       fileAuditSink{path: *auditPath},
	}
	result, err := verifier.Verify(ctx, req)
	if err != nil {
		return err
	}
	if *inventoryPath != "" {
		if err := release.WriteInventory(*inventoryPath, result.Inventory); err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}

func readRequiredFile(path string, label string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("%s path is required", label)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s %s: %w", label, path, err)
	}
	return data, nil
}

func readJSONFile(path string, target any) error {
	if path == "" {
		return fmt.Errorf("json path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

type trustedKeyFile struct {
	KeyID     string `json:"keyId"`
	Identity  string `json:"identity"`
	PublicKey string `json:"publicKey"`
}

func readTrustedKey(path string) (release.TrustedKey, error) {
	var file trustedKeyFile
	if err := readJSONFile(path, &file); err != nil {
		return release.TrustedKey{}, err
	}
	raw, err := decodeKey(file.PublicKey)
	if err != nil {
		return release.TrustedKey{}, err
	}
	if len(raw) != ed25519.PublicKeySize {
		return release.TrustedKey{}, fmt.Errorf("trusted key publicKey length = %d, want %d", len(raw), ed25519.PublicKeySize)
	}
	return release.TrustedKey{
		KeyID:    file.KeyID,
		Identity: file.Identity,
		Public:   ed25519.PublicKey(raw),
	}, nil
}

func decodeKey(value string) ([]byte, error) {
	if raw, err := base64.StdEncoding.DecodeString(value); err == nil {
		return raw, nil
	}
	if raw, err := base64.RawStdEncoding.DecodeString(value); err == nil {
		return raw, nil
	}
	if raw, err := hex.DecodeString(value); err == nil {
		return raw, nil
	}
	return nil, fmt.Errorf("trusted key publicKey must be base64 or hex encoded")
}

type fileAuditSink struct {
	path string
}

func (s fileAuditSink) AppendReleaseAudit(_ context.Context, event release.AuditEvent) error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("audit mkdir: %w", err)
	}
	file, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit open: %w", err)
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(event)
}
