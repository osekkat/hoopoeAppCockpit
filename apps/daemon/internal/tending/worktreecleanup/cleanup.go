// Package worktreecleanup implements the deterministic tending job that
// removes stale isolated health/review worktrees.
package worktreecleanup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/scheduler"
)

const (
	SchemaVersion = 1
	JobID         = "worktree-cleanup"

	KindHealth = "health"
	KindReview = "review"

	defaultHealthRetention = 72 * time.Hour
	defaultReviewRetention = 7 * 24 * time.Hour
)

var ErrUnsafePath = errors.New("worktreecleanup: unsafe worktree path")

type Config struct {
	HomeDir   string
	WorkRoot  string
	Now       func() time.Time
	Retention Retention
	Runner    CommandRunner
	Remover   Remover
}

type Retention struct {
	Health time.Duration
	Review time.Duration
}

type RetentionSnapshot struct {
	HealthSeconds int64 `json:"healthSeconds"`
	ReviewSeconds int64 `json:"reviewSeconds"`
}

type CommandRunner interface {
	Run(ctx context.Context, dir string, argv []string) error
}

type Remover interface {
	RemoveAll(path string) error
}

type Candidate struct {
	ProjectID        string    `json:"projectId"`
	Kind             string    `json:"kind"`
	RunID            string    `json:"runId"`
	Path             string    `json:"path"`
	GitWorktreePath  string    `json:"gitWorktreePath"`
	ModTime          time.Time `json:"modTime"`
	AgeSeconds       int64     `json:"ageSeconds"`
	RetentionSeconds int64     `json:"retentionSeconds"`
	Bytes            int64     `json:"bytes"`
}

type Cleaned struct {
	Candidate      Candidate `json:"candidate"`
	Method         string    `json:"method"`
	FreedBytes     int64     `json:"freedBytes"`
	PrunedPointers bool      `json:"prunedPointers"`
	GitError       string    `json:"gitError,omitempty"`
}

type Failed struct {
	Candidate      Candidate `json:"candidate"`
	Error          string    `json:"error"`
	GitError       string    `json:"gitError,omitempty"`
	PrunedPointers bool      `json:"prunedPointers"`
	Retryable      bool      `json:"retryable"`
}

type Event struct {
	Type       string `json:"type"`
	ProjectID  string `json:"projectId"`
	Kind       string `json:"kind"`
	RunID      string `json:"runId"`
	Path       string `json:"path"`
	FreedBytes int64  `json:"freedBytes,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Result struct {
	SchemaVersion int               `json:"schemaVersion"`
	JobID         string            `json:"jobId"`
	WakeAgent     bool              `json:"wakeAgent"`
	Silent        bool              `json:"silent"`
	WorkRoot      string            `json:"workRoot"`
	CheckedAt     time.Time         `json:"checkedAt"`
	Retention     RetentionSnapshot `json:"retention"`
	Scanned       int               `json:"scanned"`
	Eligible      int               `json:"eligible"`
	Cleaned       []Cleaned         `json:"cleaned,omitempty"`
	Failed        []Failed          `json:"failed,omitempty"`
	FreedBytes    int64             `json:"freedBytes"`
	Events        []Event           `json:"events,omitempty"`
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, dir string, argv []string) error {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return errors.New("worktreecleanup: empty command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", strings.Join(argv, " "), err, truncateOutput(out))
	}
	return nil
}

type osRemover struct{}

func (osRemover) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

func Run(ctx context.Context, cfg Config) (scheduler.RunResult, error) {
	result, err := Sweep(ctx, cfg)
	if err != nil {
		return scheduler.RunResult{}, err
	}
	return scheduler.RunResult{
		WakeAgent: false,
		Silent:    result.Silent,
		Context: map[string]any{
			"worktreeCleanup": result,
			"cleaned":         len(result.Cleaned),
			"failed":          len(result.Failed),
			"freedBytes":      result.FreedBytes,
		},
	}, nil
}

func Sweep(ctx context.Context, cfg Config) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return Result{}, err
	}
	now := cfg.Now().UTC()
	result := Result{
		SchemaVersion: SchemaVersion,
		JobID:         JobID,
		WakeAgent:     false,
		WorkRoot:      cfg.WorkRoot,
		CheckedAt:     now,
		Retention: RetentionSnapshot{
			HealthSeconds: int64(cfg.Retention.Health.Seconds()),
			ReviewSeconds: int64(cfg.Retention.Review.Seconds()),
		},
	}
	candidates, err := Discover(ctx, cfg)
	if err != nil {
		return Result{}, err
	}
	result.Scanned = len(candidates)
	for _, candidate := range candidates {
		if candidate.AgeSeconds < candidate.RetentionSeconds {
			continue
		}
		result.Eligible++
		cleaned, failed := cleanCandidate(ctx, cfg, candidate)
		if failed != nil {
			result.Failed = append(result.Failed, *failed)
			result.Events = append(result.Events, Event{
				Type:      "worktree_cleanup_failed",
				ProjectID: candidate.ProjectID,
				Kind:      candidate.Kind,
				RunID:     candidate.RunID,
				Path:      candidate.Path,
				Error:     failed.Error,
			})
			continue
		}
		result.Cleaned = append(result.Cleaned, cleaned)
		result.FreedBytes += cleaned.FreedBytes
		result.Events = append(result.Events, Event{
			Type:       "worktree_cleaned",
			ProjectID:  candidate.ProjectID,
			Kind:       candidate.Kind,
			RunID:      candidate.RunID,
			Path:       candidate.Path,
			FreedBytes: cleaned.FreedBytes,
		})
	}
	result.Silent = result.Eligible == 0
	return result, nil
}

func Discover(ctx context.Context, cfg Config) ([]Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg, err := normalizeConfig(cfg)
	if err != nil {
		return nil, err
	}
	root, err := filepath.Abs(cfg.WorkRoot)
	if err != nil {
		return nil, err
	}
	projects, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	now := cfg.Now().UTC()
	candidates := []Candidate{}
	for _, project := range projects {
		if !project.IsDir() || !safeSegment(project.Name()) {
			continue
		}
		for _, kind := range []string{KindHealth, KindReview} {
			parent := filepath.Join(root, project.Name(), kind)
			runs, err := os.ReadDir(parent)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			for _, run := range runs {
				if !run.IsDir() || run.Type()&os.ModeSymlink != 0 || !safeSegment(run.Name()) {
					continue
				}
				path := filepath.Join(parent, run.Name())
				if err := requireUnderRoot(root, path); err != nil {
					return nil, err
				}
				info, err := run.Info()
				if err != nil {
					return nil, err
				}
				bytes, err := dirSize(path)
				if err != nil {
					return nil, err
				}
				retention := cfg.Retention.Health
				if kind == KindReview {
					retention = cfg.Retention.Review
				}
				candidates = append(candidates, Candidate{
					ProjectID:        project.Name(),
					Kind:             kind,
					RunID:            run.Name(),
					Path:             path,
					GitWorktreePath:  gitWorktreePath(path),
					ModTime:          info.ModTime().UTC(),
					AgeSeconds:       int64(now.Sub(info.ModTime()).Seconds()),
					RetentionSeconds: int64(retention.Seconds()),
					Bytes:            bytes,
				})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ProjectID == candidates[j].ProjectID {
			if candidates[i].Kind == candidates[j].Kind {
				return candidates[i].RunID < candidates[j].RunID
			}
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].ProjectID < candidates[j].ProjectID
	})
	return candidates, nil
}

func DefaultDefinition() scheduler.Definition {
	return scheduler.Definition{
		ID:              JobID,
		Name:            "Worktree cleanup",
		Kind:            scheduler.KindDeterministic,
		Version:         scheduler.SchemaVersion,
		Schedule:        scheduler.Schedule{Type: scheduler.ScheduleInterval, Interval: time.Hour},
		EnabledToolsets: []string{"git_write"},
		Script:          "worktree-cleanup.go",
		Deliver:         "hoopoe_activity_panel",
		Repeat:          scheduler.RepeatForever(),
		Timeout:         10 * time.Minute,
		MaxConcurrency:  1,
		MisfirePolicy:   scheduler.MisfireRunOnce,
		RetryPolicy:     scheduler.RetryFixed,
		DeadLetterAfter: 3,
		AuditAlways:     true,
	}
}

func DefaultDefinitionYAML() []byte {
	return []byte(strings.Join([]string{
		"id: worktree-cleanup",
		"name: Worktree cleanup",
		"kind: deterministic",
		"version: 1",
		"schedule: every 1h",
		"enabled_toolsets: [git_write]",
		"script: worktree-cleanup.go",
		"deliver: hoopoe_activity_panel",
		"repeat: forever",
		"paused: false",
		"timeout: 10m",
		"max_concurrency: 1",
		"misfire_policy: run_once",
		"retry_policy: fixed",
		"dead_letter_after: 3",
		"audit_always: true",
		"",
	}, "\n"))
}

func cleanCandidate(ctx context.Context, cfg Config, candidate Candidate) (Cleaned, *Failed) {
	if err := requireUnderRoot(cfg.WorkRoot, candidate.Path); err != nil {
		return Cleaned{}, &Failed{Candidate: candidate, Error: err.Error(), Retryable: false}
	}
	if err := requireUnderRoot(cfg.WorkRoot, candidate.GitWorktreePath); err != nil {
		return Cleaned{}, &Failed{Candidate: candidate, Error: err.Error(), Retryable: false}
	}
	gitErr := cfg.Runner.Run(ctx, candidate.GitWorktreePath, []string{"git", "worktree", "remove", "--force", candidate.GitWorktreePath})
	pruned := false
	if gitErr != nil {
		if pruneErr := cfg.Runner.Run(ctx, candidate.GitWorktreePath, []string{"git", "worktree", "prune"}); pruneErr == nil {
			pruned = true
		}
	}
	if err := cfg.Remover.RemoveAll(candidate.Path); err != nil {
		failed := &Failed{
			Candidate:      candidate,
			Error:          err.Error(),
			PrunedPointers: pruned,
			Retryable:      true,
		}
		if gitErr != nil {
			failed.GitError = gitErr.Error()
		}
		return Cleaned{}, failed
	}
	cleaned := Cleaned{
		Candidate:      candidate,
		Method:         "git_worktree_remove",
		FreedBytes:     candidate.Bytes,
		PrunedPointers: pruned,
	}
	if gitErr != nil {
		cleaned.Method = "remove_all_after_git_error"
		cleaned.GitError = gitErr.Error()
	}
	return cleaned, nil
}

func normalizeConfig(cfg Config) (Config, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.HomeDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Config{}, err
		}
		cfg.HomeDir = home
	}
	if cfg.WorkRoot == "" {
		cfg.WorkRoot = filepath.Join(cfg.HomeDir, ".hoopoe", "work")
	}
	root, err := filepath.Abs(cfg.WorkRoot)
	if err != nil {
		return Config{}, err
	}
	cfg.WorkRoot = root
	if cfg.Retention.Health <= 0 {
		cfg.Retention.Health = defaultHealthRetention
	}
	if cfg.Retention.Review <= 0 {
		cfg.Retention.Review = defaultReviewRetention
	}
	if cfg.Runner == nil {
		cfg.Runner = osCommandRunner{}
	}
	if cfg.Remover == nil {
		cfg.Remover = osRemover{}
	}
	return cfg, nil
}

func gitWorktreePath(root string) string {
	repo := filepath.Join(root, "repo")
	if info, err := os.Stat(repo); err == nil && info.IsDir() {
		return repo
	}
	return root
}

func dirSize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	return size, err
}

func requireUnderRoot(root, path string) error {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if cleanPath != cleanRoot && !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) {
		return ErrUnsafePath
	}
	return nil
}

func safeSegment(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\`)
}

func truncateOutput(out []byte) string {
	text := strings.TrimSpace(string(out))
	if len(text) > 240 {
		return text[:240] + "..."
	}
	return text
}
