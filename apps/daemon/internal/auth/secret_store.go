package auth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// hp-0oi: ServerSecretStore owns the 32-byte signing secret used by the
// SessionCredentialService to HMAC bearer + WS tokens. Per plan.md §5.2:
// "single signing secret in ServerSecretStore (32 bytes random) — rotating
// it revokes everything."
//
// On-disk format: `~/.hoopoe/auth/server-secret.json`, atomic write via
// rename, 0o600 perms, tracked by `Generation` so callers can detect
// rotation without re-reading the file.

const (
	// SecretSchemaVersion is bumped when the on-disk shape changes (per
	// plan.md §10.3).
	SecretSchemaVersion = 1
	// SecretByteLength is the canonical signing-secret length: 32 bytes.
	SecretByteLength = 32
)

var (
	ErrSecretNotInitialized = errors.New("auth: server secret not initialized")
	ErrSecretCorrupted      = errors.New("auth: server secret file is corrupted")
)

// SecretSnapshot is what callers receive when they ask for the current
// secret. The Generation increments on every Rotate() so SessionCredentialService
// can compare token-issue-time generation against current generation and
// invalidate tokens issued before a rotation.
type SecretSnapshot struct {
	SchemaVersion int       `json:"schemaVersion"`
	Generation    int       `json:"generation"`
	Secret        []byte    `json:"-"` // never serialized; opaque bytes
	HexSecret     string    `json:"hexSecret"`
	CreatedAt     time.Time `json:"createdAt"`
	RotatedFrom   int       `json:"rotatedFrom,omitempty"`
}

type ServerSecretStoreConfig struct {
	Path   string
	Now    func() time.Time
	Random io.Reader
}

type ServerSecretStore struct {
	mu     sync.Mutex
	path   string
	now    func() time.Time
	random io.Reader
}

func NewServerSecretStore(cfg ServerSecretStoreConfig) (*ServerSecretStore, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("auth: secret store requires a non-empty path")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	random := cfg.Random
	if random == nil {
		random = rand.Reader
	}
	return &ServerSecretStore{
		path:   cfg.Path,
		now:    now,
		random: random,
	}, nil
}

// EnsureInitialized creates a fresh secret if the on-disk file is missing.
// Idempotent; subsequent calls return the existing snapshot. Callers (like
// the daemon main) invoke this at boot before SessionCredentialService is
// constructed.
func (s *ServerSecretStore) EnsureInitialized() (SecretSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.loadLocked()
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrSecretNotInitialized) {
		return SecretSnapshot{}, err
	}
	return s.createLocked(0, 0)
}

// Current returns the active secret snapshot. Returns ErrSecretNotInitialized
// if the daemon hasn't called EnsureInitialized yet.
func (s *ServerSecretStore) Current() (SecretSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// Rotate generates a new 32-byte secret and bumps the Generation counter.
// Per plan.md §5.2: "rotating it revokes everything." Callers MUST treat
// this as cascading invalidation: every outstanding bearer + WS token
// fails signature verification because the underlying secret changed.
//
// Returns the NEW snapshot. The previous generation is recorded in
// `RotatedFrom` for audit-log forensics.
func (s *ServerSecretStore) Rotate() (SecretSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	prev, err := s.loadLocked()
	prevGen := 0
	if err == nil {
		prevGen = prev.Generation
	} else if !errors.Is(err, ErrSecretNotInitialized) {
		return SecretSnapshot{}, err
	}
	return s.createLocked(prevGen+1, prevGen)
}

// loadLocked reads the secret file. Caller MUST hold s.mu.
func (s *ServerSecretStore) loadLocked() (SecretSnapshot, error) {
	bytes, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SecretSnapshot{}, ErrSecretNotInitialized
		}
		return SecretSnapshot{}, fmt.Errorf("auth: read secret %s: %w", s.path, err)
	}
	var snap SecretSnapshot
	if err := json.Unmarshal(bytes, &snap); err != nil {
		return SecretSnapshot{}, fmt.Errorf("%w: %v", ErrSecretCorrupted, err)
	}
	if snap.SchemaVersion != SecretSchemaVersion {
		return SecretSnapshot{}, fmt.Errorf("%w: unexpected schemaVersion %d",
			ErrSecretCorrupted, snap.SchemaVersion)
	}
	if snap.HexSecret == "" {
		return SecretSnapshot{}, fmt.Errorf("%w: empty hexSecret", ErrSecretCorrupted)
	}
	raw, err := hex.DecodeString(snap.HexSecret)
	if err != nil {
		return SecretSnapshot{}, fmt.Errorf("%w: hex decode: %v", ErrSecretCorrupted, err)
	}
	if len(raw) != SecretByteLength {
		return SecretSnapshot{}, fmt.Errorf("%w: expected %d bytes, got %d",
			ErrSecretCorrupted, SecretByteLength, len(raw))
	}
	snap.Secret = raw
	return snap, nil
}

// createLocked generates a new secret with the given generation + writes
// it atomically. Caller MUST hold s.mu.
func (s *ServerSecretStore) createLocked(generation int, rotatedFrom int) (SecretSnapshot, error) {
	if generation < 1 {
		generation = 1
	}
	raw := make([]byte, SecretByteLength)
	if _, err := io.ReadFull(s.random, raw); err != nil {
		return SecretSnapshot{}, fmt.Errorf("auth: generate secret: %w", err)
	}
	snap := SecretSnapshot{
		SchemaVersion: SecretSchemaVersion,
		Generation:    generation,
		Secret:        raw,
		HexSecret:     hex.EncodeToString(raw),
		CreatedAt:     s.now().UTC(),
		RotatedFrom:   rotatedFrom,
	}
	if err := s.writeLocked(snap); err != nil {
		return SecretSnapshot{}, err
	}
	return snap, nil
}

// writeLocked writes the snapshot atomically: write to a temp sibling,
// fsync, rename. The temp file inherits 0o600 perms.
func (s *ServerSecretStore) writeLocked(snap SecretSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("auth: mkdir %s: %w", filepath.Dir(s.path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("auth: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("auth: chmod temp: %w", err)
	}
	enc := json.NewEncoder(tmp)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snap); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("auth: encode secret: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("auth: fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("auth: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("auth: atomic rename: %w", err)
	}
	return nil
}
