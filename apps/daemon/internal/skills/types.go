// Package skills resolves and verifies Hoopoe skill content for tending jobs.
package skills

import (
	"errors"
	"fmt"
	"strings"
)

const SchemaVersion = 1

var (
	ErrInvalidLockFile = errors.New("skills: invalid lock file")
	ErrInvalidRequest  = errors.New("skills: invalid request")
	ErrUnavailable     = errors.New("skills: installer unavailable")
	ErrNotInCatalog    = errors.New("skills: skill not in catalog")
	ErrDigestMismatch  = errors.New("skills: digest mismatch")
)

type Installer string

const (
	InstallerJSM Installer = "jsm"
	InstallerJFP Installer = "jfp"
)

func (i Installer) Valid() bool {
	switch i {
	case InstallerJSM, InstallerJFP:
		return true
	}
	return false
}

type ResolutionStatus string

const (
	StatusOK       ResolutionStatus = "ok"
	StatusDegraded ResolutionStatus = "degraded"
	StatusBlocked  ResolutionStatus = "blocked"
)

type DiagnosticSeverity string

const (
	SeverityInfo  DiagnosticSeverity = "info"
	SeverityWarn  DiagnosticSeverity = "warn"
	SeverityBlock DiagnosticSeverity = "block"
)

type Diagnostic struct {
	SkillID  string             `json:"skillId"`
	Code     string             `json:"code"`
	Severity DiagnosticSeverity `json:"severity"`
	Message  string             `json:"message"`
}

type LoadedSkill struct {
	ID          string    `json:"id"`
	Installer   Installer `json:"installer"`
	Digest      string    `json:"digest,omitempty"`
	Version     string    `json:"version,omitempty"`
	Advisory    bool      `json:"advisory,omitempty"`
	ContentHash string    `json:"contentHash,omitempty"`
	Floating    bool      `json:"floating,omitempty"`
}

type SkillVersion struct {
	ID          string
	Installer   Installer
	Digest      string
	Version     string
	Advisory    bool
	ContentHash string
}

func (v SkillVersion) Validate() error {
	_, err := v.Normalized()
	return err
}

func (v SkillVersion) Normalized() (SkillVersion, error) {
	if !validSkillID(v.ID) {
		return SkillVersion{}, fmt.Errorf("%w: invalid skill id %q", ErrInvalidRequest, v.ID)
	}
	if !v.Installer.Valid() {
		return SkillVersion{}, fmt.Errorf("%w: invalid installer %q", ErrInvalidRequest, v.Installer)
	}
	if v.Digest != "" {
		digest, err := NormalizeDigest(v.Digest)
		if err != nil {
			return SkillVersion{}, err
		}
		v.Digest = digest
	}
	if v.ContentHash != "" {
		contentHash, err := NormalizeDigest(v.ContentHash)
		if err != nil {
			return SkillVersion{}, err
		}
		v.ContentHash = contentHash
	}
	switch v.Installer {
	case InstallerJSM:
		if v.Digest == "" {
			return SkillVersion{}, fmt.Errorf("%w: jsm skill %s missing digest", ErrInvalidRequest, v.ID)
		}
	case InstallerJFP:
		if v.Version == "" {
			return SkillVersion{}, fmt.Errorf("%w: jfp skill %s missing advisory version", ErrInvalidRequest, v.ID)
		}
	}
	return v, nil
}

func (v SkillVersion) Loaded(floating bool) LoadedSkill {
	return LoadedSkill{
		ID:          v.ID,
		Installer:   v.Installer,
		Digest:      v.Digest,
		Version:     v.Version,
		Advisory:    v.Advisory,
		ContentHash: v.ContentHash,
		Floating:    floating,
	}
}

type DigestMismatchError struct {
	SkillID  string
	Expected string
	Actual   string
}

func (e DigestMismatchError) Error() string {
	return fmt.Sprintf("skills: digest mismatch for %s: expected %s got %s", e.SkillID, e.Expected, e.Actual)
}

func (e DigestMismatchError) Is(target error) bool {
	return target == ErrDigestMismatch
}

func validSkillID(id string) bool {
	if id == "" || len(id) > 128 || strings.Contains(id, "..") {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}
