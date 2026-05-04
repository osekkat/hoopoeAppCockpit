package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/modelcontext"
)

type fakeExecutor struct {
	specs     []CommandSpec
	chunks    []StreamChunk
	result    CommandResult
	err       error
	beforeRun func() error
}

func (f *fakeExecutor) Run(_ context.Context, spec CommandSpec, onChunk func(StreamChunk) error) (CommandResult, error) {
	f.specs = append(f.specs, spec)
	if f.beforeRun != nil {
		if err := f.beforeRun(); err != nil {
			return f.result, err
		}
	}
	for _, chunk := range f.chunks {
		if onChunk != nil {
			if err := onChunk(chunk); err != nil {
				return f.result, err
			}
		}
	}
	return f.result, f.err
}

type fakeAccountRouter struct {
	calls []string
	env   []string
	err   error
}

func (f *fakeAccountRouter) EnvForAccount(_ context.Context, harness Harness, accountID string) ([]string, error) {
	f.calls = append(f.calls, string(harness)+":"+accountID)
	return append([]string(nil), f.env...), f.err
}

type auditRecorder struct {
	events []AuditEvent
	err    error
}

func (r *auditRecorder) RecordModelCall(_ context.Context, event AuditEvent) error {
	r.events = append(r.events, event)
	return r.err
}

func TestDefaultRegistryWiresThreeHarnesses(t *testing.T) {
	t.Parallel()
	registry := NewDefaultRegistry(&fakeExecutor{}, nil, nil, nil)
	got := registry.Harnesses()
	want := []Harness{HarnessClaudeCode, HarnessCodexCLI, HarnessGeminiCLI}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Harnesses() = %v, want %v", got, want)
	}
	cases := []struct {
		harness Harness
		model   string
	}{
		{HarnessClaudeCode, "claude-sonnet-4-6"},
		{HarnessCodexCLI, "gpt-5-pro"},
		{HarnessGeminiCLI, "gemini-3-pro-deep-think"},
	}
	for _, tc := range cases {
		if !registry.Supports(tc.harness, tc.model) {
			t.Fatalf("%s should support %q", tc.harness, tc.model)
		}
	}
	if registry.Supports(HarnessCodexCLI, "gemini-3-pro") {
		t.Fatalf("codex adapter should not claim gemini models")
	}
}

func TestRunStreamsAuditsRoutesAccountAndWritesArtifacts(t *testing.T) {
	t.Parallel()
	completed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	exec := &fakeExecutor{
		chunks: []StreamChunk{
			{Stream: StreamStdout, Data: []byte("partial ")},
			{Stream: StreamStderr, Data: []byte("progress\n")},
		},
		result: CommandResult{
			Stdout:      []byte("# Candidate\n\nUse the typed daemon adapter.\n"),
			Stderr:      []byte("progress\n"),
			CompletedAt: completed,
		},
	}
	accounts := &fakeAccountRouter{env: []string{"CAAM_ACTIVE_ACCOUNT=acct-codex"}}
	audit := &auditRecorder{}
	config := DefaultHarnessConfigs()[1]
	adapter := NewAdapter(config, exec)
	adapter.AccountRouter = accounts
	adapter.Audit = audit
	adapter.Artifacts = FileArtifactStore{Root: t.TempDir()}
	adapter.Now = func() time.Time { return completed.Add(-time.Minute) }

	var chunks []StreamChunk
	result, err := adapter.Run(context.Background(), RunRequest{
		Prompt:        "draft the planning candidate",
		Model:         "gpt-5.2",
		AccountID:     "acct-codex",
		PlanID:        "plan-123",
		CandidateSlug: "codex-gpt-5.2",
		Context: ContextManifest{
			PolicyRule: "planning.multi_model",
			SourceRefs: []string{"repo:plan.md"},
			Redactions: []string{"path"},
		},
	}, func(chunk StreamChunk) error {
		chunks = append(chunks, chunk)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(exec.specs) != 1 {
		t.Fatalf("expected one command execution, got %d", len(exec.specs))
	}
	spec := exec.specs[0]
	if string(spec.Stdin) != "draft the planning candidate" {
		t.Fatalf("prompt should be sent on stdin, got %q", spec.Stdin)
	}
	for _, arg := range spec.Args {
		if strings.Contains(arg, "draft the planning candidate") {
			t.Fatalf("prompt leaked into argv: %v", spec.Args)
		}
	}
	if !containsPair(spec.Args, "--model", "gpt-5.2") {
		t.Fatalf("model flag missing from args: %v", spec.Args)
	}
	if len(accounts.calls) != 1 || accounts.calls[0] != "codex_cli:acct-codex" {
		t.Fatalf("account router calls = %v", accounts.calls)
	}
	if !containsString(spec.Env, "CAAM_ACTIVE_ACCOUNT=acct-codex") {
		t.Fatalf("CAAM env not passed to command: %v", spec.Env)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected streamed chunks, got %d", len(chunks))
	}
	if chunks[0].Harness != HarnessCodexCLI || chunks[1].Harness != HarnessCodexCLI {
		t.Fatalf("chunks should be tagged with harness: %+v", chunks)
	}
	if len(audit.events) != 2 {
		t.Fatalf("expected start and completion audit events, got %d", len(audit.events))
	}
	if audit.events[0].Type != AuditModelCallStarted || audit.events[1].Type != AuditModelCallCompleted {
		t.Fatalf("unexpected audit event types: %+v", audit.events)
	}
	if audit.events[0].InputSHA256 == "" || audit.events[1].StdoutSHA256 == "" {
		t.Fatalf("audit events should carry hashes only: %+v", audit.events)
	}
	if len(result.Artifacts) != 4 {
		t.Fatalf("expected 4 artifacts, got %d: %+v", len(result.Artifacts), result.Artifacts)
	}
	candidatePath := filepath.Join(adapter.Artifacts.(FileArtifactStore).Root, ".hoopoe", "plans", "plan-123", "candidates", "codex-gpt-5.2.md")
	data, err := os.ReadFile(candidatePath)
	if err != nil {
		t.Fatalf("read candidate artifact: %v", err)
	}
	if string(data) != string(result.Stdout) {
		t.Fatalf("candidate artifact mismatch: %q", data)
	}
}

func TestRunWritesProjectContextManifestBeforeCommand(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	exec := &fakeExecutor{
		result: CommandResult{Stdout: []byte("ok")},
		beforeRun: func() error {
			matches, err := filepath.Glob(filepath.Join(root, ".hoopoe", "context-manifests", "planning", "*.json"))
			if err != nil {
				return err
			}
			if len(matches) != 1 {
				return fmt.Errorf("context manifest matches = %d, want 1", len(matches))
			}
			return nil
		},
	}
	adapter := NewAdapter(DefaultHarnessConfigs()[1], exec)
	result, err := adapter.Run(context.Background(), RunRequest{
		Prompt:  "draft plan",
		Model:   "gpt-5.2",
		WorkDir: root,
	}, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Artifacts) != 1 || !strings.HasPrefix(result.Artifacts[0].Path, ".hoopoe/context-manifests/planning/") {
		t.Fatalf("project context manifest artifact = %+v", result.Artifacts)
	}
}

func TestRunEnforcesModelContextPolicyBeforeCommand(t *testing.T) {
	t.Parallel()
	policy := modelcontext.DefaultPolicy()
	policy.SecretHandling = modelcontext.SecretHandlingBlock
	policy.StageDefaults = nil
	exec := &fakeExecutor{}
	adapter := NewAdapter(DefaultHarnessConfigs()[1], exec)
	_, err := adapter.Run(context.Background(), RunRequest{
		Prompt:        "review",
		Model:         "gpt-5.2",
		ContextPolicy: &policy,
		ContextSources: []modelcontext.Source{
			{Kind: "file", Path: "README.md", Content: []byte("Authorization: Bearer abcdefghijklmnopqrstuvwxyz")},
		},
	}, nil)
	if !errors.Is(err, modelcontext.ErrSecretBlocked) {
		t.Fatalf("err = %v, want ErrSecretBlocked", err)
	}
	if len(exec.specs) != 0 {
		t.Fatalf("command should not run when model-context policy blocks input")
	}
}

func TestAccountIDRequiresRouter(t *testing.T) {
	t.Parallel()
	exec := &fakeExecutor{}
	adapter := NewAdapter(DefaultHarnessConfigs()[0], exec)
	_, err := adapter.Run(context.Background(), RunRequest{
		Prompt:    "hello",
		Model:     "sonnet",
		AccountID: "acct-claude",
	}, nil)
	if !errors.Is(err, ErrMissingAccountRouter) {
		t.Fatalf("expected ErrMissingAccountRouter, got %v", err)
	}
	if len(exec.specs) != 0 {
		t.Fatalf("command should not run without account router")
	}
}

func TestMaxTokensRequiresHarnessFlag(t *testing.T) {
	t.Parallel()
	adapter := NewAdapter(DefaultHarnessConfigs()[2], &fakeExecutor{})
	_, err := adapter.Run(context.Background(), RunRequest{
		Prompt:    "hello",
		Model:     "gemini-3-pro",
		MaxTokens: 1024,
	}, nil)
	if !errors.Is(err, ErrUnsupportedOption) {
		t.Fatalf("expected ErrUnsupportedOption, got %v", err)
	}
}

func TestProviderCredentialEnvRejected(t *testing.T) {
	t.Parallel()
	key := strings.Join([]string{"OPENAI", "API", "KEY"}, "_")
	adapter := NewAdapter(DefaultHarnessConfigs()[1], &fakeExecutor{})
	_, err := adapter.Run(context.Background(), RunRequest{
		Prompt: "hello",
		Model:  "gpt-5",
		Env:    []string{key + "=secret"},
	}, nil)
	if !errors.Is(err, ErrProviderCredentialEnv) {
		t.Fatalf("expected ErrProviderCredentialEnv, got %v", err)
	}
	filtered := filterProviderCredentialEnv([]string{key + "=secret", "PATH=/bin"})
	if !reflect.DeepEqual(filtered, []string{"PATH=/bin"}) {
		t.Fatalf("filterProviderCredentialEnv = %v", filtered)
	}
}

func TestFileArtifactStoreRejectsTraversal(t *testing.T) {
	t.Parallel()
	store := FileArtifactStore{Root: t.TempDir()}
	_, err := store.Write(context.Background(), Artifact{
		Kind:          ArtifactCandidateMarkdown,
		PlanID:        "../plan",
		CandidateSlug: "candidate",
		Content:       []byte("x"),
	})
	if !errors.Is(err, ErrUnsafeArtifactPath) {
		t.Fatalf("expected unsafe plan path, got %v", err)
	}
	_, err = store.Write(context.Background(), Artifact{
		Kind:          ArtifactCandidateMarkdown,
		PlanID:        "plan",
		CandidateSlug: "../candidate",
		Content:       []byte("x"),
	})
	if !errors.Is(err, ErrUnsafeArtifactPath) {
		t.Fatalf("expected unsafe candidate path, got %v", err)
	}
}

func containsPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
