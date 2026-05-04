package planning

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
)

func TestBuildGraphModelsPhaseFiveDependencies(t *testing.T) {
	req := sampleRunRequest(t.TempDir())
	steps, err := BuildGraph(req)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	if len(steps) != 10 {
		t.Fatalf("steps = %d, want 10", len(steps))
	}
	byID := map[string]Step{}
	for _, step := range steps {
		byID[step.ID] = step
	}
	if got := byID["candidate_chatgpt"].Dependencies; len(got) != 1 || got[0] != "rough_idea" {
		t.Fatalf("candidate deps = %v", got)
	}
	matrixDeps := strings.Join(byID["comparative_matrix"].Dependencies, ",")
	if matrixDeps != "candidate_chatgpt,candidate_claude,candidate_gemini" {
		t.Fatalf("matrix deps = %s", matrixDeps)
	}
	if !byID["fresh_eyes_critique"].FreshSession || !byID["refinement_round_001"].FreshSession {
		t.Fatalf("fresh-session flags missing: critique=%v refinement=%v", byID["fresh_eyes_critique"].FreshSession, byID["refinement_round_001"].FreshSession)
	}
	if byID["lock_readiness"].ArtifactPath != "unresolved-decisions.md" {
		t.Fatalf("lock readiness artifact = %s", byID["lock_readiness"].ArtifactPath)
	}
}

func TestServiceRunPersistsArtifactsAndCompletesJobs(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	reg := newRegistry(t)
	runner := &recordingRunner{}
	svc := NewService(reg, testPrompts(), runner)
	svc.Now = fixedNow("2026-05-04T03:00:00Z")

	result, err := svc.Run(ctx, sampleRunRequest(projectRoot))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Steps) != 10 {
		t.Fatalf("result steps = %d, want 10", len(result.Steps))
	}
	for _, rel := range []string{
		"rough-idea.md",
		"candidates/chatgpt.md",
		"candidates/claude.md",
		"candidates/gemini.md",
		"comparative-matrix.md",
		"synthesis.md",
		"fresh-eyes-critique.md",
		"refinement-round-001.md",
		"refinement-round-002.md",
		"unresolved-decisions.md",
		"meta.json",
		"history.jsonl",
	} {
		if _, err := os.Stat(filepath.Join(result.Root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("missing artifact %s: %v", rel, err)
		}
	}
	jobsList, err := reg.List(ctx, jobs.ListFilter{Kind: "planning.candidate"})
	if err != nil {
		t.Fatalf("list candidate jobs: %v", err)
	}
	if len(jobsList) != 3 {
		t.Fatalf("candidate jobs = %d, want 3", len(jobsList))
	}
	for _, job := range jobsList {
		if job.Status != jobs.StatusSucceeded {
			t.Fatalf("job %s status = %s", job.ID, job.Status)
		}
		if len(job.Artifacts) != 1 || job.Artifacts[0].Kind != "plan_artifact" {
			t.Fatalf("job artifacts = %+v", job.Artifacts)
		}
	}
	if result.Meta.InputSHA256 == "" || result.Meta.LockState != "draft" {
		t.Fatalf("unexpected meta: %+v", result.Meta)
	}
	if runner.callCount("candidate-draft") != 3 {
		t.Fatalf("candidate prompt calls = %d, want 3", runner.callCount("candidate-draft"))
	}
}

func TestServiceMarksFailedStepAndStopsDownstream(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	reg := newRegistry(t)
	runner := &recordingRunner{failPrompt: "synthesis-best-of-all-worlds"}
	svc := NewService(reg, testPrompts(), runner)

	_, err := svc.Run(ctx, sampleRunRequest(projectRoot))
	if !errors.Is(err, ErrStepFailed) {
		t.Fatalf("Run error = %v, want ErrStepFailed", err)
	}
	failed, err := reg.Get(ctx, "planning.plan-123.synthesis")
	if err != nil {
		t.Fatalf("get failed job: %v", err)
	}
	if failed.Status != jobs.StatusFailed || failed.Failure == nil {
		t.Fatalf("failed job = %+v", failed)
	}
	if _, err := reg.Get(ctx, "planning.plan-123.fresh_eyes_critique"); !errors.Is(err, jobs.ErrNotFound) {
		t.Fatalf("fresh-eyes job should not exist after synthesis failure, got %v", err)
	}
}

func TestServiceResumesFromCompletedJobsWithoutRerunningModels(t *testing.T) {
	ctx := context.Background()
	projectRoot := t.TempDir()
	reg := newRegistry(t)
	runner := &recordingRunner{}
	svc := NewService(reg, testPrompts(), runner)
	req := sampleRunRequest(projectRoot)

	if _, err := svc.Run(ctx, req); err != nil {
		t.Fatalf("first Run: %v", err)
	}
	firstCandidateCalls := runner.callCount("candidate-draft")
	if firstCandidateCalls != 3 {
		t.Fatalf("first candidate calls = %d, want 3", firstCandidateCalls)
	}

	if _, err := svc.Run(ctx, req); err != nil {
		t.Fatalf("resume Run: %v", err)
	}
	if got := runner.callCount("candidate-draft"); got != firstCandidateCalls {
		t.Fatalf("resume reran candidate models: got %d calls, want %d", got, firstCandidateCalls)
	}
	if got := runner.callCount("synthesis-best-of-all-worlds"); got != 1 {
		t.Fatalf("resume reran synthesis: got %d calls, want 1", got)
	}
}

func TestFilePromptSourceLoadsAndVerifiesManifest(t *testing.T) {
	root := t.TempDir()
	promptDir := filepath.Join(root, "packages", "planning-prompts")
	if err := os.MkdirAll(filepath.Join(promptDir, "prompts"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("Prompt body\n")
	hash := "sha256:" + digestBytes(body)
	if err := os.WriteFile(filepath.Join(promptDir, "prompts", "candidate.md"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := `{"schemaVersion":1,"prompts":[{"id":"candidate-draft","version":7,"path":"prompts/candidate.md","hash":"` + hash + `","owner":"planning-pipeline","appliesToPipelineVersions":["phase5.v1"]}]}`
	if err := os.WriteFile(filepath.Join(promptDir, "manifest.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	prompt, err := (&FilePromptSource{Root: root}).LoadPrompt(context.Background(), "candidate-draft")
	if err != nil {
		t.Fatalf("LoadPrompt: %v", err)
	}
	if prompt.Version != 7 || prompt.Hash != hash || prompt.Body != string(body) {
		t.Fatalf("prompt = %+v", prompt)
	}
}

func sampleRunRequest(projectRoot string) RunRequest {
	return RunRequest{
		ProjectID:        "proj_01",
		ProjectRoot:      projectRoot,
		PlanID:           "plan-123",
		Title:            "Demo Plan",
		RoughIdea:        "Build a useful Hoopoe planning pipeline.",
		ProjectCommitSHA: "abc123",
		Primary: ModelRef{
			Slug:    "chatgpt",
			Harness: HarnessOracleBrowser,
			Model:   "gpt-5.4-pro",
		},
		Candidates: []ModelRef{
			{Slug: "chatgpt", Harness: HarnessOracleBrowser, Model: "gpt-5.4-pro"},
			{Slug: "claude", Harness: HarnessClaudeCode, Model: "opus"},
			{Slug: "gemini", Harness: HarnessGeminiCLI, Model: "gemini-3-pro"},
		},
		RefinementRounds: 2,
		Actor:            "test",
		CorrelationID:    "corr-123",
	}
}

func testPrompts() PromptSource {
	return MapPromptSource{
		"candidate-draft":              {ID: "candidate-draft", Version: 1, Hash: "sha256:candidate", Body: "candidate prompt"},
		"comparative-matrix":           {ID: "comparative-matrix", Version: 1, Hash: "sha256:matrix", Body: "matrix prompt"},
		"synthesis-best-of-all-worlds": {ID: "synthesis-best-of-all-worlds", Version: 1, Hash: "sha256:synthesis", Body: "synthesis prompt"},
		"fresh-eyes-critique":          {ID: "fresh-eyes-critique", Version: 1, Hash: "sha256:critique", Body: "critique prompt"},
		"refinement-round-N":           {ID: "refinement-round-N", Version: 1, Hash: "sha256:refine", Body: "refine prompt"},
		"lock-readiness":               {ID: "lock-readiness", Version: 1, Hash: "sha256:lock", Body: "lock prompt"},
	}
}

func newRegistry(t *testing.T) *jobs.FileRegistry {
	t.Helper()
	reg, err := jobs.NewFileRegistry(context.Background(), jobs.FileStore{Path: filepath.Join(t.TempDir(), "jobs.json")}, filepath.Join(t.TempDir(), "logs"))
	if err != nil {
		t.Fatalf("new registry: %v", err)
	}
	return reg
}

type recordingRunner struct {
	mu         sync.Mutex
	promptIDs  []string
	failPrompt string
}

func (r *recordingRunner) RunPlanningStep(_ context.Context, req RunnerRequest) (RunnerResult, error) {
	r.mu.Lock()
	r.promptIDs = append(r.promptIDs, req.Prompt.ID)
	r.mu.Unlock()
	if req.Prompt.ID == r.failPrompt {
		return RunnerResult{}, errors.New("synthetic model failure")
	}
	return RunnerResult{Markdown: "# " + req.Step.ID + "\nmodel: " + req.Model.Slug + "\n"}, nil
}

func (r *recordingRunner) callCount(promptID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int
	for _, got := range r.promptIDs {
		if got == promptID {
			count++
		}
	}
	return count
}

func fixedNow(raw string) func() time.Time {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return parsed }
}
