// Package br wraps the beads_rust CLI behind typed daemon operations.
//
// The adapter prefers br JSON subcommands, uses .beads/issues.jsonl only as a
// cold-start read model, and keeps git synchronization as explicit daemon
// steps. It never exposes arbitrary shell execution.
package br

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

const (
	ToolName = "br"

	CapabilityPresent       = "br._present"
	CapabilityIssuesRead    = "br.issues.read"
	CapabilityIssuesUpdate  = "br.issues.update"
	CapabilityReady         = "br.ready"
	CapabilityCreate        = "br.create"
	CapabilityClose         = "br.close"
	CapabilityDepAdd        = "br.dep.add"
	CapabilityDepRemove     = "br.dep.remove"
	CapabilityDepCycles     = "br.dep.cycles"
	CapabilitySyncFlushOnly = "br.sync.flush_only"
	CapabilitySyncFlush     = "br.sync.flush"
	CapabilityDoctor        = "br.doctor"
	CapabilitySchema        = "br.schema"
	CapabilityTUI           = "br.tui"

	defaultMaxStdoutBytes = 1 << 20
)

var (
	ErrInvalidRequest  = errors.New("br: invalid request")
	ErrListContract    = errors.New("br: list contract violation")
	ErrDependencyCycle = errors.New("br: dependency cycle detected")
	ErrOutputTooLarge  = errors.New("br: command output exceeded limit")
)

type Runner interface {
	Run(ctx context.Context, argv []string) (CommandResult, error)
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
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
	result := CommandResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	result.ExitCode = -1
	return result, err
}

type Adapter struct {
	Runner         Runner
	Now            func() time.Time
	MaxStdoutBytes int
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

type CommonOptions struct {
	DB            string
	Actor         string
	NoAutoFlush   bool
	NoAutoImport  bool
	AllowStale    bool
	NoDB          bool
	LockTimeoutMS int
	NoColor       bool
	Quiet         bool
	Verbose       int
}

type ListFilter struct {
	Common        CommonOptions
	Statuses      []string
	Types         []string
	Assignee      string
	Unassigned    bool
	IDs           []string
	Labels        []string
	LabelAny      []string
	Priorities    []int
	PriorityMin   int
	PriorityMax   int
	IncludeClosed bool
	Limit         int
	Offset        int
	Sort          string
	Reverse       bool
	Deferred      bool
	Overdue       bool
	TitleContains string
	DescContains  string
	NotesContains string
}

type ReadyFilter struct {
	Common          CommonOptions
	Limit           int
	Assignee        string
	AssigneeCurrent bool
	Unassigned      bool
	Labels          []string
	LabelAny        []string
	Types           []string
	Priorities      []int
	Sort            string
	IncludeDeferred bool
	Parent          string
	Recursive       bool
}

type BlockedFilter struct {
	Common     CommonOptions
	Limit      int
	Detailed   bool
	Types      []string
	Priorities []int
	Labels     []string
}

type CreateRequest struct {
	Common      CommonOptions
	Title       string
	IssueType   string
	Priority    string
	Description string
	Assignee    string
	Owner       string
	Labels      []string
	Parent      string
	Deps        string
	EstimateMin int
	Due         string
	Defer       string
	ExternalRef string
	Status      string
	Ephemeral   bool
	DryRun      bool
}

type UpdateRequest struct {
	Common             CommonOptions
	IDs                []string
	Title              string
	Description        string
	Design             string
	AcceptanceCriteria string
	Notes              string
	Status             string
	Priority           string
	IssueType          string
	Assignee           string
	Owner              string
	Claim              bool
	Force              bool
	Due                string
	Defer              string
	Estimate           string
	AddLabels          []string
	RemoveLabels       []string
	SetLabels          []string
	Parent             string
	ExternalRef        string
	Session            string
}

type CloseRequest struct {
	Common      CommonOptions
	IDs         []string
	Reason      string
	Force       bool
	SuggestNext bool
	Session     string
}

type ReopenRequest struct {
	Common CommonOptions
	IDs    []string
	Reason string
}

type DepRequest struct {
	Common    CommonOptions
	IssueID   string
	DependsOn string
	DepType   string
	Metadata  string
}

type DepListRequest struct {
	Common    CommonOptions
	IssueID   string
	Direction string
	DepType   string
}

type DepTreeRequest struct {
	Common    CommonOptions
	IssueID   string
	Direction string
	MaxDepth  int
}

type Issue struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Description     string       `json:"description,omitempty"`
	Status          string       `json:"status"`
	Priority        int          `json:"priority"`
	IssueType       string       `json:"issue_type,omitempty"`
	Assignee        string       `json:"assignee,omitempty"`
	Owner           string       `json:"owner,omitempty"`
	Labels          []string     `json:"labels,omitempty"`
	CreatedAt       string       `json:"created_at,omitempty"`
	CreatedBy       string       `json:"created_by,omitempty"`
	UpdatedAt       string       `json:"updated_at,omitempty"`
	ClosedAt        string       `json:"closed_at,omitempty"`
	CloseReason     string       `json:"close_reason,omitempty"`
	SourceRepo      string       `json:"source_repo,omitempty"`
	DueAt           string       `json:"due_at,omitempty"`
	DeferUntil      string       `json:"defer_until,omitempty"`
	ExternalRef     string       `json:"external_ref,omitempty"`
	DependencyCount int          `json:"dependency_count,omitempty"`
	DependentCount  int          `json:"dependent_count,omitempty"`
	Dependencies    []Dependency `json:"dependencies,omitempty"`
}

type Dependency struct {
	IssueID     string          `json:"issue_id"`
	DependsOnID string          `json:"depends_on_id"`
	Type        string          `json:"type,omitempty"`
	Title       string          `json:"title,omitempty"`
	Status      string          `json:"status,omitempty"`
	Priority    int             `json:"priority,omitempty"`
	CreatedAt   string          `json:"created_at,omitempty"`
	CreatedBy   string          `json:"created_by,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	ThreadID    string          `json:"thread_id,omitempty"`
}

type ListResponse struct {
	Issues  []Issue `json:"issues"`
	Total   int     `json:"total"`
	Limit   int     `json:"limit"`
	Offset  int     `json:"offset"`
	HasMore bool    `json:"has_more"`
}

type CycleReport struct {
	Cycles [][]string `json:"cycles"`
	Count  int        `json:"count"`
}

type DependencyCycleError struct {
	Cycles [][]string
}

func (e DependencyCycleError) Error() string {
	return fmt.Sprintf("%v: %d cycle(s)", ErrDependencyCycle, len(e.Cycles))
}

func (e DependencyCycleError) Unwrap() error {
	return ErrDependencyCycle
}

type ReadModel struct {
	Issues   []Issue
	ByID     map[string]Issue
	ByStatus map[string][]Issue
	ByOwner  map[string][]Issue
}

type ReadModelQuery struct {
	ID        string
	Status    string
	Owner     string
	Assignee  string
	MinPrio   int
	MaxPrio   int
	UseMinMax bool
}

type SyncPlan struct {
	Steps []SyncStep `json:"steps"`
}

type SyncStep struct {
	Name        string   `json:"name"`
	Tool        string   `json:"tool"`
	Argv        []string `json:"argv"`
	AuditAction string   `json:"auditAction"`
	Mutating    bool     `json:"mutating"`
}

func ListArgv(filter ListFilter) []string {
	argv := []string{ToolName, "list", "--json"}
	for _, status := range filter.Statuses {
		argv = appendNonEmptyPair(argv, "--status", status)
	}
	for _, issueType := range filter.Types {
		argv = appendNonEmptyPair(argv, "--type", issueType)
	}
	argv = appendNonEmptyPair(argv, "--assignee", filter.Assignee)
	if filter.Unassigned {
		argv = append(argv, "--unassigned")
	}
	for _, id := range filter.IDs {
		argv = appendNonEmptyPair(argv, "--id", id)
	}
	for _, label := range filter.Labels {
		argv = appendNonEmptyPair(argv, "--label", label)
	}
	for _, label := range filter.LabelAny {
		argv = appendNonEmptyPair(argv, "--label-any", label)
	}
	for _, priority := range filter.Priorities {
		argv = append(argv, "--priority", strconv.Itoa(priority))
	}
	if filter.PriorityMin > 0 {
		argv = append(argv, "--priority-min", strconv.Itoa(filter.PriorityMin))
	}
	if filter.PriorityMax > 0 {
		argv = append(argv, "--priority-max", strconv.Itoa(filter.PriorityMax))
	}
	argv = appendNonEmptyPair(argv, "--title-contains", filter.TitleContains)
	argv = appendNonEmptyPair(argv, "--desc-contains", filter.DescContains)
	argv = appendNonEmptyPair(argv, "--notes-contains", filter.NotesContains)
	if filter.IncludeClosed {
		argv = append(argv, "--all")
	}
	if filter.Limit > 0 {
		argv = append(argv, "--limit", strconv.Itoa(filter.Limit))
	}
	if filter.Offset > 0 {
		argv = append(argv, "--offset", strconv.Itoa(filter.Offset))
	}
	argv = appendNonEmptyPair(argv, "--sort", filter.Sort)
	if filter.Reverse {
		argv = append(argv, "--reverse")
	}
	if filter.Deferred {
		argv = append(argv, "--deferred")
	}
	if filter.Overdue {
		argv = append(argv, "--overdue")
	}
	return appendCommon(argv, filter.Common)
}

func ReadyArgv(filter ReadyFilter) []string {
	argv := []string{ToolName, "ready", "--json"}
	if filter.Limit > 0 {
		argv = append(argv, "--limit", strconv.Itoa(filter.Limit))
	}
	if filter.AssigneeCurrent {
		argv = append(argv, "--assignee")
	} else {
		argv = appendNonEmptyPair(argv, "--assignee", filter.Assignee)
	}
	if filter.Unassigned {
		argv = append(argv, "--unassigned")
	}
	for _, label := range filter.Labels {
		argv = appendNonEmptyPair(argv, "--label", label)
	}
	for _, label := range filter.LabelAny {
		argv = appendNonEmptyPair(argv, "--label-any", label)
	}
	for _, issueType := range filter.Types {
		argv = appendNonEmptyPair(argv, "--type", issueType)
	}
	for _, priority := range filter.Priorities {
		argv = append(argv, "--priority", strconv.Itoa(priority))
	}
	argv = appendNonEmptyPair(argv, "--sort", filter.Sort)
	if filter.IncludeDeferred {
		argv = append(argv, "--include-deferred")
	}
	argv = appendNonEmptyPair(argv, "--parent", filter.Parent)
	if filter.Recursive {
		argv = append(argv, "--recursive")
	}
	return appendCommon(argv, filter.Common)
}

func BlockedArgv(filter BlockedFilter) []string {
	argv := []string{ToolName, "blocked", "--json"}
	if filter.Limit > 0 {
		argv = append(argv, "--limit", strconv.Itoa(filter.Limit))
	}
	if filter.Detailed {
		argv = append(argv, "--detailed")
	}
	for _, issueType := range filter.Types {
		argv = appendNonEmptyPair(argv, "--type", issueType)
	}
	for _, priority := range filter.Priorities {
		argv = append(argv, "--priority", strconv.Itoa(priority))
	}
	for _, label := range filter.Labels {
		argv = appendNonEmptyPair(argv, "--label", label)
	}
	return appendCommon(argv, filter.Common)
}

func SearchArgv(query string, filter ListFilter) ([]string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("%w: query is required", ErrInvalidRequest)
	}
	argv := []string{ToolName, "search", query, "--json"}
	listArgs := ListArgv(filter)
	if len(listArgs) > 3 {
		argv = append(argv, listArgs[3:]...)
	}
	return argv, nil
}

func ShowArgv(ids ...string) ([]string, error) {
	clean, err := cleanIDs(ids)
	if err != nil {
		return nil, err
	}
	argv := append([]string{ToolName, "show"}, clean...)
	return append(argv, "--json"), nil
}

func DepTreeArgv(req DepTreeRequest) ([]string, error) {
	issueID := strings.TrimSpace(req.IssueID)
	if issueID == "" {
		return nil, fmt.Errorf("%w: issue id is required", ErrInvalidRequest)
	}
	argv := []string{ToolName, "dep", "tree", issueID, "--json"}
	argv = appendNonEmptyPair(argv, "--direction", req.Direction)
	if req.MaxDepth > 0 {
		argv = append(argv, "--max-depth", strconv.Itoa(req.MaxDepth))
	}
	return appendCommon(argv, req.Common), nil
}

func DepCyclesArgv(common CommonOptions) []string {
	return appendCommon([]string{ToolName, "dep", "cycles", "--json"}, common)
}

func DepListArgv(req DepListRequest) ([]string, error) {
	issueID := strings.TrimSpace(req.IssueID)
	if issueID == "" {
		return nil, fmt.Errorf("%w: issue id is required", ErrInvalidRequest)
	}
	argv := []string{ToolName, "dep", "list", issueID, "--json"}
	argv = appendNonEmptyPair(argv, "--direction", req.Direction)
	argv = appendNonEmptyPair(argv, "--type", req.DepType)
	return appendCommon(argv, req.Common), nil
}

func CreateArgv(req CreateRequest) ([]string, error) {
	if strings.TrimSpace(req.Title) == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidRequest)
	}
	argv := []string{ToolName, "create", "--title", strings.TrimSpace(req.Title), "--json"}
	argv = appendNonEmptyPair(argv, "--type", req.IssueType)
	argv = appendNonEmptyPair(argv, "--priority", req.Priority)
	argv = appendNonEmptyPair(argv, "--description", req.Description)
	argv = appendNonEmptyPair(argv, "--assignee", req.Assignee)
	argv = appendNonEmptyPair(argv, "--owner", req.Owner)
	if len(req.Labels) > 0 {
		argv = appendNonEmptyPair(argv, "--labels", strings.Join(trimNonEmpty(req.Labels), ","))
	}
	argv = appendNonEmptyPair(argv, "--parent", req.Parent)
	argv = appendNonEmptyPair(argv, "--deps", req.Deps)
	if req.EstimateMin > 0 {
		argv = append(argv, "--estimate", strconv.Itoa(req.EstimateMin))
	}
	argv = appendNonEmptyPair(argv, "--due", req.Due)
	argv = appendNonEmptyPair(argv, "--defer", req.Defer)
	argv = appendNonEmptyPair(argv, "--external-ref", req.ExternalRef)
	argv = appendNonEmptyPair(argv, "--status", req.Status)
	if req.Ephemeral {
		argv = append(argv, "--ephemeral")
	}
	if req.DryRun {
		argv = append(argv, "--dry-run")
	}
	return appendCommon(argv, req.Common), nil
}

func UpdateArgv(req UpdateRequest) ([]string, error) {
	ids, err := cleanIDs(req.IDs)
	if err != nil {
		return nil, err
	}
	argv := append([]string{ToolName, "update"}, ids...)
	argv = append(argv, "--json")
	changed := false
	appendChange := func(flag, value string) {
		before := len(argv)
		argv = appendNonEmptyPair(argv, flag, value)
		changed = changed || len(argv) != before
	}
	appendChange("--title", req.Title)
	appendChange("--description", req.Description)
	appendChange("--design", req.Design)
	appendChange("--acceptance-criteria", req.AcceptanceCriteria)
	appendChange("--notes", req.Notes)
	appendChange("--status", req.Status)
	appendChange("--priority", req.Priority)
	appendChange("--type", req.IssueType)
	appendChange("--assignee", req.Assignee)
	appendChange("--owner", req.Owner)
	if req.Claim {
		argv = append(argv, "--claim")
		changed = true
	}
	if req.Force {
		argv = append(argv, "--force")
	}
	appendChange("--due", req.Due)
	appendChange("--defer", req.Defer)
	appendChange("--estimate", req.Estimate)
	for _, label := range req.AddLabels {
		before := len(argv)
		argv = appendNonEmptyPair(argv, "--add-label", label)
		changed = changed || len(argv) != before
	}
	for _, label := range req.RemoveLabels {
		before := len(argv)
		argv = appendNonEmptyPair(argv, "--remove-label", label)
		changed = changed || len(argv) != before
	}
	for _, label := range req.SetLabels {
		before := len(argv)
		argv = appendNonEmptyPair(argv, "--set-labels", label)
		changed = changed || len(argv) != before
	}
	appendChange("--parent", req.Parent)
	appendChange("--external-ref", req.ExternalRef)
	appendChange("--session", req.Session)
	if !changed {
		return nil, fmt.Errorf("%w: update requires at least one changed field", ErrInvalidRequest)
	}
	return appendCommon(argv, req.Common), nil
}

func CloseArgv(req CloseRequest) ([]string, error) {
	ids, err := cleanIDs(req.IDs)
	if err != nil {
		return nil, err
	}
	argv := append([]string{ToolName, "close"}, ids...)
	argv = append(argv, "--json")
	argv = appendNonEmptyPair(argv, "--reason", req.Reason)
	if req.Force {
		argv = append(argv, "--force")
	}
	if req.SuggestNext {
		argv = append(argv, "--suggest-next")
	}
	argv = appendNonEmptyPair(argv, "--session", req.Session)
	return appendCommon(argv, req.Common), nil
}

func ReopenArgv(req ReopenRequest) ([]string, error) {
	ids, err := cleanIDs(req.IDs)
	if err != nil {
		return nil, err
	}
	argv := append([]string{ToolName, "reopen"}, ids...)
	argv = append(argv, "--json")
	argv = appendNonEmptyPair(argv, "--reason", req.Reason)
	return appendCommon(argv, req.Common), nil
}

func DepAddArgv(req DepRequest) ([]string, error) {
	issueID, dependsOn, err := cleanDepIDs(req)
	if err != nil {
		return nil, err
	}
	argv := []string{ToolName, "dep", "add", issueID, dependsOn, "--json"}
	argv = appendNonEmptyPair(argv, "--type", req.DepType)
	argv = appendNonEmptyPair(argv, "--metadata", req.Metadata)
	return appendCommon(argv, req.Common), nil
}

func DepRemoveArgv(req DepRequest) ([]string, error) {
	issueID, dependsOn, err := cleanDepIDs(req)
	if err != nil {
		return nil, err
	}
	return appendCommon([]string{ToolName, "dep", "remove", issueID, dependsOn, "--json"}, req.Common), nil
}

func SyncFlushOnlyArgv(common CommonOptions) []string {
	return appendCommon([]string{ToolName, "sync", "--flush-only", "--json"}, common)
}

func DoctorArgv(common CommonOptions) []string {
	return appendCommon([]string{ToolName, "doctor", "--json"}, common)
}

func SchemaArgv(target string, common CommonOptions) []string {
	argv := []string{ToolName, "schema"}
	if strings.TrimSpace(target) != "" {
		argv = append(argv, strings.TrimSpace(target))
	}
	argv = append(argv, "--json")
	return appendCommon(argv, common)
}

func VersionArgv() []string {
	return []string{ToolName, "version", "--short"}
}

func SyncAfterWritePlan(commitMessage string, common CommonOptions) (SyncPlan, error) {
	commitMessage = strings.TrimSpace(commitMessage)
	if commitMessage == "" {
		return SyncPlan{}, fmt.Errorf("%w: commit message is required", ErrInvalidRequest)
	}
	return SyncPlan{Steps: []SyncStep{
		{
			Name:        "flush beads jsonl",
			Tool:        ToolName,
			Argv:        SyncFlushOnlyArgv(common),
			AuditAction: CapabilitySyncFlushOnly,
			Mutating:    true,
		},
		{
			Name:        "stage beads state",
			Tool:        "git",
			Argv:        []string{"git", "add", ".beads/"},
			AuditAction: "git.add.beads",
			Mutating:    true,
		},
		{
			Name:        "commit beads state",
			Tool:        "git",
			Argv:        []string{"git", "commit", "-m", commitMessage},
			AuditAction: "git.commit.beads",
			Mutating:    true,
		},
	}}, nil
}

func (a *Adapter) List(ctx context.Context, filter ListFilter) (ListResponse, error) {
	raw, err := a.runRawJSON(ctx, ListArgv(filter))
	if err != nil {
		return ListResponse{}, err
	}
	return ParseListResponse(raw)
}

func (a *Adapter) Ready(ctx context.Context, filter ReadyFilter) ([]Issue, error) {
	raw, err := a.runRawJSON(ctx, ReadyArgv(filter))
	if err != nil {
		return nil, err
	}
	return ParseIssueRows(raw)
}

func (a *Adapter) Blocked(ctx context.Context, filter BlockedFilter) ([]Issue, error) {
	raw, err := a.runRawJSON(ctx, BlockedArgv(filter))
	if err != nil {
		return nil, err
	}
	return ParseIssueRows(raw)
}

func (a *Adapter) Search(ctx context.Context, query string, filter ListFilter) (ListResponse, error) {
	argv, err := SearchArgv(query, filter)
	if err != nil {
		return ListResponse{}, err
	}
	raw, err := a.runRawJSON(ctx, argv)
	if err != nil {
		return ListResponse{}, err
	}
	return ParseListResponse(raw)
}

func (a *Adapter) Show(ctx context.Context, ids ...string) ([]Issue, error) {
	argv, err := ShowArgv(ids...)
	if err != nil {
		return nil, err
	}
	raw, err := a.runRawJSON(ctx, argv)
	if err != nil {
		return nil, err
	}
	return ParseIssueRows(raw)
}

func (a *Adapter) DepTree(ctx context.Context, req DepTreeRequest) (json.RawMessage, error) {
	argv, err := DepTreeArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) DepList(ctx context.Context, req DepListRequest) (json.RawMessage, error) {
	argv, err := DepListArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) DepCycles(ctx context.Context, common CommonOptions) (CycleReport, error) {
	raw, err := a.runRawJSON(ctx, DepCyclesArgv(common))
	if err != nil {
		return CycleReport{}, err
	}
	return ParseCycleReport(raw)
}

func (a *Adapter) Create(ctx context.Context, req CreateRequest) (json.RawMessage, error) {
	argv, err := CreateArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Update(ctx context.Context, req UpdateRequest) (json.RawMessage, error) {
	argv, err := UpdateArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Close(ctx context.Context, req CloseRequest) (json.RawMessage, error) {
	argv, err := CloseArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) Reopen(ctx context.Context, req ReopenRequest) (json.RawMessage, error) {
	argv, err := ReopenArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) DepAdd(ctx context.Context, req DepRequest) (json.RawMessage, error) {
	argv, err := DepAddArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) DepRemove(ctx context.Context, req DepRequest) (json.RawMessage, error) {
	argv, err := DepRemoveArgv(req)
	if err != nil {
		return nil, err
	}
	return a.runRawJSON(ctx, argv)
}

func (a *Adapter) SyncFlushOnly(ctx context.Context, common CommonOptions) (json.RawMessage, error) {
	return a.runRawJSON(ctx, SyncFlushOnlyArgv(common))
}

func (a *Adapter) Doctor(ctx context.Context, common CommonOptions) (json.RawMessage, error) {
	return a.runRawJSON(ctx, DoctorArgv(common))
}

func (a *Adapter) Schema(ctx context.Context, target string, common CommonOptions) (json.RawMessage, error) {
	return a.runRawJSON(ctx, SchemaArgv(target, common))
}

func (a *Adapter) Probe(ctx context.Context) (*capabilities.ToolReport, error) {
	report := &capabilities.ToolReport{
		Tool:          capabilities.ToolBR,
		Source:        "cli",
		LastCheckedAt: a.now().UTC().Format(time.RFC3339),
		Capabilities:  missingCapabilities("not probed"),
	}

	version, err := a.runText(ctx, VersionArgv())
	if err != nil {
		note := err.Error()
		state := statusForError(err)
		report.Capabilities = missingCapabilities(note)
		for capID, cap := range report.Capabilities {
			if capID == CapabilityTUI && state != capabilities.StatusMissing {
				report.Capabilities[capID] = capabilities.Capability{Status: capabilities.StatusBlockedByPolicy, Notes: "bare br/tui surfaces are blocked by Hoopoe policy"}
				continue
			}
			cap.Status = state
			cap.Notes = note
			report.Capabilities[capID] = cap
		}
		return report, nil
	}
	report.Version = strings.TrimSpace(string(version))
	report.Capabilities[CapabilityPresent] = capabilities.Capability{Status: capabilities.StatusOK}
	report.Capabilities[CapabilityTUI] = capabilities.Capability{
		Status: capabilities.StatusBlockedByPolicy,
		Notes:  "bare br/tui surfaces are blocked by Hoopoe policy; daemon uses typed JSON subcommands",
	}

	list, err := a.List(ctx, ListFilter{Limit: 1})
	if err != nil {
		state := statusForError(err)
		report.Capabilities[CapabilityIssuesRead] = capabilities.Capability{Status: state, Notes: err.Error()}
		for _, capID := range []string{
			CapabilityIssuesUpdate,
			CapabilityReady,
			CapabilityCreate,
			CapabilityClose,
			CapabilityDepAdd,
			CapabilityDepRemove,
			CapabilitySyncFlushOnly,
			CapabilitySyncFlush,
			CapabilityDoctor,
			CapabilitySchema,
		} {
			report.Capabilities[capID] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: "br present but list probe failed: " + err.Error()}
		}
		report.Capabilities[CapabilityDepCycles] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: "br present but list probe failed: " + err.Error()}
		return report, nil
	}
	if list.Limit == 0 && len(list.Issues) > 0 {
		report.Capabilities[CapabilityIssuesRead] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: "list payload omitted a useful limit for pagination"}
	} else {
		report.Capabilities[CapabilityIssuesRead] = capabilities.Capability{Status: capabilities.StatusOK}
	}
	for _, capID := range []string{
		CapabilityIssuesUpdate,
		CapabilityReady,
		CapabilityCreate,
		CapabilityClose,
		CapabilityDepAdd,
		CapabilityDepRemove,
		CapabilitySyncFlushOnly,
		CapabilitySyncFlush,
		CapabilityDoctor,
		CapabilitySchema,
	} {
		report.Capabilities[capID] = capabilities.Capability{Status: capabilities.StatusOK}
	}
	report.Capabilities[CapabilitySyncFlushOnly] = capabilities.Capability{
		Status: capabilities.StatusOK,
		Notes:  "br sync --flush-only is non-invasive; daemon must run git add/commit separately",
	}
	report.Capabilities[CapabilitySyncFlush] = report.Capabilities[CapabilitySyncFlushOnly]

	cycles, err := a.DepCycles(ctx, CommonOptions{})
	if err != nil {
		report.Capabilities[CapabilityDepCycles] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: err.Error()}
		return report, nil
	}
	if err := EnsureNoCycles(cycles); err != nil {
		report.Capabilities[CapabilityDepCycles] = capabilities.Capability{Status: capabilities.StatusDegraded, Notes: err.Error()}
		return report, nil
	}
	report.Capabilities[CapabilityDepCycles] = capabilities.Capability{Status: capabilities.StatusOK}
	return report, nil
}

func ParseListResponse(data []byte) (ListResponse, error) {
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return ListResponse{}, fmt.Errorf("br: decode list JSON: %w", err)
	}
	required := []string{"issues", "total", "limit", "offset", "has_more"}
	for _, key := range required {
		if _, ok := keys[key]; !ok {
			return ListResponse{}, fmt.Errorf("%w: missing %s", ErrListContract, key)
		}
	}
	var response ListResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return ListResponse{}, fmt.Errorf("br: decode list response: %w", err)
	}
	if response.Total < 0 || response.Limit < 0 || response.Offset < 0 {
		return ListResponse{}, fmt.Errorf("%w: negative pagination field", ErrListContract)
	}
	if response.Total < len(response.Issues) {
		return ListResponse{}, fmt.Errorf("%w: total smaller than issues length", ErrListContract)
	}
	return response, nil
}

func ParseIssueRows(data []byte) ([]Issue, error) {
	var issues []Issue
	if err := json.Unmarshal(data, &issues); err == nil {
		return issues, nil
	}
	list, err := ParseListResponse(data)
	if err != nil {
		return nil, fmt.Errorf("br: decode issue rows: %w", err)
	}
	return list.Issues, nil
}

func ParseCycleReport(data []byte) (CycleReport, error) {
	var wrapped CycleReport
	if err := json.Unmarshal(data, &wrapped); err == nil && (wrapped.Cycles != nil || wrapped.Count != 0) {
		if wrapped.Count == 0 {
			wrapped.Count = len(wrapped.Cycles)
		}
		return wrapped, nil
	}
	var rawCycles [][]string
	if err := json.Unmarshal(data, &rawCycles); err == nil {
		return CycleReport{Cycles: rawCycles, Count: len(rawCycles)}, nil
	}
	return CycleReport{}, fmt.Errorf("br: decode dependency cycles JSON")
}

func EnsureNoCycles(report CycleReport) error {
	if report.Count > 0 || len(report.Cycles) > 0 {
		if len(report.Cycles) == 0 {
			report.Cycles = make([][]string, report.Count)
		}
		return DependencyCycleError{Cycles: report.Cycles}
	}
	return nil
}

func LoadReadModel(path string) (ReadModel, error) {
	file, err := os.Open(path)
	if err != nil {
		return ReadModel{}, fmt.Errorf("br: open issues jsonl %s: %w", path, err)
	}
	defer file.Close()

	model := ReadModel{
		ByID:     map[string]Issue{},
		ByStatus: map[string][]Issue{},
		ByOwner:  map[string][]Issue{},
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var issue Issue
		if err := json.Unmarshal(line, &issue); err != nil {
			return ReadModel{}, fmt.Errorf("br: parse issues jsonl %s line %d: %w", path, lineNo, err)
		}
		if strings.TrimSpace(issue.ID) == "" {
			return ReadModel{}, fmt.Errorf("br: parse issues jsonl %s line %d: missing id", path, lineNo)
		}
		model.Issues = append(model.Issues, issue)
		model.ByID[issue.ID] = issue
		if issue.Status != "" {
			model.ByStatus[issue.Status] = append(model.ByStatus[issue.Status], issue)
		}
		if issue.Owner != "" {
			model.ByOwner[issue.Owner] = append(model.ByOwner[issue.Owner], issue)
		}
	}
	if err := scanner.Err(); err != nil {
		return ReadModel{}, fmt.Errorf("br: scan issues jsonl %s: %w", path, err)
	}
	return model, nil
}

func (m ReadModel) Query(q ReadModelQuery) []Issue {
	var candidates []Issue
	if q.ID != "" {
		issue, ok := m.ByID[q.ID]
		if !ok {
			return nil
		}
		candidates = []Issue{issue}
	} else if q.Status != "" {
		candidates = append(candidates, m.ByStatus[q.Status]...)
	} else if q.Owner != "" {
		candidates = append(candidates, m.ByOwner[q.Owner]...)
	} else {
		candidates = append(candidates, m.Issues...)
	}
	out := candidates[:0]
	for _, issue := range candidates {
		if q.Status != "" && issue.Status != q.Status {
			continue
		}
		if q.Owner != "" && issue.Owner != q.Owner {
			continue
		}
		if q.Assignee != "" && issue.Assignee != q.Assignee {
			continue
		}
		if q.UseMinMax && (issue.Priority < q.MinPrio || issue.Priority > q.MaxPrio) {
			continue
		}
		out = append(out, issue)
	}
	return out
}

func (a *Adapter) runRawJSON(ctx context.Context, argv []string) (json.RawMessage, error) {
	result, err := a.run(ctx, argv)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(result.Stdout)) == 0 {
		return nil, fmt.Errorf("br: empty JSON response from %v", argv)
	}
	if max := a.maxStdoutBytes(); max > 0 && len(result.Stdout) > max {
		return nil, outputTooLargeError{limit: max, got: len(result.Stdout), argv: argv}
	}
	var raw json.RawMessage
	if err := json.Unmarshal(result.Stdout, &raw); err != nil {
		return nil, fmt.Errorf("br: decode JSON from %v: %w", argv, err)
	}
	return raw, nil
}

func (a *Adapter) runText(ctx context.Context, argv []string) ([]byte, error) {
	result, err := a.run(ctx, argv)
	if err != nil {
		return nil, err
	}
	if max := a.maxStdoutBytes(); max > 0 && len(result.Stdout) > max {
		return nil, outputTooLargeError{limit: max, got: len(result.Stdout), argv: argv}
	}
	return result.Stdout, nil
}

func (a *Adapter) run(ctx context.Context, argv []string) (CommandResult, error) {
	if a == nil {
		return CommandResult{}, fmt.Errorf("%w: nil adapter", ErrInvalidRequest)
	}
	runner := a.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, argv)
	if err != nil {
		return CommandResult{}, fmt.Errorf("br: run %s: %w", argv[0], err)
	}
	if result.ExitCode != 0 {
		return CommandResult{}, commandError{argv: argv, result: result}
	}
	return result, nil
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	stderr := strings.TrimSpace(string(e.result.Stderr))
	if stderr == "" {
		stderr = strings.TrimSpace(string(e.result.Stdout))
	}
	return fmt.Sprintf("br: command %v exited %d: %s", e.argv, e.result.ExitCode, stderr)
}

type outputTooLargeError struct {
	limit int
	got   int
	argv  []string
}

func (e outputTooLargeError) Error() string {
	return fmt.Sprintf("%v: %v produced %d bytes, limit %d", ErrOutputTooLarge, e.argv, e.got, e.limit)
}

func (e outputTooLargeError) Unwrap() error {
	return ErrOutputTooLarge
}

func statusForError(err error) capabilities.CapabilityStatus {
	var commandErr commandError
	if errors.As(err, &commandErr) {
		switch commandErr.result.ExitCode {
		case 124:
			return capabilities.StatusDegraded
		case 127:
			return capabilities.StatusMissing
		default:
			return capabilities.StatusDegraded
		}
	}
	if errors.Is(err, ErrOutputTooLarge) || strings.Contains(err.Error(), "decode JSON") || strings.Contains(err.Error(), ErrListContract.Error()) {
		return capabilities.StatusDegraded
	}
	if errors.Is(err, exec.ErrNotFound) || strings.Contains(err.Error(), "executable file not found") || strings.Contains(err.Error(), "command not found") {
		return capabilities.StatusMissing
	}
	return capabilities.StatusDegraded
}

func missingCapabilities(note string) map[string]capabilities.Capability {
	caps := map[string]capabilities.Capability{}
	for _, capID := range []string{
		CapabilityPresent,
		CapabilityIssuesRead,
		CapabilityIssuesUpdate,
		CapabilityReady,
		CapabilityCreate,
		CapabilityClose,
		CapabilityDepAdd,
		CapabilityDepRemove,
		CapabilityDepCycles,
		CapabilitySyncFlushOnly,
		CapabilitySyncFlush,
		CapabilityDoctor,
		CapabilitySchema,
		CapabilityTUI,
	} {
		caps[capID] = capabilities.Capability{Status: capabilities.StatusMissing, Notes: note}
	}
	return caps
}

func appendCommon(argv []string, common CommonOptions) []string {
	argv = appendNonEmptyPair(argv, "--db", common.DB)
	argv = appendNonEmptyPair(argv, "--actor", common.Actor)
	if common.NoAutoFlush {
		argv = append(argv, "--no-auto-flush")
	}
	if common.NoAutoImport {
		argv = append(argv, "--no-auto-import")
	}
	if common.AllowStale {
		argv = append(argv, "--allow-stale")
	}
	if common.NoDB {
		argv = append(argv, "--no-db")
	}
	if common.LockTimeoutMS > 0 {
		argv = append(argv, "--lock-timeout", strconv.Itoa(common.LockTimeoutMS))
	}
	if common.NoColor {
		argv = append(argv, "--no-color")
	}
	if common.Quiet {
		argv = append(argv, "--quiet")
	}
	for i := 0; i < common.Verbose; i++ {
		argv = append(argv, "-v")
	}
	return argv
}

func appendNonEmptyPair(argv []string, flag, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return argv
	}
	return append(argv, flag, value)
}

func trimNonEmpty(values []string) []string {
	out := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func cleanIDs(ids []string) ([]string, error) {
	clean := trimNonEmpty(append([]string(nil), ids...))
	if len(clean) == 0 {
		return nil, fmt.Errorf("%w: at least one issue id is required", ErrInvalidRequest)
	}
	return clean, nil
}

func cleanDepIDs(req DepRequest) (string, string, error) {
	issueID := strings.TrimSpace(req.IssueID)
	dependsOn := strings.TrimSpace(req.DependsOn)
	if issueID == "" || dependsOn == "" {
		return "", "", fmt.Errorf("%w: issue id and dependency id are required", ErrInvalidRequest)
	}
	if issueID == dependsOn {
		return "", "", fmt.Errorf("%w: issue cannot depend on itself", ErrInvalidRequest)
	}
	return issueID, dependsOn, nil
}

func (a *Adapter) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *Adapter) maxStdoutBytes() int {
	if a == nil || a.MaxStdoutBytes == 0 {
		return defaultMaxStdoutBytes
	}
	if a.MaxStdoutBytes < 0 {
		return 0
	}
	return a.MaxStdoutBytes
}
