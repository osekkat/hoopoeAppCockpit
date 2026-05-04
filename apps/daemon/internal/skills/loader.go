package skills

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Provider interface {
	Installer() Installer
	Verify(context.Context, SkillPin) (SkillVersion, error)
	Install(context.Context, string, *SkillPin) (SkillVersion, error)
}

type AuditSink func(context.Context, AuditEvent) error

type AuditEvent struct {
	Action       string    `json:"action"`
	SkillID      string    `json:"skillId,omitempty"`
	Installer    Installer `json:"installer,omitempty"`
	LockPath     string    `json:"lockPath,omitempty"`
	LockHash     string    `json:"lockHash,omitempty"`
	PreviousHash string    `json:"previousHash,omitempty"`
	Message      string    `json:"message,omitempty"`
}

type Loader struct {
	Store    LockStore
	JSM      Provider
	JFP      Provider
	Now      func() time.Time
	PinnedBy string
	Audit    AuditSink
}

type ResolveRequest struct {
	SkillIDs      []string
	AllowFloating bool
	PinnedBy      string
}

type Resolution struct {
	Status      ResolutionStatus `json:"status"`
	Skills      []LoadedSkill    `json:"skills"`
	Diagnostics []Diagnostic     `json:"diagnostics"`
	LockChanged bool             `json:"lockChanged"`
	LockPath    string           `json:"lockPath,omitempty"`
	LockHash    string           `json:"lockHash,omitempty"`
}

func (l Loader) Resolve(ctx context.Context, req ResolveRequest) (Resolution, error) {
	if l.Store == nil {
		return Resolution{}, fmt.Errorf("%w: nil lock store", ErrInvalidRequest)
	}
	if len(req.SkillIDs) == 0 {
		return Resolution{}, fmt.Errorf("%w: no skill ids", ErrInvalidRequest)
	}
	for _, id := range req.SkillIDs {
		if !validSkillID(id) {
			return Resolution{}, fmt.Errorf("%w: invalid skill id %q", ErrInvalidRequest, id)
		}
	}

	lock, existed, err := l.Store.Load(ctx)
	if err != nil {
		return Resolution{}, err
	}
	previousHash := ""
	if existed {
		if previousHash, err = LockHash(lock); err != nil {
			return Resolution{}, err
		}
	}

	out := Resolution{
		Status:   StatusOK,
		LockPath: l.Store.Path(),
	}
	changed := false
	for _, id := range req.SkillIDs {
		pin, pinned := lock.PinFor(id)
		resolved, nextPin, diagnostics := l.resolveOne(ctx, id, pin, pinned, req.AllowFloating)
		out.Diagnostics = append(out.Diagnostics, diagnostics...)
		if resolved.Blocked {
			out.Status = StatusBlocked
			continue
		}
		if resolved.Skill.ID != "" {
			out.Skills = append(out.Skills, resolved.Skill)
		}
		if nextPin != nil && !req.AllowFloating {
			lock.Upsert(*nextPin)
			changed = true
		}
		if out.Status == StatusOK && hasWarning(diagnostics) {
			out.Status = StatusDegraded
		}
	}

	if out.Status == StatusBlocked {
		return out, nil
	}
	if changed {
		normalized, err := lock.Normalized()
		if err != nil {
			return Resolution{}, err
		}
		if err := l.Store.Save(ctx, normalized); err != nil {
			return Resolution{}, err
		}
		lock = normalized
		out.LockChanged = true
		if err := l.emit(ctx, AuditEvent{
			Action:       "skills.lock.changed",
			LockPath:     l.Store.Path(),
			PreviousHash: previousHash,
			Message:      "skill lock changed during resolve",
		}); err != nil {
			return Resolution{}, err
		}
	}
	if out.LockHash, err = LockHash(lock); err != nil {
		return Resolution{}, err
	}
	return out, nil
}

type oneResult struct {
	Skill   LoadedSkill
	Blocked bool
}

func (l Loader) resolveOne(ctx context.Context, id string, pin SkillPin, pinned bool, floating bool) (oneResult, *SkillPin, []Diagnostic) {
	if pinned {
		return l.resolvePinned(ctx, pin, floating)
	}
	result, diagnostics := l.installWithFallback(ctx, id, nil, floating)
	if result.Blocked || floating {
		if floating && !result.Blocked {
			diagnostics = append(diagnostics, Diagnostic{
				SkillID:  id,
				Code:     "skill.floating",
				Severity: SeverityWarn,
				Message:  "skill resolved without a lock-file pin; development project may drift",
			})
		}
		return result, nil, diagnostics
	}
	pin = PinFromVersion(SkillVersion{
		ID:          result.Skill.ID,
		Installer:   result.Skill.Installer,
		Digest:      result.Skill.Digest,
		Version:     result.Skill.Version,
		Advisory:    result.Skill.Advisory,
		ContentHash: result.Skill.ContentHash,
	}, l.now(), l.pinnedBy(""))
	return result, &pin, diagnostics
}

func (l Loader) resolvePinned(ctx context.Context, pin SkillPin, floating bool) (oneResult, *SkillPin, []Diagnostic) {
	var diagnostics []Diagnostic
	provider := l.provider(pin.Installer)
	if provider == nil {
		diagnostics = append(diagnostics, Diagnostic{
			SkillID:  pin.ID,
			Code:     "installer.missing",
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("%s installer is unavailable; trying fallback", pin.Installer),
		})
		return l.fallbackFromPin(ctx, pin, floating, diagnostics)
	}
	version, err := provider.Verify(ctx, pin)
	if err == nil {
		normalized, err := version.Normalized()
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				SkillID:  pin.ID,
				Code:     "installer.invalid_result",
				Severity: SeverityBlock,
				Message:  err.Error(),
			})
			return oneResult{Blocked: true}, nil, diagnostics
		}
		if mismatch := digestMismatch(pin, normalized); mismatch != nil {
			diagnostics = append(diagnostics, Diagnostic{
				SkillID:  pin.ID,
				Code:     "skill.digest_mismatch",
				Severity: SeverityBlock,
				Message:  mismatch.Error(),
			})
			return oneResult{Blocked: true}, nil, diagnostics
		}
		if pin.Installer == InstallerJFP {
			diagnostics = append(diagnostics, Diagnostic{
				SkillID:  pin.ID,
				Code:     "skill.advisory_pin",
				Severity: SeverityWarn,
				Message:  "jfp skill pin is advisory; SHA-256 verification is unavailable",
			})
		}
		return oneResult{Skill: normalized.Loaded(floating)}, nil, diagnostics
	}
	if errors.Is(err, ErrDigestMismatch) {
		diagnostics = append(diagnostics, Diagnostic{
			SkillID:  pin.ID,
			Code:     "skill.digest_mismatch",
			Severity: SeverityBlock,
			Message:  err.Error(),
		})
		return oneResult{Blocked: true}, nil, diagnostics
	}
	if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNotInCatalog) {
		diagnostics = append(diagnostics, Diagnostic{
			SkillID:  pin.ID,
			Code:     "installer.fallback",
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("%s could not verify %s; trying fallback: %v", pin.Installer, pin.ID, err),
		})
		return l.fallbackFromPin(ctx, pin, floating, diagnostics)
	}
	diagnostics = append(diagnostics, Diagnostic{
		SkillID:  pin.ID,
		Code:     "skill.verify_failed",
		Severity: SeverityBlock,
		Message:  err.Error(),
	})
	return oneResult{Blocked: true}, nil, diagnostics
}

func (l Loader) fallbackFromPin(ctx context.Context, pin SkillPin, floating bool, diagnostics []Diagnostic) (oneResult, *SkillPin, []Diagnostic) {
	result, more := l.installWithFallback(ctx, pin.ID, &pin, floating)
	diagnostics = append(diagnostics, more...)
	if result.Blocked || floating {
		return result, nil, diagnostics
	}
	next := PinFromVersion(SkillVersion{
		ID:          result.Skill.ID,
		Installer:   result.Skill.Installer,
		Digest:      result.Skill.Digest,
		Version:     result.Skill.Version,
		Advisory:    result.Skill.Advisory,
		ContentHash: result.Skill.ContentHash,
	}, l.now(), l.pinnedBy(pin.PinnedBy))
	return result, &next, diagnostics
}

func (l Loader) installWithFallback(ctx context.Context, id string, pin *SkillPin, floating bool) (oneResult, []Diagnostic) {
	var diagnostics []Diagnostic
	if l.JSM != nil {
		version, err := l.JSM.Install(ctx, id, pin)
		if err == nil {
			if version.Installer == "" {
				version.Installer = InstallerJSM
			}
			if normalized, err := version.Normalized(); err == nil {
				return oneResult{Skill: normalized.Loaded(floating)}, diagnostics
			}
			diagnostics = append(diagnostics, Diagnostic{
				SkillID:  id,
				Code:     "installer.invalid_result",
				Severity: SeverityWarn,
				Message:  "jsm returned a skill without a verifiable digest; trying jfp fallback",
			})
		} else if errors.Is(err, ErrDigestMismatch) {
			diagnostics = append(diagnostics, Diagnostic{
				SkillID:  id,
				Code:     "skill.digest_mismatch",
				Severity: SeverityBlock,
				Message:  err.Error(),
			})
			return oneResult{Blocked: true}, diagnostics
		} else if errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNotInCatalog) {
			diagnostics = append(diagnostics, Diagnostic{
				SkillID:  id,
				Code:     "installer.fallback",
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("jsm unavailable for %s; trying jfp: %v", id, err),
			})
		} else {
			diagnostics = append(diagnostics, Diagnostic{
				SkillID:  id,
				Code:     "installer.error",
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("jsm failed for %s; trying jfp: %v", id, err),
			})
		}
	}
	if l.JFP != nil {
		version, err := l.JFP.Install(ctx, id, pin)
		if err == nil {
			if version.Installer == "" {
				version.Installer = InstallerJFP
			}
			version.Advisory = true
			normalized, err := version.Normalized()
			if err != nil {
				diagnostics = append(diagnostics, Diagnostic{
					SkillID:  id,
					Code:     "installer.invalid_result",
					Severity: SeverityBlock,
					Message:  err.Error(),
				})
				return oneResult{Blocked: true}, diagnostics
			}
			diagnostics = append(diagnostics, Diagnostic{
				SkillID:  id,
				Code:     "skill.advisory_pin",
				Severity: SeverityWarn,
				Message:  "skill resolved through jfp fallback; version pin is advisory",
			})
			return oneResult{Skill: normalized.Loaded(floating)}, diagnostics
		}
		diagnostics = append(diagnostics, Diagnostic{
			SkillID:  id,
			Code:     "installer.error",
			Severity: SeverityBlock,
			Message:  fmt.Sprintf("jfp failed for %s: %v", id, err),
		})
		return oneResult{Blocked: true}, diagnostics
	}
	diagnostics = append(diagnostics, Diagnostic{
		SkillID:  id,
		Code:     "installer.missing",
		Severity: SeverityBlock,
		Message:  "both jsm and jfp installers are unavailable",
	})
	return oneResult{Blocked: true}, diagnostics
}

func digestMismatch(pin SkillPin, version SkillVersion) error {
	if pin.Installer != InstallerJSM || pin.Digest == "" {
		return nil
	}
	actual := version.Digest
	if actual == "" {
		actual = version.ContentHash
	}
	if sameDigest(pin.Digest, actual) {
		return nil
	}
	return DigestMismatchError{SkillID: pin.ID, Expected: pin.Digest, Actual: actual}
}

func hasWarning(diagnostics []Diagnostic) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == SeverityWarn {
			return true
		}
	}
	return false
}

func (l Loader) provider(installer Installer) Provider {
	switch installer {
	case InstallerJSM:
		return l.JSM
	case InstallerJFP:
		return l.JFP
	default:
		return nil
	}
}

func (l Loader) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now().UTC()
}

func (l Loader) pinnedBy(fallback string) string {
	if l.PinnedBy != "" {
		return l.PinnedBy
	}
	if fallback != "" {
		return fallback
	}
	return "daemon"
}

func (l Loader) emit(ctx context.Context, event AuditEvent) error {
	if l.Audit == nil {
		return nil
	}
	if event.LockHash == "" && l.Store != nil {
		lock, _, err := l.Store.Load(ctx)
		if err == nil {
			event.LockHash, _ = LockHash(lock)
		}
	}
	return l.Audit(ctx, event)
}
