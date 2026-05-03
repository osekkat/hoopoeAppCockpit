// Package auth owns daemon bootstrap and session credential services.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"time"
)

const (
	PairingSchemaVersion = 1
	PairingTokenLength   = 12
	// PairingAlphabet is Crockford-style, with 0/I/L/O/U removed for
	// case-insensitive readability and to avoid common confusables.
	PairingAlphabet = "123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

var (
	ErrPairingNotFound       = errors.New("auth: pairing token not found")
	ErrPairingConsumed       = errors.New("auth: pairing token already consumed")
	ErrPairingRevoked        = errors.New("auth: pairing token revoked")
	ErrInvalidPairingToken   = errors.New("auth: invalid pairing token")
	ErrInvalidPairingRequest = errors.New("auth: invalid pairing request")
)

type PairingRole string

const (
	PairingRoleOwner  PairingRole = "owner"
	PairingRoleClient PairingRole = "client"
)

func (r PairingRole) valid() bool {
	switch r {
	case PairingRoleOwner, PairingRoleClient:
		return true
	}
	return false
}

type PairingRecord struct {
	TokenID      string      `json:"tokenId"`
	DisplayToken string      `json:"displayToken"`
	Role         PairingRole `json:"role"`
	CreatedAt    time.Time   `json:"createdAt"`
	ConsumedAt   *time.Time  `json:"consumedAt,omitempty"`
	ConsumedBy   string      `json:"consumedBy,omitempty"`
	RevokedAt    *time.Time  `json:"revokedAt,omitempty"`
	RevokedBy    string      `json:"revokedBy,omitempty"`
}

func (r PairingRecord) Active() bool {
	return r.ConsumedAt == nil && r.RevokedAt == nil
}

type BootstrapCredentialConfig struct {
	Path   string
	Now    func() time.Time
	Random io.Reader
}

type BootstrapCredentialService struct {
	store  *PairingJSONLStore
	now    func() time.Time
	random io.Reader
}

type CreatePairingRequest struct {
	Role PairingRole
}

type IssuedPairing struct {
	TokenID      string      `json:"tokenId"`
	DisplayToken string      `json:"displayToken"`
	Role         PairingRole `json:"role"`
	CreatedAt    time.Time   `json:"createdAt"`
}

type ConsumePairingRequest struct {
	PairingToken string
	InstanceID   string
}

type RevokePairingRequest struct {
	TokenID string
	Actor   string
}

func NewBootstrapCredentialService(cfg BootstrapCredentialConfig) (*BootstrapCredentialService, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("%w: empty pairing store path", ErrInvalidPairingRequest)
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	random := cfg.Random
	if random == nil {
		random = rand.Reader
	}
	return &BootstrapCredentialService{
		store:  NewPairingJSONLStore(cfg.Path),
		now:    now,
		random: random,
	}, nil
}

// EnsureInitialPairing creates the first owner pairing if the append-only log
// has no pairing history. It returns created=false after the first launch,
// including after that initial token is consumed.
func (s *BootstrapCredentialService) EnsureInitialPairing(ctx context.Context) (IssuedPairing, bool, error) {
	var issued IssuedPairing
	created := false
	err := s.store.withLockedState(ctx, func(state pairingState) ([]pairingEvent, error) {
		if len(state.records) > 0 {
			return nil, nil
		}
		token, err := GeneratePairingToken(s.random)
		if err != nil {
			return nil, err
		}
		now := s.now().UTC()
		issued = IssuedPairing{
			TokenID:      tokenID(token),
			DisplayToken: token,
			Role:         PairingRoleOwner,
			CreatedAt:    now,
		}
		created = true
		return []pairingEvent{newPairingCreatedEvent(issued, now)}, nil
	})
	return issued, created, err
}

func (s *BootstrapCredentialService) CreatePairing(ctx context.Context, req CreatePairingRequest) (IssuedPairing, error) {
	role := req.Role
	if role == "" {
		role = PairingRoleClient
	}
	if !role.valid() {
		return IssuedPairing{}, fmt.Errorf("%w: invalid role %q", ErrInvalidPairingRequest, role)
	}

	var issued IssuedPairing
	err := s.store.withLockedState(ctx, func(state pairingState) ([]pairingEvent, error) {
		token, err := s.uniqueToken(state)
		if err != nil {
			return nil, err
		}
		now := s.now().UTC()
		issued = IssuedPairing{
			TokenID:      tokenID(token),
			DisplayToken: token,
			Role:         role,
			CreatedAt:    now,
		}
		return []pairingEvent{newPairingCreatedEvent(issued, now)}, nil
	})
	return issued, err
}

func (s *BootstrapCredentialService) ConsumePairing(ctx context.Context, req ConsumePairingRequest) (PairingRecord, error) {
	token, err := NormalizePairingToken(req.PairingToken)
	if err != nil {
		return PairingRecord{}, err
	}
	if strings.TrimSpace(req.InstanceID) == "" {
		return PairingRecord{}, fmt.Errorf("%w: empty instanceId", ErrInvalidPairingRequest)
	}

	var consumed PairingRecord
	err = s.store.withLockedState(ctx, func(state pairingState) ([]pairingEvent, error) {
		id, ok := state.tokenIDByToken[token]
		if !ok {
			return nil, ErrPairingNotFound
		}
		record := state.records[id]
		if record.RevokedAt != nil {
			return nil, ErrPairingRevoked
		}
		if record.ConsumedAt != nil {
			return nil, ErrPairingConsumed
		}
		now := s.now().UTC()
		consumed = record
		consumed.ConsumedAt = &now
		consumed.ConsumedBy = strings.TrimSpace(req.InstanceID)
		return []pairingEvent{{
			SchemaVersion: PairingSchemaVersion,
			Type:          pairingEventConsumed,
			TokenID:       id,
			Time:          now,
			ConsumedBy:    consumed.ConsumedBy,
		}}, nil
	})
	return consumed, err
}

func (s *BootstrapCredentialService) RevokePairing(ctx context.Context, req RevokePairingRequest) (PairingRecord, error) {
	if strings.TrimSpace(req.TokenID) == "" {
		return PairingRecord{}, fmt.Errorf("%w: empty tokenId", ErrInvalidPairingRequest)
	}
	var revoked PairingRecord
	err := s.store.withLockedState(ctx, func(state pairingState) ([]pairingEvent, error) {
		record, ok := state.records[req.TokenID]
		if !ok {
			return nil, ErrPairingNotFound
		}
		if record.RevokedAt != nil {
			revoked = record
			return nil, nil
		}
		now := s.now().UTC()
		actor := strings.TrimSpace(req.Actor)
		revoked = record
		revoked.RevokedAt = &now
		revoked.RevokedBy = actor
		return []pairingEvent{{
			SchemaVersion: PairingSchemaVersion,
			Type:          pairingEventRevoked,
			TokenID:       req.TokenID,
			Time:          now,
			RevokedBy:     actor,
		}}, nil
	})
	return revoked, err
}

func (s *BootstrapCredentialService) ListPairings(ctx context.Context) ([]PairingRecord, error) {
	state, err := s.store.Load(ctx)
	if err != nil {
		return nil, err
	}
	return state.listRecords(), nil
}

func (s *BootstrapCredentialService) uniqueToken(state pairingState) (string, error) {
	for attempts := 0; attempts < 8; attempts++ {
		token, err := GeneratePairingToken(s.random)
		if err != nil {
			return "", err
		}
		if _, exists := state.tokenIDByToken[token]; !exists {
			return token, nil
		}
	}
	return "", fmt.Errorf("%w: could not generate unique token", ErrInvalidPairingRequest)
}

func GeneratePairingToken(random io.Reader) (string, error) {
	if random == nil {
		random = rand.Reader
	}
	var out strings.Builder
	out.Grow(PairingTokenLength)
	max := big.NewInt(int64(len(PairingAlphabet)))
	for out.Len() < PairingTokenLength {
		n, err := rand.Int(random, max)
		if err != nil {
			return "", err
		}
		out.WriteByte(PairingAlphabet[n.Int64()])
	}
	return out.String(), nil
}

func NormalizePairingToken(token string) (string, error) {
	normalized := strings.NewReplacer("-", "", " ", "", "\t", "", "\n", "", "\r", "").Replace(token)
	normalized = strings.ToUpper(strings.TrimSpace(normalized))
	if len(normalized) != PairingTokenLength {
		return "", ErrInvalidPairingToken
	}
	for _, ch := range normalized {
		if !strings.ContainsRune(PairingAlphabet, ch) {
			return "", ErrInvalidPairingToken
		}
	}
	return normalized, nil
}

func tokenID(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "pair_" + hex.EncodeToString(sum[:8])
}

func newPairingCreatedEvent(issued IssuedPairing, now time.Time) pairingEvent {
	return pairingEvent{
		SchemaVersion: PairingSchemaVersion,
		Type:          pairingEventCreated,
		TokenID:       issued.TokenID,
		DisplayToken:  issued.DisplayToken,
		Role:          issued.Role,
		Time:          now,
	}
}
