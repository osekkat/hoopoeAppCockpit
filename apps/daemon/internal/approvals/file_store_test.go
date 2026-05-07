package approvals

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// TestFileStoreSurvivesDaemonRestart pins hp-rh0w: pending, approved,
// denied, expired, and consumed approvals survive when a fresh Queue
// reopens the same FileStore. Each branch drives the queue through its
// state machine, then constructs a new Queue+FileStore over the same
// path and asserts the persisted record is replayable.
func TestFileStoreSurvivesDaemonRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals.jsonl")

	pendingApproval := requestThroughFileStore(t, path, "pending-bead")
	approvedID := approveThroughFileStore(t, path, "approved-bead")
	deniedID := denyThroughFileStore(t, path, "denied-bead")
	expiredID := expireThroughFileStore(t, path, "expired-bead")

	// Restart: new Queue + FileStore over the same file at the original
	// fixedTime() so the still-pending approval's default 15-minute TTL
	// is not retroactively walked onto the expire path. The Get/List
	// outputs must reproduce the prior states.
	resumeNow := fixedTime()
	resumeQueue := newFileBackedQueue(t, path, &resumeNow)

	cases := []struct {
		id    string
		state schemas.ApprovalState
		label string
	}{
		{pendingApproval, schemas.Pending, "pending"},
		{approvedID, schemas.Approved, "approved"},
		{deniedID, schemas.Denied, "denied"},
		{expiredID, schemas.Expired, "expired"},
	}
	for _, tc := range cases {
		got, ok, err := resumeQueue.Get(context.Background(), tc.id)
		if err != nil {
			t.Fatalf("%s Get: %v", tc.label, err)
		}
		if !ok {
			t.Fatalf("%s approval %q missing after restart", tc.label, tc.id)
		}
		if got.State != tc.state {
			t.Fatalf("%s state after restart = %s, want %s", tc.label, got.State, tc.state)
		}
	}

	// First-appearance ordering survives restart even though state
	// transitions appended additional log lines after each approval was
	// originally requested.
	listed, err := resumeQueue.List(context.Background(), ListFilter{IncludeExpired: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	wantOrder := []string{pendingApproval, approvedID, deniedID, expiredID}
	if len(listed) != len(wantOrder) {
		t.Fatalf("listed = %d items, want %d", len(listed), len(wantOrder))
	}
	for i, want := range wantOrder {
		if listed[i].Id != want {
			t.Fatalf("listed[%d] id = %q, want %q (preserved first-appearance order)", i, listed[i].Id, want)
		}
	}
}

// TestFileStoreRejectsCorruptLogLine guards a malformed-input case: a
// truncated/garbled entry on disk surfaces as an error instead of a
// silent partial replay that could misclassify approval state.
func TestFileStoreRejectsCorruptLogLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "approvals.jsonl")

	if err := os.WriteFile(path, []byte("{this is not json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	store := NewFileStore(path)
	if _, _, err := store.Get(context.Background(), "any"); err == nil {
		t.Fatal("Get on corrupt log returned nil error; want malformed-line failure")
	}
	if _, err := store.List(context.Background()); err == nil {
		t.Fatal("List on corrupt log returned nil error; want malformed-line failure")
	}
}

// TestFileStoreCreatesParentDirOnFirstSave guards the cold-start path:
// a daemon launching for the first time on a fresh state dir must be
// able to create the approvals.jsonl parent dir under 0700 perms,
// matching auth/pairing_store.go's behavior.
func TestFileStoreCreatesParentDirOnFirstSave(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "state", "approvals", "approvals.jsonl")
	store := NewFileStore(nested)

	approval := schemas.Approval{
		Id:            "appr_01",
		SchemaVersion: SchemaVersion,
		Source:        schemas.ApprovalSourceHoopoePolicy,
		State:         schemas.Pending,
	}
	if err := store.Save(context.Background(), approval); err != nil {
		t.Fatalf("Save on cold-start path: %v", err)
	}
	if _, err := os.Stat(nested); err != nil {
		t.Fatalf("approvals.jsonl missing after Save: %v", err)
	}
}

func newFileBackedQueue(t *testing.T, path string, now *time.Time) *Queue {
	t.Helper()
	counter := 0
	return NewQueue(Config{
		Store: NewFileStore(path),
		Now: func() time.Time {
			return *now
		},
		NewID: func(Request) (string, error) {
			counter++
			return fmt.Sprintf("appr_%02d_%d", counter, time.Now().UnixNano()), nil
		},
	})
}

func requestThroughFileStore(t *testing.T, path string, key string) string {
	t.Helper()
	clock := fixedTime()
	queue := newFileBackedQueue(t, path, &clock)
	approval, _, err := queue.Request(context.Background(), Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      "hoopoe-policy:test",
		RequestedAction: commandSpec("test.action", key),
		RequestActor:    actor(schemas.ActorKindAgent, "agent-1"),
		Reason:          "restart-pin",
		ProjectID:       "project-1",
		RiskClass:       schemas.Medium,
	})
	if err != nil {
		t.Fatalf("Request(%s): %v", key, err)
	}
	return approval.Id
}

func approveThroughFileStore(t *testing.T, path string, key string) string {
	t.Helper()
	clock := fixedTime()
	queue := newFileBackedQueue(t, path, &clock)
	approval, _, err := queue.Request(context.Background(), Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      "hoopoe-policy:test",
		RequestedAction: commandSpec("test.action", key),
		RequestActor:    actor(schemas.ActorKindAgent, "agent-1"),
		ProjectID:       "project-1",
		RiskClass:       schemas.Medium,
	})
	if err != nil {
		t.Fatalf("Request(%s): %v", key, err)
	}
	if _, err := queue.Approve(context.Background(), approval.Id, schemas.ApprovalDecisionRequest{
		DecisionActor: actor(schemas.ActorKindUser, "user-1"),
		Note:          stringPtr("ok"),
	}); err != nil {
		t.Fatalf("Approve(%s): %v", key, err)
	}
	return approval.Id
}

func denyThroughFileStore(t *testing.T, path string, key string) string {
	t.Helper()
	clock := fixedTime()
	queue := newFileBackedQueue(t, path, &clock)
	approval, _, err := queue.Request(context.Background(), Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      "hoopoe-policy:test",
		RequestedAction: commandSpec("test.action", key),
		RequestActor:    actor(schemas.ActorKindAgent, "agent-1"),
		ProjectID:       "project-1",
		RiskClass:       schemas.Medium,
	})
	if err != nil {
		t.Fatalf("Request(%s): %v", key, err)
	}
	if _, err := queue.Deny(context.Background(), approval.Id, schemas.ApprovalDecisionRequest{
		DecisionActor: actor(schemas.ActorKindUser, "user-1"),
		Note:          stringPtr("nope"),
	}); err != nil {
		t.Fatalf("Deny(%s): %v", key, err)
	}
	return approval.Id
}

func expireThroughFileStore(t *testing.T, path string, key string) string {
	t.Helper()
	clock := fixedTime()
	expiresAt := clock.Add(time.Minute)
	queue := newFileBackedQueue(t, path, &clock)
	approval, _, err := queue.Request(context.Background(), Request{
		Source:          schemas.ApprovalSourceHoopoePolicy,
		PolicyRule:      "hoopoe-policy:test",
		RequestedAction: commandSpec("test.action", key),
		RequestActor:    actor(schemas.ActorKindAgent, "agent-1"),
		ProjectID:       "project-1",
		RiskClass:       schemas.Medium,
		ExpiresAt:       &expiresAt,
	})
	if err != nil {
		t.Fatalf("Request(%s): %v", key, err)
	}
	clock = clock.Add(time.Hour) // drive past ExpiresAt so the next Get walks the expire branch.
	if _, _, err := queue.Get(context.Background(), approval.Id); err != nil {
		t.Fatalf("Get(%s) after ttl: %v", key, err)
	}
	return approval.Id
}
