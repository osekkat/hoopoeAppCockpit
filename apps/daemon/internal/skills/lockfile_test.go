package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileLockStoreLoadsLegacyShapeAndNormalizes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".hoopoe", "skills.lock.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	rawDigest := strings.TrimPrefix(DigestBytes([]byte("legacy jsm content")), sha256Prefix)
	lockJSON := `{
  "schemaVersion": 1,
  "skills": [
    {
      "name": "vibing-with-ntm",
      "source": "jsm",
      "sha256": "` + rawDigest + `",
      "installed_at": "2026-05-04T00:00:00Z"
    }
  ]
}`
	if err := os.WriteFile(path, []byte(lockJSON), 0o600); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	lock, existed, err := FileLockStore{ProjectDir: dir}.Load(context.Background())
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !existed {
		t.Fatal("lock file not reported as existing")
	}
	pin, ok := lock.PinFor("vibing-with-ntm")
	if !ok {
		t.Fatalf("pin missing: %+v", lock)
	}
	if pin.Installer != InstallerJSM || pin.Digest != sha256Prefix+rawDigest || pin.PinnedAt == "" {
		t.Fatalf("legacy pin not normalized: %+v", pin)
	}
}

func TestFileLockStoreSavesCanonicalSortedLock(t *testing.T) {
	dir := t.TempDir()
	store := FileLockStore{ProjectDir: dir}
	digestB := DigestBytes([]byte("b"))
	digestA := DigestBytes([]byte("a"))
	lock := LockFile{
		SchemaVersion: SchemaVersion,
		Skills: []SkillPin{
			{ID: "zeta", Installer: InstallerJSM, Digest: digestB},
			{ID: "alpha", Installer: InstallerJSM, Digest: digestA},
		},
	}

	if err := store.Save(context.Background(), lock); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read saved lock: %v", err)
	}
	var saved LockFile
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(saved.Skills) != 2 || saved.Skills[0].ID != "alpha" || saved.Skills[1].ID != "zeta" {
		t.Fatalf("lock not sorted: %s", data)
	}
	if _, err := LockHash(saved); err != nil {
		t.Fatalf("hash saved lock: %v", err)
	}
}

func TestLockFileRejectsDuplicateSkill(t *testing.T) {
	lock := LockFile{
		SchemaVersion: SchemaVersion,
		Skills: []SkillPin{
			{ID: "ntm", Installer: InstallerJSM, Digest: DigestBytes([]byte("a"))},
			{ID: "ntm", Installer: InstallerJFP, Version: "v1.0.0", Advisory: true},
		},
	}
	if err := lock.Validate(); err == nil {
		t.Fatal("duplicate lock entries validated")
	}
}
