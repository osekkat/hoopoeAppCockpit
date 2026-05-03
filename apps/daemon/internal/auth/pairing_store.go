package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

type pairingEventType string

const (
	pairingEventCreated  pairingEventType = "created"
	pairingEventConsumed pairingEventType = "consumed"
	pairingEventRevoked  pairingEventType = "revoked"
)

type pairingEvent struct {
	SchemaVersion int              `json:"schemaVersion"`
	Type          pairingEventType `json:"type"`
	TokenID       string           `json:"tokenId"`
	DisplayToken  string           `json:"displayToken,omitempty"`
	Role          PairingRole      `json:"role,omitempty"`
	Time          time.Time        `json:"time"`
	ConsumedBy    string           `json:"consumedBy,omitempty"`
	RevokedBy     string           `json:"revokedBy,omitempty"`
}

type pairingState struct {
	records        map[string]PairingRecord
	tokenIDByToken map[string]string
}

type PairingJSONLStore struct {
	mu   sync.Mutex
	path string
}

func NewPairingJSONLStore(path string) *PairingJSONLStore {
	return &PairingJSONLStore{path: path}
}

func (s *PairingJSONLStore) Load(ctx context.Context) (pairingState, error) {
	if err := ctx.Err(); err != nil {
		return pairingState{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withFileLock(ctx, s.loadUnlocked)
}

func (s *PairingJSONLStore) withLockedState(ctx context.Context, fn func(pairingState) ([]pairingEvent, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.withFileLock(ctx, func(context.Context) (pairingState, error) {
		state, err := s.loadUnlocked(ctx)
		if err != nil {
			return pairingState{}, err
		}
		events, err := fn(state)
		if err != nil {
			return pairingState{}, err
		}
		if len(events) == 0 {
			return state, nil
		}
		if err := s.appendUnlocked(ctx, events); err != nil {
			return pairingState{}, err
		}
		return state, nil
	})
	return err
}

func (s *PairingJSONLStore) withFileLock(ctx context.Context, fn func(context.Context) (pairingState, error)) (pairingState, error) {
	if s.path == "" {
		return pairingState{}, fmt.Errorf("%w: empty pairing store path", ErrInvalidPairingRequest)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return pairingState{}, err
	}
	lockPath := s.path + ".lock"
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return pairingState{}, err
	}
	defer lockFile.Close()
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return pairingState{}, err
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	return fn(ctx)
}

func (s *PairingJSONLStore) loadUnlocked(ctx context.Context) (pairingState, error) {
	if err := ctx.Err(); err != nil {
		return pairingState{}, err
	}
	state := pairingState{
		records:        make(map[string]PairingRecord),
		tokenIDByToken: make(map[string]string),
	}
	file, err := os.Open(s.path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return pairingState{}, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event pairingEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return pairingState{}, err
		}
		if err := applyPairingEvent(state, event); err != nil {
			return pairingState{}, err
		}
	}
	if err := scanner.Err(); err != nil {
		return pairingState{}, err
	}
	return state, nil
}

func (s *PairingJSONLStore) appendUnlocked(ctx context.Context, events []pairingEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	for _, event := range events {
		if err := encoder.Encode(event); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func applyPairingEvent(state pairingState, event pairingEvent) error {
	if event.SchemaVersion != PairingSchemaVersion {
		return fmt.Errorf("%w: unsupported pairing event schemaVersion %d", ErrInvalidPairingRequest, event.SchemaVersion)
	}
	if event.TokenID == "" {
		return fmt.Errorf("%w: empty tokenId", ErrInvalidPairingRequest)
	}
	switch event.Type {
	case pairingEventCreated:
		if event.DisplayToken == "" || !event.Role.valid() {
			return fmt.Errorf("%w: invalid created pairing event", ErrInvalidPairingRequest)
		}
		if _, exists := state.records[event.TokenID]; exists {
			return fmt.Errorf("%w: duplicate tokenId", ErrInvalidPairingRequest)
		}
		record := PairingRecord{
			TokenID:      event.TokenID,
			DisplayToken: event.DisplayToken,
			Role:         event.Role,
			CreatedAt:    event.Time,
		}
		state.records[event.TokenID] = record
		state.tokenIDByToken[event.DisplayToken] = event.TokenID
	case pairingEventConsumed:
		record, ok := state.records[event.TokenID]
		if !ok {
			return fmt.Errorf("%w: consume before create", ErrInvalidPairingRequest)
		}
		consumedAt := event.Time
		record.ConsumedAt = &consumedAt
		record.ConsumedBy = event.ConsumedBy
		state.records[event.TokenID] = record
	case pairingEventRevoked:
		record, ok := state.records[event.TokenID]
		if !ok {
			return fmt.Errorf("%w: revoke before create", ErrInvalidPairingRequest)
		}
		revokedAt := event.Time
		record.RevokedAt = &revokedAt
		record.RevokedBy = event.RevokedBy
		state.records[event.TokenID] = record
	default:
		return fmt.Errorf("%w: unknown pairing event type %q", ErrInvalidPairingRequest, event.Type)
	}
	return nil
}

func (s pairingState) listRecords() []PairingRecord {
	records := make([]PairingRecord, 0, len(s.records))
	for _, record := range s.records {
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].CreatedAt.Equal(records[j].CreatedAt) {
			return records[i].TokenID < records[j].TokenID
		}
		return records[i].CreatedAt.Before(records[j].CreatedAt)
	})
	return records
}
