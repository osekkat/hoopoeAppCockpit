// hp-uz6: `hoopoe auth` CLI subcommand surface — pairing/session
// {create,list,revoke} + rotate-secret. Same shape as t3code's
// `t3 auth` per plan.md §5.2 closing.
//
// Talks directly to the on-disk auth state (no daemon socket needed
// for v1): ~/.hoopoe/auth/pairings.jsonl + sessions.jsonl + secret.json.
// When the daemon is running, the same files back the live services —
// CLI mutations are visible to the running daemon on next reload.
//
// Audit entries are written to ~/.hoopoe/audit.jsonl per the bead's
// "writes an audit entry" requirement; the `hp-g73` audit-log writer
// (which has stable JSONL append semantics) is the consumer.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/auth"
)

// authIO bundles the IO streams the auth CLI uses. Tests substitute
// bytes.Buffer for stdout/stderr; production passes os.Stdout/Stderr.
type authIO struct {
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
	Now    func() time.Time
	// AuthDir overrides ~/.hoopoe/auth (tests use a tmp dir; production
	// resolves from $HOME).
	AuthDir string
	// AuditPath overrides ~/.hoopoe/audit.jsonl.
	AuditPath string
}

// runAuth dispatches the `hoopoe auth ...` subcommand. The first arg in
// `args` is the leaf subcommand (`pairing`, `session`, `rotate-secret`,
// or `--help` / empty for usage).
func runAuth(ctx context.Context, args []string, io *authIO) error {
	if io.Now == nil {
		io.Now = time.Now
	}
	if io.AuthDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("hoopoe auth: resolve home dir: %w", err)
		}
		io.AuthDir = filepath.Join(home, ".hoopoe", "auth")
	}
	if io.AuditPath == "" {
		base := filepath.Dir(io.AuthDir)
		io.AuditPath = filepath.Join(base, "audit.jsonl")
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printAuthUsage(io.Stdout)
		return nil
	}
	switch args[0] {
	case "pairing":
		return runAuthPairing(ctx, args[1:], io)
	case "session":
		return runAuthSession(ctx, args[1:], io)
	case "rotate-secret":
		return runAuthRotateSecret(ctx, args[1:], io)
	default:
		fmt.Fprintf(io.Stderr, "hoopoe auth: unknown subcommand %q\n", args[0])
		printAuthUsage(io.Stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func printAuthUsage(w io.Writer) {
	fmt.Fprint(w, `usage: hoopoe auth <subcommand> [args]

subcommands:
  pairing create [--role owner|client] [--json]
  pairing list   [--json]
  pairing revoke <tokenId> [--actor <name>]
  session list   [--json]
  session revoke <sid> [--actor <name>]
  rotate-secret  [--yes]

run 'hoopoe auth <subcommand> --help' for more.
`)
}

// ─── pairing ─────────────────────────────────────────────────────────────

func runAuthPairing(ctx context.Context, args []string, io *authIO) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(io.Stdout, "usage: hoopoe auth pairing {create|list|revoke}\n")
		return nil
	}
	switch args[0] {
	case "create":
		return runAuthPairingCreate(ctx, args[1:], io)
	case "list":
		return runAuthPairingList(ctx, args[1:], io)
	case "revoke":
		return runAuthPairingRevoke(ctx, args[1:], io)
	default:
		fmt.Fprintf(io.Stderr, "hoopoe auth pairing: unknown subcommand %q\n", args[0])
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runAuthPairingCreate(ctx context.Context, args []string, io *authIO) error {
	flags := flag.NewFlagSet("hoopoe auth pairing create", flag.ContinueOnError)
	flags.SetOutput(io.Stderr)
	role := flags.String("role", "client", "pairing role: owner | client")
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	svc, err := newBootstrapService(io)
	if err != nil {
		return err
	}
	pairingRole := auth.PairingRole(*role)
	issued, err := svc.CreatePairing(ctx, auth.CreatePairingRequest{Role: pairingRole})
	if err != nil {
		return fmt.Errorf("hoopoe auth pairing create: %w", err)
	}
	if err := appendAudit(io, "auth.pairing.created", map[string]any{
		"tokenId": issued.TokenID,
		"role":    issued.Role,
	}); err != nil {
		// Audit failure is non-fatal — the pairing is committed; warn
		// and continue.
		fmt.Fprintf(io.Stderr, "warn: audit write failed: %v\n", err)
	}
	if *asJSON {
		return writeJSONIndented(io.Stdout, issued)
	}
	fmt.Fprintf(io.Stdout, "pairing %s issued (role=%s)\nDISPLAY TOKEN (single use, copy now): %s\n",
		issued.TokenID, issued.Role, issued.DisplayToken)
	return nil
}

func runAuthPairingList(ctx context.Context, args []string, io *authIO) error {
	flags := flag.NewFlagSet("hoopoe auth pairing list", flag.ContinueOnError)
	flags.SetOutput(io.Stderr)
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	svc, err := newBootstrapService(io)
	if err != nil {
		return err
	}
	records, err := svc.ListPairings(ctx)
	if err != nil {
		return fmt.Errorf("hoopoe auth pairing list: %w", err)
	}
	if *asJSON {
		return writeJSONIndented(io.Stdout, records)
	}
	if len(records) == 0 {
		fmt.Fprintln(io.Stdout, "no pairings on file.")
		return nil
	}
	fmt.Fprintln(io.Stdout, "TOKENID                ROLE    STATUS    CREATED                    CONSUMEDBY")
	for _, r := range records {
		status := "active"
		consumedBy := "-"
		if r.RevokedAt != nil {
			status = "revoked"
		} else if r.ConsumedAt != nil {
			status = "consumed"
			consumedBy = r.ConsumedBy
		}
		fmt.Fprintf(io.Stdout, "%-22s %-7s %-9s %-26s %s\n",
			r.TokenID, r.Role, status, r.CreatedAt.UTC().Format(time.RFC3339), consumedBy)
	}
	return nil
}

func runAuthPairingRevoke(ctx context.Context, args []string, io *authIO) error {
	flags := flag.NewFlagSet("hoopoe auth pairing revoke", flag.ContinueOnError)
	flags.SetOutput(io.Stderr)
	actor := flags.String("actor", defaultActor(io), "actor stamped on the audit entry")
	// Go's `flag.Parse` stops at the first non-flag arg, so positional
	// + trailing-flag combos don't work out of the box. Pre-split the
	// positional from the flags so `<tokenId> --actor x` and
	// `--actor x <tokenId>` both work.
	tokenID, flagArgs, err := splitPositional(args)
	if err != nil {
		fmt.Fprintln(io.Stderr, "usage: hoopoe auth pairing revoke <tokenId>")
		return err
	}
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	svc, err := newBootstrapService(io)
	if err != nil {
		return err
	}
	revoked, err := svc.RevokePairing(ctx, auth.RevokePairingRequest{
		TokenID: tokenID,
		Actor:   *actor,
	})
	if err != nil {
		return fmt.Errorf("hoopoe auth pairing revoke: %w", err)
	}
	if err := appendAudit(io, "auth.pairing.revoked", map[string]any{
		"tokenId": revoked.TokenID,
		"actor":   *actor,
	}); err != nil {
		fmt.Fprintf(io.Stderr, "warn: audit write failed: %v\n", err)
	}
	fmt.Fprintf(io.Stdout, "pairing %s revoked by %s\n", revoked.TokenID, *actor)
	return nil
}

// ─── session ─────────────────────────────────────────────────────────────

func runAuthSession(ctx context.Context, args []string, io *authIO) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(io.Stdout, "usage: hoopoe auth session {list|revoke}\n")
		return nil
	}
	switch args[0] {
	case "list":
		return runAuthSessionList(ctx, args[1:], io)
	case "revoke":
		return runAuthSessionRevoke(ctx, args[1:], io)
	default:
		fmt.Fprintf(io.Stderr, "hoopoe auth session: unknown subcommand %q\n", args[0])
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runAuthSessionList(_ context.Context, args []string, io *authIO) error {
	flags := flag.NewFlagSet("hoopoe auth session list", flag.ContinueOnError)
	flags.SetOutput(io.Stderr)
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	svc, err := newSessionService(io)
	if err != nil {
		return err
	}
	records := svc.ListSessions()
	if *asJSON {
		return writeJSONIndented(io.Stdout, records)
	}
	if len(records) == 0 {
		fmt.Fprintln(io.Stdout, "no active sessions.")
		return nil
	}
	fmt.Fprintln(io.Stdout, "SID                          ROLE    GENERATION  ISSUED                     STATUS")
	for _, r := range records {
		status := "active"
		if r.RevokedAt != nil {
			status = "revoked"
		}
		fmt.Fprintf(io.Stdout, "%-28s %-7s %-11d %-26s %s\n",
			r.SID, r.Role, r.Generation, r.IssuedAt.UTC().Format(time.RFC3339), status)
	}
	return nil
}

func runAuthSessionRevoke(_ context.Context, args []string, io *authIO) error {
	flags := flag.NewFlagSet("hoopoe auth session revoke", flag.ContinueOnError)
	flags.SetOutput(io.Stderr)
	actor := flags.String("actor", defaultActor(io), "actor stamped on the audit entry")
	sid, flagArgs, err := splitPositional(args)
	if err != nil {
		fmt.Fprintln(io.Stderr, "usage: hoopoe auth session revoke <sid>")
		return err
	}
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	svc, err := newSessionService(io)
	if err != nil {
		return err
	}
	wasActive, err := svc.RevokeSession(sid, *actor)
	if err != nil {
		return fmt.Errorf("hoopoe auth session revoke: %w", err)
	}
	if err := appendAudit(io, "auth.session.revoked", map[string]any{
		"sid":       sid,
		"actor":     *actor,
		"wasActive": wasActive,
	}); err != nil {
		fmt.Fprintf(io.Stderr, "warn: audit write failed: %v\n", err)
	}
	if !wasActive {
		fmt.Fprintf(io.Stdout, "session %s already revoked (no-op).\n", sid)
	} else {
		fmt.Fprintf(io.Stdout, "session %s revoked by %s.\n", sid, *actor)
	}
	return nil
}

// ─── rotate-secret ───────────────────────────────────────────────────────

func runAuthRotateSecret(_ context.Context, args []string, io *authIO) error {
	flags := flag.NewFlagSet("hoopoe auth rotate-secret", flag.ContinueOnError)
	flags.SetOutput(io.Stderr)
	yes := flags.Bool("yes", false, "skip the confirmation prompt")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*yes {
		fmt.Fprint(io.Stdout, "WARNING: rotating the signing secret invalidates ALL outstanding bearers + WS tokens.\nEvery paired desktop must re-pair after this.\nProceed? [y/N]: ")
		if !confirmYes(io.Stdin) {
			fmt.Fprintln(io.Stdout, "aborted.")
			return nil
		}
	}
	svc, err := newSessionService(io)
	if err != nil {
		return err
	}
	snap, err := svc.RotateSecret()
	if err != nil {
		return fmt.Errorf("hoopoe auth rotate-secret: %w", err)
	}
	if err := appendAudit(io, "auth.secret.rotated", map[string]any{
		"generation":  snap.Generation,
		"rotatedFrom": snap.RotatedFrom,
		"createdAt":   snap.CreatedAt.UTC().Format(time.RFC3339),
	}); err != nil {
		fmt.Fprintf(io.Stderr, "warn: audit write failed: %v\n", err)
	}
	fmt.Fprintf(io.Stdout, "signing secret rotated (generation %d → %d). All sessions invalidated.\n",
		snap.RotatedFrom, snap.Generation)
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────────────

func newBootstrapService(io *authIO) (*auth.BootstrapCredentialService, error) {
	if err := os.MkdirAll(io.AuthDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", io.AuthDir, err)
	}
	return auth.NewBootstrapCredentialService(auth.BootstrapCredentialConfig{
		Path: filepath.Join(io.AuthDir, "pairings.jsonl"),
		Now:  io.Now,
	})
}

func newSessionService(io *authIO) (*auth.SessionCredentialService, error) {
	if err := os.MkdirAll(io.AuthDir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", io.AuthDir, err)
	}
	store, err := auth.NewServerSecretStore(auth.ServerSecretStoreConfig{
		Path: filepath.Join(io.AuthDir, "server-secret.json"),
		Now:  io.Now,
	})
	if err != nil {
		return nil, err
	}
	if _, err := store.EnsureInitialized(); err != nil {
		return nil, err
	}
	return auth.NewSessionCredentialService(auth.SessionCredentialConfig{
		Secrets: store,
		Now:     io.Now,
	})
}

func appendAudit(io *authIO, kind string, payload map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(io.AuditPath), 0o700); err != nil {
		return err
	}
	entry := map[string]any{
		"ts":      io.Now().UTC().Format(time.RFC3339),
		"kind":    kind,
		"actor":   defaultActor(io),
		"payload": payload,
	}
	bytes, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	f, err := os.OpenFile(io.AuditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(bytes)
	return err
}

// splitPositional scans args for the FIRST non-flag token and returns
// it as the positional, with the rest concatenated for `flag.Parse`.
// Supports both `<tokenId> --actor x` and `--actor x <tokenId>`.
// Returns an error if no positional is found OR if more than one
// positional appears (unambiguous CLI).
func splitPositional(args []string) (string, []string, error) {
	var positional string
	flagArgs := make([]string, 0, len(args))
	found := false
	for i := 0; i < len(args); i++ {
		token := args[i]
		if len(token) > 0 && token[0] == '-' {
			flagArgs = append(flagArgs, token)
			// If the flag carries an `=` it's self-contained; otherwise
			// the next token is its value.
			if !containsEquals(token) && i+1 < len(args) && !startsWithDash(args[i+1]) {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
			continue
		}
		if found {
			return "", nil, fmt.Errorf("unexpected extra positional %q", token)
		}
		positional = token
		found = true
	}
	if !found {
		return "", nil, fmt.Errorf("missing positional argument")
	}
	return positional, flagArgs, nil
}

func containsEquals(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '=' {
			return true
		}
	}
	return false
}

func startsWithDash(s string) bool {
	return len(s) > 0 && s[0] == '-'
}

func defaultActor(_ *authIO) string {
	if user := os.Getenv("USER"); user != "" {
		return user + "@cli"
	}
	return "cli"
}

func writeJSONIndented(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func confirmYes(r io.Reader) bool {
	if r == nil {
		return false
	}
	buf := make([]byte, 16)
	n, err := r.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(string(buf[:n])))
	return answer == "y" || answer == "yes"
}

// silence "imported and not used" if all branches above ever shrink:
var _ = bytes.Buffer{}
