package privsep

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type CommandRunner interface {
	Run(ctx context.Context, argv []string) (ActionResult, error)
}

type ExecRunner struct {
	Timeout time.Duration
}

func (r ExecRunner) Run(ctx context.Context, argv []string) (ActionResult, error) {
	if len(argv) == 0 {
		return ActionResult{ExitCode: 1}, fmt.Errorf("%w: empty command", ErrInvalidInvocation)
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, argv[0], argv[1:]...)
	output, err := cmd.CombinedOutput()
	result := ActionResult{ExitCode: 0, Output: string(output)}
	if cmdCtx.Err() != nil {
		result.ExitCode = 1
		return result, cmdCtx.Err()
	}
	if err != nil {
		result.ExitCode = exitCode(err)
		return result, fmt.Errorf("%v: %w: %s", argv, err, strings.TrimSpace(result.Output))
	}
	return result, nil
}

type CommandExecutor struct {
	Runner CommandRunner
}

func (e CommandExecutor) ExecutePrivilegedAction(ctx context.Context, action string, args []string) (ActionResult, error) {
	argv, err := actionArgv(action, args)
	if err != nil {
		return ActionResult{ExitCode: 1}, err
	}
	runner := e.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	return runner.Run(ctx, argv)
}

func actionArgv(action string, args []string) ([]string, error) {
	arg := func(prefix string) (string, bool) {
		for _, value := range args {
			if strings.HasPrefix(value, prefix) {
				return strings.TrimPrefix(value, prefix), true
			}
		}
		return "", false
	}
	switch action {
	case "create-hoopoe-user":
		return DefaultDaemonUserSpec().UserAddArgv(), nil
	case "install-systemd-unit":
		unitPath, ok := arg("--unit-path=")
		if !ok {
			return nil, fmt.Errorf("%w: --unit-path is required", ErrArgvShapeMismatch)
		}
		source, ok := arg("--source=")
		if !ok {
			return nil, fmt.Errorf("%w: --source is required", ErrArgvShapeMismatch)
		}
		return []string{"install", "-m", "0644", source, unitPath}, nil
	case "uninstall-systemd-unit":
		unitPath, ok := arg("--unit-path=")
		if !ok {
			return nil, fmt.Errorf("%w: --unit-path is required", ErrArgvShapeMismatch)
		}
		return []string{"systemctl", "disable", "--now", filepath.Base(unitPath)}, nil
	case "chown-projects":
		path, ok := arg("--path=")
		if !ok {
			return nil, fmt.Errorf("%w: --path is required", ErrArgvShapeMismatch)
		}
		return []string{"chown", "-R", DaemonUser + ":" + DaemonGroup, path}, nil
	case "chown-acfs-paths":
		path, ok := arg("--path=")
		if !ok {
			return nil, fmt.Errorf("%w: --path is required", ErrArgvShapeMismatch)
		}
		return []string{"chown", "-R", DaemonUser + ":" + DaemonGroup, path}, nil
	case "repair-acfs":
		autoFix, ok := arg("--auto-fix=")
		if !ok {
			return nil, fmt.Errorf("%w: --auto-fix is required", ErrArgvShapeMismatch)
		}
		if autoFix == "true" {
			return []string{"acfs", "doctor", "--auto-fix"}, nil
		}
		return []string{"acfs", "doctor"}, nil
	case "restart-service":
		service, ok := arg("--service=")
		if !ok {
			return nil, fmt.Errorf("%w: --service is required", ErrArgvShapeMismatch)
		}
		return []string{"systemctl", "restart", service}, nil
	case "register-helper-allowlist":
		path, ok := arg("--path=")
		if !ok {
			return nil, fmt.Errorf("%w: --path is required", ErrArgvShapeMismatch)
		}
		return []string{"install", "-m", "0644", path, DefaultAllowlistPath}, nil
	case "bind-privileged-port":
		return ActionResultArgv("true"), nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrActionNotAllowed, action)
	}
}

func ActionResultArgv(command string) []string {
	return []string{command}
}

func exitCode(err error) int {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return 1
}

type FileAuditSink struct {
	Path string
}

func (s FileAuditSink) AppendSetupHelperAudit(ctx context.Context, event AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := strings.TrimSpace(s.Path)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("privsep: mkdir audit dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("privsep: open audit log: %w", err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(event); err != nil {
		return fmt.Errorf("privsep: write audit log: %w", err)
	}
	return f.Sync()
}
