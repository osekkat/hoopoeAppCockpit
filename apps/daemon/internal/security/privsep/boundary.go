package privsep

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	DaemonUser           = "hoopoe"
	DaemonGroup          = "hoopoe"
	DaemonHome           = "/var/lib/hoopoe"
	DaemonShell          = "/usr/sbin/nologin"
	DefaultHelperPath    = "/usr/local/bin/hoopoe-setup-helper"
	DefaultAllowlistPath = "/etc/hoopoe/setup-helper.allowed"
	DefaultSudoersPath   = "/etc/sudoers.d/hoopoe"
)

type DaemonUserSpec struct {
	User           string
	Group          string
	Home           string
	Shell          string
	WritablePaths  []string
	ReadOnlyPaths  []string
	ForbiddenPaths []string
}

func DefaultDaemonUserSpec() DaemonUserSpec {
	return DaemonUserSpec{
		User:  DaemonUser,
		Group: DaemonGroup,
		Home:  DaemonHome,
		Shell: DaemonShell,
		WritablePaths: []string{
			"/var/lib/hoopoe",
			"/data/projects",
			"/tmp",
		},
		ReadOnlyPaths: []string{
			"/etc/hoopoe",
		},
		ForbiddenPaths: []string{
			"/etc/systemd/system",
			"/usr/local/bin",
			"/var/log",
		},
	}
}

func (s DaemonUserSpec) UserAddArgv() []string {
	user := firstNonEmpty(s.User, DaemonUser)
	home := firstNonEmpty(s.Home, DaemonHome)
	shell := firstNonEmpty(s.Shell, DaemonShell)
	return []string{
		"useradd",
		"--system",
		"--home-dir", home,
		"--shell", shell,
		"--user-group",
		"--comment", "Hoopoe daemon least-privilege user",
		user,
	}
}

func SudoersRule(helperPath string) string {
	helperPath = filepath.Clean(firstNonEmpty(helperPath, DefaultHelperPath))
	return fmt.Sprintf("%s ALL=(root) NOPASSWD: %s run --approval-token=*\n", DaemonUser, helperPath)
}

func ValidateSudoersRule(rule, helperPath string) error {
	want := SudoersRule(helperPath)
	if strings.TrimSpace(rule) != strings.TrimSpace(want) {
		return fmt.Errorf("privsep: sudoers rule mismatch: got %q want %q", strings.TrimSpace(rule), strings.TrimSpace(want))
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
