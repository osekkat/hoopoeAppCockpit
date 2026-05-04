package upgrade

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/release"
)

func TestUpgradeSuccessLocksWritesAndRunsLifecycle(t *testing.T) {
	ctx := context.Background()
	fixture := newServiceFixture(t)
	var svc *Service
	installer := &fakeInstaller{}
	installer.onInstall = func() {
		state := svc.State()
		if !state.InProgress || !state.RefusesWrites() || state.Phase != PhaseInstalling {
			t.Fatalf("state during install = %+v, want installing read-only", state)
		}
	}
	fixture.installer = installer
	svc = fixture.service()

	result, err := svc.Upgrade(ctx, fixture.request())
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	if result.Status != "succeeded" || result.Version != "1.2.3" || result.BackupID != "backup-1" {
		t.Fatalf("result = %+v", result)
	}
	if result.State.InProgress || result.State.WriteMode != WriteModeNormal || result.State.Phase != PhaseSucceeded {
		t.Fatalf("final state = %+v", result.State)
	}
	if got, want := fixture.manager.calls, []string{"stop", "start"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("service calls = %v, want %v", got, want)
	}
	if !fixture.backup.backedUp || fixture.backup.restored {
		t.Fatalf("backup state backedUp=%v restored=%v", fixture.backup.backedUp, fixture.backup.restored)
	}
}

func TestUpgradeBackupFailureAbortsBeforeServiceStop(t *testing.T) {
	fixture := newServiceFixture(t)
	fixture.backup.backupErr = errors.New("disk full")
	svc := fixture.service()

	result, err := svc.Upgrade(context.Background(), fixture.request())
	if !errors.Is(err, ErrBackupFailed) {
		t.Fatalf("err = %v, want ErrBackupFailed", err)
	}
	if len(fixture.manager.calls) != 0 {
		t.Fatalf("service calls after backup failure = %v", fixture.manager.calls)
	}
	if fixture.installer.installed {
		t.Fatalf("installer ran despite backup failure")
	}
	if result.State.InProgress || result.State.WriteMode != WriteModeNormal || result.State.Phase != PhaseFailed {
		t.Fatalf("final state = %+v", result.State)
	}
}

func TestUpgradePostInstallFailureRollsBackBackupAndBinary(t *testing.T) {
	fixture := newServiceFixture(t)
	fixture.checker.err = errors.New("version endpoint returned 1.2.2")
	svc := fixture.service()

	result, err := svc.Upgrade(context.Background(), fixture.request())
	if !errors.Is(err, ErrPostInstallFailed) {
		t.Fatalf("err = %v, want ErrPostInstallFailed", err)
	}
	if !result.RolledBack || result.Status != "rolled_back" || result.State.Phase != PhaseRolledBack {
		t.Fatalf("result = %+v", result)
	}
	if result.State.WriteMode != WriteModeNormal || result.State.InProgress {
		t.Fatalf("final state = %+v", result.State)
	}
	if !fixture.backup.restored {
		t.Fatalf("backup was not restored")
	}
	if got, want := fixture.manager.calls, []string{"stop", "start", "stop", "start"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("service calls = %v, want %v", got, want)
	}
}

func TestUpgradeInsecureDevOverrideIsAuditedAndBannered(t *testing.T) {
	fixture := newServiceFixture(t)
	fixture.verifier.result.Status = release.StatusOverride
	fixture.verifier.result.DiagnosticsBanner = release.InsecureOverrideBanner
	fixture.verifier.result.Diagnostics = []release.Diagnostic{{
		Code:     "checksum_mismatch",
		Severity: release.SeverityCritical,
		Message:  "checksum mismatch",
	}}
	req := fixture.request()
	req.Override = release.Override{
		Enabled: true,
		Actor:   "dev@example.com",
		Reason:  "fixture override",
		At:      time.Unix(200, 0).UTC(),
	}
	svc := fixture.service()

	result, err := svc.Upgrade(context.Background(), req)
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	if result.DiagnosticsBanner != release.InsecureOverrideBanner || !result.State.InsecureOverride {
		t.Fatalf("override result = %+v", result)
	}
	if !fixture.audit.has(ActionUpgradeInsecureDevOverride) {
		t.Fatalf("audit events = %+v, want insecure override event", fixture.audit.events)
	}
}

func TestUpgradeBlocksIncompatibleDesktopBeforeBackup(t *testing.T) {
	fixture := newServiceFixture(t)
	fixture.fetcher.bundle.Manifest.Compatibility.MinDesktopVersion = "2.0.0"
	req := fixture.request()
	req.CurrentDesktopVersion = "1.9.9"
	svc := fixture.service()

	result, err := svc.Upgrade(context.Background(), req)
	if !errors.Is(err, ErrDesktopUpgradeRequired) {
		t.Fatalf("err = %v, want ErrDesktopUpgradeRequired", err)
	}
	if fixture.backup.backedUp || fixture.installer.installed {
		t.Fatalf("backup/install ran despite incompatible desktop")
	}
	if result.State.Phase != PhaseFailed || result.State.WriteMode != WriteModeNormal {
		t.Fatalf("state = %+v", result.State)
	}
}

func TestHandlerServesStateAndUpgradeProblems(t *testing.T) {
	fixture := newServiceFixture(t)
	svc := fixture.service()
	handler := Handler{Service: svc}

	stateReq := httptest.NewRequest(http.MethodGet, "/v1/bootstrap/daemon/upgrade", nil)
	stateRec := httptest.NewRecorder()
	handler.ServeHTTP(stateRec, stateReq)
	if stateRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", stateRec.Code, stateRec.Body.String())
	}

	badReq := httptest.NewRequest(http.MethodPost, "/v1/bootstrap/daemon/upgrade", strings.NewReader(`{"schemaVersion":99}`))
	badRec := httptest.NewRecorder()
	handler.ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest || !strings.Contains(badRec.Body.String(), "upgrade.invalid_request") {
		t.Fatalf("POST bad status=%d body=%s", badRec.Code, badRec.Body.String())
	}
}

func TestFileBackupStoreRestoresBinaryDatabaseAndConfig(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	binaryPath := filepath.Join(root, "bin", "hoopoed")
	dbPath := filepath.Join(root, "daemon.db")
	configPath := filepath.Join(root, "config.json")
	mustWrite(t, binaryPath, "old-binary", 0o755)
	mustWrite(t, dbPath, "old-db", 0o600)
	mustWrite(t, configPath, "old-config", 0o600)

	store := FileBackupStore{
		BackupDir: filepath.Join(root, "backups"),
		Now:       func() time.Time { return time.Unix(100, 0).UTC() },
	}
	record, err := store.Backup(ctx, BackupRequest{
		TargetBinaryPath: binaryPath,
		DatabasePath:     dbPath,
		ConfigPaths:      []string{configPath},
		Version:          "1.2.3",
	})
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if len(record.Files) != 3 {
		t.Fatalf("backup files = %+v", record.Files)
	}

	mustWrite(t, binaryPath, "new-binary", 0o755)
	mustWrite(t, dbPath, "new-db", 0o600)
	mustWrite(t, configPath, "new-config", 0o600)

	if err := store.Restore(ctx, record); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	assertFile(t, binaryPath, "old-binary")
	assertFile(t, dbPath, "old-db")
	assertFile(t, configPath, "old-config")
	info, err := os.Stat(binaryPath)
	if err != nil {
		t.Fatalf("Stat binary: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("binary mode = %v, want 0755", info.Mode().Perm())
	}
}

type serviceFixture struct {
	fetcher   *fakeFetcher
	verifier  *fakeVerifier
	backup    *fakeBackup
	manager   *fakeServiceManager
	installer *fakeInstaller
	checker   *fakeChecker
	audit     *fakeAudit
	now       time.Time
}

func newServiceFixture(t *testing.T) *serviceFixture {
	t.Helper()
	now := time.Unix(100, 0).UTC()
	bundle := ReleaseBundle{
		Manifest: release.Manifest{
			SchemaVersion: release.SchemaVersion,
			Version:       "1.2.3",
			Artifact: release.ArtifactManifest{
				Name:   "hoopoed",
				SHA256: release.DigestBytes([]byte("new-binary")),
			},
			SourceCommit: "abc123",
			Signing: release.SigningManifest{
				KeyID:     "key-1",
				Identity:  "Hoopoe Test Release Key",
				Algorithm: release.SignatureAlgorithmEd25519,
			},
			Provenance: release.ProvenancePolicy{
				BuilderID:    "github-actions://hoopoe/release",
				WorkflowRef:  "refs/tags/v1.2.3",
				WorkflowPath: ".github/workflows/release.yml",
			},
			Compatibility: release.Compatibility{
				MinDesktopVersion: "1.0.0",
				MinAPIVersion:     "1.0.0",
			},
		},
		Binary:      []byte("new-binary"),
		Signature:   []byte("sig"),
		Attestation: []byte(`{}`),
		SBOM:        []byte(`{}`),
		CVEDatabase: release.CVEDatabase{SchemaVersion: release.SchemaVersion},
	}
	verification := release.VerificationResult{
		Status: release.StatusVerified,
		Inventory: release.InventoryRecord{
			SchemaVersion:     release.SchemaVersion,
			VerifiedAt:        now.Format(time.RFC3339Nano),
			Status:            release.StatusVerified,
			Version:           "1.2.3",
			ArtifactName:      "hoopoed",
			Checksum:          release.DigestBytes([]byte("new-binary")),
			SBOMDigest:        release.DigestBytes([]byte(`{}`)),
			AttestationDigest: release.DigestBytes([]byte(`{}`)),
			SourceCommit:      "abc123",
			BuilderID:         "github-actions://hoopoe/release",
			WorkflowRef:       "refs/tags/v1.2.3",
			WorkflowPath:      ".github/workflows/release.yml",
			SLSALevel:         release.DefaultMinimumSLSALevel,
			Reproducible:      true,
			SigningIdentity:   "Hoopoe Test Release Key",
			SigningKeyID:      "key-1",
			MinDesktopVersion: "1.0.0",
			MinAPIVersion:     "1.0.0",
		},
	}
	return &serviceFixture{
		fetcher:  &fakeFetcher{bundle: bundle},
		verifier: &fakeVerifier{result: verification},
		backup: &fakeBackup{record: BackupRecord{
			ID:               "backup-1",
			TargetBinaryPath: "/tmp/hoopoed",
			DatabasePath:     "/tmp/daemon.db",
			ConfigPaths:      []string{"/tmp/config.json"},
			Files: []BackupFile{{
				Kind:       "database",
				SourcePath: "/tmp/daemon.db",
				BackupPath: "/tmp/backups/daemon.db",
				Present:    true,
			}},
		}},
		manager:   &fakeServiceManager{},
		installer: &fakeInstaller{},
		checker:   &fakeChecker{},
		audit:     &fakeAudit{},
		now:       now,
	}
}

func (f *serviceFixture) service() *Service {
	return NewService(Config{
		Fetcher:        f.fetcher,
		Verifier:       f.verifier,
		Backup:         f.backup,
		ServiceManager: f.manager,
		Installer:      f.installer,
		Checker:        f.checker,
		Audit:          f.audit,
		Now:            func() time.Time { return f.now },
	})
}

func (f *serviceFixture) request() Request {
	return Request{
		SchemaVersion:         SchemaVersion,
		Release:               ReleaseLocation{BinaryURL: "https://example.test/hoopoed"},
		TargetBinaryPath:      "/tmp/hoopoed",
		InventoryPath:         "/tmp/inventory.json",
		DatabasePath:          "/tmp/daemon.db",
		ConfigPaths:           []string{"/tmp/config.json"},
		BackupDir:             "/tmp/backups",
		CurrentDesktopVersion: "1.2.0",
		CurrentAPIVersion:     "1.0.0",
		Actor:                 "operator",
		Reason:                "test upgrade",
	}
}

type fakeFetcher struct {
	bundle ReleaseBundle
	err    error
	calls  int
}

func (f *fakeFetcher) Fetch(context.Context, ReleaseLocation) (ReleaseBundle, error) {
	f.calls++
	if f.err != nil {
		return ReleaseBundle{}, f.err
	}
	return f.bundle, nil
}

type fakeVerifier struct {
	result release.VerificationResult
	err    error
	calls  int
}

func (f *fakeVerifier) Verify(context.Context, release.VerifyRequest) (release.VerificationResult, error) {
	f.calls++
	return f.result, f.err
}

type fakeBackup struct {
	record     BackupRecord
	backupErr  error
	restoreErr error
	backedUp   bool
	restored   bool
}

func (f *fakeBackup) Backup(context.Context, BackupRequest) (BackupRecord, error) {
	if f.backupErr != nil {
		return BackupRecord{}, f.backupErr
	}
	f.backedUp = true
	return f.record, nil
}

func (f *fakeBackup) Restore(context.Context, BackupRecord) error {
	f.restored = true
	return f.restoreErr
}

type fakeServiceManager struct {
	calls []string
	errs  map[string]error
}

func (f *fakeServiceManager) Stop(context.Context) error {
	f.calls = append(f.calls, "stop")
	if f.errs != nil {
		return f.errs["stop"]
	}
	return nil
}

func (f *fakeServiceManager) Start(context.Context) error {
	f.calls = append(f.calls, "start")
	if f.errs != nil {
		return f.errs["start"]
	}
	return nil
}

type fakeInstaller struct {
	installed bool
	err       error
	onInstall func()
}

func (f *fakeInstaller) Install(context.Context, InstallRequest) error {
	if f.onInstall != nil {
		f.onInstall()
	}
	if f.err != nil {
		return f.err
	}
	f.installed = true
	return nil
}

type fakeChecker struct {
	err error
}

func (f *fakeChecker) Check(context.Context, PostInstallCheck) error {
	return f.err
}

type fakeAudit struct {
	events []AuditEvent
	err    error
}

func (f *fakeAudit) AppendUpgradeAudit(_ context.Context, event AuditEvent) error {
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, event)
	return nil
}

func (f *fakeAudit) has(action string) bool {
	for _, event := range f.events {
		if event.Action == action {
			return true
		}
	}
	return false
}

func mustWrite(t *testing.T, path, value string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(value), mode); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}
