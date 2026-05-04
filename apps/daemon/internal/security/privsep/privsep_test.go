package privsep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAllowlistEnforcesKnownActionsAndArgShapes(t *testing.T) {
	t.Parallel()
	allowlist := DefaultAllowlist()
	if _, ok := allowlist.Entry("arbitrary-shell-command"); ok {
		t.Fatalf("unexpected arbitrary action in allowlist")
	}
	entry, ok := allowlist.Entry("install-systemd-unit")
	if !ok {
		t.Fatalf("missing install-systemd-unit")
	}
	err := entry.ValidateArgs([]string{"--unit-path=/tmp/evil.service", "--source=/tmp/unit"})
	if !errors.Is(err, ErrArgvShapeMismatch) {
		t.Fatalf("evil unit path err = %v, want ErrArgvShapeMismatch", err)
	}
	if err := entry.ValidateArgs([]string{"--unit-path=/etc/systemd/system/hoopoe.service", "--source=/tmp/unit"}); err != nil {
		t.Fatalf("valid unit args: %v", err)
	}
	chownEntry, ok := allowlist.Entry("chown-acfs-paths")
	if !ok {
		t.Fatalf("missing chown-acfs-paths")
	}
	if err := chownEntry.ValidateArgs([]string{"--path=/data/projects"}); err != nil {
		t.Fatalf("valid project root chown args: %v", err)
	}
	if err := chownEntry.ValidateArgs([]string{"--path=/data/projects/alpha"}); err != nil {
		t.Fatalf("valid project child chown args: %v", err)
	}
	if err := chownEntry.ValidateArgs([]string{"--path=/data/projects_evil"}); !errors.Is(err, ErrArgvShapeMismatch) {
		t.Fatalf("evil project path err = %v, want ErrArgvShapeMismatch", err)
	}
}

func TestEngineRejectsUnknownActionAndAuditsRejection(t *testing.T) {
	t.Parallel()
	allowlist := DefaultAllowlist()
	audit := &recordingAudit{}
	engine := Engine{
		Executor: fakeExecutor{},
		Audit:    audit,
		Now:      fixedNow,
	}

	_, err := engine.Run(context.Background(), Invocation{
		Mode:      ModeBootstrap,
		Action:    "arbitrary-shell-command",
		Args:      []string{"--cmd=sh"},
		Allowlist: allowlist,
	})
	if !errors.Is(err, ErrActionNotAllowed) {
		t.Fatalf("err = %v, want ErrActionNotAllowed", err)
	}
	if len(audit.events) != 1 || audit.events[0].Result != string(ResultRejected) {
		t.Fatalf("audit events = %+v", audit.events)
	}
}

func TestEngineDetectsAllowlistChecksumTampering(t *testing.T) {
	t.Parallel()
	allowlist := DefaultAllowlist()
	engine := Engine{
		Executor: fakeExecutor{},
		Audit:    &recordingAudit{},
		Now:      fixedNow,
	}

	_, err := engine.Run(context.Background(), Invocation{
		Mode:                    ModeBootstrap,
		Action:                  "install-systemd-unit",
		Args:                    []string{"--unit-path=/etc/systemd/system/hoopoe.service", "--source=/tmp/unit"},
		Allowlist:               allowlist,
		ExpectedAllowlistDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	})
	if !errors.Is(err, ErrAllowlistChecksumMismatch) {
		t.Fatalf("err = %v, want ErrAllowlistChecksumMismatch", err)
	}
}

func TestApprovalTokenValidationBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	store := NewMemoryTokenStore()
	store.now = func() time.Time { return now }
	authority := HMACTokenAuthority{
		Secret: []byte("0123456789abcdef0123456789abcdef"),
		Store:  store,
		Now:    func() time.Time { return now },
	}
	allowlist := DefaultAllowlist()
	token, err := authority.Mint(TokenRequest{
		ApprovalID:      "appr_123",
		Actor:           "owner",
		Action:          "repair-acfs",
		ExpiresAt:       now.Add(10 * time.Minute),
		AllowlistDigest: allowlist.Digest,
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if _, err := authority.ValidateApprovalToken(ctx, "", TokenValidationRequest{Action: "repair-acfs", AllowlistDigest: allowlist.Digest, Now: now}); !errors.Is(err, ErrApprovalTokenMissing) {
		t.Fatalf("missing token err = %v", err)
	}
	if _, err := authority.ValidateApprovalToken(ctx, token, TokenValidationRequest{Action: "restart-service", AllowlistDigest: allowlist.Digest, Now: now}); !errors.Is(err, ErrApprovalActionMismatch) {
		t.Fatalf("action mismatch err = %v", err)
	}
	wrongSecret := HMACTokenAuthority{Secret: []byte("fedcba9876543210fedcba9876543210"), Store: NewMemoryTokenStore(), Now: authority.Now}
	if _, err := wrongSecret.ValidateApprovalToken(ctx, token, TokenValidationRequest{Action: "repair-acfs", AllowlistDigest: allowlist.Digest, Now: now}); !errors.Is(err, ErrApprovalInvalid) {
		t.Fatalf("wrong secret err = %v", err)
	}
	noReplayStore := HMACTokenAuthority{Secret: authority.Secret, Now: authority.Now}
	if _, err := noReplayStore.ValidateApprovalToken(ctx, token, TokenValidationRequest{Action: "repair-acfs", AllowlistDigest: allowlist.Digest, Now: now}); !errors.Is(err, ErrApprovalInvalid) {
		t.Fatalf("missing replay store err = %v", err)
	}
	expiredToken, err := authority.Mint(TokenRequest{
		ApprovalID:      "appr_expired",
		Actor:           "owner",
		Action:          "repair-acfs",
		ExpiresAt:       now.Add(-time.Minute),
		AllowlistDigest: allowlist.Digest,
	})
	if err != nil {
		t.Fatalf("Mint expired: %v", err)
	}
	if _, err := authority.ValidateApprovalToken(ctx, expiredToken, TokenValidationRequest{Action: "repair-acfs", AllowlistDigest: allowlist.Digest, Now: now}); !errors.Is(err, ErrApprovalExpired) {
		t.Fatalf("expired err = %v", err)
	}
	if _, err := authority.ValidateApprovalToken(ctx, token, TokenValidationRequest{Action: "repair-acfs", AllowlistDigest: allowlist.Digest, Now: now}); err != nil {
		t.Fatalf("first validation: %v", err)
	}
	if _, err := authority.ValidateApprovalToken(ctx, token, TokenValidationRequest{Action: "repair-acfs", AllowlistDigest: allowlist.Digest, Now: now}); !errors.Is(err, ErrApprovalAlreadyUsed) {
		t.Fatalf("replay err = %v", err)
	}
}

func TestSteadyStateRequiresApprovalTokenAndConsumesIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Unix(100, 0).UTC()
	allowlist := DefaultAllowlist()
	store := NewMemoryTokenStore()
	store.now = func() time.Time { return now }
	authority := HMACTokenAuthority{
		Secret: []byte("0123456789abcdef0123456789abcdef"),
		Store:  store,
		Now:    func() time.Time { return now },
	}
	token, err := authority.Mint(TokenRequest{
		ApprovalID:      "appr_456",
		Actor:           "owner",
		Action:          "repair-acfs",
		ExpiresAt:       now.Add(10 * time.Minute),
		AllowlistDigest: allowlist.Digest,
	})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	audit := &recordingAudit{}
	executor := fakeExecutor{}
	engine := Engine{
		TokenValidator: authority,
		Executor:       executor,
		Audit:          audit,
		Now:            func() time.Time { return now },
	}
	result, err := engine.Run(ctx, Invocation{
		Mode:          ModeSteadyState,
		Action:        "repair-acfs",
		Args:          []string{"--doctor=true", "--auto-fix=true"},
		ApprovalToken: token,
		Allowlist:     allowlist,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Status != ResultSucceeded || result.ApprovalID != "appr_456" || result.Actor != "owner" {
		t.Fatalf("result = %+v", result)
	}
	if len(audit.events) != 1 || audit.events[0].ApprovalID != "appr_456" {
		t.Fatalf("audit events = %+v", audit.events)
	}
}

func TestBootstrapModeAllowsOnlyBootstrapActionsAndDeferredAudit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	allowlist := DefaultAllowlist()
	audit := &recordingAudit{}
	engine := Engine{
		Executor: fakeExecutor{},
		Audit:    audit,
		Now:      fixedNow,
	}
	result, err := engine.Run(ctx, Invocation{
		Mode:      ModeBootstrap,
		Action:    "install-systemd-unit",
		Args:      []string{"--unit-path=/etc/systemd/system/hoopoe.service", "--source=/tmp/unit"},
		Allowlist: allowlist,
	})
	if err != nil {
		t.Fatalf("bootstrap install: %v", err)
	}
	if !result.BootstrapMode || result.Status != ResultSucceeded {
		t.Fatalf("bootstrap result = %+v", result)
	}
	if len(audit.events) != 1 || !audit.events[0].BootstrapMode {
		t.Fatalf("audit events = %+v", audit.events)
	}
	_, err = engine.Run(ctx, Invocation{
		Mode:      ModeBootstrap,
		Action:    "repair-acfs",
		Args:      []string{"--doctor=true", "--auto-fix=false"},
		Allowlist: allowlist,
	})
	if !errors.Is(err, ErrBootstrapRejected) {
		t.Fatalf("steady action in bootstrap err = %v", err)
	}
}

func TestFileAuditSinkWritesJSONL(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "setup-helper.bootstrap.log")
	sink := FileAuditSink{Path: path}
	if err := sink.AppendSetupHelperAudit(context.Background(), AuditEvent{
		SchemaVersion: SchemaVersion,
		Source:        "hoopoe-setup-helper",
		Action:        "install-systemd-unit",
		Result:        "succeeded",
		BootstrapMode: true,
		Time:          fixedNow(),
	}); err != nil {
		t.Fatalf("AppendSetupHelperAudit: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"bootstrapMode":true`) {
		t.Fatalf("audit data = %s", data)
	}
}

func TestDaemonUserSpecAndSudoersRuleAreNarrow(t *testing.T) {
	t.Parallel()
	spec := DefaultDaemonUserSpec()
	argv := spec.UserAddArgv()
	if !contains(argv, "--system") || !contains(argv, DaemonUser) || !contains(argv, DaemonShell) {
		t.Fatalf("useradd argv = %v", argv)
	}
	rule := SudoersRule(DefaultHelperPath)
	if !strings.Contains(rule, "NOPASSWD: /usr/local/bin/hoopoe-setup-helper run --approval-token=*") {
		t.Fatalf("sudoers rule = %q", rule)
	}
	if strings.Contains(rule, "ALL") && strings.Contains(rule, " /bin/") {
		t.Fatalf("sudoers rule grants broad bin path: %q", rule)
	}
	if err := ValidateSudoersRule(rule, DefaultHelperPath); err != nil {
		t.Fatalf("ValidateSudoersRule: %v", err)
	}
}

type fakeExecutor struct{}

func (fakeExecutor) ExecutePrivilegedAction(context.Context, string, []string) (ActionResult, error) {
	return ActionResult{ExitCode: 0, Output: "ok"}, nil
}

type recordingAudit struct {
	events []AuditEvent
}

func (a *recordingAudit) AppendSetupHelperAudit(_ context.Context, event AuditEvent) error {
	a.events = append(a.events, event)
	return nil
}

func fixedNow() time.Time {
	return time.Unix(100, 0).UTC()
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
