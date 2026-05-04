// Package planning implements the daemon-side Phase 5 plan-generation DAG.
//
// The package owns Hoopoe-created planning artifacts only. Model execution is
// delegated to typed runners for subscription-backed CLIs or Oracle browser
// mode; this package never reaches provider APIs directly.
package planning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
)

const (
	SchemaVersion           = 1
	PipelineVersion         = "phase5.v1"
	DefaultRefinementRounds = 4
	MaxRefinementRounds     = 5
)

var (
	ErrInvalidRequest = errors.New("planning: invalid request")
	ErrStepFailed     = errors.New("planning: step failed")
)

type Harness string

const (
	HarnessOracleBrowser Harness = "oracle_browser"
	HarnessClaudeCode    Harness = "claude_code"
	HarnessCodexCLI      Harness = "codex_cli"
	HarnessGeminiCLI     Harness = "gemini_cli"
)

type StepKind string

const (
	StepRoughIdea         StepKind = "rough_idea"
	StepCandidate         StepKind = "candidate"
	StepComparativeMatrix StepKind = "comparative_matrix"
	StepSynthesis         StepKind = "synthesis"
	StepFreshEyesCritique StepKind = "fresh_eyes_critique"
	StepRefinementRound   StepKind = "refinement_round"
	StepLockReadiness     StepKind = "lock_readiness"
)

type ModelRef struct {
	Slug      string  `json:"slug"`
	Harness   Harness `json:"harness"`
	Model     string  `json:"model,omitempty"`
	AccountID string  `json:"accountId,omitempty"`
}

type RunRequest struct {
	ProjectID         string
	ProjectRoot       string
	PlanID            string
	Title             string
	RoughIdea         string
	SourceMode        string
	ProjectCommitSHA  string
	Primary           ModelRef
	Candidates        []ModelRef
	RefinementRounds  int
	Actor             string
	CorrelationID     string
	IdempotencyPrefix string
}

type Step struct {
	ID             string
	Kind           StepKind
	PromptID       string
	ArtifactPath   string
	Model          ModelRef
	Dependencies   []string
	FreshSession   bool
	RefinementTurn int
}

type Prompt struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Hash    string `json:"hash"`
	Body    string `json:"body"`
}

type PromptSource interface {
	LoadPrompt(context.Context, string) (Prompt, error)
}

type Runner interface {
	RunPlanningStep(context.Context, RunnerRequest) (RunnerResult, error)
}

type RunnerRequest struct {
	ProjectRoot   string
	PlanID        string
	Step          Step
	Prompt        Prompt
	Inputs        map[string]string
	Model         ModelRef
	FreshSession  bool
	RequestedAt   time.Time
	CorrelationID string
}

type RunnerResult struct {
	Markdown string
}

type Service struct {
	Registry      jobs.Registry
	Prompts       PromptSource
	Runner        Runner
	Now           func() time.Time
	WorkerID      string
	LeaseDuration time.Duration
}

type PipelineResult struct {
	PlanID string
	Root   string
	Steps  []StepRecord
	Meta   PlanMeta
}

type PlanMeta struct {
	SchemaVersion    int               `json:"schemaVersion"`
	PipelineVersion  string            `json:"pipelineVersion"`
	PlanID           string            `json:"planId"`
	ProjectID        string            `json:"projectId"`
	Title            string            `json:"title,omitempty"`
	SourceMode       string            `json:"sourceMode"`
	ProjectCommitSHA string            `json:"projectCommitSha,omitempty"`
	InputSHA256      string            `json:"inputSha256"`
	Primary          ModelRef          `json:"primary"`
	Candidates       []ModelRef        `json:"candidates"`
	RefinementRounds int               `json:"refinementRounds"`
	LockState        string            `json:"lockState"`
	Artifacts        map[string]string `json:"artifacts"`
	Steps            []StepRecord      `json:"steps"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}

type StepRecord struct {
	ID            string    `json:"id"`
	Kind          StepKind  `json:"kind"`
	JobID         string    `json:"jobId"`
	PromptID      string    `json:"promptId,omitempty"`
	PromptVersion int       `json:"promptVersion,omitempty"`
	PromptSHA256  string    `json:"promptSha256,omitempty"`
	ArtifactPath  string    `json:"artifactPath"`
	Model         ModelRef  `json:"model,omitempty"`
	FreshSession  bool      `json:"freshSession,omitempty"`
	Dependencies  []string  `json:"dependencies,omitempty"`
	StartedAt     time.Time `json:"startedAt"`
	CompletedAt   time.Time `json:"completedAt"`
	OutputSHA256  string    `json:"outputSha256"`
}

type historyEntry struct {
	SchemaVersion int       `json:"schemaVersion"`
	PlanID        string    `json:"planId"`
	StepID        string    `json:"stepId"`
	Kind          StepKind  `json:"kind"`
	JobID         string    `json:"jobId"`
	ArtifactPath  string    `json:"artifactPath"`
	OutputSHA256  string    `json:"outputSha256"`
	At            time.Time `json:"at"`
}

func NewService(registry jobs.Registry, prompts PromptSource, runner Runner) *Service {
	return &Service{
		Registry:      registry,
		Prompts:       prompts,
		Runner:        runner,
		Now:           time.Now,
		WorkerID:      "planning-pipeline",
		LeaseDuration: 15 * time.Minute,
	}
}

func BuildGraph(req RunRequest) ([]Step, error) {
	normalized, err := normalizeRequest(req)
	if err != nil {
		return nil, err
	}
	steps := []Step{{
		ID:           "rough_idea",
		Kind:         StepRoughIdea,
		ArtifactPath: "rough-idea.md",
	}}
	candidateIDs := make([]string, 0, len(normalized.Candidates))
	for _, candidate := range normalized.Candidates {
		id := "candidate_" + candidate.Slug
		candidateIDs = append(candidateIDs, id)
		steps = append(steps, Step{
			ID:           id,
			Kind:         StepCandidate,
			PromptID:     "candidate-draft",
			ArtifactPath: filepath.ToSlash(filepath.Join("candidates", candidate.Slug+".md")),
			Model:        candidate,
			Dependencies: []string{"rough_idea"},
		})
	}
	sort.Strings(candidateIDs)
	steps = append(steps,
		Step{
			ID:           "comparative_matrix",
			Kind:         StepComparativeMatrix,
			PromptID:     "comparative-matrix",
			ArtifactPath: "comparative-matrix.md",
			Model:        normalized.Primary,
			Dependencies: candidateIDs,
		},
		Step{
			ID:           "synthesis",
			Kind:         StepSynthesis,
			PromptID:     "synthesis-best-of-all-worlds",
			ArtifactPath: "synthesis.md",
			Model:        normalized.Primary,
			Dependencies: []string{"comparative_matrix"},
		},
		Step{
			ID:           "fresh_eyes_critique",
			Kind:         StepFreshEyesCritique,
			PromptID:     "fresh-eyes-critique",
			ArtifactPath: "fresh-eyes-critique.md",
			Model:        normalized.Primary,
			Dependencies: []string{"synthesis"},
			FreshSession: true,
		},
	)
	last := "fresh_eyes_critique"
	for i := 1; i <= normalized.RefinementRounds; i++ {
		id := fmt.Sprintf("refinement_round_%03d", i)
		steps = append(steps, Step{
			ID:             id,
			Kind:           StepRefinementRound,
			PromptID:       "refinement-round-N",
			ArtifactPath:   fmt.Sprintf("refinement-round-%03d.md", i),
			Model:          normalized.Primary,
			Dependencies:   []string{last},
			FreshSession:   true,
			RefinementTurn: i,
		})
		last = id
	}
	steps = append(steps, Step{
		ID:           "lock_readiness",
		Kind:         StepLockReadiness,
		PromptID:     "lock-readiness",
		ArtifactPath: "unresolved-decisions.md",
		Model:        normalized.Primary,
		Dependencies: []string{last},
	})
	return steps, nil
}

func (s *Service) Run(ctx context.Context, req RunRequest) (PipelineResult, error) {
	if s == nil || s.Registry == nil || s.Prompts == nil || s.Runner == nil {
		return PipelineResult{}, fmt.Errorf("%w: service dependencies are required", ErrInvalidRequest)
	}
	req, err := normalizeRequest(req)
	if err != nil {
		return PipelineResult{}, err
	}
	steps, err := BuildGraph(req)
	if err != nil {
		return PipelineResult{}, err
	}
	now := s.now().UTC()
	meta := PlanMeta{
		SchemaVersion:    SchemaVersion,
		PipelineVersion:  PipelineVersion,
		PlanID:           req.PlanID,
		ProjectID:        req.ProjectID,
		Title:            req.Title,
		SourceMode:       req.SourceMode,
		ProjectCommitSHA: req.ProjectCommitSHA,
		InputSHA256:      digestString(req.RoughIdea),
		Primary:          req.Primary,
		Candidates:       append([]ModelRef(nil), req.Candidates...),
		RefinementRounds: req.RefinementRounds,
		LockState:        "draft",
		Artifacts:        map[string]string{},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	planRoot := filepath.Join(req.ProjectRoot, ".hoopoe", "plans", req.PlanID)
	if err := os.MkdirAll(planRoot, 0o700); err != nil {
		return PipelineResult{}, err
	}
	outputs := map[string]string{}
	records := map[string]StepRecord{}
	if existing, ok, err := readPlanMeta(filepath.Join(planRoot, "meta.json")); err != nil {
		return PipelineResult{}, err
	} else if ok && existing.PlanID == req.PlanID && existing.SchemaVersion == SchemaVersion {
		if !existing.CreatedAt.IsZero() {
			meta.CreatedAt = existing.CreatedAt
		}
		meta.Artifacts = cloneStringMap(existing.Artifacts)
		records = recordsFromMeta(existing)
	}

	if err := s.runStep(ctx, req, planRoot, steps[0], outputs, records, &meta, nil); err != nil {
		return PipelineResult{}, err
	}

	candidateSteps := make([]Step, 0, len(req.Candidates))
	stepByID := make(map[string]Step, len(steps))
	for _, step := range steps {
		stepByID[step.ID] = step
		if step.Kind == StepCandidate {
			candidateSteps = append(candidateSteps, step)
		}
	}
	if err := s.runCandidateSteps(ctx, req, planRoot, candidateSteps, outputs, records, &meta); err != nil {
		return PipelineResult{}, err
	}
	for _, id := range []string{"comparative_matrix", "synthesis", "fresh_eyes_critique"} {
		if err := s.runStep(ctx, req, planRoot, stepByID[id], outputs, records, &meta, nil); err != nil {
			return PipelineResult{}, err
		}
	}
	for i := 1; i <= req.RefinementRounds; i++ {
		id := fmt.Sprintf("refinement_round_%03d", i)
		if err := s.runStep(ctx, req, planRoot, stepByID[id], outputs, records, &meta, nil); err != nil {
			return PipelineResult{}, err
		}
	}
	if err := s.runStep(ctx, req, planRoot, stepByID["lock_readiness"], outputs, records, &meta, nil); err != nil {
		return PipelineResult{}, err
	}

	ordered := make([]StepRecord, 0, len(steps))
	for _, step := range steps {
		ordered = append(ordered, records[step.ID])
	}
	meta.Steps = ordered
	meta.UpdatedAt = s.now().UTC()
	if err := writeJSONFile(filepath.Join(planRoot, "meta.json"), meta); err != nil {
		return PipelineResult{}, err
	}
	return PipelineResult{PlanID: req.PlanID, Root: planRoot, Steps: ordered, Meta: meta}, nil
}

func (s *Service) runCandidateSteps(ctx context.Context, req RunRequest, planRoot string, steps []Step, outputs map[string]string, records map[string]StepRecord, meta *PlanMeta) error {
	var mu sync.Mutex
	var metaMu sync.Mutex
	var wg sync.WaitGroup
	errs := make(chan error, len(steps))
	baseOutputs, baseRecords := snapshotExecutionState(outputs, records)
	for _, step := range steps {
		step := step
		wg.Add(1)
		go func() {
			defer wg.Done()
			localOutputs, localRecords := snapshotExecutionState(baseOutputs, baseRecords)
			if err := s.runStep(ctx, req, planRoot, step, localOutputs, localRecords, meta, &metaMu); err != nil {
				errs <- err
				return
			}
			mu.Lock()
			outputs[step.ID] = localOutputs[step.ID]
			records[step.ID] = localRecords[step.ID]
			mu.Unlock()
		}()
	}
	wg.Wait()
	close(errs)
	var joined error
	for err := range errs {
		joined = errors.Join(joined, err)
	}
	return joined
}

func snapshotExecutionState(outputs map[string]string, records map[string]StepRecord) (map[string]string, map[string]StepRecord) {
	outputCopy := make(map[string]string, len(outputs)+1)
	for k, v := range outputs {
		outputCopy[k] = v
	}
	recordCopy := make(map[string]StepRecord, len(records)+1)
	for k, v := range records {
		recordCopy[k] = v
	}
	return outputCopy, recordCopy
}

func (s *Service) runStep(ctx context.Context, req RunRequest, planRoot string, step Step, outputs map[string]string, records map[string]StepRecord, meta *PlanMeta, metaMu *sync.Mutex) error {
	for _, dep := range step.Dependencies {
		if _, ok := outputs[dep]; !ok {
			return fmt.Errorf("%w: step %s missing dependency %s", ErrInvalidRequest, step.ID, dep)
		}
	}
	jobID := jobIDForStep(req.PlanID, step.ID)
	job, err := s.ensureJob(ctx, req, jobID, step)
	if err != nil {
		return err
	}
	if job.Status == jobs.StatusSucceeded {
		return s.restoreCompletedStep(planRoot, step, outputs, records, meta, metaMu)
	}
	holder := s.workerID()
	if _, err := s.Registry.Lease(ctx, jobs.LeaseRequest{JobID: job.ID, Holder: holder, Duration: s.leaseDuration()}); err != nil {
		return fmt.Errorf("%w: lease %s: %w", ErrStepFailed, step.ID, err)
	}
	started := s.now().UTC()

	var prompt Prompt
	var markdown string
	if step.Kind == StepRoughIdea {
		markdown = strings.TrimSpace(req.RoughIdea) + "\n"
	} else {
		prompt, err = s.Prompts.LoadPrompt(ctx, step.PromptID)
		if err != nil {
			_ = s.failJob(ctx, job.ID, holder, step, err)
			return err
		}
		result, err := s.Runner.RunPlanningStep(ctx, RunnerRequest{
			ProjectRoot:   req.ProjectRoot,
			PlanID:        req.PlanID,
			Step:          step,
			Prompt:        prompt,
			Inputs:        stepInputs(step, outputs),
			Model:         step.Model,
			FreshSession:  step.FreshSession,
			RequestedAt:   started,
			CorrelationID: req.CorrelationID,
		})
		if err != nil {
			_ = s.failJob(ctx, job.ID, holder, step, err)
			return fmt.Errorf("%w: %s: %w", ErrStepFailed, step.ID, err)
		}
		markdown = strings.TrimSpace(result.Markdown)
		if markdown == "" {
			err := fmt.Errorf("%w: %s produced empty markdown", ErrStepFailed, step.ID)
			_ = s.failJob(ctx, job.ID, holder, step, err)
			return err
		}
		markdown += "\n"
	}

	rel := filepath.ToSlash(step.ArtifactPath)
	abs := filepath.Join(planRoot, filepath.FromSlash(rel))
	if err := writeTextFile(abs, markdown); err != nil {
		_ = s.failJob(ctx, job.ID, holder, step, err)
		return err
	}
	sha := digestString(markdown)
	artifact := jobs.Artifact{
		ID:   step.ID + ".artifact",
		Kind: "plan_artifact",
		URI:  filepath.ToSlash(filepath.Join(".hoopoe", "plans", req.PlanID, rel)),
	}
	if _, err := s.Registry.AddArtifact(ctx, job.ID, artifact); err != nil {
		_ = s.failJob(ctx, job.ID, holder, step, err)
		return err
	}
	if _, err := s.Registry.AppendLog(ctx, job.ID, []byte("wrote "+rel+"\n")); err != nil {
		_ = s.failJob(ctx, job.ID, holder, step, err)
		return err
	}
	if _, err := s.Registry.Complete(ctx, jobs.CompleteRequest{JobID: job.ID, Holder: holder, Audit: jobs.AuditMetadata{Actor: req.Actor, CorrelationID: req.CorrelationID}}); err != nil {
		return err
	}
	completed := s.now().UTC()
	record := StepRecord{
		ID:            step.ID,
		Kind:          step.Kind,
		JobID:         job.ID,
		PromptID:      prompt.ID,
		PromptVersion: prompt.Version,
		PromptSHA256:  prompt.Hash,
		ArtifactPath:  rel,
		Model:         step.Model,
		FreshSession:  step.FreshSession,
		Dependencies:  append([]string(nil), step.Dependencies...),
		StartedAt:     started,
		CompletedAt:   completed,
		OutputSHA256:  "sha256:" + sha,
	}
	if metaMu != nil {
		metaMu.Lock()
		defer metaMu.Unlock()
	}
	records[step.ID] = record
	outputs[step.ID] = markdown
	meta.Artifacts[step.ID] = rel
	meta.UpdatedAt = completed
	if err := appendHistory(filepath.Join(planRoot, "history.jsonl"), historyEntry{
		SchemaVersion: SchemaVersion,
		PlanID:        req.PlanID,
		StepID:        step.ID,
		Kind:          step.Kind,
		JobID:         job.ID,
		ArtifactPath:  rel,
		OutputSHA256:  "sha256:" + sha,
		At:            completed,
	}); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(planRoot, "meta.json"), *meta)
}

func (s *Service) restoreCompletedStep(planRoot string, step Step, outputs map[string]string, records map[string]StepRecord, meta *PlanMeta, metaMu *sync.Mutex) error {
	rel := filepath.ToSlash(step.ArtifactPath)
	abs := filepath.Join(planRoot, filepath.FromSlash(rel))
	data, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("%w: restore completed step %s artifact: %w", ErrStepFailed, step.ID, err)
	}
	outputs[step.ID] = string(data)
	record, ok := records[step.ID]
	if !ok {
		now := s.now().UTC()
		record = StepRecord{
			ID:           step.ID,
			Kind:         step.Kind,
			JobID:        jobIDForStep(meta.PlanID, step.ID),
			ArtifactPath: rel,
			Model:        step.Model,
			FreshSession: step.FreshSession,
			Dependencies: append([]string(nil), step.Dependencies...),
			StartedAt:    now,
			CompletedAt:  now,
			OutputSHA256: "sha256:" + digestString(string(data)),
		}
		records[step.ID] = record
	}
	if metaMu != nil {
		metaMu.Lock()
		defer metaMu.Unlock()
	}
	meta.Artifacts[step.ID] = rel
	if record.CompletedAt.After(meta.UpdatedAt) {
		meta.UpdatedAt = record.CompletedAt
	}
	return nil
}

func (s *Service) ensureJob(ctx context.Context, req RunRequest, jobID string, step Step) (jobs.Job, error) {
	if job, err := s.Registry.Get(ctx, jobID); err == nil {
		return job, nil
	} else if !errors.Is(err, jobs.ErrNotFound) {
		return jobs.Job{}, err
	}
	idempotency := req.IdempotencyPrefix
	if idempotency == "" {
		idempotency = req.PlanID
	}
	return s.Registry.Create(ctx, jobs.CreateRequest{
		ID:             jobID,
		Kind:           "planning." + string(step.Kind),
		SchemaVersion:  jobs.SchemaVersion,
		CorrelationID:  req.CorrelationID,
		IdempotencyKey: idempotency + ":" + step.ID,
		Audit: jobs.AuditMetadata{
			Actor:         req.Actor,
			Reason:        "phase5 planning pipeline step",
			CorrelationID: req.CorrelationID,
		},
	})
}

func (s *Service) failJob(ctx context.Context, jobID string, holder string, step Step, err error) error {
	_, failErr := s.Registry.Fail(ctx, jobs.FailRequest{
		JobID:  jobID,
		Holder: holder,
		Failure: jobs.Failure{
			Code:               "planning." + string(step.Kind) + ".failed",
			Message:            err.Error(),
			FailureFingerprint: digestString(step.ID + ":" + err.Error()),
		},
	})
	return failErr
}

func stepInputs(step Step, outputs map[string]string) map[string]string {
	out := make(map[string]string, len(outputs)+1)
	for _, dep := range step.Dependencies {
		out[dep] = outputs[dep]
	}
	if step.Kind == StepComparativeMatrix || step.Kind == StepSynthesis {
		keys := make([]string, 0, len(outputs))
		for key := range outputs {
			if strings.HasPrefix(key, "candidate_") {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		var joined strings.Builder
		for _, key := range keys {
			joined.WriteString("## ")
			joined.WriteString(strings.TrimPrefix(key, "candidate_"))
			joined.WriteString("\n")
			joined.WriteString(outputs[key])
			joined.WriteString("\n")
		}
		out["candidates"] = joined.String()
	}
	if step.Kind == StepRefinementRound {
		out["round"] = fmt.Sprintf("%d", step.RefinementTurn)
	}
	return out
}

func normalizeRequest(req RunRequest) (RunRequest, error) {
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.ProjectRoot = strings.TrimSpace(req.ProjectRoot)
	req.PlanID = sanitizeSegment(req.PlanID)
	req.Title = strings.TrimSpace(req.Title)
	req.RoughIdea = strings.TrimSpace(req.RoughIdea)
	req.SourceMode = strings.TrimSpace(req.SourceMode)
	req.ProjectCommitSHA = strings.TrimSpace(req.ProjectCommitSHA)
	req.Actor = strings.TrimSpace(req.Actor)
	req.CorrelationID = strings.TrimSpace(req.CorrelationID)
	req.IdempotencyPrefix = strings.TrimSpace(req.IdempotencyPrefix)
	if req.ProjectID == "" || req.ProjectRoot == "" || req.PlanID == "" || req.RoughIdea == "" {
		return RunRequest{}, fmt.Errorf("%w: projectId, projectRoot, planId, and roughIdea are required", ErrInvalidRequest)
	}
	if req.SourceMode == "" {
		req.SourceMode = "chat-box"
	}
	if req.Actor == "" {
		req.Actor = "daemon"
	}
	if req.RefinementRounds == 0 {
		req.RefinementRounds = DefaultRefinementRounds
	}
	if req.RefinementRounds < 0 || req.RefinementRounds > MaxRefinementRounds {
		return RunRequest{}, fmt.Errorf("%w: refinement rounds must be 0-%d", ErrInvalidRequest, MaxRefinementRounds)
	}
	req.Primary = normalizeModel(req.Primary)
	if req.Primary.Slug == "" || req.Primary.Harness == "" {
		return RunRequest{}, fmt.Errorf("%w: primary model slug and harness are required", ErrInvalidRequest)
	}
	if len(req.Candidates) == 0 {
		req.Candidates = []ModelRef{req.Primary}
	}
	if len(req.Candidates) > 4 {
		return RunRequest{}, fmt.Errorf("%w: candidates must be 1-4", ErrInvalidRequest)
	}
	seen := map[string]bool{}
	for i := range req.Candidates {
		req.Candidates[i] = normalizeModel(req.Candidates[i])
		if req.Candidates[i].Slug == "" || req.Candidates[i].Harness == "" {
			return RunRequest{}, fmt.Errorf("%w: candidate %d model slug and harness are required", ErrInvalidRequest, i)
		}
		if seen[req.Candidates[i].Slug] {
			return RunRequest{}, fmt.Errorf("%w: duplicate candidate slug %q", ErrInvalidRequest, req.Candidates[i].Slug)
		}
		seen[req.Candidates[i].Slug] = true
	}
	return req, nil
}

func normalizeModel(model ModelRef) ModelRef {
	model.Slug = sanitizeSegment(model.Slug)
	model.Model = strings.TrimSpace(model.Model)
	model.AccountID = strings.TrimSpace(model.AccountID)
	return model
}

func sanitizeSegment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '.' || r == ' ':
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-.")
}

func jobIDForStep(planID string, stepID string) string {
	base := "planning." + sanitizeSegment(planID) + "." + sanitizeSegment(stepID)
	if len(base) <= 128 {
		return base
	}
	return base[:95] + "." + digestString(base)[:32]
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now()
	}
	return s.Now()
}

func (s *Service) workerID() string {
	if strings.TrimSpace(s.WorkerID) == "" {
		return "planning-pipeline"
	}
	return strings.TrimSpace(s.WorkerID)
}

func (s *Service) leaseDuration() time.Duration {
	if s.LeaseDuration <= 0 {
		return 15 * time.Minute
	}
	return s.LeaseDuration
}

func writeTextFile(path string, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(value), 0o600)
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(temp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

func readPlanMeta(path string) (PlanMeta, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return PlanMeta{}, false, nil
	}
	if err != nil {
		return PlanMeta{}, false, err
	}
	var meta PlanMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return PlanMeta{}, false, err
	}
	return meta, true, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func recordsFromMeta(meta PlanMeta) map[string]StepRecord {
	out := make(map[string]StepRecord, len(meta.Steps))
	for _, record := range meta.Steps {
		if record.ID != "" {
			out[record.ID] = record
		}
	}
	return out
}

func appendHistory(path string, entry historyEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
