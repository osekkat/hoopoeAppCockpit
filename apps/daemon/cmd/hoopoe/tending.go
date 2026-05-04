package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/agent"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/scheduler"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/tending/prescript"
)

type tendingIO struct {
	Stdout         io.Writer
	Stderr         io.Writer
	Stdin          io.Reader
	Now            func() time.Time
	StatePath      string
	DefinitionsDir string
	AuditPath      string
	Editor         string
}

func runTending(ctx context.Context, args []string, io *tendingIO) error {
	if err := io.setDefaults(); err != nil {
		return err
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printTendingUsage(io.Stdout)
		return nil
	}
	switch args[0] {
	case "list":
		return runTendingList(ctx, args[1:], io)
	case "create":
		return runTendingCreate(ctx, args[1:], io)
	case "edit":
		return runTendingEdit(ctx, args[1:], io)
	case "pause":
		return runTendingPause(ctx, args[1:], io)
	case "resume":
		return runTendingResume(ctx, args[1:], io)
	case "run":
		return runTendingRun(ctx, args[1:], io)
	case "remove":
		return runTendingRemove(ctx, args[1:], io)
	case "status":
		return runTendingStatus(ctx, args[1:], io)
	case "tick":
		return runTendingTick(ctx, args[1:], io)
	default:
		fmt.Fprintf(io.Stderr, "hoopoe tending: unknown subcommand %q\n", args[0])
		printTendingUsage(io.Stderr)
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func (io *tendingIO) setDefaults() error {
	if io.Stdout == nil {
		io.Stdout = os.Stdout
	}
	if io.Stderr == nil {
		io.Stderr = os.Stderr
	}
	if io.Stdin == nil {
		io.Stdin = os.Stdin
	}
	if io.Now == nil {
		io.Now = time.Now
	}
	if io.StatePath != "" && io.DefinitionsDir != "" && io.AuditPath != "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("hoopoe tending: resolve home dir: %w", err)
	}
	base := filepath.Join(home, ".hoopoe")
	if io.StatePath == "" {
		io.StatePath = filepath.Join(base, "tending", "scheduler-state.json")
	}
	if io.DefinitionsDir == "" {
		io.DefinitionsDir = filepath.Join(base, "tending", "jobs.d")
	}
	if io.AuditPath == "" {
		io.AuditPath = filepath.Join(base, "audit.jsonl")
	}
	return nil
}

func printTendingUsage(w io.Writer) {
	fmt.Fprint(w, `usage: hoopoe tending <subcommand> [args]

subcommands:
  list [--json]
  create <id> --schedule <expr> [--kind <type>] [--name <name>] [--skills a,b]
  edit <id> [--editor <path>]
  pause <id> [--actor <name>] [--json]
  resume <id> [--actor <name>] [--json]
  run <id> [--json]
  remove <id> --yes [--actor <name>] [--json]
  status [<id>] [--json]
  tick [--json]

run 'hoopoe tending <subcommand> --help' for command-specific flags.
`)
}

func runTendingList(ctx context.Context, args []string, io *tendingIO) error {
	flags := newTendingFlagSet("hoopoe tending list", io)
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	registry, err := openTendingRegistry(ctx, io)
	if err != nil {
		return err
	}
	jobs, err := registry.ListJobs(ctx)
	if err != nil {
		return err
	}
	views := jobViews(jobs)
	if *asJSON {
		return writeJSONIndented(io.Stdout, views)
	}
	if len(views) == 0 {
		fmt.Fprintln(io.Stdout, "no tending jobs on file.")
		return nil
	}
	fmt.Fprintln(io.Stdout, "JOBID                  STATUS          SCHEDULE                  NEXT RUN                  LAST DECISION")
	for _, job := range views {
		fmt.Fprintf(io.Stdout, "%-22s %-15s %-25s %-25s %s\n",
			job.JobID, job.Status, job.Schedule, job.NextRunAt, job.LastDecision)
	}
	return nil
}

func runTendingCreate(ctx context.Context, args []string, io *tendingIO) error {
	jobID, flagArgs, err := splitPositional(args)
	if err != nil {
		fmt.Fprintln(io.Stderr, "usage: hoopoe tending create <id> --schedule <expr>")
		return err
	}
	flags := newTendingFlagSet("hoopoe tending create", io)
	name := flags.String("name", jobID, "job display name")
	kind := flags.String("kind", string(scheduler.KindDeterministic), "job kind")
	scheduleRaw := flags.String("schedule", "", "schedule expression")
	skills := flags.String("skills", "", "comma-separated skill ids")
	script := flags.String("script", "", "pre-script path")
	prompt := flags.String("prompt", "", "prompt template")
	deliver := flags.String("deliver", "hoopoe_activity_panel", "delivery target")
	paused := flags.Bool("paused", false, "create job paused")
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	if strings.TrimSpace(*scheduleRaw) == "" {
		return fmt.Errorf("hoopoe tending create: --schedule is required")
	}
	schedule, err := scheduler.ParseSchedule(*scheduleRaw)
	if err != nil {
		return err
	}
	definition := scheduler.Definition{
		ID:          jobID,
		Name:        *name,
		Kind:        scheduler.JobKind(*kind),
		Version:     scheduler.SchemaVersion,
		Schedule:    schedule,
		Script:      *script,
		Skills:      commaList(*skills),
		Prompt:      *prompt,
		Deliver:     *deliver,
		Repeat:      scheduler.RepeatForever(),
		Paused:      *paused,
		AuditAlways: true,
	}
	if err := scheduler.WriteDefinitionFile(ctx, definitionPath(io, jobID), definition); err != nil {
		return fmt.Errorf("hoopoe tending create: write definition: %w", err)
	}
	registry, err := openTendingRegistry(ctx, io)
	if err != nil {
		return err
	}
	job, err := registry.ImportDefinition(ctx, definition)
	if err != nil {
		return err
	}
	if err := appendTendingAudit(io, "tending.job.created", map[string]any{"jobId": jobID}); err != nil {
		fmt.Fprintf(io.Stderr, "warn: audit write failed: %v\n", err)
	}
	if *asJSON {
		return writeJSONIndented(io.Stdout, jobView(job))
	}
	fmt.Fprintf(io.Stdout, "tending job %s created (%s)\n", job.Definition.ID, job.Definition.Schedule.String())
	return nil
}

func runTendingEdit(ctx context.Context, args []string, io *tendingIO) error {
	jobID, flagArgs, err := splitPositional(args)
	if err != nil {
		fmt.Fprintln(io.Stderr, "usage: hoopoe tending edit <id>")
		return err
	}
	flags := newTendingFlagSet("hoopoe tending edit", io)
	editor := flags.String("editor", io.Editor, "editor executable")
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	if *editor == "" {
		if fromEnv := os.Getenv("EDITOR"); fromEnv != "" {
			*editor = fromEnv
		}
	}
	if *editor == "" {
		return fmt.Errorf("hoopoe tending edit: EDITOR is required")
	}
	path := definitionPath(io, jobID)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		registry, err := openTendingRegistry(ctx, io)
		if err != nil {
			return err
		}
		job, err := registry.GetJob(ctx, jobID)
		if err != nil {
			return err
		}
		if err := scheduler.WriteDefinitionFile(ctx, path, job.Definition); err != nil {
			return err
		}
	}
	cmd := exec.CommandContext(ctx, *editor, path)
	cmd.Stdin = io.Stdin
	cmd.Stdout = io.Stdout
	cmd.Stderr = io.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("hoopoe tending edit: %w", err)
	}
	return appendTendingAudit(io, "tending.job.edited", map[string]any{"jobId": jobID, "path": path})
}

func runTendingPause(ctx context.Context, args []string, io *tendingIO) error {
	return runTendingStateChange(ctx, args, io, "pause", func(registry *scheduler.Registry, id string) (scheduler.Job, error) {
		return registry.PauseJob(ctx, id)
	})
}

func runTendingResume(ctx context.Context, args []string, io *tendingIO) error {
	return runTendingStateChange(ctx, args, io, "resume", func(registry *scheduler.Registry, id string) (scheduler.Job, error) {
		return registry.ResumeJob(ctx, id)
	})
}

func runTendingStateChange(ctx context.Context, args []string, io *tendingIO, verb string, change func(*scheduler.Registry, string) (scheduler.Job, error)) error {
	jobID, flagArgs, err := splitPositional(args)
	if err != nil {
		fmt.Fprintf(io.Stderr, "usage: hoopoe tending %s <id>\n", verb)
		return err
	}
	flags := newTendingFlagSet("hoopoe tending "+verb, io)
	actor := flags.String("actor", defaultTendingActor(), "actor stamped on the audit entry")
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	registry, err := openTendingRegistry(ctx, io)
	if err != nil {
		return err
	}
	job, err := change(registry, jobID)
	if err != nil {
		return err
	}
	if err := appendTendingAudit(io, "tending.job."+verb+"d", map[string]any{"jobId": jobID, "actor": *actor}); err != nil {
		fmt.Fprintf(io.Stderr, "warn: audit write failed: %v\n", err)
	}
	if *asJSON {
		return writeJSONIndented(io.Stdout, jobView(job))
	}
	fmt.Fprintf(io.Stdout, "tending job %s %sd by %s\n", jobID, verb, *actor)
	return nil
}

func runTendingRun(ctx context.Context, args []string, io *tendingIO) error {
	jobID, flagArgs, err := splitPositional(args)
	if err != nil {
		fmt.Fprintln(io.Stderr, "usage: hoopoe tending run <id>")
		return err
	}
	flags := newTendingFlagSet("hoopoe tending run", io)
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	registry, err := openTendingRegistry(ctx, io)
	if err != nil {
		return err
	}
	sched, err := newTendingScheduler(io, registry)
	if err != nil {
		return err
	}
	decision, err := sched.RunNow(ctx, jobID)
	if err != nil {
		return err
	}
	sched.Wait()
	if err := appendTendingAudit(io, "tending.job.run_now", map[string]any{"jobId": jobID, "outcome": decision.Outcome}); err != nil {
		fmt.Fprintf(io.Stderr, "warn: audit write failed: %v\n", err)
	}
	if *asJSON {
		return writeJSONIndented(io.Stdout, decision)
	}
	fmt.Fprintf(io.Stdout, "tending job %s run requested: %s\n", jobID, decision.Outcome)
	return nil
}

func runTendingRemove(ctx context.Context, args []string, io *tendingIO) error {
	jobID, flagArgs, err := splitPositional(args)
	if err != nil {
		fmt.Fprintln(io.Stderr, "usage: hoopoe tending remove <id> --yes")
		return err
	}
	flags := newTendingFlagSet("hoopoe tending remove", io)
	actor := flags.String("actor", defaultTendingActor(), "actor stamped on the audit entry")
	yes := flags.Bool("yes", false, "confirm removal")
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	if !*yes {
		fmt.Fprintln(io.Stdout, "aborted. pass --yes to remove a tending job from runtime state.")
		return nil
	}
	registry, err := openTendingRegistry(ctx, io)
	if err != nil {
		return err
	}
	job, err := registry.RemoveJob(ctx, jobID)
	if err != nil {
		return err
	}
	if err := appendTendingAudit(io, "tending.job.removed", map[string]any{"jobId": jobID, "actor": *actor}); err != nil {
		fmt.Fprintf(io.Stderr, "warn: audit write failed: %v\n", err)
	}
	if *asJSON {
		return writeJSONIndented(io.Stdout, jobView(job))
	}
	fmt.Fprintf(io.Stdout, "tending job %s removed from runtime state by %s\n", jobID, *actor)
	return nil
}

func runTendingStatus(ctx context.Context, args []string, io *tendingIO) error {
	flags := newTendingFlagSet("hoopoe tending status", io)
	asJSON := flags.Bool("json", false, "emit JSON")
	jobID, flagArgs, err := optionalPositional(args)
	if err != nil {
		return err
	}
	if err := flags.Parse(flagArgs); err != nil {
		return err
	}
	registry, err := openTendingRegistry(ctx, io)
	if err != nil {
		return err
	}
	if jobID != "" {
		job, err := registry.GetJob(ctx, jobID)
		if err != nil {
			return err
		}
		view := jobView(job)
		if *asJSON {
			return writeJSONIndented(io.Stdout, view)
		}
		fmt.Fprintf(io.Stdout, "%s: %s; schedule=%s; next=%s; last=%s\n", view.JobID, view.Status, view.Schedule, view.NextRunAt, view.LastDecision)
		return nil
	}
	state, err := registry.Snapshot(ctx)
	if err != nil {
		return err
	}
	status := map[string]any{
		"schemaVersion": state.SchemaVersion,
		"jobCount":      len(state.Jobs),
		"runCount":      len(state.Runs),
		"metrics":       state.Metrics,
	}
	if *asJSON {
		return writeJSONIndented(io.Stdout, status)
	}
	fmt.Fprintf(io.Stdout, "jobs=%d runs=%d due=%d started=%d skipped=%d deadLetters=%d\n",
		len(state.Jobs), len(state.Runs), state.Metrics.DueRuns, state.Metrics.StartedRuns, state.Metrics.SkippedRuns, state.Metrics.DeadLetters)
	return nil
}

func runTendingTick(ctx context.Context, args []string, io *tendingIO) error {
	flags := newTendingFlagSet("hoopoe tending tick", io)
	asJSON := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	registry, err := openTendingRegistry(ctx, io)
	if err != nil {
		return err
	}
	sched, err := newTendingScheduler(io, registry)
	if err != nil {
		return err
	}
	decisions, err := sched.Tick(ctx)
	if err != nil {
		return err
	}
	sched.Wait()
	if err := appendTendingAudit(io, "tending.scheduler.tick", map[string]any{"decisions": len(decisions)}); err != nil {
		fmt.Fprintf(io.Stderr, "warn: audit write failed: %v\n", err)
	}
	if *asJSON {
		return writeJSONIndented(io.Stdout, decisions)
	}
	fmt.Fprintf(io.Stdout, "scheduler tick completed (%d decisions)\n", len(decisions))
	return nil
}

func newTendingFlagSet(name string, io *tendingIO) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Stderr)
	flags.StringVar(&io.StatePath, "state", io.StatePath, "scheduler state path")
	flags.StringVar(&io.DefinitionsDir, "definitions", io.DefinitionsDir, "job definitions directory")
	flags.StringVar(&io.AuditPath, "audit", io.AuditPath, "audit JSONL path")
	return flags
}

func openTendingRegistry(ctx context.Context, io *tendingIO) (*scheduler.Registry, error) {
	return scheduler.NewRegistry(ctx, scheduler.RegistryConfig{
		Store:       scheduler.FileStore{Path: io.StatePath},
		Now:         io.Now,
		LeaseHolder: "hoopoe-cli",
		LeaseTTL:    time.Minute,
	})
}

func newTendingScheduler(io *tendingIO, registry *scheduler.Registry) (*scheduler.Scheduler, error) {
	runner, err := newTendingPrescriptRunner(io, registry)
	if err != nil {
		return nil, err
	}
	return scheduler.New(scheduler.Config{
		Registry: registry,
		Runner:   runner,
	})
}

func newTendingPrescriptRunner(io *tendingIO, registry *scheduler.Registry) (*prescript.Runner, error) {
	executor := agent.NewExecutor()
	executor.Now = io.Now
	executor.Audit = tendingAgentAuditSink{io: io}
	runtime := &agent.Runtime{
		Runner:   tendingAgentRunner{},
		Executor: executor,
		Audit:    tendingAgentAuditSink{io: io},
		Now:      io.Now,
	}
	return prescript.New(prescript.Config{
		Definitions: registry,
		Snapshots:   tendingSnapshotSource{registry: registry},
		Scripts:     prescript.ExecScriptInvoker{},
		Executor:    executor,
		Agent:       runtime,
		Now:         io.Now,
	})
}

type tendingSnapshotSource struct {
	registry *scheduler.Registry
}

func (s tendingSnapshotSource) Snapshot(ctx context.Context, job scheduler.Job, run scheduler.Run) (prescript.Snapshot, error) {
	state, err := s.registry.Snapshot(ctx)
	if err != nil {
		return prescript.Snapshot{}, err
	}
	return prescript.Snapshot{
		Canonical: map[string]any{
			"scheduler": state,
			"job":       job,
			"run":       run,
		},
		Capabilities: map[string]any{
			"tending.prescript.runner": true,
			"tending.prescript.exec":   true,
			"tending.action_executor":  true,
			"tending.agent_runtime":    true,
		},
	}, nil
}

type tendingAgentRunner struct{}

func (tendingAgentRunner) RunAgent(context.Context, agent.AgentInvocation) (agent.AgentReply, error) {
	return agent.AgentReply{}, fmt.Errorf("hoopoe tending: agent runtime runner is not configured for the CLI")
}

type tendingAgentAuditSink struct {
	io *tendingIO
}

func (s tendingAgentAuditSink) RecordAuditEvent(ctx context.Context, event agent.AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return appendTendingAudit(s.io, "tending.agent."+event.Action, map[string]any{
		"jobId":          event.JobID,
		"runId":          event.RunID,
		"action":         event.Action,
		"actionKind":     event.ActionKind,
		"idempotencyKey": event.IdempotencyKey,
		"status":         event.Status,
		"reason":         event.Reason,
		"data":           event.Data,
	})
}

func definitionPath(io *tendingIO, jobID string) string {
	return filepath.Join(io.DefinitionsDir, jobID+".json")
}

type tendingJobView struct {
	JobID        string              `json:"jobId"`
	Name         string              `json:"name"`
	Status       string              `json:"status"`
	Schedule     string              `json:"schedule"`
	Kind         scheduler.JobKind   `json:"kind"`
	NextRunAt    string              `json:"nextRunAt,omitempty"`
	LastDecision string              `json:"lastDecision,omitempty"`
	LastRunID    string              `json:"lastRunId,omitempty"`
	RawStatus    scheduler.JobStatus `json:"rawStatus"`
}

func jobViews(jobs []scheduler.Job) []tendingJobView {
	views := make([]tendingJobView, 0, len(jobs))
	for _, job := range jobs {
		views = append(views, jobView(job))
	}
	return views
}

func jobView(job scheduler.Job) tendingJobView {
	view := tendingJobView{
		JobID:     job.Definition.ID,
		Name:      job.Definition.Name,
		Status:    displayJobStatus(job),
		Schedule:  job.Definition.Schedule.String(),
		Kind:      job.Definition.Kind,
		RawStatus: job.Status,
	}
	if job.NextRunAt != nil {
		view.NextRunAt = job.NextRunAt.UTC().Format(time.RFC3339)
	}
	if job.LastDecision != nil {
		view.LastDecision = string(job.LastDecision.Outcome)
		view.LastRunID = job.LastDecision.RunID
	}
	return view
}

func displayJobStatus(job scheduler.Job) string {
	if job.Definition.Paused || job.Status == scheduler.JobStatusPaused {
		return "paused"
	}
	if job.Status == scheduler.JobStatusReady {
		return "active"
	}
	return string(job.Status)
}

func appendTendingAudit(io *tendingIO, kind string, payload map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(io.AuditPath), 0o700); err != nil {
		return err
	}
	entry := map[string]any{
		"ts":      io.Now().UTC().Format(time.RFC3339),
		"kind":    kind,
		"actor":   defaultTendingActor(),
		"payload": payload,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(io.AuditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func defaultTendingActor() string {
	if user := os.Getenv("USER"); user != "" {
		return user + "@cli"
	}
	return "cli"
}

func commaList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func optionalPositional(args []string) (string, []string, error) {
	var positional string
	flagArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		token := args[i]
		if startsWithDash(token) {
			flagArgs = append(flagArgs, token)
			if !containsEquals(token) && i+1 < len(args) && !startsWithDash(args[i+1]) {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
			continue
		}
		if positional != "" {
			return "", nil, fmt.Errorf("unexpected extra positional %q", token)
		}
		positional = token
	}
	return positional, flagArgs, nil
}
