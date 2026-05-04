package transportsecurity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestVerifyTOFUTunnelModeDoesNotRequireFingerprint(t *testing.T) {
	t.Parallel()
	decision, err := VerifyTOFU(VerifyRequest{Mode: ModeTunnel})
	if err != nil {
		t.Fatalf("VerifyTOFU tunnel: %v", err)
	}
	if !decision.Trusted || decision.Action != ActionTunnelNoPinNeeded || decision.Pin != nil {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestVerifyTOFUDirectModeRequiresAuthenticatedSSHBootstrapForNewPin(t *testing.T) {
	t.Parallel()
	fp := mustFingerprint(t, "daemon")
	_, err := VerifyTOFU(VerifyRequest{Mode: ModeDirect, Presented: fp})
	if !errors.Is(err, ErrUnauthenticatedTOFU) {
		t.Fatalf("without evidence err = %v, want ErrUnauthenticatedTOFU", err)
	}
	_, err = VerifyTOFU(VerifyRequest{
		Mode:      ModeDirect,
		Presented: fp,
		Evidence:  &BootstrapEvidence{Channel: EvidenceSSHBootstrap, Authenticated: false},
	})
	if !errors.Is(err, ErrUnauthenticatedTOFU) {
		t.Fatalf("unauth evidence err = %v, want ErrUnauthenticatedTOFU", err)
	}

	now := time.Unix(1234, 0).UTC()
	decision, err := VerifyTOFU(VerifyRequest{
		Mode:      ModeDirect,
		Presented: fp,
		Evidence: &BootstrapEvidence{
			Channel:       EvidenceSSHBootstrap,
			Authenticated: true,
			CapturedAt:    now,
			Remote:        "vps.example",
		},
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("authenticated evidence: %v", err)
	}
	if !decision.Trusted || decision.Action != ActionPinEstablished || decision.Pin == nil {
		t.Fatalf("decision = %+v", decision)
	}
	if decision.Pin.Source != EvidenceSSHBootstrap || decision.Pin.EstablishedAt != now {
		t.Fatalf("pin = %+v", decision.Pin)
	}
}

func TestVerifyTOFUMatchesExistingPinsAndRejectsMismatches(t *testing.T) {
	t.Parallel()
	fp := mustFingerprint(t, "daemon")
	pin := &Pin{Fingerprint: fp, EstablishedAt: time.Unix(100, 0).UTC(), Source: EvidenceSSHBootstrap}
	decision, err := VerifyTOFU(VerifyRequest{
		Mode:        ModeTailnet,
		Presented:   fp,
		ExistingPin: pin,
	})
	if err != nil {
		t.Fatalf("matching pin: %v", err)
	}
	if decision.Action != ActionPinMatched || decision.Pin == pin {
		t.Fatalf("decision should return copied pin, got %+v", decision)
	}

	_, err = VerifyTOFU(VerifyRequest{
		Mode:        ModeTailnet,
		Presented:   mustFingerprint(t, "attacker"),
		ExistingPin: pin,
	})
	if !errors.Is(err, ErrFingerprintMismatch) {
		t.Fatalf("mismatch err = %v, want ErrFingerprintMismatch", err)
	}
}

func TestParseFingerprintNormalizesCommonSHA256Formats(t *testing.T) {
	t.Parallel()
	raw := "SHA256:AA:BB:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99:aa:bb:cc:dd:ee:ff:00:11:22:33:44:55:66:77:88:99"
	got, err := ParseFingerprint(raw)
	if err != nil {
		t.Fatalf("ParseFingerprint: %v", err)
	}
	want := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	if got.Algorithm != FingerprintSHA256 || got.Value != want {
		t.Fatalf("fingerprint = %+v, want %s", got, want)
	}
}

func TestFingerprintFromCertificateDERIsSHA256Hex(t *testing.T) {
	t.Parallel()
	der := []byte("fake cert der")
	got := FingerprintFromCertificateDER(der)
	sum := sha256.Sum256(der)
	if got.Algorithm != FingerprintSHA256 || got.Value != hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint = %+v", got)
	}
}

func TestVerifyTOFURejectsUnsupportedModesAndBadFingerprints(t *testing.T) {
	t.Parallel()
	if _, err := VerifyTOFU(VerifyRequest{Mode: "public"}); !errors.Is(err, ErrUnsupportedMode) {
		t.Fatalf("unsupported mode err = %v", err)
	}
	_, err := VerifyTOFU(VerifyRequest{
		Mode:      ModeDirect,
		Presented: Fingerprint{Algorithm: "unsupported", Value: "abc"},
		Evidence:  &BootstrapEvidence{Channel: EvidenceSSHBootstrap, Authenticated: true},
	})
	if !errors.Is(err, ErrFingerprintRequired) {
		t.Fatalf("bad fingerprint err = %v, want ErrFingerprintRequired", err)
	}
}

func mustFingerprint(t *testing.T, seed string) Fingerprint {
	t.Helper()
	fp := FingerprintFromCertificateDER([]byte(seed))
	normalized, err := NormalizeFingerprint(fp)
	if err != nil {
		t.Fatalf("NormalizeFingerprint: %v", err)
	}
	return normalized
}
