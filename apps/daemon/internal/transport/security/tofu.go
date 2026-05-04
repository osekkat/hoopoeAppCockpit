// Package transportsecurity enforces TLS fingerprint policy for direct daemon
// transports. SSH-tunnel mode remains the v1 default; direct and tailnet modes
// must pin what was learned through authenticated bootstrap.
package transportsecurity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	ModeTunnel  TransportMode = "ssh_tunnel"
	ModeDirect  TransportMode = "direct"
	ModeTailnet TransportMode = "tailnet"

	FingerprintSHA256       = "sha256"
	EvidenceSSHBootstrap    = "ssh_bootstrap"
	ActionTunnelNoPinNeeded = "tunnel_no_pin_needed"
	ActionPinMatched        = "pin_matched"
	ActionPinEstablished    = "pin_established"
)

var (
	ErrInvalidFingerprint  = errors.New("transport security: invalid fingerprint")
	ErrFingerprintRequired = errors.New("transport security: fingerprint required")
	ErrFingerprintMismatch = errors.New("transport security: fingerprint mismatch")
	ErrUnauthenticatedTOFU = errors.New("transport security: TOFU requires authenticated SSH bootstrap evidence")
	ErrUnsupportedMode     = errors.New("transport security: unsupported transport mode")
)

type TransportMode string

type Fingerprint struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

type Pin struct {
	Fingerprint   Fingerprint `json:"fingerprint"`
	EstablishedAt time.Time   `json:"establishedAt"`
	Source        string      `json:"source"`
}

type BootstrapEvidence struct {
	Channel       string    `json:"channel"`
	Authenticated bool      `json:"authenticated"`
	CapturedAt    time.Time `json:"capturedAt"`
	Remote        string    `json:"remote,omitempty"`
}

type VerifyRequest struct {
	Mode        TransportMode
	Presented   Fingerprint
	ExistingPin *Pin
	Evidence    *BootstrapEvidence
	Now         func() time.Time
}

type Decision struct {
	Trusted bool   `json:"trusted"`
	Action  string `json:"action"`
	Pin     *Pin   `json:"pin,omitempty"`
}

func FingerprintFromCertificateDER(der []byte) Fingerprint {
	sum := sha256.Sum256(der)
	return Fingerprint{Algorithm: FingerprintSHA256, Value: hex.EncodeToString(sum[:])}
}

func ParseFingerprint(raw string) (Fingerprint, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Fingerprint{}, fmt.Errorf("%w: empty value", ErrInvalidFingerprint)
	}
	algorithm := FingerprintSHA256
	value := trimmed
	if before, after, ok := strings.Cut(trimmed, ":"); ok && strings.EqualFold(before, FingerprintSHA256) {
		value = after
	}
	fp := Fingerprint{Algorithm: algorithm, Value: value}
	return NormalizeFingerprint(fp)
}

func NormalizeFingerprint(fp Fingerprint) (Fingerprint, error) {
	algorithm := strings.ToLower(strings.TrimSpace(fp.Algorithm))
	if algorithm == "" {
		algorithm = FingerprintSHA256
	}
	if algorithm != FingerprintSHA256 {
		return Fingerprint{}, fmt.Errorf("%w: unsupported algorithm %q", ErrInvalidFingerprint, fp.Algorithm)
	}
	value := strings.TrimSpace(fp.Value)
	if value == "" {
		return Fingerprint{}, fmt.Errorf("%w: empty value", ErrInvalidFingerprint)
	}
	noDelimiters := strings.NewReplacer(":", "", "-", "", " ", "").Replace(value)
	if isHex(noDelimiters) {
		value = strings.ToLower(noDelimiters)
		if len(value) != sha256.Size*2 {
			return Fingerprint{}, fmt.Errorf("%w: sha256 hex length %d", ErrInvalidFingerprint, len(value))
		}
	} else if strings.ContainsAny(value, "\x00\r\n\t ") {
		return Fingerprint{}, fmt.Errorf("%w: invalid characters", ErrInvalidFingerprint)
	}
	return Fingerprint{Algorithm: algorithm, Value: value}, nil
}

func VerifyTOFU(req VerifyRequest) (Decision, error) {
	switch req.Mode {
	case ModeTunnel:
		return Decision{Trusted: true, Action: ActionTunnelNoPinNeeded}, nil
	case ModeDirect, ModeTailnet:
	default:
		return Decision{}, fmt.Errorf("%w: %s", ErrUnsupportedMode, req.Mode)
	}

	presented, err := NormalizeFingerprint(req.Presented)
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %v", ErrFingerprintRequired, err)
	}
	if req.ExistingPin != nil {
		stored, err := NormalizeFingerprint(req.ExistingPin.Fingerprint)
		if err != nil {
			return Decision{}, err
		}
		if stored != presented {
			return Decision{}, fmt.Errorf("%w: presented %s:%s does not match stored %s:%s", ErrFingerprintMismatch, presented.Algorithm, presented.Value, stored.Algorithm, stored.Value)
		}
		pin := *req.ExistingPin
		pin.Fingerprint = stored
		return Decision{Trusted: true, Action: ActionPinMatched, Pin: &pin}, nil
	}

	if !authenticatedBootstrap(req.Evidence) {
		return Decision{}, ErrUnauthenticatedTOFU
	}
	now := req.Now
	if now == nil {
		now = time.Now
	}
	pin := Pin{
		Fingerprint:   presented,
		EstablishedAt: now().UTC(),
		Source:        EvidenceSSHBootstrap,
	}
	return Decision{Trusted: true, Action: ActionPinEstablished, Pin: &pin}, nil
}

func authenticatedBootstrap(evidence *BootstrapEvidence) bool {
	return evidence != nil &&
		evidence.Authenticated &&
		evidence.Channel == EvidenceSSHBootstrap
}

func isHex(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' {
			continue
		}
		return false
	}
	return true
}
