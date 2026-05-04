package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/modelcontext"
)

var (
	ErrInvalidRequest       = errors.New("cli: invalid request")
	ErrUnsupportedModel     = errors.New("cli: unsupported model")
	ErrUnsupportedOption    = errors.New("cli: unsupported option")
	ErrMissingAccountRouter = errors.New("cli: account router required")
	ErrCommandFailed        = errors.New("cli: command failed")
)

type AccountRouter interface {
	EnvForAccount(ctx context.Context, harness Harness, accountID string) ([]string, error)
}

type AuditSink interface {
	RecordModelCall(ctx context.Context, event AuditEvent) error
}

type AuditFunc func(ctx context.Context, event AuditEvent) error

func (f AuditFunc) RecordModelCall(ctx context.Context, event AuditEvent) error {
	return f(ctx, event)
}

type Adapter struct {
	Config        HarnessConfig
	Exec          Executor
	AccountRouter AccountRouter
	Artifacts     ArtifactStore
	Audit         AuditSink
	Now           func() time.Time
}

func NewAdapter(config HarnessConfig, exec Executor) *Adapter {
	if exec == nil {
		exec = OSExecutor{}
	}
	return &Adapter{Config: config, Exec: exec, Now: time.Now}
}

func (a *Adapter) Supports(model string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return true
	}
	if len(a.Config.SupportedModels) == 0 {
		return true
	}
	for _, supported := range a.Config.SupportedModels {
		s := strings.ToLower(strings.TrimSpace(supported))
		if model == s || strings.HasPrefix(model, s+"-") {
			return true
		}
	}
	return false
}

func (a *Adapter) Run(ctx context.Context, req RunRequest, onChunk func(StreamChunk) error) (RunResult, error) {
	if a == nil {
		return RunResult{}, fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	if a.Exec == nil {
		a.Exec = OSExecutor{}
	}
	now := a.Now
	if now == nil {
		now = time.Now
	}
	started := nowUTC(now)
	result := RunResult{
		Harness:   a.Config.Harness,
		Model:     strings.TrimSpace(req.Model),
		AccountID: strings.TrimSpace(req.AccountID),
		StartedAt: started,
	}
	if err := a.validateRunRequest(req); err != nil {
		result.CompletedAt = nowUTC(now)
		result.Error = err.Error()
		return result, err
	}
	accountEnv, err := a.accountEnv(ctx, result.AccountID)
	if err != nil {
		result.CompletedAt = nowUTC(now)
		result.Error = err.Error()
		return result, err
	}
	manifest, err := a.contextManifest(req, result, started)
	if err != nil {
		result.CompletedAt = nowUTC(now)
		result.Error = err.Error()
		return result, err
	}
	manifestBytes, manifestHash, err := digestJSON(manifest)
	if err != nil {
		result.CompletedAt = nowUTC(now)
		result.Error = err.Error()
		return result, fmt.Errorf("cli: encode context manifest: %w", err)
	}
	result.InputSHA256 = manifest.InputSHA256
	result.ManifestSHA256 = manifestHash
	projectManifestRef, err := a.writeProjectContextManifest(ctx, req, manifest)
	if err != nil {
		result.CompletedAt = nowUTC(now)
		result.Error = err.Error()
		return result, err
	}
	if projectManifestRef.Written {
		result.Artifacts = append(result.Artifacts, projectManifestRef)
	}

	if err := a.recordAudit(ctx, AuditEvent{
		Type:           AuditModelCallStarted,
		Harness:        result.Harness,
		Model:          result.Model,
		AccountID:      result.AccountID,
		InputSHA256:    result.InputSHA256,
		ManifestSHA256: result.ManifestSHA256,
		At:             started,
	}); err != nil {
		result.CompletedAt = nowUTC(now)
		result.Error = err.Error()
		return result, err
	}

	spec, err := a.commandSpec(req, accountEnv)
	if err != nil {
		result.CompletedAt = nowUTC(now)
		result.Error = err.Error()
		_ = a.recordAudit(ctx, completeEvent(result, nowUTC(now)))
		return result, err
	}
	cmdResult, runErr := a.Exec.Run(ctx, spec, func(chunk StreamChunk) error {
		chunk.Harness = a.Config.Harness
		return onChunkOrNil(onChunk, chunk)
	})
	result.ExitCode = cmdResult.ExitCode
	result.Stdout = cmdResult.Stdout
	result.Stderr = cmdResult.Stderr
	result.StdoutSHA256 = digestBytes(result.Stdout)
	result.StderrSHA256 = digestBytes(result.Stderr)
	if !cmdResult.CompletedAt.IsZero() {
		result.CompletedAt = cmdResult.CompletedAt.UTC()
	} else {
		result.CompletedAt = nowUTC(now)
	}
	if runErr == nil && result.ExitCode != 0 {
		runErr = fmt.Errorf("%w: %s exited %d", ErrCommandFailed, a.Config.Harness, result.ExitCode)
	}

	artifactRefs, artifactErr := a.writeArtifacts(ctx, req, result, manifestBytes)
	result.Artifacts = append(result.Artifacts, artifactRefs...)
	if artifactErr != nil && runErr == nil {
		runErr = artifactErr
	}
	if len(result.Artifacts) == 0 {
		result.Artifacts = nil
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	if err := a.recordAudit(ctx, completeEvent(result, result.CompletedAt)); err != nil && runErr == nil {
		result.Error = err.Error()
		runErr = err
	}
	return result, runErr
}

func (a *Adapter) validateRunRequest(req RunRequest) error {
	if strings.TrimSpace(req.Prompt) == "" {
		return fmt.Errorf("%w: empty prompt", ErrInvalidRequest)
	}
	if strings.TrimSpace(a.Config.Binary) == "" {
		return fmt.Errorf("%w: empty binary for %s", ErrInvalidRequest, a.Config.Harness)
	}
	if !a.Supports(req.Model) {
		return fmt.Errorf("%w: %s does not support %q", ErrUnsupportedModel, a.Config.Harness, req.Model)
	}
	if req.MaxTokens < 0 {
		return fmt.Errorf("%w: negative maxTokens", ErrInvalidRequest)
	}
	if req.MaxTokens > 0 && strings.TrimSpace(a.Config.MaxTokensFlag) == "" {
		return fmt.Errorf("%w: maxTokens for %s", ErrUnsupportedOption, a.Config.Harness)
	}
	if err := rejectProviderCredentialEnv(req.Env); err != nil {
		return err
	}
	return nil
}

func (a *Adapter) accountEnv(ctx context.Context, accountID string) ([]string, error) {
	if accountID == "" {
		return nil, nil
	}
	if a.AccountRouter == nil {
		return nil, fmt.Errorf("%w: %s requested account %q", ErrMissingAccountRouter, a.Config.Harness, accountID)
	}
	env, err := a.AccountRouter.EnvForAccount(ctx, a.Config.Harness, accountID)
	if err != nil {
		return nil, err
	}
	if err := rejectProviderCredentialEnv(env); err != nil {
		return nil, err
	}
	return env, nil
}

func (a *Adapter) contextManifest(req RunRequest, result RunResult, started time.Time) (modelcontext.Manifest, error) {
	policy, err := a.contextPolicy(req)
	if err != nil {
		return modelcontext.Manifest{}, err
	}
	stage := req.ContextStage
	if stage == "" {
		stage = modelcontext.StagePlanning
	}
	policyRule := strings.TrimSpace(req.Context.PolicyRule)
	if policyRule == "" {
		policyRule = "subscription_cli_only"
	}
	resultManifest, err := modelcontext.Evaluate(policy, modelcontext.EvaluationRequest{
		Stage:      stage,
		Harness:    string(a.Config.Harness),
		Model:      result.Model,
		AccountID:  result.AccountID,
		PolicyRule: policyRule,
		Prompt:     []byte(req.Prompt),
		Sources:    modelContextSources(req),
		Now:        func() time.Time { return started },
	})
	if err != nil {
		return modelcontext.Manifest{}, err
	}
	return resultManifest.Manifest, nil
}

func (a *Adapter) contextPolicy(req RunRequest) (modelcontext.Policy, error) {
	if req.ContextPolicy != nil {
		return *req.ContextPolicy, nil
	}
	if strings.TrimSpace(req.WorkDir) == "" {
		return modelcontext.DefaultPolicy(), nil
	}
	policyFile, err := modelcontext.LoadPolicyFile(filepath.Join(req.WorkDir, ".hoopoe", "model-context-policy.json"))
	if err == nil {
		return policyFile.ContextPolicy, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return modelcontext.DefaultPolicy(), nil
	}
	return modelcontext.Policy{}, err
}

func (a *Adapter) writeProjectContextManifest(ctx context.Context, req RunRequest, manifest modelcontext.Manifest) (ArtifactRef, error) {
	if strings.TrimSpace(req.WorkDir) == "" {
		return ArtifactRef{}, nil
	}
	ref, err := (modelcontext.ManifestStore{ProjectRoot: req.WorkDir}).Write(ctx, manifest)
	if err != nil {
		return ArtifactRef{}, err
	}
	return ArtifactRef{
		Kind:    ArtifactContextManifest,
		Path:    ref.Path,
		SHA256:  strings.TrimPrefix(ref.SHA256, "sha256:"),
		Size:    ref.Size,
		Written: ref.Written,
	}, nil
}

func modelContextSources(req RunRequest) []modelcontext.Source {
	sources := append([]modelcontext.Source(nil), req.ContextSources...)
	for _, sourceRef := range req.Context.SourceRefs {
		sourceRef = strings.TrimSpace(sourceRef)
		if sourceRef == "" {
			continue
		}
		source := modelcontext.Source{Kind: "source_ref", Path: sourceRef}
		if strings.Contains(sourceRef, "://") {
			source.Path = ""
			source.URI = sourceRef
		}
		sources = append(sources, source)
	}
	for _, artifact := range req.Context.InputArtifacts {
		if strings.TrimSpace(artifact.Path) == "" {
			continue
		}
		sources = append(sources, modelcontext.Source{Kind: string(artifact.Kind), Path: artifact.Path})
	}
	return sources
}

func (a *Adapter) commandSpec(req RunRequest, accountEnv []string) (CommandSpec, error) {
	args := append([]string(nil), a.Config.Args...)
	if req.Model != "" && a.Config.ModelFlag != "" {
		args = append(args, a.Config.ModelFlag, req.Model)
	}
	if req.MaxTokens > 0 {
		args = append(args, a.Config.MaxTokensFlag, strconv.Itoa(req.MaxTokens))
	}
	if a.Config.StdinArg != "" {
		args = append(args, a.Config.StdinArg)
	}
	for _, arg := range args {
		if strings.Contains(arg, req.Prompt) {
			return CommandSpec{}, fmt.Errorf("%w: prompt must be sent on stdin", ErrInvalidRequest)
		}
	}
	timeout := req.Timeout
	if timeout == 0 {
		timeout = a.Config.DefaultTimeout
	}
	if timeout == 0 {
		timeout = DefaultCommandTimeout
	}
	maxOutput := a.Config.MaxOutputBytes
	if maxOutput == 0 {
		maxOutput = DefaultMaxOutputBytes
	}
	env := append([]string(nil), req.Env...)
	env = append(env, accountEnv...)
	return CommandSpec{
		Binary:         a.Config.Binary,
		Args:           args,
		Stdin:          []byte(req.Prompt),
		Dir:            req.WorkDir,
		Env:            env,
		Timeout:        timeout,
		MaxOutputBytes: maxOutput,
	}, nil
}

func (a *Adapter) writeArtifacts(ctx context.Context, req RunRequest, result RunResult, manifest []byte) ([]ArtifactRef, error) {
	if strings.TrimSpace(req.PlanID) == "" && strings.TrimSpace(req.CandidateSlug) == "" {
		return nil, nil
	}
	if a.Artifacts == nil {
		return nil, ErrArtifactStoreMissing
	}
	slug := strings.TrimSpace(req.CandidateSlug)
	if slug == "" {
		slug = strings.ReplaceAll(string(a.Config.Harness), "_", "-")
	}
	artifacts := []Artifact{
		{Kind: ArtifactCandidateMarkdown, PlanID: req.PlanID, CandidateSlug: slug, Content: result.Stdout, MediaType: "text/markdown"},
		{Kind: ArtifactStdout, PlanID: req.PlanID, CandidateSlug: slug, Content: result.Stdout, MediaType: "text/plain"},
		{Kind: ArtifactStderr, PlanID: req.PlanID, CandidateSlug: slug, Content: result.Stderr, MediaType: "text/plain"},
		{Kind: ArtifactContextManifest, PlanID: req.PlanID, CandidateSlug: slug, Content: manifest, MediaType: "application/json"},
	}
	refs := make([]ArtifactRef, 0, len(artifacts))
	for _, artifact := range artifacts {
		ref, err := a.Artifacts.Write(ctx, artifact)
		if err != nil {
			return refs, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

func (a *Adapter) recordAudit(ctx context.Context, event AuditEvent) error {
	if a.Audit == nil {
		return nil
	}
	return a.Audit.RecordModelCall(ctx, event)
}

func completeEvent(result RunResult, at time.Time) AuditEvent {
	return AuditEvent{
		Type:           AuditModelCallCompleted,
		Harness:        result.Harness,
		Model:          result.Model,
		AccountID:      result.AccountID,
		InputSHA256:    result.InputSHA256,
		StdoutSHA256:   result.StdoutSHA256,
		StderrSHA256:   result.StderrSHA256,
		ManifestSHA256: result.ManifestSHA256,
		ArtifactRefs:   result.Artifacts,
		ExitCode:       result.ExitCode,
		Error:          result.Error,
		At:             at,
	}
}

func onChunkOrNil(onChunk func(StreamChunk) error, chunk StreamChunk) error {
	if onChunk == nil {
		return nil
	}
	return onChunk(chunk)
}
