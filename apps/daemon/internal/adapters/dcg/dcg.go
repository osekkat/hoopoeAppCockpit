// Package dcg wraps the Destructive Command Guard (DCG) CLI. DCG is the
// agent-side guard installed by ACFS as a Claude Code / Codex / Gemini
// pre-tool hook; Hoopoe does not run a parallel guard. This adapter ingests
// DCG verdicts so the daemon can fold them into the unified approvals queue
// alongside Hoopoe-policy and SLB approvals (plan.md §5.3).
//
// Verdict source-of-truth: `dcg explain --format json <command>` returns
// `{schema_version, command, decision, match{rule_id, pack_id, severity,
// reason, ...}, suggestions, ...}`. We normalize `decision` from DCG's
// allow/deny vocabulary into the approvals-queue vocabulary
// allowed/blocked/requires_confirmation, accepting a future `confirm`
// emission without synthesizing it from severity heuristics.
package dcg

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "dcg"

	CapabilityVerdictsSubscribe = "dcg.verdicts.subscribe"
	CapabilityDoctor            = "dcg.doctor.read"

	// MinSchemaVersion is the lowest `schema_version` in `dcg explain --format
	// json` we know how to parse. DCG 0.5.0 emits schema_version=2.
	MinSchemaVersion = 2
)

var ErrInvalidRequest = errors.New("dcg: invalid request")

// Decision is the normalized verdict the unified approvals queue consumes.
type Decision string

const (
	DecisionAllowed               Decision = "allowed"
	DecisionBlocked               Decision = "blocked"
	DecisionRequiresConfirmation  Decision = "requires_confirmation"
)

// Final reports whether the verdict is end-state for approvals: blocked
// verdicts cannot be overridden through the queue (DCG is the spec; even
// autopilot does not bypass per §5.3). Allowed verdicts also need no further
// gate from this source.
func (d Decision) Final() bool {
	return d == DecisionBlocked || d == DecisionAllowed
}

// Approvable reports whether the user can act on this verdict in the
// ApprovalDialog UI. Only requires_confirmation surfaces as approvable.
func (d Decision) Approvable() bool {
	return d == DecisionRequiresConfirmation
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

// Match captures the rule that produced the verdict. The unified approvals
// queue stamps `dcg:<rule_id>` as the source identifier.
type Match struct {
	RuleID              string `json:"rule_id"`
	PackID              string `json:"pack_id,omitempty"`
	PatternName         string `json:"pattern_name,omitempty"`
	Severity            string `json:"severity,omitempty"`
	Reason              string `json:"reason,omitempty"`
	Source              string `json:"source,omitempty"`
	MatchedTextPreview  string `json:"matched_text_preview,omitempty"`
	Explanation         string `json:"explanation,omitempty"`
}

// Suggestion mirrors DCG's user-facing remediation hints. We carry them
// through verbatim so the renderer can show them with the approval prompt.
type Suggestion struct {
	Kind    string `json:"kind,omitempty"`
	Text    string `json:"text,omitempty"`
	Command string `json:"command,omitempty"`
	URL     string `json:"url,omitempty"`
}

// Verdict is the normalized DCG verdict envelope. RawDecision carries DCG's
// own decision label so audit entries record exactly what the guard said.
type Verdict struct {
	SchemaVersion  int          `json:"schema_version"`
	Command        string       `json:"command"`
	Decision       Decision     `json:"decision"`
	RawDecision    string       `json:"raw_decision,omitempty"`
	Match          *Match       `json:"match,omitempty"`
	Suggestions    []Suggestion `json:"suggestions,omitempty"`
	TotalDurationUS int64       `json:"total_duration_us,omitempty"`
}

// ApprovalSourceEntry is the shape the unified approvals queue (hp-v0g)
// consumes. The adapter does not call into the queue itself — it builds the
// entry and lets the queue ingest it through whatever subscription wiring it
// exposes.
type ApprovalSourceEntry struct {
	Source     string       `json:"source"`
	Actor      string       `json:"actor,omitempty"`
	Command    string       `json:"command"`
	Decision   Decision     `json:"decision"`
	Final      bool         `json:"final"`
	Approvable bool         `json:"approvable"`
	Severity   string       `json:"severity,omitempty"`
	Reason     string       `json:"reason,omitempty"`
	Evidence   Evidence     `json:"evidence"`
	IssuedAt   string       `json:"issued_at,omitempty"`
	Suggestions []Suggestion `json:"suggestions,omitempty"`
}

// Evidence carries the rule-trace fields auditors need to reconstruct why
// the verdict fired without re-running DCG.
type Evidence struct {
	RuleID             string `json:"rule_id,omitempty"`
	PackID             string `json:"pack_id,omitempty"`
	PatternName        string `json:"pattern_name,omitempty"`
	MatchedTextPreview string `json:"matched_text_preview,omitempty"`
	Explanation        string `json:"explanation,omitempty"`
	RawDecision        string `json:"raw_decision,omitempty"`
	SchemaVersion      int    `json:"schema_version,omitempty"`
}

// ToApprovalSourceEntry builds the queue-ready entry for this verdict. actor
// names the agent that attempted the command; the queue uses it for audit
// attribution.
func (v Verdict) ToApprovalSourceEntry(actor, issuedAt string) ApprovalSourceEntry {
	entry := ApprovalSourceEntry{
		Source:      sourceFor(v),
		Actor:       strings.TrimSpace(actor),
		Command:     v.Command,
		Decision:    v.Decision,
		Final:       v.Decision.Final(),
		Approvable:  v.Decision.Approvable(),
		IssuedAt:    issuedAt,
		Suggestions: v.Suggestions,
		Evidence: Evidence{
			RawDecision:   v.RawDecision,
			SchemaVersion: v.SchemaVersion,
		},
	}
	if v.Match != nil {
		entry.Severity = v.Match.Severity
		entry.Reason = v.Match.Reason
		entry.Evidence.RuleID = v.Match.RuleID
		entry.Evidence.PackID = v.Match.PackID
		entry.Evidence.PatternName = v.Match.PatternName
		entry.Evidence.MatchedTextPreview = v.Match.MatchedTextPreview
		entry.Evidence.Explanation = v.Match.Explanation
	}
	return entry
}

func sourceFor(v Verdict) string {
	if v.Match != nil && strings.TrimSpace(v.Match.RuleID) != "" {
		return "dcg:" + v.Match.RuleID
	}
	return "dcg"
}

// DoctorReport is `dcg doctor --format json` flattened to the fields
// Hoopoe's Diagnostics surface needs.
type DoctorReport struct {
	SchemaVersion int           `json:"schema_version"`
	OK            bool          `json:"ok"`
	Issues        int           `json:"issues"`
	Fixed         int           `json:"fixed"`
	Checks        []DoctorCheck `json:"checks,omitempty"`
}

type DoctorCheck struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

// ExplainArgv builds the argv that produces a JSON verdict for `command`.
// The adapter never executes the inspected command — it asks DCG to dry-run
// its decision logic against the string.
func ExplainArgv(command string) ([]string, error) {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return nil, fmt.Errorf("%w: command is required", ErrInvalidRequest)
	}
	return []string{ToolName, "explain", "--format", "json", command}, nil
}

func DoctorArgv() []string {
	return []string{ToolName, "doctor", "--format", "json"}
}

// rawVerdict mirrors `dcg explain --format json` byte-for-byte so we keep
// raw fields the normalizer needs without leaking DCG's wire shape into our
// public Verdict type.
type rawVerdict struct {
	SchemaVersion   int          `json:"schema_version"`
	Command         string       `json:"command"`
	Decision        string       `json:"decision"`
	TotalDurationUS int64        `json:"total_duration_us"`
	Match           *Match       `json:"match,omitempty"`
	Suggestions     []Suggestion `json:"suggestions,omitempty"`
}

// Explain runs `dcg explain` against command and returns a normalized
// Verdict. The inspected command is never executed; DCG only evaluates its
// own rule packs against the literal string.
func (a *Adapter) Explain(ctx context.Context, command string) (Verdict, error) {
	argv, err := ExplainArgv(command)
	if err != nil {
		return Verdict{}, err
	}
	var raw rawVerdict
	if err := a.runJSON(ctx, argv, &raw); err != nil {
		return Verdict{}, err
	}
	if raw.SchemaVersion < MinSchemaVersion {
		return Verdict{}, fmt.Errorf("dcg: unsupported schema_version %d (need >= %d)", raw.SchemaVersion, MinSchemaVersion)
	}
	decision, err := normalizeDecision(raw.Decision)
	if err != nil {
		return Verdict{}, err
	}
	return Verdict{
		SchemaVersion:   raw.SchemaVersion,
		Command:         raw.Command,
		Decision:        decision,
		RawDecision:     raw.Decision,
		Match:           raw.Match,
		Suggestions:     raw.Suggestions,
		TotalDurationUS: raw.TotalDurationUS,
	}, nil
}

// Doctor runs `dcg doctor --format json` for the Diagnostics screen. It
// surfaces installation health (hook wiring, packs enabled, allowlists) so
// users can see why DCG is reporting degraded.
func (a *Adapter) Doctor(ctx context.Context) (DoctorReport, error) {
	var report DoctorReport
	if err := a.runJSON(ctx, DoctorArgv(), &report); err != nil {
		return DoctorReport{}, err
	}
	return report, nil
}

// Probe declares dcg.verdicts.subscribe so the capability registry can gate
// the unified approvals queue's DCG ingestion path. The capability is
// reported `ok` once a doctor run reports OK; otherwise degraded with the
// remediation surfaced in Diagnostics.
func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	checkedAt := a.now().UTC().Format(time.RFC3339)
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolDCG,
		Source:        "cli",
		LastCheckedAt: checkedAt,
		Capabilities: map[string]capabilities.Capability{
			CapabilityVerdictsSubscribe: {Status: capabilities.StatusMissing},
			CapabilityDoctor:            {Status: capabilities.StatusMissing},
		},
	}
	doctor, err := a.Doctor(ctx)
	if err != nil {
		statusValue := statusForError(err)
		note := err.Error()
		for capID := range report.Capabilities {
			report.Capabilities[capID] = capabilities.Capability{Status: statusValue, Notes: note}
		}
		return report, nil
	}
	report.Capabilities[CapabilityDoctor] = capabilities.Capability{Status: capabilities.StatusOK}
	if doctor.OK {
		report.Capabilities[CapabilityVerdictsSubscribe] = capabilities.Capability{Status: capabilities.StatusOK}
	} else {
		report.Capabilities[CapabilityVerdictsSubscribe] = capabilities.Capability{
			Status: capabilities.StatusDegraded,
			Notes:  fmt.Sprintf("dcg doctor reports %d issue(s)", doctor.Issues),
		}
	}
	return report, nil
}

func normalizeDecision(raw string) (Decision, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "allow", "allowed":
		return DecisionAllowed, nil
	case "deny", "denied", "block", "blocked":
		return DecisionBlocked, nil
	case "confirm", "requires_confirmation", "ask":
		return DecisionRequiresConfirmation, nil
	case "":
		return "", fmt.Errorf("%w: empty decision", ErrInvalidRequest)
	default:
		return "", fmt.Errorf("dcg: unknown decision %q", raw)
	}
}

func (a *Adapter) runJSON(ctx context.Context, argv []string, target any) error {
	if a == nil {
		return fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	runner := a.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, argv)
	if err != nil {
		return fmt.Errorf("dcg: run %s: %w", argv[0], err)
	}
	if result.ExitCode != 0 {
		return commandError{argv: argv, result: result}
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		return fmt.Errorf("dcg: empty JSON response from %v", argv)
	}
	if err := json.Unmarshal(result.Stdout, target); err != nil {
		return fmt.Errorf("dcg: decode JSON from %v: %w", argv, err)
	}
	return nil
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	return fmt.Sprintf("dcg: command %v exited %d: %s", e.argv, e.result.ExitCode, strings.TrimSpace(string(e.result.Stderr)))
}

func statusForError(err error) capabilities.CapabilityStatus {
	var commandErr commandError
	if errors.As(err, &commandErr) {
		if commandErr.result.ExitCode == 124 {
			return capabilities.StatusDegraded
		}
		return capabilities.StatusMissing
	}
	if strings.Contains(err.Error(), "decode JSON") || strings.Contains(err.Error(), "unsupported schema_version") {
		return capabilities.StatusDegraded
	}
	if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") || strings.Contains(err.Error(), "command not found") {
		return capabilities.StatusMissing
	}
	return capabilities.StatusDegraded
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now()
}
