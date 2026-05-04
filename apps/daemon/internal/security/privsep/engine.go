package privsep

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidInvocation = errors.New("privsep: invalid helper invocation")
	ErrBootstrapRejected = errors.New("privsep: action is not permitted in bootstrap mode")
)

type Mode string

const (
	ModeBootstrap   Mode = "bootstrap"
	ModeSteadyState Mode = "steady_state"
)

type ResultStatus string

const (
	ResultSucceeded ResultStatus = "succeeded"
	ResultRejected  ResultStatus = "rejected"
	ResultFailed    ResultStatus = "failed"
)

type Invocation struct {
	Mode                    Mode
	Action                  string
	Args                    []string
	Actor                   string
	ApprovalToken           string
	Allowlist               Allowlist
	ExpectedAllowlistDigest string
	BootstrapAuditPath      string
}

type Result struct {
	SchemaVersion   int          `json:"schemaVersion"`
	Status          ResultStatus `json:"status"`
	Action          string       `json:"action"`
	Actor           string       `json:"actor,omitempty"`
	BootstrapMode   bool         `json:"bootstrapMode,omitempty"`
	ApprovalID      string       `json:"approvalId,omitempty"`
	AllowlistDigest string       `json:"allowlistDigest,omitempty"`
	CommandPreview  string       `json:"commandPreview,omitempty"`
	ExitCode        int          `json:"exitCode"`
	Error           string       `json:"error,omitempty"`
	StartedAt       time.Time    `json:"startedAt"`
	CompletedAt     time.Time    `json:"completedAt"`
}

type Engine struct {
	TokenValidator TokenValidator
	Executor       ActionExecutor
	Audit          AuditSink
	Now            func() time.Time
}

type ActionExecutor interface {
	ExecutePrivilegedAction(ctx context.Context, action string, args []string) (ActionResult, error)
}

type ActionResult struct {
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output,omitempty"`
}

type AuditSink interface {
	AppendSetupHelperAudit(ctx context.Context, event AuditEvent) error
}

type AuditEvent struct {
	SchemaVersion   int            `json:"schemaVersion"`
	Source          string         `json:"source"`
	Action          string         `json:"action"`
	Result          string         `json:"result"`
	Actor           string         `json:"actor,omitempty"`
	BootstrapMode   bool           `json:"bootstrapMode,omitempty"`
	ApprovalID      string         `json:"approvalId,omitempty"`
	AllowlistDigest string         `json:"allowlistDigest,omitempty"`
	CommandPreview  string         `json:"commandPreview,omitempty"`
	ExitCode        int            `json:"exitCode,omitempty"`
	Error           string         `json:"error,omitempty"`
	Time            time.Time      `json:"time"`
	Data            map[string]any `json:"data,omitempty"`
}

func (e Engine) Run(ctx context.Context, inv Invocation) (Result, error) {
	started := e.now()
	result := Result{
		SchemaVersion:   SchemaVersion,
		Action:          strings.TrimSpace(inv.Action),
		Actor:           strings.TrimSpace(inv.Actor),
		BootstrapMode:   inv.Mode == ModeBootstrap,
		AllowlistDigest: inv.Allowlist.Digest,
		StartedAt:       started,
		CompletedAt:     started,
	}
	if result.Action == "" {
		return e.finish(ctx, result, ErrInvalidInvocation, ResultRejected)
	}
	if inv.Mode != ModeBootstrap && inv.Mode != ModeSteadyState {
		return e.finish(ctx, result, fmt.Errorf("%w: mode %q", ErrInvalidInvocation, inv.Mode), ResultRejected)
	}
	if len(inv.Allowlist.Entries) == 0 {
		return e.finish(ctx, result, fmt.Errorf("%w: empty allowlist", ErrInvalidAllowlist), ResultRejected)
	}
	if err := inv.Allowlist.ValidateChecksum(inv.ExpectedAllowlistDigest); err != nil {
		return e.finish(ctx, result, err, ResultRejected)
	}
	entry, ok := inv.Allowlist.Entry(result.Action)
	if !ok {
		return e.finish(ctx, result, fmt.Errorf("%w: %s", ErrActionNotAllowed, result.Action), ResultRejected)
	}
	if err := entry.ValidateArgs(inv.Args); err != nil {
		return e.finish(ctx, result, err, ResultRejected)
	}
	result.CommandPreview = entry.CommandPreview(inv.Args)

	switch inv.Mode {
	case ModeBootstrap:
		if !BootstrapActionAllowed(result.Action) {
			return e.finish(ctx, result, fmt.Errorf("%w: %s", ErrBootstrapRejected, result.Action), ResultRejected)
		}
		if result.Actor == "" {
			result.Actor = "bootstrap"
		}
	case ModeSteadyState:
		validator := e.TokenValidator
		if validator == nil {
			return e.finish(ctx, result, fmt.Errorf("%w: token validator is required", ErrInvalidInvocation), ResultRejected)
		}
		claims, err := validator.ValidateApprovalToken(ctx, inv.ApprovalToken, TokenValidationRequest{
			Action:          result.Action,
			AllowlistDigest: inv.Allowlist.Digest,
			Now:             e.now(),
		})
		if err != nil {
			return e.finish(ctx, result, err, ResultRejected)
		}
		result.ApprovalID = claims.ApprovalID
		if result.Actor == "" {
			result.Actor = claims.Actor
		}
	}

	executor := e.Executor
	if executor == nil {
		return e.finish(ctx, result, fmt.Errorf("%w: executor is required", ErrInvalidInvocation), ResultRejected)
	}
	actionResult, err := executor.ExecutePrivilegedAction(ctx, result.Action, append([]string(nil), inv.Args...))
	result.ExitCode = actionResult.ExitCode
	if err != nil {
		return e.finish(ctx, result, err, ResultFailed)
	}
	return e.finish(ctx, result, nil, ResultSucceeded)
}

func (e Engine) finish(ctx context.Context, result Result, cause error, status ResultStatus) (Result, error) {
	result.Status = status
	result.CompletedAt = e.now()
	if cause != nil {
		result.Error = cause.Error()
		if result.ExitCode == 0 {
			result.ExitCode = 1
		}
	}
	audit := e.Audit
	if audit == nil {
		audit = NoopAuditSink{}
	}
	_ = audit.AppendSetupHelperAudit(ctx, AuditEvent{
		SchemaVersion:   SchemaVersion,
		Source:          "hoopoe-setup-helper",
		Action:          result.Action,
		Result:          string(status),
		Actor:           result.Actor,
		BootstrapMode:   result.BootstrapMode,
		ApprovalID:      result.ApprovalID,
		AllowlistDigest: result.AllowlistDigest,
		CommandPreview:  result.CommandPreview,
		ExitCode:        result.ExitCode,
		Error:           result.Error,
		Time:            result.CompletedAt,
	})
	return result, cause
}

func (e Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

type NoopAuditSink struct{}

func (NoopAuditSink) AppendSetupHelperAudit(context.Context, AuditEvent) error { return nil }

func BootstrapActionAllowed(action string) bool {
	switch action {
	case "install-systemd-unit", "create-hoopoe-user", "chown-acfs-paths", "register-helper-allowlist":
		return true
	default:
		return false
	}
}

func isKnownAction(action string) bool {
	switch action {
	case "install-systemd-unit", "uninstall-systemd-unit", "chown-projects", "repair-acfs", "restart-service", "bind-privileged-port",
		"create-hoopoe-user", "chown-acfs-paths", "register-helper-allowlist":
		return true
	default:
		return false
	}
}
