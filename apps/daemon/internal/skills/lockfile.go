package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type LockFile struct {
	SchemaVersion int        `json:"schemaVersion"`
	Skills        []SkillPin `json:"skills"`
}

type SkillPin struct {
	ID          string    `json:"id"`
	Installer   Installer `json:"installer"`
	Digest      string    `json:"digest,omitempty"`
	Version     string    `json:"version,omitempty"`
	Advisory    bool      `json:"advisory,omitempty"`
	PinnedAt    string    `json:"pinnedAt,omitempty"`
	PinnedBy    string    `json:"pinnedBy,omitempty"`
	ContentHash string    `json:"contentHash,omitempty"`
}

type skillPinJSON struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Installer   Installer `json:"installer"`
	Source      Installer `json:"source"`
	Digest      string    `json:"digest"`
	SHA256      *string   `json:"sha256"`
	Version     string    `json:"version"`
	Advisory    bool      `json:"advisory"`
	PinnedAt    string    `json:"pinnedAt"`
	PinnedBy    string    `json:"pinnedBy"`
	InstalledAt string    `json:"installed_at"`
	ContentHash string    `json:"contentHash"`
}

func (p *SkillPin) UnmarshalJSON(data []byte) error {
	var in skillPinJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	id := in.ID
	if id == "" {
		id = in.Name
	}
	installer := in.Installer
	if installer == "" {
		installer = in.Source
	}
	digest := in.Digest
	if digest == "" && in.SHA256 != nil && *in.SHA256 != "" {
		digest = *in.SHA256
	}
	pinnedAt := in.PinnedAt
	if pinnedAt == "" {
		pinnedAt = in.InstalledAt
	}
	*p = SkillPin{
		ID:          id,
		Installer:   installer,
		Digest:      digest,
		Version:     in.Version,
		Advisory:    in.Advisory,
		PinnedAt:    pinnedAt,
		PinnedBy:    in.PinnedBy,
		ContentHash: in.ContentHash,
	}
	return nil
}

func (p SkillPin) MarshalJSON() ([]byte, error) {
	type out SkillPin
	return json.Marshal(out(p))
}

func (p SkillPin) Validate() error {
	if !validSkillID(p.ID) {
		return fmt.Errorf("%w: invalid skill id %q", ErrInvalidLockFile, p.ID)
	}
	if !p.Installer.Valid() {
		return fmt.Errorf("%w: skill %s has invalid installer %q", ErrInvalidLockFile, p.ID, p.Installer)
	}
	if p.Digest != "" {
		digest, err := NormalizeDigest(p.Digest)
		if err != nil {
			return fmt.Errorf("%w: skill %s has invalid digest: %v", ErrInvalidLockFile, p.ID, err)
		}
		p.Digest = digest
	}
	if p.ContentHash != "" {
		if _, err := NormalizeDigest(p.ContentHash); err != nil {
			return fmt.Errorf("%w: skill %s has invalid content hash: %v", ErrInvalidLockFile, p.ID, err)
		}
	}
	switch p.Installer {
	case InstallerJSM:
		if p.Digest == "" {
			return fmt.Errorf("%w: jsm skill %s missing digest", ErrInvalidLockFile, p.ID)
		}
	case InstallerJFP:
		if p.Version == "" {
			return fmt.Errorf("%w: jfp skill %s missing version", ErrInvalidLockFile, p.ID)
		}
		if !p.Advisory {
			return fmt.Errorf("%w: jfp skill %s must be advisory", ErrInvalidLockFile, p.ID)
		}
	}
	return nil
}

func (p SkillPin) VersionSpec() SkillVersion {
	return SkillVersion{
		ID:          p.ID,
		Installer:   p.Installer,
		Digest:      p.Digest,
		Version:     p.Version,
		Advisory:    p.Advisory,
		ContentHash: p.ContentHash,
	}
}

func PinFromVersion(version SkillVersion, pinnedAt time.Time, pinnedBy string) SkillPin {
	return SkillPin{
		ID:          version.ID,
		Installer:   version.Installer,
		Digest:      version.Digest,
		Version:     version.Version,
		Advisory:    version.Installer == InstallerJFP || version.Advisory,
		PinnedAt:    pinnedAt.UTC().Format(time.RFC3339Nano),
		PinnedBy:    pinnedBy,
		ContentHash: version.ContentHash,
	}
}

func (lf LockFile) Validate() error {
	if lf.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schemaVersion %d != %d", ErrInvalidLockFile, lf.SchemaVersion, SchemaVersion)
	}
	seen := make(map[string]struct{}, len(lf.Skills))
	for _, pin := range lf.Skills {
		if err := pin.Validate(); err != nil {
			return err
		}
		if _, ok := seen[pin.ID]; ok {
			return fmt.Errorf("%w: duplicate skill %s", ErrInvalidLockFile, pin.ID)
		}
		seen[pin.ID] = struct{}{}
	}
	return nil
}

func (lf LockFile) Normalized() (LockFile, error) {
	if lf.SchemaVersion == 0 {
		lf.SchemaVersion = SchemaVersion
	}
	for i := range lf.Skills {
		if lf.Skills[i].Digest != "" {
			digest, err := NormalizeDigest(lf.Skills[i].Digest)
			if err != nil {
				return LockFile{}, err
			}
			lf.Skills[i].Digest = digest
		}
		if lf.Skills[i].ContentHash != "" {
			contentHash, err := NormalizeDigest(lf.Skills[i].ContentHash)
			if err != nil {
				return LockFile{}, err
			}
			lf.Skills[i].ContentHash = contentHash
		}
	}
	sort.Slice(lf.Skills, func(i, j int) bool {
		return lf.Skills[i].ID < lf.Skills[j].ID
	})
	return lf, nil
}

func (lf LockFile) PinFor(id string) (SkillPin, bool) {
	for _, pin := range lf.Skills {
		if pin.ID == id {
			return pin, true
		}
	}
	return SkillPin{}, false
}

func (lf *LockFile) Upsert(pin SkillPin) {
	for i := range lf.Skills {
		if lf.Skills[i].ID == pin.ID {
			lf.Skills[i] = pin
			return
		}
	}
	lf.Skills = append(lf.Skills, pin)
}

func CanonicalBytes(lock LockFile) ([]byte, error) {
	normalized, err := lock.Normalized()
	if err != nil {
		return nil, err
	}
	if err := normalized.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func LockHash(lock LockFile) (string, error) {
	data, err := CanonicalBytes(lock)
	if err != nil {
		return "", err
	}
	return DigestBytes(data), nil
}

type LockStore interface {
	Load(context.Context) (LockFile, bool, error)
	Save(context.Context, LockFile) error
	Path() string
}

type FileLockStore struct {
	ProjectDir string
}

func (s FileLockStore) Path() string {
	return filepath.Join(s.ProjectDir, ".hoopoe", "skills.lock.json")
}

func (s FileLockStore) Load(ctx context.Context) (LockFile, bool, error) {
	if err := ctx.Err(); err != nil {
		return LockFile{}, false, err
	}
	data, err := os.ReadFile(s.Path())
	if os.IsNotExist(err) {
		return LockFile{SchemaVersion: SchemaVersion}, false, nil
	}
	if err != nil {
		return LockFile{}, false, err
	}
	var lock LockFile
	if err := json.Unmarshal(data, &lock); err != nil {
		return LockFile{}, false, fmt.Errorf("%w: %v", ErrInvalidLockFile, err)
	}
	normalized, err := lock.Normalized()
	if err != nil {
		return LockFile{}, false, err
	}
	if err := normalized.Validate(); err != nil {
		return LockFile{}, false, err
	}
	return normalized, true, nil
}

func (s FileLockStore) Save(ctx context.Context, lock LockFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := CanonicalBytes(lock)
	if err != nil {
		return err
	}
	path := s.Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".skills.lock.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}
