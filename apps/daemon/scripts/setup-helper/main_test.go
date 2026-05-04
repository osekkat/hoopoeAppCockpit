package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/security/privsep"
)

func TestDefaultAllowlistCommand(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(context.Background(), []string{"default-allowlist"}, &stdout, &stderr); err != nil {
		t.Fatalf("run default-allowlist: %v stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "action: repair-acfs") {
		t.Fatalf("default allowlist = %s", stdout.String())
	}
}

func TestSudoersRuleCommand(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(context.Background(), []string{"sudoers-rule", "/opt/hoopoe/bin/hoopoe-setup-helper"}, &stdout, &stderr); err != nil {
		t.Fatalf("run sudoers-rule: %v stderr=%s", err, stderr.String())
	}
	if got := stdout.String(); got != privsep.SudoersRule("/opt/hoopoe/bin/hoopoe-setup-helper") {
		t.Fatalf("sudoers rule = %q", got)
	}
}

func TestBootstrapDryRunValidatesAllowlistAndWritesAudit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	allowlistPath := filepath.Join(dir, "setup-helper.allowed")
	auditPath := filepath.Join(dir, "setup-helper.bootstrap.log")
	if err := os.WriteFile(allowlistPath, []byte(privsep.DefaultAllowlistText()), 0o644); err != nil {
		t.Fatalf("WriteFile allowlist: %v", err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	err := run(context.Background(), []string{
		"bootstrap",
		"--allowlist", allowlistPath,
		"--audit-log", auditPath,
		"--dry-run",
		"--action", "install-systemd-unit",
		"--",
		"--unit-path=/etc/systemd/system/hoopoe.service",
		"--source=/tmp/hoopoe.service",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run bootstrap: %v stderr=%s stdout=%s", err, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), `"status":"succeeded"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
	audit, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile audit: %v", err)
	}
	if !strings.Contains(string(audit), `"bootstrapMode":true`) {
		t.Fatalf("audit = %s", audit)
	}
}
