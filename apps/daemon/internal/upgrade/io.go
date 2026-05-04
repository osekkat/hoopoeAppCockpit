package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/release"
)

const (
	defaultDownloadLimit  = 256 << 20
	defaultCommandTimeout = 30 * time.Second
)

type HTTPFetcher struct {
	Client   *http.Client
	MaxBytes int64
}

func (f HTTPFetcher) Fetch(ctx context.Context, location ReleaseLocation) (ReleaseBundle, error) {
	client := f.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	limit := f.MaxBytes
	if limit <= 0 {
		limit = defaultDownloadLimit
	}

	binary, err := fetchURL(ctx, client, location.BinaryURL, limit)
	if err != nil {
		return ReleaseBundle{}, fmt.Errorf("binary: %w", err)
	}
	manifestBytes, err := fetchURL(ctx, client, location.ManifestURL, limit)
	if err != nil {
		return ReleaseBundle{}, fmt.Errorf("manifest: %w", err)
	}
	signature, err := fetchURL(ctx, client, location.SignatureURL, limit)
	if err != nil {
		return ReleaseBundle{}, fmt.Errorf("signature: %w", err)
	}
	attestation, err := fetchURL(ctx, client, location.AttestationURL, limit)
	if err != nil {
		return ReleaseBundle{}, fmt.Errorf("attestation: %w", err)
	}
	sbom, err := fetchURL(ctx, client, location.SBOMURL, limit)
	if err != nil {
		return ReleaseBundle{}, fmt.Errorf("sbom: %w", err)
	}

	var manifest release.Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return ReleaseBundle{}, fmt.Errorf("manifest decode: %w", err)
	}

	cveDB := release.CVEDatabase{SchemaVersion: release.SchemaVersion}
	if strings.TrimSpace(location.CVEDatabaseURL) != "" {
		cveBytes, err := fetchURL(ctx, client, location.CVEDatabaseURL, limit)
		if err != nil {
			return ReleaseBundle{}, fmt.Errorf("cve database: %w", err)
		}
		if err := json.Unmarshal(cveBytes, &cveDB); err != nil {
			return ReleaseBundle{}, fmt.Errorf("cve database decode: %w", err)
		}
	}

	return ReleaseBundle{
		Manifest:    manifest,
		Binary:      binary,
		Signature:   signature,
		Attestation: attestation,
		SBOM:        sbom,
		CVEDatabase: cveDB,
	}, nil
}

func fetchURL(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, errors.New("url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

type FileBackupStore struct {
	BackupDir string
	Now       func() time.Time
}

func (s FileBackupStore) Backup(ctx context.Context, req BackupRequest) (BackupRecord, error) {
	backupDir := firstNonEmpty(req.BackupDir, s.BackupDir)
	if backupDir == "" {
		return BackupRecord{}, fmt.Errorf("%w: backupDir is required", ErrInvalidRequest)
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	id := fmt.Sprintf("daemon-upgrade-%s-%s", safeName(firstNonEmpty(req.Version, "unknown")), now().UTC().Format("20060102T150405Z"))
	root := filepath.Join(backupDir, id)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return BackupRecord{}, fmt.Errorf("mkdir backup dir: %w", err)
	}

	record := BackupRecord{
		ID:               id,
		TargetBinaryPath: req.TargetBinaryPath,
		DatabasePath:     req.DatabasePath,
		ConfigPaths:      append([]string(nil), req.ConfigPaths...),
	}
	candidates := []struct {
		kind string
		path string
	}{
		{kind: "binary", path: req.TargetBinaryPath},
		{kind: "database", path: req.DatabasePath},
	}
	for i, path := range req.ConfigPaths {
		candidates = append(candidates, struct {
			kind string
			path string
		}{kind: fmt.Sprintf("config-%02d", i+1), path: path})
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.path) == "" {
			continue
		}
		backupPath := filepath.Join(root, candidate.kind+"-"+safeName(filepath.Base(candidate.path)))
		present, err := copyIfExists(ctx, candidate.path, backupPath)
		if err != nil {
			return BackupRecord{}, fmt.Errorf("backup %s: %w", candidate.kind, err)
		}
		record.Files = append(record.Files, BackupFile{
			Kind:       candidate.kind,
			SourcePath: candidate.path,
			BackupPath: backupPath,
			Present:    present,
		})
	}
	if len(record.Files) == 0 {
		return BackupRecord{}, fmt.Errorf("%w: no binary, database, or config paths to back up", ErrBackupFailed)
	}
	return record, nil
}

func (s FileBackupStore) Restore(ctx context.Context, record BackupRecord) error {
	if record.ID == "" {
		return fmt.Errorf("%w: backup record has empty ID", ErrRollbackFailed)
	}
	for _, file := range record.Files {
		if !file.Present {
			continue
		}
		if err := copyFileAtomic(ctx, file.BackupPath, file.SourcePath, 0); err != nil {
			return fmt.Errorf("restore %s: %w", file.Kind, err)
		}
	}
	return nil
}

type FileInstaller struct{}

func (FileInstaller) Install(ctx context.Context, req InstallRequest) error {
	if strings.TrimSpace(req.TargetBinaryPath) == "" {
		return fmt.Errorf("%w: targetBinaryPath is required", ErrInvalidRequest)
	}
	if len(req.Binary) == 0 {
		return fmt.Errorf("%w: binary is empty", ErrInvalidRequest)
	}
	if err := writeFileAtomic(ctx, req.TargetBinaryPath, req.Binary, 0o755); err != nil {
		return err
	}
	if strings.TrimSpace(req.InventoryPath) != "" {
		if err := release.WriteInventory(req.InventoryPath, req.Inventory); err != nil {
			return fmt.Errorf("write release inventory: %w", err)
		}
	}
	return nil
}

type CommandRunner interface {
	Run(ctx context.Context, invocation Invocation) (CommandResult, error)
}

type Invocation struct {
	Argv    []string
	Timeout time.Duration
}

type CommandResult struct {
	Argv   []string
	Output string
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, invocation Invocation) (CommandResult, error) {
	if len(invocation.Argv) == 0 {
		return CommandResult{}, errors.New("upgrade: empty command")
	}
	timeout := invocation.Timeout
	if timeout <= 0 {
		timeout = defaultCommandTimeout
	}
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, invocation.Argv[0], invocation.Argv[1:]...)
	output, err := cmd.CombinedOutput()
	result := CommandResult{
		Argv:   append([]string(nil), invocation.Argv...),
		Output: string(output),
	}
	if cmdCtx.Err() != nil {
		return result, cmdCtx.Err()
	}
	if err != nil {
		return result, fmt.Errorf("%v: %w: %s", invocation.Argv, err, strings.TrimSpace(result.Output))
	}
	return result, nil
}

type SystemdManager struct {
	Unit    string
	User    bool
	Runner  CommandRunner
	Timeout time.Duration
}

func (m SystemdManager) Stop(ctx context.Context) error {
	return m.run(ctx, "stop")
}

func (m SystemdManager) Start(ctx context.Context) error {
	return m.run(ctx, "start")
}

func (m SystemdManager) run(ctx context.Context, action string) error {
	runner := m.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	unit := firstNonEmpty(m.Unit, "hoopoe.service")
	argv := []string{"systemctl"}
	if m.User {
		argv = append(argv, "--user")
	}
	argv = append(argv, action, unit)
	_, err := runner.Run(ctx, Invocation{Argv: argv, Timeout: m.Timeout})
	return err
}

func copyIfExists(ctx context.Context, src, dst string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	info, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if info.IsDir() {
		return false, fmt.Errorf("%s is a directory", src)
	}
	if err := copyFileAtomic(ctx, src, dst, info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func copyFileAtomic(ctx context.Context, src, dst string, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = in.Close() }()
	if mode == 0 {
		if info, err := in.Stat(); err == nil {
			mode = info.Mode().Perm()
		}
	}
	if mode == 0 {
		mode = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("mkdir destination: %w", err)
	}
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("open temp: %w", err)
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		return fmt.Errorf("copy: %w", copyErr)
	}
	if syncErr != nil {
		return fmt.Errorf("sync: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close: %w", closeErr)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func writeFileAtomic(ctx context.Context, dst string, data []byte, mode os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("mkdir destination: %w", err)
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func safeName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "empty"
	}
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
