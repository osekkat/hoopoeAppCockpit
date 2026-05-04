// Command hoopoe-setup-helper is the privileged companion executable for
// setup and repair actions that the steady-state daemon must not perform as
// root. It intentionally accepts only named actions and validates every argv
// shape against a root-owned allowlist before execution.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/security/privsep"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "hoopoe-setup-helper: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return fmt.Errorf("missing subcommand")
	}
	switch args[0] {
	case "run":
		return runHelper(ctx, privsep.ModeSteadyState, args[1:], stdout)
	case "bootstrap":
		return runHelper(ctx, privsep.ModeBootstrap, args[1:], stdout)
	case "default-allowlist":
		fmt.Fprint(stdout, privsep.DefaultAllowlistText())
		return nil
	case "sudoers-rule":
		helperPath := privsep.DefaultHelperPath
		if len(args) > 1 {
			helperPath = args[1]
		}
		fmt.Fprint(stdout, privsep.SudoersRule(helperPath))
		return nil
	case "--help", "-h", "help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runHelper(ctx context.Context, mode privsep.Mode, args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet(string(mode), flag.ContinueOnError)
	action := flags.String("action", "", "allowlisted helper action")
	allowlistPath := flags.String("allowlist", privsep.DefaultAllowlistPath, "root-owned setup-helper allowlist")
	expectedDigest := flags.String("expected-allowlist-sha256", "", "daemon-recorded allowlist sha256")
	approvalToken := flags.String("approval-token", "", "daemon-issued one-use approval token")
	daemonSocket := flags.String("daemon-socket", "", "daemon UNIX socket for steady-state token validation")
	approvalSecretFile := flags.String("approval-secret-file", "", "development HMAC approval secret file")
	auditLog := flags.String("audit-log", "", "JSONL audit log path")
	dryRun := flags.Bool("dry-run", false, "validate and audit without executing privileged command")
	if err := flags.Parse(args); err != nil {
		return err
	}
	allowlist, err := privsep.LoadAllowlist(*allowlistPath)
	if err != nil {
		return err
	}
	engine := privsep.Engine{
		TokenValidator: tokenValidator(*daemonSocket, *approvalSecretFile),
		Executor:       executor(*dryRun),
		Audit:          privsep.FileAuditSink{Path: *auditLog},
		Now:            time.Now,
	}
	result, err := engine.Run(ctx, privsep.Invocation{
		Mode:                    mode,
		Action:                  *action,
		Args:                    append([]string(nil), flags.Args()...),
		ApprovalToken:           *approvalToken,
		Allowlist:               allowlist,
		ExpectedAllowlistDigest: *expectedDigest,
	})
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(result)
	return err
}

func tokenValidator(socketPath, secretPath string) privsep.TokenValidator {
	if strings.TrimSpace(socketPath) != "" {
		return privsep.DaemonSocketTokenValidator{SocketPath: socketPath}
	}
	if strings.TrimSpace(secretPath) == "" {
		return nil
	}
	data, err := os.ReadFile(secretPath)
	if err != nil {
		return brokenTokenValidator{err: err}
	}
	return privsep.HMACTokenAuthority{
		Secret: []byte(strings.TrimSpace(string(data))),
		Store:  privsep.NewMemoryTokenStore(),
		Now:    time.Now,
	}
}

func executor(dryRun bool) privsep.ActionExecutor {
	if dryRun {
		return dryRunExecutor{}
	}
	return privsep.CommandExecutor{}
}

type dryRunExecutor struct{}

func (dryRunExecutor) ExecutePrivilegedAction(context.Context, string, []string) (privsep.ActionResult, error) {
	return privsep.ActionResult{ExitCode: 0, Output: "dry-run"}, nil
}

type brokenTokenValidator struct {
	err error
}

func (v brokenTokenValidator) ValidateApprovalToken(context.Context, string, privsep.TokenValidationRequest) (privsep.TokenClaims, error) {
	return privsep.TokenClaims{}, v.err
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: hoopoe-setup-helper <subcommand> [flags] -- [action args]

subcommands:
  bootstrap             run a bootstrap-only setup action as root
  run                   run a steady-state action with a daemon approval token
  default-allowlist     print the default /etc/hoopoe/setup-helper.allowed body
  sudoers-rule [path]   print the narrow sudoers rule for the helper path

steady-state example:
  hoopoe-setup-helper run --approval-token=$TOKEN --daemon-socket=/run/hoopoe/daemon.sock \
    --action=repair-acfs --expected-allowlist-sha256=sha256:... -- --doctor=true --auto-fix=true
`)
}
