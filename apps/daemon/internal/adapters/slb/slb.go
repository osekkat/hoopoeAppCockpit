// Package slb wraps the Simultaneous Launch Button (SLB) CLI. SLB is the
// optional two-person-rule add-on for the autopilot safety preset (§5.3 +
// §7.3). Hoopoe ingests SLB request state and folds it into the unified
// approvals queue so every destructive-class request shows the same
// approver/co-signer surface regardless of source.
//
// Wire surfaces consumed:
//   - `slb pending --json` — list pending requests awaiting decisions.
//   - `slb show <reqId> --json` — full request detail with reviews.
//   - `slb status <reqId> --json` — terminal/in-flight state.
//   - `slb patterns test <cmd> --json` — pre-flight risk classification.
//
// Capability registry note: `ToolID("slb")` is not yet in
// `capabilities.KnownClosedTools` or the OpenAPI `ToolId` enum. Capability
// registration is gated on a coordinated enum bump (Go + OpenAPI +
// regenerated TS/Go); this adapter ships the read-side wrappers and
// approvals-queue ingestion shape so that bump is mechanical when scheduled.
package slb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	ToolName = "slb"

	// CapabilityRequestsRead and CapabilityRequestsCreate are reserved
	// capability IDs for the eventual capability-registry enum bump. Adapter
	// callers can use them as keys today; the registry will accept them once
	// `ToolID("slb")` lands in the closed enum.
	CapabilityRequestsRead   = "slb.requests.read"
	CapabilityRequestsCreate = "slb.requests.create"
	CapabilityClassify       = "slb.patterns.classify"
)

var ErrInvalidRequest = errors.New("slb: invalid request")

// Decision normalizes SLB's terminal vocabulary into the approvals-queue
// vocabulary so the unified queue does not need to know SLB-specific labels.
type Decision string

const (
	DecisionPending              Decision = "pending"
	DecisionApproved             Decision = "approved"
	DecisionRejected             Decision = "rejected"
	DecisionCancelled            Decision = "cancelled"
	DecisionTimedOut             Decision = "timeout"
	DecisionExecuted             Decision = "executed"
	DecisionRequiresConfirmation Decision = "requires_confirmation"
)

// Final reports whether the verdict is end-state for the queue: rejected /
// cancelled / timeout / executed are terminal. approved is also terminal
// from the approvals perspective — execution is a separate phase.
func (d Decision) Final() bool {
	switch d {
	case DecisionApproved, DecisionRejected, DecisionCancelled, DecisionTimedOut, DecisionExecuted:
		return true
	}
	return false
}

// Approvable reports whether the queue UI should let users act on this
// state. pending / requires_confirmation surface as approvable; executed
// cannot be re-approved.
func (d Decision) Approvable() bool {
	switch d {
	case DecisionPending, DecisionRequiresConfirmation:
		return true
	}
	return false
}

// Tier mirrors SLB's risk classification.
type Tier string

const (
	TierCritical  Tier = "CRITICAL"
	TierDangerous Tier = "DANGEROUS"
	TierCaution   Tier = "CAUTION"
	TierSafe      Tier = "SAFE"
)

func (t Tier) Valid() bool {
	switch t {
	case TierCritical, TierDangerous, TierCaution, TierSafe:
		return true
	case "":
		return true
	}
	return false
}

type Runner interface {
	Run(ctx context.Context, argv []string) (CommandResult, error)
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, argv []string) (CommandResult, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return CommandResult{}, fmt.Errorf("%w: empty argv", ErrInvalidRequest)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	result.ExitCode = -1
	return result, err
}

type Adapter struct {
	Runner Runner
	Now    func() time.Time
}

func New(runner Runner) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{Runner: runner, Now: time.Now}
}

// Review captures one reviewer's decision against a pending request. The
// queue records both actors when SLB requires a co-signer.
type Review struct {
	Actor    string `json:"actor"`
	Decision string `json:"decision"`
	Comment  string `json:"comment,omitempty"`
	At       string `json:"at,omitempty"`
}

// Request mirrors `slb show --json` flattened to the fields the queue needs.
type Request struct {
	ID            string   `json:"id"`
	Command       string   `json:"command"`
	Tier          Tier     `json:"tier,omitempty"`
	MinApprovals  int      `json:"min_approvals"`
	Status        string   `json:"status"`
	Reason        string   `json:"reason,omitempty"`
	Goal          string   `json:"goal,omitempty"`
	SafetyArg     string   `json:"safety,omitempty"`
	Requester     string   `json:"requester,omitempty"`
	CreatedAt     string   `json:"created_at,omitempty"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
	Reviews       []Review `json:"reviews,omitempty"`
	ExpectedEffect string  `json:"expected_effect,omitempty"`
}

// PendingSummary mirrors one element of `slb pending --json`. The full
// detail is fetched via `slb show <id> --json` once a renderer or ingestion
// loop wants more than the badge.
type PendingSummary struct {
	ID            string `json:"id"`
	Command       string `json:"command"`
	Tier          Tier   `json:"tier,omitempty"`
	MinApprovals  int    `json:"min_approvals"`
	ApprovalsHave int    `json:"approvals_have"`
	Status        string `json:"status"`
	Requester     string `json:"requester,omitempty"`
	CreatedAt     string `json:"created_at,omitempty"`
}

// Status mirrors `slb status --json`.
type Status struct {
	ID            string `json:"id"`
	Status        string `json:"status"`
	MinApprovals  int    `json:"min_approvals"`
	ApprovalsHave int    `json:"approvals_have"`
	Final         bool   `json:"final,omitempty"`
}

// Classification mirrors `slb patterns test <cmd> --json` — used by Hoopoe
// pre-flight gating to decide whether to even call SLB.
type Classification struct {
	Command        string `json:"command"`
	Tier           Tier   `json:"tier,omitempty"`
	IsSafe         bool   `json:"is_safe"`
	MinApprovals   int    `json:"min_approvals"`
	NeedsApproval  bool   `json:"needs_approval"`
}

// ApprovalSourceEntry is the queue-ready record. Approvers carries every
// reviewer who voted approve so the audit log shows BOTH actors when SLB's
// 2-of-N threshold is crossed; Rejectors carries reject votes (typically
// just one reaching minimum is enough to terminate).
type ApprovalSourceEntry struct {
	Source       string   `json:"source"`
	Command      string   `json:"command"`
	Decision     Decision `json:"decision"`
	Final        bool     `json:"final"`
	Approvable   bool     `json:"approvable"`
	Tier         Tier     `json:"tier,omitempty"`
	MinApprovals int      `json:"min_approvals,omitempty"`
	Approvers    []string `json:"approvers,omitempty"`
	Rejectors    []string `json:"rejectors,omitempty"`
	Requester    string   `json:"requester,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	IssuedAt     string   `json:"issued_at,omitempty"`
	Evidence     Evidence `json:"evidence"`
}

// Evidence carries the raw SLB fields for audit reconstruction without
// re-running SLB.
type Evidence struct {
	RequestID  string   `json:"request_id"`
	RawStatus  string   `json:"raw_status,omitempty"`
	Reviews    []Review `json:"reviews,omitempty"`
	ExpectedEffect string `json:"expected_effect,omitempty"`
}

// PendingArgv builds `slb pending --json`.
func PendingArgv() []string { return []string{ToolName, "pending", "--json"} }

// ShowArgv builds `slb show <id> --json --output=json`. SLB accepts either
// `--json` shorthand or `--output=json`; we use the explicit shorthand.
func ShowArgv(id string) ([]string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: request id is required", ErrInvalidRequest)
	}
	return []string{ToolName, "show", id, "--json"}, nil
}

// StatusArgv builds `slb status <id> --json`. The `--wait` flag is NOT
// added — Hoopoe's queue polls explicitly and never blocks the daemon
// goroutine in `slb status --wait`.
func StatusArgv(id string) ([]string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: request id is required", ErrInvalidRequest)
	}
	return []string{ToolName, "status", id, "--json"}, nil
}

// ClassifyArgv builds `slb patterns test <cmd> --json`. Used pre-flight to
// decide whether Hoopoe should even create an SLB request.
func ClassifyArgv(command string) ([]string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("%w: command is required", ErrInvalidRequest)
	}
	return []string{ToolName, "patterns", "test", command, "--json"}, nil
}

// Pending returns SLB's current pending list. Empty list is a valid result.
func (a *Adapter) Pending(ctx context.Context) ([]PendingSummary, error) {
	var summaries []PendingSummary
	if err := a.runJSON(ctx, PendingArgv(), &summaries, true); err != nil {
		return nil, err
	}
	return summaries, nil
}

// Show fetches the full detail for a request including reviews.
func (a *Adapter) Show(ctx context.Context, id string) (Request, error) {
	argv, err := ShowArgv(id)
	if err != nil {
		return Request{}, err
	}
	var req Request
	if err := a.runJSON(ctx, argv, &req, false); err != nil {
		return Request{}, err
	}
	return req, nil
}

// Status fetches the current decision state without the review payload.
func (a *Adapter) Status(ctx context.Context, id string) (Status, error) {
	argv, err := StatusArgv(id)
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := a.runJSON(ctx, argv, &status, false); err != nil {
		return Status{}, err
	}
	return status, nil
}

// Classify runs `slb patterns test --json` to decide tier + approval count
// without creating a request.
func (a *Adapter) Classify(ctx context.Context, command string) (Classification, error) {
	argv, err := ClassifyArgv(command)
	if err != nil {
		return Classification{}, err
	}
	var c Classification
	if err := a.runJSON(ctx, argv, &c, false); err != nil {
		return Classification{}, err
	}
	return c, nil
}

// ToApprovalSourceEntry maps an SLB Request into the unified-queue shape.
// Reviews are split by decision so the queue records both approvers (when
// SLB's 2-of-N threshold is crossed) AND any rejectors that terminated the
// request.
func (r Request) ToApprovalSourceEntry(issuedAt string) ApprovalSourceEntry {
	decision := normalizeDecision(r.Status)
	approvers, rejectors := splitReviewers(r.Reviews)
	return ApprovalSourceEntry{
		Source:       sourceFor(r),
		Command:      r.Command,
		Decision:     decision,
		Final:        decision.Final(),
		Approvable:   decision.Approvable(),
		Tier:         r.Tier,
		MinApprovals: r.MinApprovals,
		Approvers:    approvers,
		Rejectors:    rejectors,
		Requester:    r.Requester,
		Reason:       r.Reason,
		IssuedAt:     issuedAt,
		Evidence: Evidence{
			RequestID:      r.ID,
			RawStatus:      r.Status,
			Reviews:        append([]Review(nil), r.Reviews...),
			ExpectedEffect: r.ExpectedEffect,
		},
	}
}

func sourceFor(r Request) string {
	if id := strings.TrimSpace(r.ID); id != "" {
		return "slb:" + id
	}
	return "slb"
}

func normalizeDecision(raw string) Decision {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "pending", "awaiting", "awaiting_review":
		return DecisionPending
	case "approved", "approve":
		return DecisionApproved
	case "rejected", "reject", "denied":
		return DecisionRejected
	case "cancelled", "canceled":
		return DecisionCancelled
	case "timeout", "timed_out", "expired":
		return DecisionTimedOut
	case "executed", "complete", "completed":
		return DecisionExecuted
	case "requires_confirmation", "confirm", "ask":
		return DecisionRequiresConfirmation
	}
	return DecisionPending
}

func splitReviewers(reviews []Review) (approvers, rejectors []string) {
	for _, rev := range reviews {
		actor := strings.TrimSpace(rev.Actor)
		if actor == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(rev.Decision)) {
		case "approve", "approved":
			approvers = append(approvers, actor)
		case "reject", "rejected", "deny", "denied":
			rejectors = append(rejectors, actor)
		}
	}
	return approvers, rejectors
}

func (a *Adapter) runJSON(ctx context.Context, argv []string, target any, allowEmpty bool) error {
	if a == nil {
		return fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	runner := a.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, argv)
	if err != nil {
		return fmt.Errorf("slb: run %s: %w", argv[0], err)
	}
	if result.ExitCode != 0 {
		return commandError{argv: argv, result: result}
	}
	stdout := bytes.TrimSpace(result.Stdout)
	if len(stdout) == 0 {
		if allowEmpty {
			// `slb pending --json` emits `[]` for an empty queue, not bare
			// whitespace; treat truly-empty stdout as malformed unless the
			// caller opted in.
			return nil
		}
		return fmt.Errorf("slb: empty JSON response from %v", argv)
	}
	if err := json.Unmarshal(stdout, target); err != nil {
		return fmt.Errorf("slb: decode JSON from %v: %w", argv, err)
	}
	return nil
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	return fmt.Sprintf("slb: command %v exited %d: %s", e.argv, e.result.ExitCode, strings.TrimSpace(string(e.result.Stderr)))
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
