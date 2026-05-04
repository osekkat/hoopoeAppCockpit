package slb

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPendingArgv(t *testing.T) {
	got := PendingArgv()
	want := []string{"slb", "pending", "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestShowArgvRequiresID(t *testing.T) {
	if _, err := ShowArgv(""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
	if _, err := ShowArgv("   "); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("whitespace err = %v, want ErrInvalidRequest", err)
	}
	got, err := ShowArgv("req_42")
	if err != nil {
		t.Fatalf("ShowArgv: %v", err)
	}
	want := []string{"slb", "show", "req_42", "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestStatusArgvDoesNotIncludeWait(t *testing.T) {
	// Hoopoe's queue polls explicitly; --wait is forbidden so the daemon
	// goroutine never blocks inside slb.
	argv, err := StatusArgv("req_42")
	if err != nil {
		t.Fatalf("StatusArgv: %v", err)
	}
	for _, v := range argv {
		if v == "--wait" {
			t.Fatalf("argv must not include --wait: %v", argv)
		}
	}
}

func TestClassifyArgvRequiresCommand(t *testing.T) {
	if _, err := ClassifyArgv(""); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err = %v, want ErrInvalidRequest", err)
	}
	got, err := ClassifyArgv("git push --force")
	if err != nil {
		t.Fatalf("ClassifyArgv: %v", err)
	}
	want := []string{"slb", "patterns", "test", "git push --force", "--json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv = %#v, want %#v", got, want)
	}
}

func TestPendingParsesEmptyQueue(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"slb pending --json": {Stdout: []byte("[]\n")},
	}})
	got, err := adapter.Pending(context.Background())
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want empty", len(got))
	}
}

func TestPendingTrulyEmptyStdoutTreatedAsNoQueue(t *testing.T) {
	// Some shells may strip "[]" through pipelines. Pending tolerates a
	// truly-empty stdout as the same as `[]`, which Show/Status reject.
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"slb pending --json": {Stdout: []byte{}},
	}})
	got, err := adapter.Pending(context.Background())
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestPendingParsesItems(t *testing.T) {
	stdout := []byte(`[
		{"id":"req_1","command":"rm -rf /tmp/x","tier":"DANGEROUS","min_approvals":1,"approvals_have":0,"status":"pending","requester":"agent.worker.1","created_at":"2026-05-04T01:00:00Z"},
		{"id":"req_2","command":"git push --force","tier":"CRITICAL","min_approvals":2,"approvals_have":1,"status":"pending","requester":"agent.worker.2"}
	]`)
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"slb pending --json": {Stdout: stdout},
	}})
	got, err := adapter.Pending(context.Background())
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(got) != 2 || got[0].ID != "req_1" || got[1].MinApprovals != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestShowParsesFullRequestWithReviews(t *testing.T) {
	stdout := []byte(`{
		"id": "req_3",
		"command": "git push --force origin main",
		"tier": "CRITICAL",
		"min_approvals": 2,
		"status": "approved",
		"requester": "agent.worker.5",
		"reason": "release rebase",
		"goal": "ship v1.2.0",
		"safety": "feature branch only",
		"reviews": [
			{"actor":"alice","decision":"approve","comment":"diff lgtm","at":"2026-05-04T01:05:00Z"},
			{"actor":"bob","decision":"approve","comment":"matches release plan","at":"2026-05-04T01:06:00Z"}
		],
		"expected_effect": "force-push to origin/main"
	}`)
	argv, _ := ShowArgv("req_3")
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {Stdout: stdout},
	}})
	req, err := adapter.Show(context.Background(), "req_3")
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if req.ID != "req_3" || req.Tier != TierCritical || len(req.Reviews) != 2 {
		t.Fatalf("got %+v", req)
	}
	if req.Reviews[0].Actor != "alice" || req.Reviews[1].Actor != "bob" {
		t.Fatalf("reviews = %+v", req.Reviews)
	}
}

func TestStatusParsesTerminalState(t *testing.T) {
	stdout := []byte(`{"id":"req_3","status":"approved","min_approvals":2,"approvals_have":2,"final":true}`)
	argv, _ := StatusArgv("req_3")
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {Stdout: stdout},
	}})
	got, err := adapter.Status(context.Background(), "req_3")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Status != "approved" || got.MinApprovals != 2 || got.ApprovalsHave != 2 || !got.Final {
		t.Fatalf("got %+v", got)
	}
}

func TestClassifyParses(t *testing.T) {
	stdout := []byte(`{"command":"git push --force","is_safe":false,"min_approvals":2,"needs_approval":true,"tier":"CRITICAL"}`)
	argv, _ := ClassifyArgv("git push --force")
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {Stdout: stdout},
	}})
	got, err := adapter.Classify(context.Background(), "git push --force")
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if got.Tier != TierCritical || !got.NeedsApproval || got.MinApprovals != 2 {
		t.Fatalf("got %+v", got)
	}
}

func TestRequestToApprovalSourceEntryRecordsBothApprovers(t *testing.T) {
	// 2-of-N threshold reached → ApprovalSourceEntry must list BOTH actors
	// in `approvers` so audit shows the co-signature trail (§5.3).
	req := Request{
		ID:           "req_3",
		Command:      "git push --force origin main",
		Tier:         TierCritical,
		MinApprovals: 2,
		Status:       "approved",
		Requester:    "agent.worker.5",
		Reason:       "release rebase",
		Reviews: []Review{
			{Actor: "alice", Decision: "approve"},
			{Actor: "bob", Decision: "approve"},
		},
		ExpectedEffect: "force-push",
	}
	entry := req.ToApprovalSourceEntry("2026-05-04T00:00:00Z")
	if entry.Source != "slb:req_3" {
		t.Errorf("source = %q, want slb:req_3", entry.Source)
	}
	if entry.Decision != DecisionApproved {
		t.Errorf("decision = %s, want approved", entry.Decision)
	}
	if !entry.Final || entry.Approvable {
		t.Errorf("approved must be final + non-approvable; got final=%v approvable=%v", entry.Final, entry.Approvable)
	}
	if !reflect.DeepEqual(entry.Approvers, []string{"alice", "bob"}) {
		t.Errorf("approvers = %v, want [alice bob]", entry.Approvers)
	}
	if len(entry.Rejectors) != 0 {
		t.Errorf("rejectors should be empty: %v", entry.Rejectors)
	}
	if entry.Tier != TierCritical || entry.MinApprovals != 2 {
		t.Errorf("tier/min lost: %+v", entry)
	}
	if entry.Evidence.RequestID != "req_3" || entry.Evidence.RawStatus != "approved" {
		t.Errorf("evidence = %+v", entry.Evidence)
	}
	if entry.Evidence.ExpectedEffect != "force-push" {
		t.Errorf("expected_effect lost: %q", entry.Evidence.ExpectedEffect)
	}
}

func TestRequestToApprovalSourceEntryRejectionRecordsRejector(t *testing.T) {
	req := Request{
		ID:           "req_4",
		Command:      "git push --force origin main",
		Tier:         TierCritical,
		MinApprovals: 2,
		Status:       "rejected",
		Reviews: []Review{
			{Actor: "alice", Decision: "approve"},
			{Actor: "bob", Decision: "reject", Comment: "not after freeze"},
		},
	}
	entry := req.ToApprovalSourceEntry("2026-05-04T00:00:00Z")
	if entry.Decision != DecisionRejected {
		t.Errorf("decision = %s, want rejected", entry.Decision)
	}
	if !entry.Final {
		t.Errorf("rejected must be final")
	}
	if !reflect.DeepEqual(entry.Approvers, []string{"alice"}) {
		t.Errorf("approvers = %v, want [alice]", entry.Approvers)
	}
	if !reflect.DeepEqual(entry.Rejectors, []string{"bob"}) {
		t.Errorf("rejectors = %v, want [bob]", entry.Rejectors)
	}
}

func TestRequestToApprovalSourceEntryPendingIsApprovable(t *testing.T) {
	req := Request{ID: "req_5", Command: "rm -rf /tmp/x", Tier: TierDangerous, MinApprovals: 1, Status: "pending"}
	entry := req.ToApprovalSourceEntry("2026-05-04T00:00:00Z")
	if entry.Decision != DecisionPending {
		t.Errorf("decision = %s", entry.Decision)
	}
	if entry.Final {
		t.Errorf("pending must not be final")
	}
	if !entry.Approvable {
		t.Errorf("pending must be approvable")
	}
}

func TestNormalizeDecisionDefaultsToPending(t *testing.T) {
	if got := normalizeDecision("unknown-state"); got != DecisionPending {
		t.Errorf("unknown → %s, want pending", got)
	}
	for raw, want := range map[string]Decision{
		"awaiting_review": DecisionPending,
		"approve":         DecisionApproved,
		"denied":          DecisionRejected,
		"canceled":        DecisionCancelled,
		"expired":         DecisionTimedOut,
		"completed":       DecisionExecuted,
		"confirm":         DecisionRequiresConfirmation,
	} {
		if got := normalizeDecision(raw); got != want {
			t.Errorf("%q → %s, want %s", raw, got, want)
		}
	}
}

func TestTierValid(t *testing.T) {
	for _, tier := range []Tier{TierCritical, TierDangerous, TierCaution, TierSafe, ""} {
		if !tier.Valid() {
			t.Errorf("%s should be valid", tier)
		}
	}
	if Tier("WHATEVER").Valid() {
		t.Errorf("unknown tier should be invalid")
	}
}

func TestShowSurfacesNonZeroExit(t *testing.T) {
	argv, _ := ShowArgv("req_3")
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {ExitCode: 1, Stderr: []byte("not found")},
	}})
	_, err := adapter.Show(context.Background(), "req_3")
	if err == nil || !strings.Contains(err.Error(), "exited 1") {
		t.Fatalf("err = %v, want exited 1", err)
	}
}

func TestShowRejectsMalformedJSON(t *testing.T) {
	argv, _ := ShowArgv("req_3")
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {Stdout: []byte(`{"id":`)},
	}})
	_, err := adapter.Show(context.Background(), "req_3")
	if err == nil || !strings.Contains(err.Error(), "decode JSON") {
		t.Fatalf("err = %v, want decode JSON", err)
	}
}

func TestShowRejectsEmptyStdout(t *testing.T) {
	argv, _ := ShowArgv("req_3")
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {Stdout: []byte("")},
	}})
	_, err := adapter.Show(context.Background(), "req_3")
	if err == nil || !strings.Contains(err.Error(), "empty JSON response") {
		t.Fatalf("err = %v, want empty JSON response", err)
	}
}

func TestPendingSurfacesMissingCLI(t *testing.T) {
	adapter := New(&fakeRunner{
		responses: map[string]CommandResult{},
		err:       errors.New("exec: \"slb\": executable file not found in $PATH"),
	})
	_, err := adapter.Pending(context.Background())
	if err == nil || !strings.Contains(err.Error(), "executable file not found") {
		t.Fatalf("err = %v, want missing CLI", err)
	}
}

func TestStatusRejectsTimeoutExitCode(t *testing.T) {
	argv, _ := StatusArgv("req_3")
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		strings.Join(argv, " "): {ExitCode: 124, Stderr: []byte("timeout")},
	}})
	_, err := adapter.Status(context.Background(), "req_3")
	if err == nil || !strings.Contains(err.Error(), "exited 124") {
		t.Fatalf("err = %v, want exited 124", err)
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
