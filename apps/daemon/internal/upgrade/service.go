// Package upgrade orchestrates daemon binary upgrades.
//
// The release package proves a candidate binary is authentic. This package
// turns that proof into the daemon lifecycle: fetch, verify, compatibility
// gate, backup, stop, install, start, post-install check, and rollback.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/release"
)

const SchemaVersion = 1

const (
	ActionUpgradeStarted             = "daemon.upgrade.started"
	ActionUpgradeSucceeded           = "daemon.upgrade.succeeded"
	ActionUpgradeFailed              = "daemon.upgrade.failed"
	ActionUpgradeRolledBack          = "daemon.upgrade.rolled_back"
	ActionUpgradeInsecureDevOverride = "daemon.upgrade.insecure_dev_override"
)

var (
	ErrInvalidRequest         = errors.New("upgrade: invalid request")
	ErrUpgradeInProgress      = errors.New("upgrade: already in progress")
	ErrVerificationFailed     = errors.New("upgrade: release verification failed")
	ErrDesktopUpgradeRequired = errors.New("upgrade: desktop upgrade required")
	ErrBackupFailed           = errors.New("upgrade: backup failed")
	ErrStopFailed             = errors.New("upgrade: stop service failed")
	ErrInstallFailed          = errors.New("upgrade: install failed")
	ErrStartFailed            = errors.New("upgrade: start service failed")
	ErrPostInstallFailed      = errors.New("upgrade: post-install check failed")
	ErrRollbackFailed         = errors.New("upgrade: rollback failed")
)

type Phase string

const (
	PhaseIdle        Phase = "idle"
	PhaseFetching    Phase = "fetching"
	PhaseVerifying   Phase = "verifying"
	PhaseBackingUp   Phase = "backing_up"
	PhaseStopping    Phase = "stopping"
	PhaseInstalling  Phase = "installing"
	PhaseStarting    Phase = "starting"
	PhaseChecking    Phase = "checking"
	PhaseRollingBack Phase = "rolling_back"
	PhaseSucceeded   Phase = "succeeded"
	PhaseFailed      Phase = "failed"
	PhaseRolledBack  Phase = "rolled_back"
)

type WriteMode string

const (
	WriteModeNormal              WriteMode = "normal"
	WriteModeReadOnlyDiagnostics WriteMode = "read_only_diagnostics"
)

// State is the desktop-facing upgrade state. While InProgress is true,
// mutating desktop/daemon actions must be refused and Diagnostics may continue
// to read state.
type State struct {
	SchemaVersion     int       `json:"schemaVersion"`
	Phase             Phase     `json:"phase"`
	WriteMode         WriteMode `json:"writeMode"`
	InProgress        bool      `json:"inProgress"`
	TargetVersion     string    `json:"targetVersion,omitempty"`
	StartedAt         time.Time `json:"startedAt,omitempty"`
	CompletedAt       time.Time `json:"completedAt,omitempty"`
	LastError         string    `json:"lastError,omitempty"`
	InsecureOverride  bool      `json:"insecureOverride,omitempty"`
	DiagnosticsBanner string    `json:"diagnosticsBanner,omitempty"`
}

func (s State) RefusesWrites() bool {
	return s.WriteMode == WriteModeReadOnlyDiagnostics
}

type Request struct {
	SchemaVersion int `json:"schemaVersion"`

	Release ReleaseLocation `json:"release"`

	TargetBinaryPath string   `json:"targetBinaryPath"`
	InventoryPath    string   `json:"inventoryPath,omitempty"`
	DatabasePath     string   `json:"databasePath,omitempty"`
	ConfigPaths      []string `json:"configPaths,omitempty"`
	BackupDir        string   `json:"backupDir,omitempty"`

	CurrentDesktopVersion string `json:"currentDesktopVersion,omitempty"`
	CurrentAPIVersion     string `json:"currentApiVersion,omitempty"`
	ExpectedVersion       string `json:"expectedVersion,omitempty"`

	Override            release.Override            `json:"override,omitempty"`
	SBOMAcknowledgement release.SBOMAcknowledgement `json:"sbomAcknowledgement,omitempty"`

	Actor  string `json:"actor,omitempty"`
	Reason string `json:"reason,omitempty"`
	Force  bool   `json:"force,omitempty"`
}

type ReleaseLocation struct {
	BinaryURL      string `json:"binaryUrl"`
	ManifestURL    string `json:"manifestUrl"`
	SignatureURL   string `json:"signatureUrl"`
	AttestationURL string `json:"attestationUrl"`
	SBOMURL        string `json:"sbomUrl"`
	CVEDatabaseURL string `json:"cveDatabaseUrl,omitempty"`
}

type ReleaseBundle struct {
	Manifest    release.Manifest
	Binary      []byte
	Signature   []byte
	Attestation []byte
	SBOM        []byte
	CVEDatabase release.CVEDatabase
}

type UpgradeResult struct {
	SchemaVersion     int                        `json:"schemaVersion"`
	Status            string                     `json:"status"`
	Version           string                     `json:"version,omitempty"`
	BackupID          string                     `json:"backupId,omitempty"`
	RolledBack        bool                       `json:"rolledBack,omitempty"`
	Verification      release.VerificationResult `json:"verification,omitempty"`
	Diagnostics       []Diagnostic               `json:"diagnostics,omitempty"`
	DiagnosticsBanner string                     `json:"diagnosticsBanner,omitempty"`
	State             State                      `json:"state"`
}

type Diagnostic struct {
	Code     string           `json:"code"`
	Severity release.Severity `json:"severity"`
	Message  string           `json:"message"`
}

type Fetcher interface {
	Fetch(ctx context.Context, location ReleaseLocation) (ReleaseBundle, error)
}

type ReleaseVerifier interface {
	Verify(ctx context.Context, req release.VerifyRequest) (release.VerificationResult, error)
}

type BackupStore interface {
	Backup(ctx context.Context, req BackupRequest) (BackupRecord, error)
	Restore(ctx context.Context, record BackupRecord) error
}

type ServiceManager interface {
	Stop(ctx context.Context) error
	Start(ctx context.Context) error
}

type Installer interface {
	Install(ctx context.Context, req InstallRequest) error
}

type PostInstallChecker interface {
	Check(ctx context.Context, req PostInstallCheck) error
}

type AuditSink interface {
	AppendUpgradeAudit(ctx context.Context, event AuditEvent) error
}

type BackupRequest struct {
	TargetBinaryPath string
	DatabasePath     string
	ConfigPaths      []string
	BackupDir        string
	Version          string
}

type BackupRecord struct {
	ID               string
	TargetBinaryPath string
	DatabasePath     string
	ConfigPaths      []string
	Files            []BackupFile
}

type BackupFile struct {
	Kind       string
	SourcePath string
	BackupPath string
	Present    bool
}

type InstallRequest struct {
	TargetBinaryPath string
	InventoryPath    string
	Binary           []byte
	Inventory        release.InventoryRecord
}

type PostInstallCheck struct {
	ExpectedVersion       string
	ExpectedAPIVersion    string
	CurrentDesktopVersion string
	Inventory             release.InventoryRecord
}

type AuditEvent struct {
	Action    string         `json:"action"`
	Actor     string         `json:"actor,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Result    string         `json:"result"`
	Data      map[string]any `json:"data,omitempty"`
}

type Service struct {
	fetcher        Fetcher
	verifier       ReleaseVerifier
	backup         BackupStore
	serviceManager ServiceManager
	installer      Installer
	checker        PostInstallChecker
	audit          AuditSink
	now            func() time.Time

	mu    sync.Mutex
	state State
}

type Config struct {
	Fetcher        Fetcher
	Verifier       ReleaseVerifier
	Backup         BackupStore
	ServiceManager ServiceManager
	Installer      Installer
	Checker        PostInstallChecker
	Audit          AuditSink
	Now            func() time.Time
}

func NewService(cfg Config) *Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	audit := cfg.Audit
	if audit == nil {
		audit = NoopAudit{}
	}
	return &Service{
		fetcher:        cfg.Fetcher,
		verifier:       cfg.Verifier,
		backup:         cfg.Backup,
		serviceManager: cfg.ServiceManager,
		installer:      cfg.Installer,
		checker:        cfg.Checker,
		audit:          audit,
		now:            now,
		state: State{
			SchemaVersion: SchemaVersion,
			Phase:         PhaseIdle,
			WriteMode:     WriteModeNormal,
		},
	}
}

func (s *Service) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *Service) Upgrade(ctx context.Context, req Request) (UpgradeResult, error) {
	if err := s.validateRequest(req); err != nil {
		return s.result("", release.VerificationResult{}, nil), err
	}
	if !s.begin(req) {
		return s.result("", release.VerificationResult{}, nil), ErrUpgradeInProgress
	}

	actor := firstNonEmpty(req.Actor, req.Override.Actor, "operator")
	reason := firstNonEmpty(req.Reason, req.Override.Reason)
	_ = s.audit.AppendUpgradeAudit(ctx, AuditEvent{
		Action:    ActionUpgradeStarted,
		Actor:     actor,
		Reason:    reason,
		Timestamp: s.now().UTC(),
		Result:    "started",
	})

	s.setPhase(PhaseFetching, "", "")
	bundle, err := s.fetcher.Fetch(ctx, req.Release)
	if err != nil {
		return s.fail(ctx, actor, reason, "", release.VerificationResult{}, nil, fmt.Errorf("upgrade: fetch release: %w", err))
	}
	s.setTarget(bundle.Manifest.Version)

	s.setPhase(PhaseVerifying, "", "")
	verification, err := s.verifier.Verify(ctx, release.VerifyRequest{
		Manifest:            bundle.Manifest,
		Binary:              bundle.Binary,
		Signature:           bundle.Signature,
		Attestation:         bundle.Attestation,
		SBOM:                bundle.SBOM,
		CVEDatabase:         bundle.CVEDatabase,
		Override:            req.Override,
		SBOMAcknowledgement: req.SBOMAcknowledgement,
	})
	if err != nil {
		return s.fail(ctx, actor, reason, bundle.Manifest.Version, verification, nil, fmt.Errorf("%w: %v", ErrVerificationFailed, err))
	}
	if verification.Status == release.StatusOverride {
		if err := s.audit.AppendUpgradeAudit(ctx, AuditEvent{
			Action:    ActionUpgradeInsecureDevOverride,
			Actor:     firstNonEmpty(req.Override.Actor, actor),
			Reason:    firstNonEmpty(req.Override.Reason, reason),
			Timestamp: s.now().UTC(),
			Result:    "override",
			Data: map[string]any{
				"version":       bundle.Manifest.Version,
				"artifactName":  bundle.Manifest.Artifact.Name,
				"diagnostics":   releaseDiagnosticCodes(verification.Diagnostics),
				"insecureLabel": "INSECURE",
			},
		}); err != nil {
			return s.fail(ctx, actor, reason, bundle.Manifest.Version, verification, nil, fmt.Errorf("upgrade: audit insecure override: %w", err))
		}
	}
	if err := checkCompatibility(bundle.Manifest, req.CurrentDesktopVersion, req.CurrentAPIVersion); err != nil {
		return s.fail(ctx, actor, reason, bundle.Manifest.Version, verification, nil, err)
	}

	s.setPhase(PhaseBackingUp, "", verification.DiagnosticsBanner)
	backup, err := s.backup.Backup(ctx, BackupRequest{
		TargetBinaryPath: req.TargetBinaryPath,
		DatabasePath:     req.DatabasePath,
		ConfigPaths:      req.ConfigPaths,
		BackupDir:        req.BackupDir,
		Version:          bundle.Manifest.Version,
	})
	if err != nil {
		return s.fail(ctx, actor, reason, bundle.Manifest.Version, verification, nil, fmt.Errorf("%w: %v", ErrBackupFailed, err))
	}

	s.setPhase(PhaseStopping, "", verification.DiagnosticsBanner)
	if err := s.serviceManager.Stop(ctx); err != nil {
		return s.fail(ctx, actor, reason, bundle.Manifest.Version, verification, &backup, fmt.Errorf("%w: %v", ErrStopFailed, err))
	}

	s.setPhase(PhaseInstalling, "", verification.DiagnosticsBanner)
	if err := s.installer.Install(ctx, InstallRequest{
		TargetBinaryPath: req.TargetBinaryPath,
		InventoryPath:    req.InventoryPath,
		Binary:           bundle.Binary,
		Inventory:        verification.Inventory,
	}); err != nil {
		return s.rollback(ctx, actor, reason, bundle.Manifest.Version, verification, backup, fmt.Errorf("%w: %v", ErrInstallFailed, err))
	}

	s.setPhase(PhaseStarting, "", verification.DiagnosticsBanner)
	if err := s.serviceManager.Start(ctx); err != nil {
		return s.rollback(ctx, actor, reason, bundle.Manifest.Version, verification, backup, fmt.Errorf("%w: %v", ErrStartFailed, err))
	}

	s.setPhase(PhaseChecking, "", verification.DiagnosticsBanner)
	expectedVersion := firstNonEmpty(req.ExpectedVersion, bundle.Manifest.Version)
	if err := s.checker.Check(ctx, PostInstallCheck{
		ExpectedVersion:       expectedVersion,
		ExpectedAPIVersion:    bundle.Manifest.Compatibility.MinAPIVersion,
		CurrentDesktopVersion: req.CurrentDesktopVersion,
		Inventory:             verification.Inventory,
	}); err != nil {
		return s.rollback(ctx, actor, reason, bundle.Manifest.Version, verification, backup, fmt.Errorf("%w: %v", ErrPostInstallFailed, err))
	}

	s.finish(PhaseSucceeded, "", verification.DiagnosticsBanner, false)
	_ = s.audit.AppendUpgradeAudit(ctx, AuditEvent{
		Action:    ActionUpgradeSucceeded,
		Actor:     actor,
		Reason:    reason,
		Timestamp: s.now().UTC(),
		Result:    "succeeded",
		Data: map[string]any{
			"version":  bundle.Manifest.Version,
			"backupId": backup.ID,
			"status":   verification.Status,
		},
	})
	return s.result(bundle.Manifest.Version, verification, &backup), nil
}

func (s *Service) rollback(ctx context.Context, actor, reason, version string, verification release.VerificationResult, backup BackupRecord, cause error) (UpgradeResult, error) {
	s.setPhase(PhaseRollingBack, cause.Error(), verification.DiagnosticsBanner)
	var rollbackErr error
	if err := s.serviceManager.Stop(ctx); err != nil {
		rollbackErr = errors.Join(rollbackErr, fmt.Errorf("stop before rollback: %w", err))
	}
	if err := s.backup.Restore(ctx, backup); err != nil {
		rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore backup: %w", err))
	}
	if err := s.serviceManager.Start(ctx); err != nil {
		rollbackErr = errors.Join(rollbackErr, fmt.Errorf("start after rollback: %w", err))
	}
	if rollbackErr != nil {
		combined := fmt.Errorf("%w: %v; original failure: %v", ErrRollbackFailed, rollbackErr, cause)
		s.finish(PhaseFailed, combined.Error(), verification.DiagnosticsBanner, true)
		_ = s.audit.AppendUpgradeAudit(ctx, AuditEvent{
			Action:    ActionUpgradeFailed,
			Actor:     actor,
			Reason:    reason,
			Timestamp: s.now().UTC(),
			Result:    "rollback_failed",
			Data: map[string]any{
				"version":  version,
				"backupId": backup.ID,
				"error":    combined.Error(),
			},
		})
		return s.result(version, verification, &backup), combined
	}

	s.finish(PhaseRolledBack, cause.Error(), verification.DiagnosticsBanner, false)
	_ = s.audit.AppendUpgradeAudit(ctx, AuditEvent{
		Action:    ActionUpgradeRolledBack,
		Actor:     actor,
		Reason:    reason,
		Timestamp: s.now().UTC(),
		Result:    "rolled_back",
		Data: map[string]any{
			"version":  version,
			"backupId": backup.ID,
			"error":    cause.Error(),
		},
	})
	result := s.result(version, verification, &backup)
	result.RolledBack = true
	result.Status = "rolled_back"
	return result, cause
}

func (s *Service) fail(ctx context.Context, actor, reason, version string, verification release.VerificationResult, backup *BackupRecord, err error) (UpgradeResult, error) {
	keepReadOnly := errors.Is(err, ErrRollbackFailed)
	s.finish(PhaseFailed, err.Error(), verification.DiagnosticsBanner, keepReadOnly)
	_ = s.audit.AppendUpgradeAudit(ctx, AuditEvent{
		Action:    ActionUpgradeFailed,
		Actor:     actor,
		Reason:    reason,
		Timestamp: s.now().UTC(),
		Result:    "failed",
		Data: map[string]any{
			"version": version,
			"error":   err.Error(),
		},
	})
	return s.result(version, verification, backup), err
}

func (s *Service) validateRequest(req Request) error {
	if req.SchemaVersion != 0 && req.SchemaVersion != SchemaVersion {
		return fmt.Errorf("%w: schemaVersion %d != %d", ErrInvalidRequest, req.SchemaVersion, SchemaVersion)
	}
	if s.fetcher == nil {
		return fmt.Errorf("%w: fetcher is required", ErrInvalidRequest)
	}
	if s.verifier == nil {
		return fmt.Errorf("%w: verifier is required", ErrInvalidRequest)
	}
	if s.backup == nil {
		return fmt.Errorf("%w: backup store is required", ErrInvalidRequest)
	}
	if s.serviceManager == nil {
		return fmt.Errorf("%w: service manager is required", ErrInvalidRequest)
	}
	if s.installer == nil {
		return fmt.Errorf("%w: installer is required", ErrInvalidRequest)
	}
	if s.checker == nil {
		return fmt.Errorf("%w: post-install checker is required", ErrInvalidRequest)
	}
	if strings.TrimSpace(req.TargetBinaryPath) == "" {
		return fmt.Errorf("%w: targetBinaryPath is required", ErrInvalidRequest)
	}
	return nil
}

func (s *Service) begin(req Request) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.InProgress {
		return false
	}
	s.state = State{
		SchemaVersion:    SchemaVersion,
		Phase:            PhaseFetching,
		WriteMode:        WriteModeReadOnlyDiagnostics,
		InProgress:       true,
		StartedAt:        s.now().UTC(),
		TargetVersion:    req.ExpectedVersion,
		InsecureOverride: req.Override.Enabled,
	}
	return true
}

func (s *Service) setTarget(version string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.TargetVersion = version
}

func (s *Service) setPhase(phase Phase, lastErr, banner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Phase = phase
	if lastErr != "" {
		s.state.LastError = lastErr
	}
	if banner != "" {
		s.state.DiagnosticsBanner = banner
	}
}

func (s *Service) finish(phase Phase, lastErr, banner string, keepReadOnly bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state.Phase = phase
	s.state.InProgress = false
	if keepReadOnly {
		s.state.WriteMode = WriteModeReadOnlyDiagnostics
	} else {
		s.state.WriteMode = WriteModeNormal
	}
	s.state.CompletedAt = s.now().UTC()
	s.state.LastError = lastErr
	s.state.DiagnosticsBanner = banner
}

func (s *Service) result(version string, verification release.VerificationResult, backup *BackupRecord) UpgradeResult {
	state := s.State()
	status := string(state.Phase)
	if state.Phase == PhaseSucceeded {
		status = "succeeded"
	}
	result := UpgradeResult{
		SchemaVersion:     SchemaVersion,
		Status:            status,
		Version:           version,
		Verification:      verification,
		DiagnosticsBanner: verification.DiagnosticsBanner,
		State:             state,
	}
	if backup != nil {
		result.BackupID = backup.ID
	}
	for _, diagnostic := range verification.Diagnostics {
		result.Diagnostics = append(result.Diagnostics, Diagnostic{
			Code:     diagnostic.Code,
			Severity: diagnostic.Severity,
			Message:  diagnostic.Message,
		})
	}
	return result
}

func checkCompatibility(manifest release.Manifest, currentDesktop, currentAPI string) error {
	minDesktop := manifest.Compatibility.MinDesktopVersion
	if minDesktop != "" && currentDesktop != "" && !versionAtLeast(currentDesktop, minDesktop) {
		return fmt.Errorf("%w: daemon %s requires desktop >= %s, current desktop is %s",
			ErrDesktopUpgradeRequired, manifest.Version, minDesktop, currentDesktop)
	}
	minAPI := manifest.Compatibility.MinAPIVersion
	if minAPI != "" && currentAPI != "" && !versionAtLeast(currentAPI, minAPI) {
		return fmt.Errorf("%w: daemon %s requires API >= %s, current API is %s",
			ErrDesktopUpgradeRequired, manifest.Version, minAPI, currentAPI)
	}
	return nil
}

func versionAtLeast(current, minimum string) bool {
	currentParts := parseVersion(current)
	minimumParts := parseVersion(minimum)
	for i := 0; i < len(currentParts) || i < len(minimumParts); i++ {
		cur, min := 0, 0
		if i < len(currentParts) {
			cur = currentParts[i]
		}
		if i < len(minimumParts) {
			min = minimumParts[i]
		}
		if cur > min {
			return true
		}
		if cur < min {
			return false
		}
	}
	return true
}

func parseVersion(value string) []int {
	value = strings.TrimSpace(strings.TrimPrefix(value, "v"))
	value = strings.TrimPrefix(value, "api-")
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		digits := strings.Builder{}
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			digits.WriteRune(r)
		}
		if digits.Len() == 0 {
			out = append(out, 0)
			continue
		}
		n, err := strconv.Atoi(digits.String())
		if err != nil {
			out = append(out, 0)
			continue
		}
		out = append(out, n)
	}
	return out
}

func releaseDiagnosticCodes(diagnostics []release.Diagnostic) []string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return codes
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

type NoopAudit struct{}

func (NoopAudit) AppendUpgradeAudit(context.Context, AuditEvent) error { return nil }
