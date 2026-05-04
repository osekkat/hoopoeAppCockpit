// Package privsep defines Hoopoe's least-privilege setup-helper contract.
package privsep

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const SchemaVersion = 1

var (
	ErrInvalidAllowlist          = errors.New("privsep: invalid allowlist")
	ErrActionNotAllowed          = errors.New("privsep: action is not allowlisted")
	ErrArgvShapeMismatch         = errors.New("privsep: argv does not match allowlist shape")
	ErrAllowlistChecksumMismatch = errors.New("privsep: allowlist checksum mismatch")

	serviceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_.@-]+\.service$`)
)

type Allowlist struct {
	Entries []AllowlistEntry
	Digest  string
}

type AllowlistEntry struct {
	Action        string
	ArgPatterns   []string
	AuditRequired bool
}

func LoadAllowlist(path string) (Allowlist, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Allowlist{}, fmt.Errorf("%w: path is required", ErrInvalidAllowlist)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Allowlist{}, fmt.Errorf("privsep: read allowlist: %w", err)
	}
	return ParseAllowlist(strings.NewReader(string(data)))
}

func ParseAllowlist(r io.Reader) (Allowlist, error) {
	if r == nil {
		return Allowlist{}, fmt.Errorf("%w: nil reader", ErrInvalidAllowlist)
	}
	var entries []AllowlistEntry
	current := AllowlistEntry{}
	flush := func() error {
		if current.Action == "" && len(current.ArgPatterns) == 0 && !current.AuditRequired {
			return nil
		}
		if !isKnownAction(current.Action) {
			return fmt.Errorf("%w: unknown action %q", ErrInvalidAllowlist, current.Action)
		}
		if len(current.ArgPatterns) == 0 {
			return fmt.Errorf("%w: action %s has no args shape", ErrInvalidAllowlist, current.Action)
		}
		entries = append(entries, current)
		current = AllowlistEntry{}
		return nil
	}

	var canonical strings.Builder
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == "----" {
			if err := flush(); err != nil {
				return Allowlist{}, err
			}
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Allowlist{}, fmt.Errorf("%w: malformed line %q", ErrInvalidAllowlist, line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "action":
			current.Action = value
			canonical.WriteString("action:")
			canonical.WriteString(value)
			canonical.WriteByte('\n')
		case "args":
			current.ArgPatterns = splitArgs(value)
			canonical.WriteString("args:")
			canonical.WriteString(strings.Join(current.ArgPatterns, " "))
			canonical.WriteByte('\n')
		case "audit":
			if value != "required" {
				return Allowlist{}, fmt.Errorf("%w: action %s audit must be required", ErrInvalidAllowlist, current.Action)
			}
			current.AuditRequired = true
			canonical.WriteString("audit:required\n")
		default:
			return Allowlist{}, fmt.Errorf("%w: unknown key %q", ErrInvalidAllowlist, key)
		}
	}
	if err := scanner.Err(); err != nil {
		return Allowlist{}, fmt.Errorf("privsep: scan allowlist: %w", err)
	}
	if err := flush(); err != nil {
		return Allowlist{}, err
	}
	if len(entries) == 0 {
		return Allowlist{}, fmt.Errorf("%w: no entries", ErrInvalidAllowlist)
	}
	sum := sha256.Sum256([]byte(canonical.String()))
	return Allowlist{
		Entries: entries,
		Digest:  "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}

func (a Allowlist) Entry(action string) (AllowlistEntry, bool) {
	for _, entry := range a.Entries {
		if entry.Action == action {
			return entry, true
		}
	}
	return AllowlistEntry{}, false
}

func (a Allowlist) ValidateChecksum(expected string) error {
	expected = normalizeDigest(expected)
	if expected == "" {
		return nil
	}
	if normalizeDigest(a.Digest) != expected {
		return fmt.Errorf("%w: got %s want %s", ErrAllowlistChecksumMismatch, a.Digest, expected)
	}
	return nil
}

func (entry AllowlistEntry) ValidateArgs(args []string) error {
	if len(args) != len(entry.ArgPatterns) {
		return fmt.Errorf("%w: action %s got %d args want %d", ErrArgvShapeMismatch, entry.Action, len(args), len(entry.ArgPatterns))
	}
	for i, pattern := range entry.ArgPatterns {
		if !argPatternMatches(pattern, args[i]) {
			return fmt.Errorf("%w: action %s arg %d got %q want %q", ErrArgvShapeMismatch, entry.Action, i+1, args[i], pattern)
		}
	}
	return nil
}

func (entry AllowlistEntry) CommandPreview(args []string) string {
	parts := append([]string{entry.Action}, args...)
	return strings.Join(parts, " ")
}

func DefaultAllowlist() Allowlist {
	list, err := ParseAllowlist(strings.NewReader(DefaultAllowlistText()))
	if err != nil {
		panic(err)
	}
	return list
}

func DefaultAllowlistText() string {
	return strings.TrimSpace(`
action: install-systemd-unit
args: --unit-path=/etc/systemd/system/hoopoe.service --source=*
audit: required
----
action: uninstall-systemd-unit
args: --unit-path=/etc/systemd/system/hoopoe.service
audit: required
----
action: chown-projects
args: --path=<project-path>
audit: required
----
action: repair-acfs
args: --doctor=true --auto-fix=<bool>
audit: required
----
action: restart-service
args: --service=<service>
audit: required
----
action: bind-privileged-port
args: --port=<privileged-port>
audit: required
----
action: create-hoopoe-user
args: --user=hoopoe
audit: required
----
action: chown-acfs-paths
args: --path=<project-path>
audit: required
----
action: register-helper-allowlist
args: --path=*
audit: required
`) + "\n"
}

func WriteDefaultAllowlist(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("%w: path is required", ErrInvalidAllowlist)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("privsep: mkdir allowlist dir: %w", err)
	}
	return os.WriteFile(path, []byte(DefaultAllowlistText()), 0o644)
}

func splitArgs(value string) []string {
	fields := strings.Fields(value)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func argPatternMatches(pattern, value string) bool {
	switch {
	case strings.Contains(pattern, "<bool>"):
		prefix := strings.TrimSuffix(pattern, "<bool>")
		v, ok := strings.CutPrefix(value, prefix)
		return ok && (v == "true" || v == "false")
	case strings.Contains(pattern, "<service>"):
		prefix := strings.TrimSuffix(pattern, "<service>")
		v, ok := strings.CutPrefix(value, prefix)
		return ok && validServiceName(v)
	case strings.Contains(pattern, "<privileged-port>"):
		prefix := strings.TrimSuffix(pattern, "<privileged-port>")
		v, ok := strings.CutPrefix(value, prefix)
		return ok && validPrivilegedPort(v)
	case strings.Contains(pattern, "<project-path>"):
		prefix := strings.TrimSuffix(pattern, "<project-path>")
		v, ok := strings.CutPrefix(value, prefix)
		return ok && validProjectPath(v)
	case strings.HasSuffix(pattern, "*"):
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(value, prefix) && len(value) > len(prefix)
	default:
		return value == pattern
	}
}

func normalizeDigest(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if !strings.HasPrefix(value, "sha256:") {
		value = "sha256:" + value
	}
	return value
}

func validServiceName(value string) bool {
	if value == "" || strings.Contains(value, "/") || strings.Contains(value, "..") {
		return false
	}
	return serviceNamePattern.MatchString(value)
}

func validPrivilegedPort(value string) bool {
	if value == "" {
		return false
	}
	port := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
		port = port*10 + int(r-'0')
	}
	return port > 0 && port < 1024
}

func validProjectPath(value string) bool {
	cleaned := filepath.Clean(value)
	return cleaned == "/data/projects" || strings.HasPrefix(cleaned, "/data/projects/")
}
