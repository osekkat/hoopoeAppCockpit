package skills

import (
	"context"
	"testing"
	"time"
)

func TestLoaderVerifiesJSMPinFromLockFile(t *testing.T) {
	ctx := context.Background()
	digest := DigestBytes([]byte("vibing-with-ntm skill"))
	store := &memoryStore{
		exists: true,
		lock: LockFile{
			SchemaVersion: SchemaVersion,
			Skills: []SkillPin{{
				ID:        "vibing-with-ntm",
				Installer: InstallerJSM,
				Digest:    digest,
				PinnedAt:  "2026-05-04T00:00:00Z",
				PinnedBy:  "test",
			}},
		},
	}
	jsm := &fakeProvider{
		installer: InstallerJSM,
		verifyFn: func(context.Context, SkillPin) (SkillVersion, error) {
			return SkillVersion{ID: "vibing-with-ntm", Installer: InstallerJSM, Digest: digest}, nil
		},
	}
	jfp := &fakeProvider{installer: InstallerJFP}
	loader := Loader{Store: store, JSM: jsm, JFP: jfp}

	resolution, err := loader.Resolve(ctx, ResolveRequest{SkillIDs: []string{"vibing-with-ntm"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Status != StatusOK || resolution.LockChanged {
		t.Fatalf("unexpected resolution: %+v", resolution)
	}
	if len(resolution.Skills) != 1 || resolution.Skills[0].Installer != InstallerJSM || resolution.Skills[0].Digest != digest {
		t.Fatalf("bad loaded skill: %+v", resolution.Skills)
	}
	if jsm.verifyCalls != 1 || jsm.installCalls != 0 || jfp.installCalls != 0 {
		t.Fatalf("unexpected provider calls: jsm verify=%d install=%d jfp install=%d", jsm.verifyCalls, jsm.installCalls, jfp.installCalls)
	}
	if len(store.saved) != 0 {
		t.Fatalf("verified lock was rewritten: %d saves", len(store.saved))
	}
}

func TestLoaderBlocksDigestMismatchWithoutFallback(t *testing.T) {
	ctx := context.Background()
	expected := DigestBytes([]byte("expected"))
	actual := DigestBytes([]byte("actual"))
	store := &memoryStore{
		exists: true,
		lock: LockFile{
			SchemaVersion: SchemaVersion,
			Skills: []SkillPin{{
				ID:        "ntm",
				Installer: InstallerJSM,
				Digest:    expected,
			}},
		},
	}
	jsm := &fakeProvider{
		installer: InstallerJSM,
		verifyFn: func(context.Context, SkillPin) (SkillVersion, error) {
			return SkillVersion{ID: "ntm", Installer: InstallerJSM, Digest: actual}, nil
		},
	}
	jfp := &fakeProvider{
		installer: InstallerJFP,
		installFn: func(context.Context, string, *SkillPin) (SkillVersion, error) {
			return SkillVersion{ID: "ntm", Installer: InstallerJFP, Version: "v1.0.0", Advisory: true}, nil
		},
	}
	loader := Loader{Store: store, JSM: jsm, JFP: jfp}

	resolution, err := loader.Resolve(ctx, ResolveRequest{SkillIDs: []string{"ntm"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Status != StatusBlocked {
		t.Fatalf("status = %s, want blocked; resolution=%+v", resolution.Status, resolution)
	}
	if len(resolution.Diagnostics) != 1 || resolution.Diagnostics[0].Code != "skill.digest_mismatch" {
		t.Fatalf("bad diagnostics: %+v", resolution.Diagnostics)
	}
	if jfp.installCalls != 0 {
		t.Fatalf("digest mismatch fell back to jfp")
	}
	if len(store.saved) != 0 {
		t.Fatalf("blocked resolve rewrote lock: %d saves", len(store.saved))
	}
}

func TestLoaderFallsBackToJFPAndWritesAdvisoryLock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	store := &memoryStore{lock: LockFile{SchemaVersion: SchemaVersion}}
	jsm := &fakeProvider{
		installer: InstallerJSM,
		installFn: func(context.Context, string, *SkillPin) (SkillVersion, error) {
			return SkillVersion{}, ErrUnavailable
		},
	}
	contentHash := DigestBytes([]byte("jfp ntm content"))
	jfp := &fakeProvider{
		installer: InstallerJFP,
		installFn: func(_ context.Context, id string, _ *SkillPin) (SkillVersion, error) {
			return SkillVersion{ID: id, Installer: InstallerJFP, Version: "v1.2.3", Advisory: true, ContentHash: contentHash}, nil
		},
	}
	var audits []AuditEvent
	loader := Loader{
		Store:    store,
		JSM:      jsm,
		JFP:      jfp,
		Now:      func() time.Time { return now },
		PinnedBy: "tester",
		Audit: func(_ context.Context, event AuditEvent) error {
			audits = append(audits, event)
			return nil
		},
	}

	resolution, err := loader.Resolve(ctx, ResolveRequest{SkillIDs: []string{"ntm"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Status != StatusDegraded || !resolution.LockChanged {
		t.Fatalf("unexpected resolution: %+v", resolution)
	}
	if len(store.saved) != 1 {
		t.Fatalf("lock saves = %d, want 1", len(store.saved))
	}
	pin, ok := store.lock.PinFor("ntm")
	if !ok {
		t.Fatal("ntm pin missing")
	}
	if pin.Installer != InstallerJFP || !pin.Advisory || pin.Version != "v1.2.3" || pin.PinnedBy != "tester" || pin.ContentHash != contentHash {
		t.Fatalf("bad fallback pin: %+v", pin)
	}
	if len(audits) != 1 || audits[0].Action != "skills.lock.changed" || audits[0].LockHash == "" {
		t.Fatalf("bad audit events: %+v", audits)
	}
	if !hasDiagnostic(resolution.Diagnostics, "installer.fallback") || !hasDiagnostic(resolution.Diagnostics, "skill.advisory_pin") {
		t.Fatalf("missing fallback diagnostics: %+v", resolution.Diagnostics)
	}
}

func TestLoaderWarnsWhenVerifyingJFPAdvisoryPin(t *testing.T) {
	ctx := context.Background()
	store := &memoryStore{
		exists: true,
		lock: LockFile{
			SchemaVersion: SchemaVersion,
			Skills: []SkillPin{{
				ID:        "ntm",
				Installer: InstallerJFP,
				Version:   "v1.2.3",
				Advisory:  true,
			}},
		},
	}
	jfp := &fakeProvider{
		installer: InstallerJFP,
		verifyFn: func(_ context.Context, pin SkillPin) (SkillVersion, error) {
			return SkillVersion{ID: pin.ID, Installer: InstallerJFP, Version: pin.Version, Advisory: true}, nil
		},
	}
	loader := Loader{Store: store, JFP: jfp}

	resolution, err := loader.Resolve(ctx, ResolveRequest{SkillIDs: []string{"ntm"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Status != StatusDegraded {
		t.Fatalf("status = %s, want degraded; resolution=%+v", resolution.Status, resolution)
	}
	if !hasDiagnostic(resolution.Diagnostics, "skill.advisory_pin") {
		t.Fatalf("missing advisory diagnostic: %+v", resolution.Diagnostics)
	}
	if len(store.saved) != 0 {
		t.Fatalf("verified advisory lock was rewritten")
	}
}

func TestLoaderAllowsFloatingDevelopmentSkillWithWarning(t *testing.T) {
	ctx := context.Background()
	digest := DigestBytes([]byte("floating skill"))
	store := &memoryStore{lock: LockFile{SchemaVersion: SchemaVersion}}
	jsm := &fakeProvider{
		installer: InstallerJSM,
		installFn: func(_ context.Context, id string, _ *SkillPin) (SkillVersion, error) {
			return SkillVersion{ID: id, Installer: InstallerJSM, Digest: digest}, nil
		},
	}
	loader := Loader{Store: store, JSM: jsm}

	resolution, err := loader.Resolve(ctx, ResolveRequest{
		SkillIDs:      []string{"vibing-with-ntm"},
		AllowFloating: true,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolution.Status != StatusDegraded || resolution.LockChanged {
		t.Fatalf("unexpected resolution: %+v", resolution)
	}
	if len(store.saved) != 0 {
		t.Fatalf("floating resolve wrote lock")
	}
	if len(resolution.Skills) != 1 || !resolution.Skills[0].Floating {
		t.Fatalf("floating skill not marked: %+v", resolution.Skills)
	}
	if !hasDiagnostic(resolution.Diagnostics, "skill.floating") {
		t.Fatalf("missing floating diagnostic: %+v", resolution.Diagnostics)
	}
}

type memoryStore struct {
	lock   LockFile
	exists bool
	saved  []LockFile
}

func (s *memoryStore) Load(context.Context) (LockFile, bool, error) {
	normalized, err := s.lock.Normalized()
	if err != nil {
		return LockFile{}, false, err
	}
	return normalized, s.exists, nil
}

func (s *memoryStore) Save(_ context.Context, lock LockFile) error {
	normalized, err := lock.Normalized()
	if err != nil {
		return err
	}
	s.lock = normalized
	s.exists = true
	s.saved = append(s.saved, normalized)
	return nil
}

func (s *memoryStore) Path() string {
	return "/project/.hoopoe/skills.lock.json"
}

type fakeProvider struct {
	installer    Installer
	verifyFn     func(context.Context, SkillPin) (SkillVersion, error)
	installFn    func(context.Context, string, *SkillPin) (SkillVersion, error)
	verifyCalls  int
	installCalls int
}

func (p *fakeProvider) Installer() Installer {
	return p.installer
}

func (p *fakeProvider) Verify(ctx context.Context, pin SkillPin) (SkillVersion, error) {
	p.verifyCalls++
	if p.verifyFn == nil {
		return SkillVersion{}, ErrUnavailable
	}
	return p.verifyFn(ctx, pin)
}

func (p *fakeProvider) Install(ctx context.Context, id string, pin *SkillPin) (SkillVersion, error) {
	p.installCalls++
	if p.installFn == nil {
		return SkillVersion{}, ErrUnavailable
	}
	return p.installFn(ctx, id, pin)
}

func hasDiagnostic(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
