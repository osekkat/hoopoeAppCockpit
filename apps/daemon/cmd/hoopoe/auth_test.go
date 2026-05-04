package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// osReadFile is a thin wrapper so the test file's helpers stay grouped.
func osReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func newTestIO(t *testing.T) *authIO {
	t.Helper()
	dir := t.TempDir()
	return &authIO{
		Stdout:    new(bytes.Buffer),
		Stderr:    new(bytes.Buffer),
		Stdin:     strings.NewReader(""),
		Now:       func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) },
		AuthDir:   filepath.Join(dir, "auth"),
		AuditPath: filepath.Join(dir, "audit.jsonl"),
	}
}

func stdout(io *authIO) string {
	return io.Stdout.(*bytes.Buffer).String()
}

func stderr(io *authIO) string {
	return io.Stderr.(*bytes.Buffer).String()
}

// ─── pairing ─────────────────────────────────────────────────────────────

func TestRunAuthHelp(t *testing.T) {
	io := newTestIO(t)
	if err := runAuth(context.Background(), nil, io); err != nil {
		t.Fatal(err)
	}
	out := stdout(io)
	if !strings.Contains(out, "usage: hoopoe auth") {
		t.Errorf("expected usage block, got %q", out)
	}
}

func TestPairingCreateAndList(t *testing.T) {
	io := newTestIO(t)
	ctx := context.Background()

	if err := runAuth(ctx, []string{"pairing", "create", "--role", "owner"}, io); err != nil {
		t.Fatal(err)
	}
	out := stdout(io)
	if !strings.Contains(out, "DISPLAY TOKEN") {
		t.Errorf("create should print display token, got %q", out)
	}
	io.Stdout = new(bytes.Buffer)

	if err := runAuth(ctx, []string{"pairing", "list"}, io); err != nil {
		t.Fatal(err)
	}
	listOut := stdout(io)
	if !strings.Contains(listOut, "TOKENID") {
		t.Errorf("list missing header, got %q", listOut)
	}
	if !strings.Contains(listOut, "owner") {
		t.Errorf("list should include the new owner pairing, got %q", listOut)
	}
}

func TestPairingCreateJSON(t *testing.T) {
	io := newTestIO(t)
	if err := runAuth(context.Background(), []string{"pairing", "create", "--json", "--role", "client"}, io); err != nil {
		t.Fatal(err)
	}
	var issued map[string]any
	if err := json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &issued); err != nil {
		t.Fatalf("expected JSON output, got %q (%v)", stdout(io), err)
	}
	if issued["role"] != "client" {
		t.Errorf("role=%v", issued["role"])
	}
}

func TestPairingListEmpty(t *testing.T) {
	io := newTestIO(t)
	if err := runAuth(context.Background(), []string{"pairing", "list"}, io); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout(io), "no pairings") {
		t.Errorf("empty list should print no pairings, got %q", stdout(io))
	}
}

func TestPairingRevoke(t *testing.T) {
	io := newTestIO(t)
	ctx := context.Background()

	if err := runAuth(ctx, []string{"pairing", "create", "--role", "owner", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	rawOut := io.Stdout.(*bytes.Buffer).Bytes()
	var issued map[string]any
	if err := json.Unmarshal(rawOut, &issued); err != nil {
		t.Fatalf("decode pairing-create JSON: %v\nraw=%q\nstderr=%q", err, rawOut, stderr(io))
	}
	tokenID, _ := issued["tokenId"].(string)
	if tokenID == "" {
		t.Fatalf("missing tokenId in %v", issued)
	}
	io.Stdout = new(bytes.Buffer)

	if err := runAuth(ctx, []string{"pairing", "revoke", tokenID, "--actor", "tester"}, io); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout(io), "revoked by tester") {
		t.Errorf("revoke output missing actor, got %q", stdout(io))
	}
}

func TestPairingRevokeMissingTokenID(t *testing.T) {
	io := newTestIO(t)
	err := runAuth(context.Background(), []string{"pairing", "revoke"}, io)
	if err == nil {
		t.Fatal("expected error for missing tokenId")
	}
}

// ─── session ─────────────────────────────────────────────────────────────

func TestSessionListEmpty(t *testing.T) {
	io := newTestIO(t)
	if err := runAuth(context.Background(), []string{"session", "list"}, io); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout(io), "no active sessions") {
		t.Errorf("empty session list should print no active sessions, got %q", stdout(io))
	}
}

func TestSessionListJSON(t *testing.T) {
	io := newTestIO(t)
	if err := runAuth(context.Background(), []string{"session", "list", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	out := stdout(io)
	if strings.TrimSpace(out) != "[]" && strings.TrimSpace(out) != "null" {
		t.Errorf("empty session list JSON should be [] or null, got %q", out)
	}
}

func TestSessionRevokeMissingSID(t *testing.T) {
	io := newTestIO(t)
	if err := runAuth(context.Background(), []string{"session", "revoke"}, io); err == nil {
		t.Fatal("expected error for missing sid")
	}
}

// ─── rotate-secret ───────────────────────────────────────────────────────

func TestRotateSecretRequiresConfirmation(t *testing.T) {
	io := newTestIO(t)
	io.Stdin = strings.NewReader("n\n")
	if err := runAuth(context.Background(), []string{"rotate-secret"}, io); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout(io), "aborted") {
		t.Errorf("expected abort, got %q", stdout(io))
	}
}

func TestRotateSecretInteractiveYes(t *testing.T) {
	io := newTestIO(t)
	io.Stdin = strings.NewReader("yes\n")
	if err := runAuth(context.Background(), []string{"rotate-secret"}, io); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout(io), "signing secret rotated") {
		t.Errorf("expected rotation message, got %q", stdout(io))
	}
}

func TestRotateSecretWithYesFlag(t *testing.T) {
	io := newTestIO(t)
	if err := runAuth(context.Background(), []string{"rotate-secret", "--yes"}, io); err != nil {
		t.Fatal(err)
	}
	out := stdout(io)
	if !strings.Contains(out, "signing secret rotated") {
		t.Errorf("expected rotation message, got %q", out)
	}
	if !strings.Contains(out, "generation 1 → 2") && !strings.Contains(out, "generation 0 → 1") {
		t.Errorf("expected generation transition, got %q", out)
	}
}

// ─── audit ───────────────────────────────────────────────────────────────

func TestPairingCreateWritesAuditEntry(t *testing.T) {
	io := newTestIO(t)
	if err := runAuth(context.Background(), []string{"pairing", "create"}, io); err != nil {
		t.Fatal(err)
	}
	bytes := readAuditFile(t, io.AuditPath)
	if !strings.Contains(string(bytes), "auth.pairing.created") {
		t.Errorf("audit log missing entry, got %q", bytes)
	}
}

func TestPairingRevokeWritesAuditEntry(t *testing.T) {
	io := newTestIO(t)
	ctx := context.Background()
	if err := runAuth(ctx, []string{"pairing", "create", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	var issued map[string]any
	_ = json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &issued)
	tokenID, _ := issued["tokenId"].(string)
	io.Stdout = new(bytes.Buffer)

	if err := runAuth(ctx, []string{"pairing", "revoke", tokenID}, io); err != nil {
		t.Fatal(err)
	}
	bytes := readAuditFile(t, io.AuditPath)
	if !strings.Contains(string(bytes), "auth.pairing.revoked") {
		t.Errorf("audit log missing revoke entry, got %q", bytes)
	}
}

func TestRotateSecretWritesAuditEntry(t *testing.T) {
	io := newTestIO(t)
	if err := runAuth(context.Background(), []string{"rotate-secret", "--yes"}, io); err != nil {
		t.Fatal(err)
	}
	bytes := readAuditFile(t, io.AuditPath)
	if !strings.Contains(string(bytes), "auth.secret.rotated") {
		t.Errorf("audit log missing rotation entry, got %q", bytes)
	}
}

// ─── dispatch ────────────────────────────────────────────────────────────

func TestUnknownSubcommand(t *testing.T) {
	io := newTestIO(t)
	err := runAuth(context.Background(), []string{"frobulate"}, io)
	if err == nil || !strings.Contains(err.Error(), "unknown subcommand") {
		t.Errorf("expected unknown subcommand error, got %v", err)
	}
}

func TestUnknownPairingSubcommand(t *testing.T) {
	io := newTestIO(t)
	err := runAuth(context.Background(), []string{"pairing", "frobulate"}, io)
	if err == nil {
		t.Error("expected error for unknown pairing subcommand")
	}
}

func TestUnknownSessionSubcommand(t *testing.T) {
	io := newTestIO(t)
	err := runAuth(context.Background(), []string{"session", "frobulate"}, io)
	if err == nil {
		t.Error("expected error for unknown session subcommand")
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────

func readAuditFile(t *testing.T, path string) []byte {
	t.Helper()
	bytes, err := osReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	return bytes
}
