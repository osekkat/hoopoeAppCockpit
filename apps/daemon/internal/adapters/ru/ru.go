// Package ru wraps Repo Updater's narrow Hoopoe-approved surfaces.
//
// Adopted runtime commands are limited to VPS-side multi-project Git plumbing:
// sync/status/list/prune plus schema/docs probes. The richer ru workflows
// (review, agent-sweep, ai-sync, dep-update) are intentionally represented as
// blocked capabilities and have no execution path here because they own a
// parallel workflow state model.
package ru

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "ru"

	CapabilityPresent        = "ru._present"
	CapabilitySyncDryRun     = "ru.sync.dry_run"
	CapabilitySync           = "ru.sync"
	CapabilityStatusRead     = "ru.status.read"
	CapabilityListPaths      = "ru.list.paths"
	CapabilityPruneDryRun    = "ru.prune.dry_run"
	CapabilityPruneArchive   = "ru.prune.archive"
	CapabilitySchema         = "ru.schema"
	CapabilityRobotDocs      = "ru.robot.docs"
	CapabilityReview         = "ru.review"
	CapabilityAgentSweep     = "ru.agent_sweep"
	CapabilityAISync         = "ru.ai_sync"
	CapabilityDepUpdate      = "ru.dep_update"
	defaultMaxStdoutBytes    = 4 << 20
	minCompatibleVersion     = "1.2.0"
	referenceFixturesVersion = "phase0-2026-05-02"
)

var (
	ErrInvalidRequest     = errors.New("ru: invalid request")
	ErrMissingBinary      = errors.New("ru: binary not found")
	ErrOutputTooLarge     = errors.New("ru: command output exceeded limit")
	ErrSchemaContract     = errors.New("ru: schema contract violation")
	ErrCommandContract    = errors.New("ru: command contract violation")
	ErrUnsupportedVersion = errors.New("ru: unsupported version")
)

type Runner interface {
	Run(ctx context.Context, argv []string) (CommandResult, error)
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

func ExitCodeMeaning(exit int) string {
	switch exit {
	case 0:
		return "ok"
	case 124:
		return "timeout"
	case 127:
		return "missing-tool"
	default:
		return "ru-error"
	}
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, argv []string) (CommandResult, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return CommandResult{}, fmt.Errorf("%w: empty argv", ErrInvalidRequest)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if isExecNotFoundErr(err) {
		result.ExitCode = -1
		return result, ErrMissingBinary
	}
	result.ExitCode = -1
	return result, err
}

type Adapter struct {
	Runner         Runner
	Now            func() time.Time
	MaxStdoutBytes int
	Schema         *SchemaDocument
}

func New(runner Runner) *Adapter {
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Adapter{
		Runner:         runner,
		Now:            time.Now,
		MaxStdoutBytes: defaultMaxStdoutBytes,
	}
}

func (a *Adapter) Init(ctx context.Context) error {
	schema, err := a.LoadSchema(ctx)
	if err != nil {
		return err
	}
	a.Schema = schema
	return nil
}

type SyncOptions struct {
	DryRun    bool
	Parallel  int
	CloneOnly bool
	PullOnly  bool
}

type Envelope struct {
	GeneratedAt  string          `json:"generated_at"`
	Version      string          `json:"version"`
	OutputFormat string          `json:"output_format"`
	Command      string          `json:"command"`
	Data         json.RawMessage `json:"data"`
	Meta         json.RawMessage `json:"_meta,omitempty"`
	Raw          json.RawMessage `json:"-"`
}

type SchemaDocument struct {
	SchemaVersion string                   `json:"schema_version"`
	Topic         string                   `json:"topic"`
	Content       SchemaContent            `json:"content"`
	Commands      map[string]CommandSchema `json:"-"`
	Envelope      json.RawMessage          `json:"-"`
	Version       string                   `json:"-"`
}

type SchemaContent struct {
	Description string                   `json:"description,omitempty"`
	Envelope    json.RawMessage          `json:"envelope,omitempty"`
	Commands    map[string]CommandSchema `json:"commands,omitempty"`
}

type CommandSchema struct {
	Description string          `json:"description,omitempty"`
	DataSchema  json.RawMessage `json:"data_schema,omitempty"`
}

func (d *SchemaDocument) HasCommand(name string) bool {
	if d == nil {
		return false
	}
	_, ok := d.Commands[name]
	return ok
}

type StatusResponse struct {
	Total int          `json:"total"`
	Repos []RepoStatus `json:"repos"`
	Raw   []byte       `json:"-"`
}

type RepoStatus struct {
	Repo     string `json:"repo"`
	Path     string `json:"path"`
	Status   string `json:"status"`
	Branch   string `json:"branch"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Dirty    bool   `json:"dirty"`
	Mismatch bool   `json:"mismatch"`
}

type ListResponse struct {
	Total int        `json:"total"`
	Repos []ListRepo `json:"repos"`
	Raw   []byte     `json:"-"`
}

type ListRepo struct {
	Repo       string `json:"repo"`
	URL        string `json:"url"`
	Branch     string `json:"branch,omitempty"`
	CustomName string `json:"custom_name,omitempty"`
	Path       string `json:"path,omitempty"`
	Source     string `json:"source,omitempty"`
}

type SyncResponse struct {
	Config       SyncConfig        `json:"config,omitempty"`
	Summary      SyncSummary       `json:"summary"`
	Repos        []SyncRepoResult  `json:"repos"`
	RawDocuments []json.RawMessage `json:"-"`
	Raw          []byte            `json:"-"`
}

type SyncConfig struct {
	ProjectsDir string `json:"projects_dir,omitempty"`
	Layout      string `json:"layout,omitempty"`
	Parallel    int    `json:"parallel,omitempty"`
	CloneOnly   bool   `json:"clone_only,omitempty"`
	PullOnly    bool   `json:"pull_only,omitempty"`
	DryRun      bool   `json:"dry_run,omitempty"`
}

type SyncSummary struct {
	Total   int `json:"total"`
	Cloned  int `json:"cloned"`
	Pulled  int `json:"pulled"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

type SyncRepoResult struct {
	Repo       string          `json:"repo"`
	Status     string          `json:"status"`
	Path       string          `json:"path,omitempty"`
	Detail     string          `json:"detail,omitempty"`
	DurationMS int             `json:"duration_ms,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

type PruneMode string

const (
	PruneModeDryRun  PruneMode = "dry_run"
	PruneModeArchive PruneMode = "archive"
)

type PruneResult struct {
	Mode       PruneMode        `json:"mode"`
	Candidates []PruneCandidate `json:"candidates"`
	RawLines   []string         `json:"rawLines"`
}

type PruneCandidate struct {
	Path string `json:"path"`
	Line string `json:"line"`
}

func SchemaArgv() []string {
	return []string{ToolName, "--schema"}
}

func VersionArgv() []string {
	return []string{ToolName, "--version"}
}

func RobotDocsArgv() []string {
	return []string{ToolName, "robot-docs"}
}

func StatusArgv() []string {
	return []string{ToolName, "status", "--no-fetch", "--json"}
}

func ListArgv() []string {
	return []string{ToolName, "list", "--json"}
}

func ListPathsArgv() []string {
	return []string{ToolName, "list", "--paths"}
}

func SyncArgv(opts SyncOptions) ([]string, error) {
	if opts.CloneOnly && opts.PullOnly {
		return nil, fmt.Errorf("%w: clone-only and pull-only are mutually exclusive", ErrInvalidRequest)
	}
	argv := []string{ToolName, "sync"}
	if opts.DryRun {
		argv = append(argv, "--dry-run")
	}
	argv = append(argv, "--json", "--non-interactive")
	if opts.Parallel > 0 {
		argv = append(argv, "--parallel", strconv.Itoa(opts.Parallel))
	}
	if opts.CloneOnly {
		argv = append(argv, "--clone-only")
	}
	if opts.PullOnly {
		argv = append(argv, "--pull-only")
	}
	return argv, nil
}

func PruneDryRunArgv() []string {
	return []string{ToolName, "prune", "--dry-run"}
}

func PruneArchiveArgv() []string {
	return []string{ToolName, "prune", "--archive"}
}

func (a *Adapter) LoadSchema(ctx context.Context) (*SchemaDocument, error) {
	raw, err := a.runJSON(ctx, SchemaArgv())
	if err != nil {
		return nil, err
	}
	return ParseSchema(raw)
}

func (a *Adapter) RobotDocs(ctx context.Context) (string, error) {
	out, err := a.runText(ctx, RobotDocsArgv())
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (a *Adapter) Status(ctx context.Context) (StatusResponse, error) {
	raw, err := a.runJSON(ctx, StatusArgv())
	if err != nil {
		return StatusResponse{}, err
	}
	return ParseStatus(raw)
}

func (a *Adapter) List(ctx context.Context) (ListResponse, error) {
	raw, err := a.runJSON(ctx, ListArgv())
	if err != nil {
		return ListResponse{}, err
	}
	return ParseList(raw)
}

func (a *Adapter) ListPaths(ctx context.Context) ([]string, error) {
	raw, err := a.runText(ctx, ListPathsArgv())
	if err != nil {
		return nil, err
	}
	return ParseListPaths(raw)
}

func (a *Adapter) Sync(ctx context.Context, opts SyncOptions) (SyncResponse, error) {
	argv, err := SyncArgv(opts)
	if err != nil {
		return SyncResponse{}, err
	}
	raw, err := a.runText(ctx, argv)
	if err != nil {
		return SyncResponse{}, err
	}
	return ParseSync(raw)
}

func (a *Adapter) SyncDryRun(ctx context.Context) (SyncResponse, error) {
	return a.Sync(ctx, SyncOptions{DryRun: true})
}

func (a *Adapter) PruneDryRun(ctx context.Context) (PruneResult, error) {
	raw, err := a.runText(ctx, PruneDryRunArgv())
	if err != nil {
		return PruneResult{}, err
	}
	return ParsePrune(raw, PruneModeDryRun), nil
}

func (a *Adapter) PruneArchive(ctx context.Context) (PruneResult, error) {
	raw, err := a.runText(ctx, PruneArchiveArgv())
	if err != nil {
		return PruneResult{}, err
	}
	return ParsePrune(raw, PruneModeArchive), nil
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	report := &capabilities.ToolReport{
		Tool:            capabilities.ToolRU,
		Source:          "cli",
		LastCheckedAt:   a.now().UTC().Format(time.RFC3339),
		FixturesVersion: referenceFixturesVersion,
		Capabilities:    defaultCapabilities("not probed"),
	}

	versionRaw, err := a.runText(ctx, VersionArgv())
	if err != nil {
		state := statusForError(err)
		for id, cap := range report.Capabilities {
			cap.Status = state
			cap.Notes = err.Error()
			report.Capabilities[id] = cap
		}
		return report, nil
	}
	report.Version = normalizeVersion(versionRaw)
	report.Capabilities[CapabilityPresent] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	if !compatibleVersion(report.Version) {
		report.Capabilities[CapabilityPresent] = capabilities.Capability{
			Status: capabilities.StatusDegraded,
			Notes:  fmt.Sprintf("%v: got %q, need >= %s", ErrUnsupportedVersion, report.Version, minCompatibleVersion),
		}
	}

	schema, err := a.LoadSchema(ctx)
	if err != nil {
		report.Capabilities[CapabilitySchema] = capabilities.Capability{Status: statusForError(err), Notes: err.Error(), Transport: "stdio"}
	} else {
		a.Schema = schema
		report.Capabilities[CapabilitySchema] = capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
		for _, command := range []string{"status", "list", "sync"} {
			if !schema.HasCommand(command) {
				report.Capabilities[CapabilitySchema] = capabilities.Capability{
					Status: capabilities.StatusDegraded,
					Notes:  fmt.Sprintf("%v: schema missing %s command", ErrSchemaContract, command),
				}
				break
			}
		}
	}

	report.Capabilities[CapabilityStatusRead] = probeOne(ctx, CapabilityStatusRead, "status --no-fetch --json", func(ctx context.Context) error {
		_, err := a.Status(ctx)
		return err
	})
	report.Capabilities[CapabilityListPaths] = probeOne(ctx, CapabilityListPaths, "list --paths", func(ctx context.Context) error {
		_, err := a.ListPaths(ctx)
		return err
	})
	report.Capabilities[CapabilitySyncDryRun] = probeOne(ctx, CapabilitySyncDryRun, "sync --dry-run --json --non-interactive", func(ctx context.Context) error {
		_, err := a.SyncDryRun(ctx)
		return err
	})
	report.Capabilities[CapabilityPruneDryRun] = probeOne(ctx, CapabilityPruneDryRun, "prune --dry-run", func(ctx context.Context) error {
		_, err := a.PruneDryRun(ctx)
		return err
	})
	report.Capabilities[CapabilityRobotDocs] = probeOne(ctx, CapabilityRobotDocs, "robot-docs", func(ctx context.Context) error {
		_, err := a.RobotDocs(ctx)
		return err
	})

	report.Capabilities[CapabilitySync] = capabilities.Capability{
		Status: capabilities.StatusUntested,
		Notes:  "sync mutates VPS checkouts; dry-run probe covers command shape",
	}
	report.Capabilities[CapabilityPruneArchive] = capabilities.Capability{
		Status: capabilities.StatusBlockedByPolicy,
		Notes:  "archive repair is only invoked from Diagnostics with explicit user intent",
	}
	for _, id := range []string{CapabilityReview, CapabilityAgentSweep, CapabilityAISync, CapabilityDepUpdate} {
		report.Capabilities[id] = capabilities.Capability{
			Status: capabilities.StatusBlockedByPolicy,
			Notes:  "not adopted by Hoopoe; routes through beads/NTM/Activity primitives instead",
		}
	}
	return report, nil
}

func ParseSchema(data []byte) (*SchemaDocument, error) {
	env, err := ParseEnvelope(data)
	if err != nil {
		return nil, err
	}
	var doc SchemaDocument
	if err := json.Unmarshal(env.Data, &doc); err != nil {
		return nil, fmt.Errorf("ru: decode schema data: %w", err)
	}
	if doc.SchemaVersion == "" {
		return nil, fmt.Errorf("%w: missing schema_version", ErrSchemaContract)
	}
	doc.Commands = doc.Content.Commands
	doc.Envelope = doc.Content.Envelope
	doc.Version = env.Version
	if doc.Commands == nil {
		doc.Commands = map[string]CommandSchema{}
	}
	return &doc, nil
}

func ParseEnvelope(data []byte) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return Envelope{}, fmt.Errorf("ru: decode envelope: %w", err)
	}
	if env.GeneratedAt == "" || env.Version == "" || env.OutputFormat == "" || env.Command == "" || len(env.Data) == 0 {
		return Envelope{}, fmt.Errorf("%w: missing required envelope field", ErrCommandContract)
	}
	if env.OutputFormat != "json" && env.OutputFormat != "toon" {
		return Envelope{}, fmt.Errorf("%w: unsupported output_format %q", ErrCommandContract, env.OutputFormat)
	}
	env.Raw = append([]byte(nil), data...)
	return env, nil
}

func ParseStatus(data []byte) (StatusResponse, error) {
	env, err := ParseEnvelope(data)
	if err != nil {
		return StatusResponse{}, err
	}
	var status StatusResponse
	if err := json.Unmarshal(env.Data, &status); err != nil {
		return StatusResponse{}, fmt.Errorf("ru: decode status data: %w", err)
	}
	if status.Total < 0 || status.Total < len(status.Repos) {
		return StatusResponse{}, fmt.Errorf("%w: invalid status totals", ErrCommandContract)
	}
	status.Raw = append([]byte(nil), data...)
	return status, nil
}

func ParseList(data []byte) (ListResponse, error) {
	env, err := ParseEnvelope(data)
	if err != nil {
		return ListResponse{}, err
	}
	var list ListResponse
	if err := json.Unmarshal(env.Data, &list); err != nil {
		return ListResponse{}, fmt.Errorf("ru: decode list data: %w", err)
	}
	if list.Total < 0 || list.Total < len(list.Repos) {
		return ListResponse{}, fmt.Errorf("%w: invalid list totals", ErrCommandContract)
	}
	list.Raw = append([]byte(nil), data...)
	return list, nil
}

func ParseListPaths(data []byte) ([]string, error) {
	var paths []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())
		if path == "" {
			continue
		}
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%w: ru list --paths returned non-absolute path %q", ErrCommandContract, path)
		}
		paths = append(paths, path)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ru: scan list paths: %w", err)
	}
	return paths, nil
}

func ParseSync(data []byte) (SyncResponse, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return SyncResponse{}, fmt.Errorf("%w: empty sync output", ErrCommandContract)
	}
	if json.Valid(trimmed) {
		if response, err := parseSyncDocument(trimmed); err == nil {
			return response, nil
		}
		var repo SyncRepoResult
		if err := json.Unmarshal(trimmed, &repo); err == nil && repo.Repo != "" && repo.Status != "" {
			repo.Raw = append(json.RawMessage(nil), trimmed...)
			return SyncResponse{
				Summary:      summarizeRepos([]SyncRepoResult{repo}),
				Repos:        []SyncRepoResult{repo},
				RawDocuments: []json.RawMessage{repo.Raw},
				Raw:          append([]byte(nil), data...),
			}, nil
		}
		return SyncResponse{}, fmt.Errorf("%w: sync JSON missing repos/summary or repo/status", ErrCommandContract)
	}
	var response SyncResponse
	response.Raw = append([]byte(nil), data...)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		doc := append(json.RawMessage(nil), line...)
		response.RawDocuments = append(response.RawDocuments, doc)
		parsed, err := parseSyncDocument(line)
		if err == nil && (len(parsed.Repos) > 0 || parsed.Summary.Total > 0) {
			response.Config = parsed.Config
			response.Summary = mergeSummaries(response.Summary, parsed.Summary)
			response.Repos = append(response.Repos, parsed.Repos...)
			continue
		}
		var repo SyncRepoResult
		if err := json.Unmarshal(line, &repo); err != nil {
			return SyncResponse{}, fmt.Errorf("ru: decode sync NDJSON line %d: %w", len(response.RawDocuments), err)
		}
		if repo.Repo == "" || repo.Status == "" {
			return SyncResponse{}, fmt.Errorf("%w: sync NDJSON line %d missing repo/status", ErrCommandContract, len(response.RawDocuments))
		}
		repo.Raw = doc
		response.Repos = append(response.Repos, repo)
		response.Summary = mergeSummaries(response.Summary, summarizeRepos([]SyncRepoResult{repo}))
	}
	if err := scanner.Err(); err != nil {
		return SyncResponse{}, fmt.Errorf("ru: scan sync NDJSON: %w", err)
	}
	return response, nil
}

func ParsePrune(data []byte, mode PruneMode) PruneResult {
	var result PruneResult
	result.Mode = mode
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		result.RawLines = append(result.RawLines, line)
		if path := firstAbsolutePath(line); path != "" {
			result.Candidates = append(result.Candidates, PruneCandidate{Path: path, Line: line})
		}
	}
	return result
}

func parseSyncDocument(data []byte) (SyncResponse, error) {
	if env, err := ParseEnvelope(data); err == nil {
		var response SyncResponse
		if err := json.Unmarshal(env.Data, &response); err != nil {
			return SyncResponse{}, fmt.Errorf("ru: decode sync envelope data: %w", err)
		}
		if response.Summary.Total == 0 && len(response.Repos) > 0 {
			response.Summary = summarizeRepos(response.Repos)
		}
		response.Raw = append([]byte(nil), data...)
		response.RawDocuments = []json.RawMessage{append(json.RawMessage(nil), data...)}
		return response, nil
	}
	var response SyncResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return SyncResponse{}, err
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return SyncResponse{}, err
	}
	if _, ok := keys["summary"]; !ok {
		return SyncResponse{}, fmt.Errorf("%w: sync document missing repos/summary", ErrCommandContract)
	}
	if _, ok := keys["repos"]; !ok {
		return SyncResponse{}, fmt.Errorf("%w: sync document missing repos/summary", ErrCommandContract)
	}
	if response.Summary.Total == 0 && len(response.Repos) > 0 {
		response.Summary = summarizeRepos(response.Repos)
	}
	response.Raw = append([]byte(nil), data...)
	response.RawDocuments = []json.RawMessage{append(json.RawMessage(nil), data...)}
	return response, nil
}

func mergeSummaries(a, b SyncSummary) SyncSummary {
	return SyncSummary{
		Total:   a.Total + b.Total,
		Cloned:  a.Cloned + b.Cloned,
		Pulled:  a.Pulled + b.Pulled,
		Skipped: a.Skipped + b.Skipped,
		Failed:  a.Failed + b.Failed,
	}
}

func summarizeRepos(repos []SyncRepoResult) SyncSummary {
	var s SyncSummary
	for _, repo := range repos {
		s.Total++
		switch repo.Status {
		case "cloned":
			s.Cloned++
		case "pulled":
			s.Pulled++
		case "failed":
			s.Failed++
		case "skipped", "dirty", "up-to-date":
			s.Skipped++
		}
	}
	return s
}

func (a *Adapter) runJSON(ctx context.Context, argv []string) ([]byte, error) {
	out, err := a.runText(ctx, argv)
	if err != nil {
		return nil, err
	}
	if !json.Valid(bytes.TrimSpace(out)) {
		return nil, fmt.Errorf("ru: %q produced malformed JSON", strings.Join(argv, " "))
	}
	return out, nil
}

func (a *Adapter) runText(ctx context.Context, argv []string) ([]byte, error) {
	if err := validateAdoptedArgv(argv); err != nil {
		return nil, err
	}
	runner := a.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, argv)
	if err != nil {
		if errors.Is(err, ErrMissingBinary) {
			return nil, err
		}
		return nil, fmt.Errorf("ru: invoke %q: %w (stderr: %s)", strings.Join(argv, " "), err, truncateStderr(result.Stderr))
	}
	if result.ExitCode != 0 {
		return nil, fmt.Errorf("ru: %q exited %d (stderr: %s)", strings.Join(argv, " "), result.ExitCode, truncateStderr(result.Stderr))
	}
	max := a.MaxStdoutBytes
	if max <= 0 {
		max = defaultMaxStdoutBytes
	}
	if len(result.Stdout) > max {
		return nil, fmt.Errorf("%w: %q produced %d bytes (limit %d)", ErrOutputTooLarge, strings.Join(argv, " "), len(result.Stdout), max)
	}
	return result.Stdout, nil
}

func validateAdoptedArgv(argv []string) error {
	if len(argv) == 0 || argv[0] != ToolName {
		return fmt.Errorf("%w: argv must start with ru", ErrInvalidRequest)
	}
	joined := strings.Join(argv, " ")
	switch {
	case len(argv) >= 2 && argv[1] == "sync":
		if !containsArg(argv, "--json") || !containsArg(argv, "--non-interactive") {
			return fmt.Errorf("%w: ru sync requires --json and --non-interactive", ErrInvalidRequest)
		}
	case len(argv) >= 2 && argv[1] == "status":
		if !containsArg(argv, "--json") || !containsArg(argv, "--no-fetch") {
			return fmt.Errorf("%w: ru status requires --no-fetch --json", ErrInvalidRequest)
		}
	case len(argv) >= 2 && argv[1] == "list":
		if !containsArg(argv, "--json") && !containsArg(argv, "--paths") {
			return fmt.Errorf("%w: ru list requires --json or --paths", ErrInvalidRequest)
		}
	case len(argv) >= 2 && argv[1] == "prune":
		if containsArg(argv, "--delete") {
			return fmt.Errorf("%w: ru prune --delete is not adopted", ErrInvalidRequest)
		}
		if !containsArg(argv, "--dry-run") && !containsArg(argv, "--archive") {
			return fmt.Errorf("%w: ru prune requires --dry-run or --archive", ErrInvalidRequest)
		}
	case joined == strings.Join(SchemaArgv(), " "),
		joined == strings.Join(VersionArgv(), " "),
		joined == strings.Join(RobotDocsArgv(), " "):
		return nil
	default:
		return fmt.Errorf("%w: ru surface not adopted: %s", ErrInvalidRequest, joined)
	}
	return nil
}

func defaultCapabilities(note string) map[string]capabilities.Capability {
	caps := make(map[string]capabilities.Capability, len(CapabilityIDs()))
	for _, id := range CapabilityIDs() {
		caps[id] = capabilities.Capability{Status: capabilities.StatusUntested, Notes: note}
	}
	for _, id := range []string{CapabilityReview, CapabilityAgentSweep, CapabilityAISync, CapabilityDepUpdate} {
		caps[id] = capabilities.Capability{
			Status: capabilities.StatusBlockedByPolicy,
			Notes:  "not adopted by Hoopoe; routes through beads/NTM/Activity primitives instead",
		}
	}
	return caps
}

func CapabilityIDs() []string {
	return []string{
		CapabilityPresent,
		CapabilitySyncDryRun,
		CapabilitySync,
		CapabilityStatusRead,
		CapabilityListPaths,
		CapabilityPruneDryRun,
		CapabilityPruneArchive,
		CapabilitySchema,
		CapabilityRobotDocs,
		CapabilityReview,
		CapabilityAgentSweep,
		CapabilityAISync,
		CapabilityDepUpdate,
	}
}

func probeOne(ctx context.Context, id, summary string, call func(context.Context) error) capabilities.Capability {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := call(probeCtx)
	if err == nil {
		return capabilities.Capability{Status: capabilities.StatusOK, Transport: "stdio"}
	}
	return capabilities.Capability{
		Status:    statusForError(err),
		Transport: "stdio",
		Notes:     fmt.Sprintf("%s probe error: %s", summary, truncateStderr([]byte(err.Error()))),
	}
}

func statusForError(err error) capabilities.CapabilityStatus {
	switch {
	case errors.Is(err, ErrMissingBinary):
		return capabilities.StatusMissing
	case errors.Is(err, ErrUnsupportedVersion):
		return capabilities.StatusDegraded
	default:
		return capabilities.StatusDegraded
	}
}

func compatibleVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	if errMajor != nil || errMinor != nil {
		return false
	}
	return major > 1 || (major == 1 && minor >= 2)
}

func normalizeVersion(data []byte) string {
	version := strings.TrimSpace(string(data))
	version = strings.TrimPrefix(version, "ru version ")
	version = strings.TrimPrefix(version, "ru ")
	version = strings.TrimPrefix(version, "v")
	if newline := strings.Index(version, "\n"); newline >= 0 {
		version = version[:newline]
	}
	return strings.TrimSpace(version)
}

func (a *Adapter) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func containsArg(argv []string, want string) bool {
	for _, arg := range argv {
		if arg == want {
			return true
		}
	}
	return false
}

func firstAbsolutePath(line string) string {
	for _, field := range strings.Fields(line) {
		trimmed := strings.Trim(field, "\"'(),")
		if filepath.IsAbs(trimmed) {
			return trimmed
		}
	}
	return ""
}

func truncateStderr(b []byte) string {
	const max = 512
	s := string(b)
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func isExecNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "executable file not found") ||
		strings.Contains(s, "no such file or directory")
}
