package install_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestBootstrapShellSyntax(t *testing.T) {
	cmd := exec.Command("bash", "-n", "bootstrap.sh")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n bootstrap.sh: %v\n%s", err, output)
	}
}

func TestBootstrapKeepsReleaseVerificationAndPairingTokenFlow(t *testing.T) {
	body := readFile(t, "bootstrap.sh")
	for _, required := range []string{
		"verify_release",
		"verify-release",
		"-bootstrap-token-only",
		"systemctl --user restart hoopoe.service",
		"daemon.service.started",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("bootstrap.sh missing %q", required)
		}
	}
}

func TestSystemdUnitContainsRequiredHardening(t *testing.T) {
	body := readFile(t, "../../systemd/hoopoe.service")
	for _, required := range []string{
		"Type=notify",
		"Restart=on-failure",
		"WatchdogSec=30",
		"KillMode=mixed",
		"TimeoutStopSec=20",
		"LimitNOFILE=65536",
		"ProtectSystem=strict",
		"ReadWritePaths=%h/.hoopoe /data/projects /tmp",
		"NoNewPrivileges=true",
		"PrivateTmp=true",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("hoopoe.service missing %q", required)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
