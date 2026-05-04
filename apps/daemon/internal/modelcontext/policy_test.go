package modelcontext

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultPolicyRoundTripAndStageOverride(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), ".hoopoe", "model-context-policy.json")
	if err := WriteDefaultPolicyIfMissing(context.Background(), path); err != nil {
		t.Fatalf("WriteDefaultPolicyIfMissing: %v", err)
	}
	loaded, err := LoadPolicyFile(path)
	if err != nil {
		t.Fatalf("LoadPolicyFile: %v", err)
	}
	if loaded.SchemaVersion != SchemaVersion {
		t.Fatalf("schemaVersion = %d", loaded.SchemaVersion)
	}
	policy := loaded.ContextPolicy.ForStage(StageTending)
	if policy.RawSourceMode != RawSourceNever || policy.SecretHandling != SecretHandlingBlock {
		t.Fatalf("tending policy = %+v", policy)
	}
	if policy.Digest() == "" {
		t.Fatalf("policy digest missing")
	}
}

func TestEvaluateExcludesEnvAndRedactsSecretLikeContent(t *testing.T) {
	t.Parallel()
	result, err := Evaluate(DefaultPolicy(), EvaluationRequest{
		ProjectID:  "proj",
		Stage:      StagePlanning,
		Harness:    "codex_cli",
		Model:      "gpt-subscription-cli",
		AccountID:  "acct-1",
		PolicyRule: "planning.default",
		Prompt:     []byte("summarize repo"),
		Sources: []Source{
			{Kind: "file", Path: ".env", Content: []byte("TOKEN=secret"), Raw: true},
			{Kind: "file", Path: "cmd/server/main.go", Content: []byte("const token = \"sk-ant-1234567890abcdef1234567890\""), Raw: false},
		},
		Now: fixedNow,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if len(result.Manifest.ExcludedRefs) != 1 || result.Manifest.ExcludedRefs[0].Path != ".env" {
		t.Fatalf("excluded refs = %+v", result.Manifest.ExcludedRefs)
	}
	if len(result.Manifest.Redactions) == 0 {
		t.Fatalf("expected redaction records: %+v", result.Manifest)
	}
	body, err := json.Marshal(result.Manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if strings.Contains(string(body), "sk-ant-1234567890abcdef1234567890") || strings.Contains(string(body), "TOKEN=secret") {
		t.Fatalf("manifest leaked sensitive input: %s", body)
	}
	if got := string(result.RedactedSources[0].Content); strings.Contains(got, "sk-ant-1234567890abcdef1234567890") {
		t.Fatalf("redacted source leaked token: %q", got)
	}
}

func TestEvaluateBlocksSecretsWhenPolicyRequires(t *testing.T) {
	t.Parallel()
	policy := DefaultPolicy()
	policy.SecretHandling = SecretHandlingBlock
	policy.StageDefaults = nil
	_, err := Evaluate(policy, EvaluationRequest{
		ProjectID: "proj",
		Stage:     StageReview,
		Harness:   "claude_code",
		Prompt:    []byte("review"),
		Sources: []Source{
			{Kind: "file", Path: "README.md", Content: []byte("Authorization: Bearer abcdefghijklmnopqrstuvwxyz"), Raw: false},
		},
		Now: fixedNow,
	})
	if !errors.Is(err, ErrSecretBlocked) {
		t.Fatalf("err = %v, want ErrSecretBlocked", err)
	}
}

func TestEvaluateRejectsDisallowedHarnessAndOversizedContext(t *testing.T) {
	t.Parallel()
	_, err := Evaluate(DefaultPolicy(), EvaluationRequest{
		ProjectID: "proj",
		Stage:     StagePlanning,
		Harness:   "direct_provider_api",
		Prompt:    []byte("x"),
		Now:       fixedNow,
	})
	if !errors.Is(err, ErrProviderDenied) {
		t.Fatalf("provider err = %v, want ErrProviderDenied", err)
	}
	policy := DefaultPolicy()
	policy.MaxContextBytes = 8
	policy.StageDefaults = nil
	_, err = Evaluate(policy, EvaluationRequest{
		ProjectID: "proj",
		Stage:     StageReview,
		Harness:   "codex_cli",
		Prompt:    []byte("123456789"),
		Now:       fixedNow,
	})
	if !errors.Is(err, ErrContextTooLarge) {
		t.Fatalf("size err = %v, want ErrContextTooLarge", err)
	}
}

func TestManifestStoreWritesScopedJSON(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	result, err := Evaluate(DefaultPolicy(), EvaluationRequest{
		ProjectID: "proj",
		Stage:     StageReview,
		Harness:   "gemini_cli",
		Prompt:    []byte("review"),
		Sources:   []Source{{Kind: "artifact", Path: "summary.md", Content: []byte("ok")}},
		Now:       fixedNow,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	ref, err := (ManifestStore{ProjectRoot: root}).Write(context.Background(), result.Manifest)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !ref.Written || !strings.HasPrefix(ref.Path, ".hoopoe/context-manifests/review/") {
		t.Fatalf("manifest ref = %+v", ref)
	}
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref.Path)))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), `"policyDigest"`) {
		t.Fatalf("manifest data = %s", data)
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
}
