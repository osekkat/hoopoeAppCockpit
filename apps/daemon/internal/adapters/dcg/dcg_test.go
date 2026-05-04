package dcg

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
}

func TestExplainArgvRequiresCommand(t *testing.T) {
	if _, err := ExplainArgv(""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty argv err = %v, want ErrInvalidRequest", err)
	}
	if _, err := ExplainArgv("   "); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("whitespace argv err = %v, want ErrInvalidRequest", err)
	}
	got, err := ExplainArgv("git status")
	if err != nil {
		t.Fatalf("ExplainArgv: %v", err)
	}
	want := []string{"dcg", "explain", "--format", "json", "git status"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestExplainParsesAllowVerdict(t *testing.T) {
	stdout := []byte(`{"schema_version":2,"command":"echo hi","decision":"allow","total_duration_us":1200,"steps":[]}`)
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg explain --format json echo hi": {Stdout: stdout},
	}})
	verdict, err := adapter.Explain(context.Background(), "echo hi")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if verdict.Decision != DecisionAllowed {
		t.Fatalf("decision = %s, want %s", verdict.Decision, DecisionAllowed)
	}
	if verdict.RawDecision != "allow" {
		t.Fatalf("raw_decision = %q, want %q", verdict.RawDecision, "allow")
	}
	if verdict.Decision.Approvable() {
		t.Fatalf("allowed verdict must not be approvable")
	}
	if !verdict.Decision.Final() {
		t.Fatalf("allowed verdict must be final")
	}
}

func TestExplainParsesDenyVerdictAsBlockedWithRule(t *testing.T) {
	stdout := []byte(`{
		"schema_version": 2,
		"command": "git reset --hard HEAD~1",
		"decision": "deny",
		"total_duration_us": 14000,
		"match": {
			"rule_id": "core.git:reset-hard",
			"pack_id": "core.git",
			"pattern_name": "reset-hard",
			"severity": "critical",
			"reason": "git reset --hard destroys uncommitted changes.",
			"source": "pack",
			"matched_text_preview": "git reset --hard",
			"explanation": "git reset --hard discards ALL uncommitted changes."
		},
		"suggestions": [
			{"kind": "Safer alternative", "text": "Use --soft", "command": "git reset --soft HEAD~1"}
		]
	}`)
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg explain --format json git reset --hard HEAD~1": {Stdout: stdout},
	}})
	verdict, err := adapter.Explain(context.Background(), "git reset --hard HEAD~1")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if verdict.Decision != DecisionBlocked {
		t.Fatalf("decision = %s, want %s", verdict.Decision, DecisionBlocked)
	}
	if !verdict.Decision.Final() {
		t.Fatalf("blocked verdict must be final (no UI override)")
	}
	if verdict.Decision.Approvable() {
		t.Fatalf("blocked verdict must not be approvable")
	}
	if verdict.Match == nil || verdict.Match.RuleID != "core.git:reset-hard" {
		t.Fatalf("rule = %+v, want core.git:reset-hard", verdict.Match)
	}
	if len(verdict.Suggestions) != 1 || verdict.Suggestions[0].Command != "git reset --soft HEAD~1" {
		t.Fatalf("suggestions = %+v", verdict.Suggestions)
	}
}

func TestExplainParsesConfirmVerdictAsRequiresConfirmation(t *testing.T) {
	// Forward-compat: DCG 0.5 emits allow/deny only, but we accept a future
	// `confirm` decision (or its `ask` synonym) and route it to the standard
	// approval flow. This guards us against double-handling the decision in
	// the unified queue once DCG starts emitting it.
	for _, raw := range []string{"confirm", "requires_confirmation", "ask"} {
		stdout := []byte(`{"schema_version":2,"command":"git push --force","decision":"` + raw + `","match":{"rule_id":"core.git:push-force"}}`)
		adapter := New(&fakeRunner{responses: map[string]CommandResult{
			"dcg explain --format json git push --force": {Stdout: stdout},
		}})
		verdict, err := adapter.Explain(context.Background(), "git push --force")
		if err != nil {
			t.Fatalf("Explain(%s): %v", raw, err)
		}
		if verdict.Decision != DecisionRequiresConfirmation {
			t.Fatalf("decision[%s] = %s, want %s", raw, verdict.Decision, DecisionRequiresConfirmation)
		}
		if !verdict.Decision.Approvable() {
			t.Fatalf("decision[%s] must be approvable", raw)
		}
		if verdict.Decision.Final() {
			t.Fatalf("decision[%s] must not be final", raw)
		}
	}
}

func TestExplainRejectsUnknownDecision(t *testing.T) {
	stdout := []byte(`{"schema_version":2,"command":"echo hi","decision":"banana"}`)
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg explain --format json echo hi": {Stdout: stdout},
	}})
	_, err := adapter.Explain(context.Background(), "echo hi")
	if err == nil || !strings.Contains(err.Error(), "unknown decision") {
		t.Fatalf("err = %v, want unknown decision", err)
	}
}

func TestExplainRejectsUnsupportedSchemaVersion(t *testing.T) {
	stdout := []byte(`{"schema_version":1,"command":"echo hi","decision":"allow"}`)
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg explain --format json echo hi": {Stdout: stdout},
	}})
	_, err := adapter.Explain(context.Background(), "echo hi")
	if err == nil || !strings.Contains(err.Error(), "unsupported schema_version") {
		t.Fatalf("err = %v, want unsupported schema_version", err)
	}
}

func TestExplainRejectsMalformedJSON(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg explain --format json echo hi": {Stdout: []byte(`{"decision":`)},
	}})
	_, err := adapter.Explain(context.Background(), "echo hi")
	if err == nil || !strings.Contains(err.Error(), "decode JSON") {
		t.Fatalf("err = %v, want decode JSON failure", err)
	}
}

func TestExplainRejectsEmptyResponse(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg explain --format json echo hi": {Stdout: []byte(``)},
	}})
	_, err := adapter.Explain(context.Background(), "echo hi")
	if err == nil || !strings.Contains(err.Error(), "empty JSON response") {
		t.Fatalf("err = %v, want empty JSON response", err)
	}
}

func TestExplainSurfacesNonZeroExit(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg explain --format json echo hi": {ExitCode: 1, Stderr: []byte("boom")},
	}})
	_, err := adapter.Explain(context.Background(), "echo hi")
	if err == nil || !strings.Contains(err.Error(), "exited 1") {
		t.Fatalf("err = %v, want non-zero exit", err)
	}
}

func TestDoctorParsesReport(t *testing.T) {
	stdout := []byte(`{
		"schema_version": 1,
		"checks": [
			{"id": "binary_path", "name": "Binary in PATH", "status": "ok", "message": "dcg found in PATH"},
			{"id": "config", "name": "Configuration", "status": "warning", "message": "No config file found", "remediation": "Run 'dcg init'"}
		],
		"issues": 0,
		"fixed": 0,
		"ok": true
	}`)
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg doctor --format json": {Stdout: stdout},
	}})
	report, err := adapter.Doctor(context.Background())
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if !report.OK || report.Issues != 0 || len(report.Checks) != 2 {
		t.Fatalf("doctor = %+v", report)
	}
	if report.Checks[1].Remediation == "" {
		t.Fatalf("remediation lost: %+v", report.Checks[1])
	}
}

func TestProbeOKWhenDoctorOK(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg doctor --format json": {Stdout: []byte(`{"schema_version":1,"ok":true,"issues":0,"fixed":0,"checks":[]}`)},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := report.Capabilities[CapabilityVerdictsSubscribe]; got.Status != capabilities.StatusOK {
		t.Fatalf("verdicts.subscribe = %+v, want ok", got)
	}
	if got := report.Capabilities[CapabilityDoctor]; got.Status != capabilities.StatusOK {
		t.Fatalf("doctor = %+v, want ok", got)
	}
	if report.Tool != capabilities.ToolDCG {
		t.Fatalf("tool = %s, want %s", report.Tool, capabilities.ToolDCG)
	}
	if report.LastCheckedAt != "2026-05-04T00:00:00Z" {
		t.Fatalf("LastCheckedAt = %q", report.LastCheckedAt)
	}
}

func TestProbeDegradedWhenDoctorReportsIssues(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg doctor --format json": {Stdout: []byte(`{"schema_version":1,"ok":false,"issues":2,"fixed":0,"checks":[]}`)},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	got := report.Capabilities[CapabilityVerdictsSubscribe]
	if got.Status != capabilities.StatusDegraded {
		t.Fatalf("verdicts.subscribe = %+v, want degraded", got)
	}
	if !strings.Contains(got.Notes, "2 issue(s)") {
		t.Fatalf("notes = %q", got.Notes)
	}
}

func TestProbeMissingWhenCLINotInstalled(t *testing.T) {
	adapter := New(&fakeRunner{
		responses: map[string]CommandResult{},
		err:       errors.New("exec: \"dcg\": executable file not found in $PATH"),
	})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := report.Capabilities[CapabilityVerdictsSubscribe]; got.Status != capabilities.StatusMissing {
		t.Fatalf("verdicts.subscribe = %+v, want missing", got)
	}
}

func TestProbeDegradedOnTimeout(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg doctor --format json": {ExitCode: 124, Stderr: []byte("timeout")},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := report.Capabilities[CapabilityVerdictsSubscribe]; got.Status != capabilities.StatusDegraded {
		t.Fatalf("verdicts.subscribe (timeout) = %+v, want degraded", got)
	}
}

func TestProbeDegradedOnMalformedDoctorJSON(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"dcg doctor --format json": {Stdout: []byte(`{"ok":`)},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if got := report.Capabilities[CapabilityVerdictsSubscribe]; got.Status != capabilities.StatusDegraded {
		t.Fatalf("verdicts.subscribe (malformed) = %+v, want degraded", got)
	}
}

func TestVerdictToApprovalSourceEntryStampsRuleSource(t *testing.T) {
	verdict := Verdict{
		SchemaVersion: 2,
		Command:       "git reset --hard HEAD~1",
		Decision:      DecisionBlocked,
		RawDecision:   "deny",
		Match: &Match{
			RuleID:             "core.git:reset-hard",
			PackID:             "core.git",
			PatternName:        "reset-hard",
			Severity:           "critical",
			Reason:             "destroys uncommitted changes",
			MatchedTextPreview: "git reset --hard",
			Explanation:        "discards changes",
		},
		Suggestions: []Suggestion{{Kind: "Safer", Text: "use stash"}},
	}
	entry := verdict.ToApprovalSourceEntry("agent.worker.7", "2026-05-04T00:00:00Z")

	if entry.Source != "dcg:core.git:reset-hard" {
		t.Errorf("source = %q, want dcg:core.git:reset-hard", entry.Source)
	}
	if !entry.Final {
		t.Errorf("blocked verdict must be final")
	}
	if entry.Approvable {
		t.Errorf("blocked verdict must not be approvable")
	}
	if entry.Actor != "agent.worker.7" {
		t.Errorf("actor = %q", entry.Actor)
	}
	if entry.Severity != "critical" || entry.Reason == "" {
		t.Errorf("severity/reason lost: %+v", entry)
	}
	if entry.Evidence.RuleID != "core.git:reset-hard" || entry.Evidence.PackID != "core.git" {
		t.Errorf("evidence rule trace = %+v", entry.Evidence)
	}
	if entry.Evidence.RawDecision != "deny" {
		t.Errorf("raw decision lost: %q", entry.Evidence.RawDecision)
	}
	if entry.Evidence.SchemaVersion != 2 {
		t.Errorf("schema version lost: %d", entry.Evidence.SchemaVersion)
	}
	if len(entry.Suggestions) != 1 {
		t.Errorf("suggestions dropped: %+v", entry.Suggestions)
	}
}

func TestVerdictToApprovalSourceEntryAllowedIsFinalAndUnapprovable(t *testing.T) {
	verdict := Verdict{
		SchemaVersion: 2,
		Command:       "echo hi",
		Decision:      DecisionAllowed,
		RawDecision:   "allow",
	}
	entry := verdict.ToApprovalSourceEntry("agent.worker.1", "2026-05-04T00:00:00Z")

	if entry.Source != "dcg" {
		t.Errorf("source = %q, want bare dcg when no rule", entry.Source)
	}
	if !entry.Final {
		t.Errorf("allowed must be final")
	}
	if entry.Approvable {
		t.Errorf("allowed must not be approvable")
	}
}

func TestVerdictToApprovalSourceEntryRequiresConfirmationIsApprovable(t *testing.T) {
	verdict := Verdict{
		SchemaVersion: 2,
		Command:       "git push --force",
		Decision:      DecisionRequiresConfirmation,
		RawDecision:   "confirm",
		Match:         &Match{RuleID: "core.git:push-force"},
	}
	entry := verdict.ToApprovalSourceEntry("agent.worker.2", "2026-05-04T00:00:00Z")

	if entry.Final {
		t.Errorf("requires_confirmation must not be final")
	}
	if !entry.Approvable {
		t.Errorf("requires_confirmation must be approvable")
	}
	if entry.Source != "dcg:core.git:push-force" {
		t.Errorf("source = %q, want dcg:core.git:push-force", entry.Source)
	}
}

type fakeRunner struct {
	responses map[string]CommandResult
	err       error
}

func (r *fakeRunner) Run(_ context.Context, argv []string) (CommandResult, error) {
	if r.err != nil {
		return CommandResult{}, r.err
	}
	key := strings.Join(argv, " ")
	result, ok := r.responses[key]
	if !ok {
		return CommandResult{ExitCode: 127, Stderr: []byte("unexpected argv: " + key)}, nil
	}
	return result, nil
}
