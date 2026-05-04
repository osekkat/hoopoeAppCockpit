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
	Quality          *QualityReport    `json:"quality,omitempty"`
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

type QualityDimension string

const (
	QualityIntentClarity    QualityDimension = "intent_clarity"
	QualityArchitecture     QualityDimension = "architecture_specificity"
	QualityWorkflowCoverage QualityDimension = "workflow_coverage"
	QualityImplementation   QualityDimension = "implementation_detail"
	QualityTesting          QualityDimension = "testing_specificity"
	QualityRisk             QualityDimension = "risk_coverage"
	QualityBeadReadiness    QualityDimension = "bead_readiness"
	qualityArtifactPath                      = "quality.json"
)

type QualityReport struct {
	SchemaVersion   int                     `json:"schemaVersion"`
	PlanID          string                  `json:"planId"`
	PipelineVersion string                  `json:"pipelineVersion"`
	SourceStepID    string                  `json:"sourceStepId,omitempty"`
	GeneratedAt     time.Time               `json:"generatedAt"`
	OverallScore    int                     `json:"overallScore"`
	Advisory        bool                    `json:"advisory"`
	Guidance        string                  `json:"guidance"`
	Dimensions      []QualityDimensionScore `json:"dimensions"`
}

type QualityDimensionScore struct {
	Dimension QualityDimension  `json:"dimension"`
	Label     string            `json:"label"`
	Score     int               `json:"score"`
	Delta     *int              `json:"delta,omitempty"`
	Evidence  []QualityEvidence `json:"evidence"`
	Guidance  string            `json:"guidance"`
}

type QualityEvidence struct {
	Kind     string `json:"kind"`
	Detail   string `json:"detail"`
	Matched  bool   `json:"matched"`
	Weight   int    `json:"weight"`
	Location string `json:"location,omitempty"`
}

type QualityRequest struct {
	PlanID       string
	SourceStepID string
	Markdown     string
	GeneratedAt  time.Time
	Previous     *QualityReport
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
		meta.Quality = existing.Quality
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

	quality, err := EvaluatePlanQuality(QualityRequest{
		PlanID:       req.PlanID,
		SourceStepID: "lock_readiness",
		Markdown:     qualityInput(outputs),
		GeneratedAt:  s.now().UTC(),
		Previous:     meta.Quality,
	})
	if err != nil {
		return PipelineResult{}, err
	}
	if err := writeJSONFile(filepath.Join(planRoot, qualityArtifactPath), quality); err != nil {
		return PipelineResult{}, err
	}
	meta.Quality = &quality
	meta.Artifacts["quality"] = qualityArtifactPath

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

func EvaluatePlanQuality(req QualityRequest) (QualityReport, error) {
	req.PlanID = sanitizeSegment(req.PlanID)
	req.SourceStepID = sanitizeSegment(req.SourceStepID)
	req.Markdown = strings.TrimSpace(req.Markdown)
	if req.PlanID == "" || req.Markdown == "" {
		return QualityReport{}, fmt.Errorf("%w: planId and markdown are required", ErrInvalidRequest)
	}
	generatedAt := req.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	ctx := qualityContext(req.Markdown)
	scores := make([]QualityDimensionScore, 0, len(qualitySpecs()))
	for _, spec := range qualitySpecs() {
		score := spec.score(ctx)
		if req.Previous != nil {
			if previous, ok := req.Previous.dimensionScore(score.Dimension); ok {
				delta := score.Score - previous.Score
				score.Delta = &delta
			}
		}
		scores = append(scores, score)
	}
	total := 0
	for _, score := range scores {
		total += score.Score
	}
	overall := 0
	if len(scores) > 0 {
		overall = total / len(scores)
	}
	return QualityReport{
		SchemaVersion:   SchemaVersion,
		PlanID:          req.PlanID,
		PipelineVersion: PipelineVersion,
		SourceStepID:    req.SourceStepID,
		GeneratedAt:     generatedAt.UTC(),
		OverallScore:    overall,
		Advisory:        true,
		Guidance:        "Plan quality scores are advisory decision aids; inspect the evidence before locking or converting to beads.",
		Dimensions:      scores,
	}, nil
}

func (r QualityReport) dimensionScore(dimension QualityDimension) (QualityDimensionScore, bool) {
	for _, score := range r.Dimensions {
		if score.Dimension == dimension {
			return score, true
		}
	}
	return QualityDimensionScore{}, false
}

type qualitySpec struct {
	dimension QualityDimension
	label     string
	guidance  string
	checks    []qualityCheck
}

type qualityCheck struct {
	kind     string
	detail   string
	weight   int
	location string
	matches  func(qualityDoc) bool
}

type qualityDoc struct {
	raw         string
	lower       string
	headings    []string
	bullets     int
	numbered    int
	codeBlocks  int
	words       int
	fileRefs    int
	commandRefs int
}

func (s qualitySpec) score(doc qualityDoc) QualityDimensionScore {
	evidence := make([]QualityEvidence, 0, len(s.checks))
	totalWeight := 0
	matchedWeight := 0
	for _, check := range s.checks {
		totalWeight += check.weight
		matched := check.matches(doc)
		if matched {
			matchedWeight += check.weight
		}
		evidence = append(evidence, QualityEvidence{
			Kind:     check.kind,
			Detail:   check.detail,
			Matched:  matched,
			Weight:   check.weight,
			Location: check.location,
		})
	}
	score := 0
	if totalWeight > 0 {
		score = matchedWeight * 100 / totalWeight
	}
	return QualityDimensionScore{
		Dimension: s.dimension,
		Label:     s.label,
		Score:     score,
		Evidence:  evidence,
		Guidance:  s.guidance,
	}
}

func qualitySpecs() []qualitySpec {
	return []qualitySpec{
		{
			dimension: QualityIntentClarity,
			label:     "Intent clarity",
			guidance:  "Clarify the user goal, success criteria, and expected outcome before locking.",
			checks: []qualityCheck{
				{kind: "length", detail: "plan has enough substance to evaluate intent", weight: 15, matches: func(d qualityDoc) bool { return d.words >= 80 }},
				{kind: "heading", detail: "goal or objective section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "goal", "objective", "intent", "problem") }},
				{kind: "keyword", detail: "success or acceptance language is present", weight: 30, matches: func(d qualityDoc) bool { return containsAny(d.lower, "success", "acceptance", "outcome", "done when") }},
				{kind: "structure", detail: "plan uses bullets or numbered steps", weight: 25, matches: func(d qualityDoc) bool { return d.bullets+d.numbered >= 3 }},
			},
		},
		{
			dimension: QualityArchitecture,
			label:     "Architecture specificity",
			guidance:  "Name components, boundaries, APIs, storage, and data flow explicitly.",
			checks: []qualityCheck{
				{kind: "heading", detail: "architecture or design section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "architecture", "design", "system", "component") }},
				{kind: "keyword", detail: "component or boundary terms are present", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "component", "boundary", "module", "service", "daemon", "desktop")
				}},
				{kind: "keyword", detail: "data flow, API, schema, or storage terms are present", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "api", "schema", "database", "sqlite", "event", "data flow")
				}},
				{kind: "artifact", detail: "file/module references or code blocks anchor the architecture", weight: 20, matches: func(d qualityDoc) bool { return d.fileRefs > 0 || d.codeBlocks > 0 }},
			},
		},
		{
			dimension: QualityWorkflowCoverage,
			label:     "Workflow coverage",
			guidance:  "Cover happy paths, edge cases, failure paths, and user-visible state transitions.",
			checks: []qualityCheck{
				{kind: "heading", detail: "workflow or user journey section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "workflow", "journey", "flow", "scenario") }},
				{kind: "keyword", detail: "happy-path language is present", weight: 20, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "happy path", "primary flow", "user can", "when the user")
				}},
				{kind: "keyword", detail: "edge or failure cases are named", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "edge case", "failure", "fallback", "error", "offline", "retry")
				}},
				{kind: "structure", detail: "multiple ordered or bulleted workflow steps are present", weight: 25, matches: func(d qualityDoc) bool { return d.bullets+d.numbered >= 5 }},
			},
		},
		{
			dimension: QualityImplementation,
			label:     "Implementation detail",
			guidance:  "Specify files, commands, typed interfaces, and execution order.",
			checks: []qualityCheck{
				{kind: "heading", detail: "implementation section is present", weight: 25, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "implementation", "engineering", "tasks", "plan") }},
				{kind: "artifact", detail: "file or package references are present", weight: 25, matches: func(d qualityDoc) bool { return d.fileRefs >= 2 }},
				{kind: "artifact", detail: "commands or fenced code blocks are present", weight: 25, matches: func(d qualityDoc) bool { return d.commandRefs > 0 || d.codeBlocks > 0 }},
				{kind: "keyword", detail: "concrete technology or interface terms are present", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "endpoint", "rpc", "interface", "struct", "component", "test harness")
				}},
			},
		},
		{
			dimension: QualityTesting,
			label:     "Testing specificity",
			guidance:  "Name unit, integration, fixture, and verification commands with expected evidence.",
			checks: []qualityCheck{
				{kind: "heading", detail: "testing or verification section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "test", "testing", "verification", "qa") }},
				{kind: "keyword", detail: "test levels are named", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "unit test", "integration", "e2e", "fixture", "golden")
				}},
				{kind: "artifact", detail: "verification commands are present", weight: 25, matches: func(d qualityDoc) bool {
					return d.commandRefs > 0 && containsAny(d.lower, "go test", "bun run", "rch exec", "playwright")
				}},
				{kind: "keyword", detail: "evidence or acceptance requirements are present", weight: 20, matches: func(d qualityDoc) bool { return containsAny(d.lower, "evidence", "assert", "coverage", "regression") }},
			},
		},
		{
			dimension: QualityRisk,
			label:     "Risk coverage",
			guidance:  "Surface risks, mitigations, rollback paths, and high-stakes failure modes.",
			checks: []qualityCheck{
				{kind: "heading", detail: "risk or mitigation section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "risk", "mitigation", "failure", "rollback") }},
				{kind: "keyword", detail: "security or privacy risks are named", weight: 20, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "security", "privacy", "secret", "redaction", "credential")
				}},
				{kind: "keyword", detail: "operational failure modes are named", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "rollback", "retry", "timeout", "rate limit", "offline", "crash")
				}},
				{kind: "keyword", detail: "mitigation language is present", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "mitigate", "fallback", "guard", "recover", "verify before")
				}},
			},
		},
		{
			dimension: QualityBeadReadiness,
			label:     "Bead readiness",
			guidance:  "Break work into traceable tasks with dependencies, acceptance criteria, and verification.",
			checks: []qualityCheck{
				{kind: "heading", detail: "bead, task, or acceptance section is present", weight: 30, location: "headings", matches: func(d qualityDoc) bool { return headingContains(d, "bead", "task", "acceptance", "definition of done") }},
				{kind: "keyword", detail: "dependency or sequencing language is present", weight: 20, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "depends", "dependency", "blocked", "unblocks", "sequence")
				}},
				{kind: "structure", detail: "enough discrete bullets exist to convert into beads", weight: 25, matches: func(d qualityDoc) bool { return d.bullets+d.numbered >= 7 }},
				{kind: "keyword", detail: "verification and acceptance criteria are explicit", weight: 25, matches: func(d qualityDoc) bool {
					return containsAny(d.lower, "acceptance criteria", "definition of done", "verify", "test evidence")
				}},
			},
		},
	}
}

func qualityInput(outputs map[string]string) string {
	keys := make([]string, 0, len(outputs))
	for key := range outputs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString("# ")
		b.WriteString(key)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(outputs[key]))
		b.WriteString("\n\n")
	}
	return b.String()
}

func qualityContext(markdown string) qualityDoc {
	doc := qualityDoc{
		raw:   markdown,
		lower: strings.ToLower(markdown),
	}
	inCode := false
	for _, line := range strings.Split(markdown, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			doc.codeBlocks++
			inCode = !inCode
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			doc.headings = append(doc.headings, strings.ToLower(strings.TrimSpace(strings.TrimLeft(trimmed, "#"))))
		}
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			doc.bullets++
		}
		if isNumberedLine(trimmed) {
			doc.numbered++
		}
		if inCode || strings.HasPrefix(trimmed, "$ ") || strings.HasPrefix(trimmed, "go test ") || strings.HasPrefix(trimmed, "rch exec ") || strings.HasPrefix(trimmed, "bun run ") {
			doc.commandRefs++
		}
		for _, field := range strings.Fields(trimmed) {
			if strings.Contains(field, "/") || strings.HasSuffix(field, ".go") || strings.HasSuffix(field, ".ts") || strings.HasSuffix(field, ".tsx") || strings.HasSuffix(field, ".md") || strings.HasSuffix(field, ".json") {
				doc.fileRefs++
			}
		}
	}
	doc.codeBlocks /= 2
	doc.words = len(strings.Fields(markdown))
	return doc
}

func headingContains(doc qualityDoc, needles ...string) bool {
	for _, heading := range doc.headings {
		if containsAny(heading, needles...) {
			return true
		}
	}
	return false
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func isNumberedLine(line string) bool {
	if line == "" || line[0] < '0' || line[0] > '9' {
		return false
	}
	for i := 1; i < len(line); i++ {
		if line[i] >= '0' && line[i] <= '9' {
			continue
		}
		return (line[i] == '.' || line[i] == ')') && i+1 < len(line) && line[i+1] == ' '
	}
	return false
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
