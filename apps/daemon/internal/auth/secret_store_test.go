package auth

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTempStore(t *testing.T) *ServerSecretStore {
	t.Helper()
	dir := t.TempDir()
	store, err := NewServerSecretStore(ServerSecretStoreConfig{
		Path: filepath.Join(dir, "server-secret.json"),
		Now:  func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestSecretStoreRequiresPath(t *testing.T) {
	if _, err := NewServerSecretStore(ServerSecretStoreConfig{}); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestEnsureInitializedCreatesFreshSecret(t *testing.T) {
	store := newTempStore(t)
	snap, err := store.EnsureInitialized()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Generation != 1 {
		t.Errorf("first generation=%d", snap.Generation)
	}
	if len(snap.Secret) != SecretByteLength {
		t.Errorf("secret length=%d", len(snap.Secret))
	}
	if snap.SchemaVersion != SecretSchemaVersion {
		t.Errorf("schemaVersion=%d", snap.SchemaVersion)
	}
	if snap.RotatedFrom != 0 {
		t.Errorf("first generation should have rotatedFrom=0, got %d", snap.RotatedFrom)
	}
}

func TestEnsureInitializedIsIdempotent(t *testing.T) {
	store := newTempStore(t)
	a, err := store.EnsureInitialized()
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.EnsureInitialized()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Secret, b.Secret) {
		t.Errorf("secret should be stable across EnsureInitialized calls")
	}
	if a.Generation != b.Generation {
		t.Errorf("generation should be stable, got %d → %d", a.Generation, b.Generation)
	}
}

func TestCurrentReturnsErrIfNotInitialized(t *testing.T) {
	store := newTempStore(t)
	_, err := store.Current()
	if !errors.Is(err, ErrSecretNotInitialized) {
		t.Errorf("expected ErrSecretNotInitialized, got %v", err)
	}
}

func TestRotateBumpsGenerationAndChangesSecret(t *testing.T) {
	store := newTempStore(t)
	a, err := store.EnsureInitialized()
	if err != nil {
		t.Fatal(err)
	}
	b, err := store.Rotate()
	if err != nil {
		t.Fatal(err)
	}
	if b.Generation != a.Generation+1 {
		t.Errorf("generation: %d → %d", a.Generation, b.Generation)
	}
	if bytes.Equal(a.Secret, b.Secret) {
		t.Errorf("rotation didn't change secret")
	}
	if b.RotatedFrom != a.Generation {
		t.Errorf("rotatedFrom=%d (want %d)", b.RotatedFrom, a.Generation)
	}
}

func TestRotateFromUninitializedCreatesGeneration1(t *testing.T) {
	store := newTempStore(t)
	snap, err := store.Rotate()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Generation != 1 {
		t.Errorf("generation=%d", snap.Generation)
	}
}

func TestSecretFileIsAtomicAndRestricted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth", "server-secret.json")
	store, err := NewServerSecretStore(ServerSecretStoreConfig{
		Path: path,
		Now:  func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureInitialized(); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := stat.Mode().Perm(); mode != 0o600 {
		t.Errorf("expected 0600, got %o", mode)
	}
	// No temp files left behind.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == filepath.Base(path) {
			continue
		}
		t.Errorf("leftover sibling: %s", e.Name())
	}
}

func TestRotateRoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.json")
	store, _ := NewServerSecretStore(ServerSecretStoreConfig{Path: path})
	if _, err := store.EnsureInitialized(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Rotate(); err != nil {
		t.Fatal(err)
	}
	rotated, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}

	// Re-open with a fresh store; verify Generation persists.
	fresh, _ := NewServerSecretStore(ServerSecretStoreConfig{Path: path})
	loaded, err := fresh.Current()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != rotated.Generation {
		t.Errorf("persisted generation=%d (want %d)", loaded.Generation, rotated.Generation)
	}
	if !bytes.Equal(loaded.Secret, rotated.Secret) {
		t.Errorf("persisted secret bytes differ")
	}
}

func TestCorruptedSecretFileSurfaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := NewServerSecretStore(ServerSecretStoreConfig{Path: path})
	_, err := store.Current()
	if !errors.Is(err, ErrSecretCorrupted) {
		t.Errorf("expected ErrSecretCorrupted, got %v", err)
	}
}

// Generated random feed for deterministic byte-level assertions.
type fixedReader struct {
	buf io.Reader
}

func TestSecretBytesUseProvidedRandom(t *testing.T) {
	dir := t.TempDir()
	deterministic := bytes.Repeat([]byte{0xAB}, SecretByteLength)
	store, err := NewServerSecretStore(ServerSecretStoreConfig{
		Path:   filepath.Join(dir, "secret.json"),
		Random: bytes.NewReader(deterministic),
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := store.EnsureInitialized()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(snap.Secret, deterministic) {
		t.Errorf("secret didn't use provided random feed")
	}
}

// Sanity: real rand.Reader works (not just our test reader).
func TestSecretWithRealRandReader(t *testing.T) {
	store, _ := NewServerSecretStore(ServerSecretStoreConfig{
		Path:   filepath.Join(t.TempDir(), "secret.json"),
		Random: rand.Reader,
	})
	snap, err := store.EnsureInitialized()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Secret) != SecretByteLength {
		t.Errorf("real-random secret length=%d", len(snap.Secret))
	}
}

// fixedReader stays for future expansion (e.g., simulating short reads);
// silence unused warning.
var _ = fixedReader{}
