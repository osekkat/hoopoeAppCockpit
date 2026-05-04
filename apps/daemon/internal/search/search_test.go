package search

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildCommandUsesSafeRipgrepJSONArgs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".hoopoeignore"), "docs/**\n")
	writeFile(t, filepath.Join(root, "docs", "guide.md"), "needle\n")
	spec, err := BuildCommand("rg", Request{
		RepoPath:   root,
		Query:      "needle",
		Paths:      []string{"docs"},
		Literal:    true,
		MaxResults: 12,
	})
	if err != nil {
		t.Fatalf("BuildCommand: %v", err)
	}
	want := []string{
		"--json",
		"--color=never",
		"--hidden",
		"--glob",
		"!.git",
		"--max-count",
		"12",
		"--fixed-strings",
		"--ignore-file",
		".hoopoeignore",
		"--",
		"needle",
		"docs",
	}
	if spec.Path != "rg" || spec.Dir == "" || !reflect.DeepEqual(spec.Args, want) {
		t.Fatalf("spec = %+v, want args %+v", spec, want)
	}
}

func TestBuildCommandRejectsTraversalAndShell(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "secret\n")
	_, err := BuildCommand("sh", Request{RepoPath: root, Query: "secret"})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("shell err = %v, want ErrInvalidRequest", err)
	}
	_, err = BuildCommand("rg", Request{RepoPath: root, Query: "secret", Paths: []string{outside}})
	if !errors.Is(err, ErrPathOutsideRoot) {
		t.Fatalf("traversal err = %v, want ErrPathOutsideRoot", err)
	}
}

func TestParseRipgrepJSONParsesMatchesAndCapsStoredResults(t *testing.T) {
	t.Parallel()
	body := strings.Join([]string{
		`{"type":"begin","data":{"path":{"text":"README.md"}}}`,
		`{"type":"match","data":{"path":{"text":"README.md"},"lines":{"text":"alpha needle beta\n"},"line_number":7,"absolute_offset":10,"submatches":[{"match":{"text":"needle"},"start":6,"end":12}]}}`,
		`{"type":"match","data":{"path":{"text":"docs/guide.md"},"lines":{"text":"needle again\n"},"line_number":3,"absolute_offset":20,"submatches":[{"match":{"text":"needle"},"start":0,"end":6}]}}`,
		`{"type":"summary","data":{"stats":{"matches":2}}}`,
	}, "\n")
	got, err := ParseRipgrepJSON(strings.NewReader(body), 1)
	if err != nil {
		t.Fatalf("ParseRipgrepJSON: %v", err)
	}
	if got.MatchCount != 2 || !got.Capped || len(got.Results) != 1 {
		t.Fatalf("parsed counts = %+v", got)
	}
	result := got.Results[0]
	if result.Path != "README.md" || result.Line != 7 || result.Column != 7 || result.Text != "alpha needle beta" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Submatches) != 1 || result.Submatches[0].Text != "needle" {
		t.Fatalf("submatches = %+v", result.Submatches)
	}
}

func TestServiceTreatsRipgrepNoMatchExitAsEmptyResult(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runner := fakeRunner{run: RunResult{ExitCode: 1}}
	service := NewService(Config{
		Runner: runner,
		Now: func() time.Time {
			return time.Date(2026, 5, 4, 2, 0, 0, 0, time.UTC)
		},
	})
	got, err := service.Search(context.Background(), Request{
		ProjectID: "proj_1",
		RepoPath:  root,
		Query:     "missing",
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got.MatchCount != 0 || len(got.Results) != 0 || got.ProjectID != "proj_1" {
		t.Fatalf("response = %+v", got)
	}
}

func TestServiceReturnsCommandFailureForRipgrepErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := NewService(Config{Runner: fakeRunner{run: RunResult{
		ExitCode: 2,
		Stderr:   []byte("regex parse error"),
	}}})
	_, err := service.Search(context.Background(), Request{RepoPath: root, Query: "["})
	if !errors.Is(err, ErrCommandFailed) {
		t.Fatalf("Search err = %v, want ErrCommandFailed", err)
	}
}

func TestServicePreservesOutputLimitError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	service := NewService(Config{Runner: fakeRunner{err: ErrOutputTooLarge}})
	_, err := service.Search(context.Background(), Request{RepoPath: root, Query: "needle"})
	if !errors.Is(err, ErrCommandFailed) || !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("Search err = %v, want ErrCommandFailed wrapping ErrOutputTooLarge", err)
	}
}

func TestOSRunnerCancelsCommandWhenStdoutExceedsLimit(t *testing.T) {
	t.Parallel()
	yesPath, err := exec.LookPath("yes")
	if err != nil {
		t.Skip("yes binary unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	run, err := OSRunner{}.Run(ctx, CommandSpec{
		Path:           yesPath,
		Args:           []string{"needle"},
		Dir:            t.TempDir(),
		MaxStdoutBytes: 128,
		MaxStderrBytes: DefaultMaxStderrBytes,
	})
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("Run err = %v, want ErrOutputTooLarge", err)
	}
	if len(run.Stdout) > 128 {
		t.Fatalf("stdout length = %d, want <= 128", len(run.Stdout))
	}
	if ctx.Err() != nil {
		t.Fatalf("runner should cancel only the child process before test context expires: %v", ctx.Err())
	}
}

type fakeRunner struct {
	run RunResult
	err error
}

func (r fakeRunner) Run(context.Context, CommandSpec) (RunResult, error) {
	return r.run, r.err
}

func writeFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
