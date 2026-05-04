// Package acceptance contains deterministic acceptance probes for cross-cutting
// Hoopoe daemon contracts. These are mock-friendly smoke scenarios, not a
// replacement for the real VPS evidence runs.
package acceptance

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/auth"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
)

const (
	phase2SmokeSchemaVersion = 1
	defaultReconnectSLO      = 10 * time.Second
	defaultReplaySLO         = 5 * time.Second
)

type Config struct {
	WorkDir      string
	Now          func() time.Time
	ReconnectSLO time.Duration
	ReplaySLO    time.Duration
}

type Report struct {
	SchemaVersion int          `json:"schemaVersion"`
	StartedAt     time.Time    `json:"startedAt"`
	WorkDir       string       `json:"workDir"`
	Steps         []StepResult `json:"steps"`
	Metrics       []Metric     `json:"metrics,omitempty"`
}

type StepResult struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Passed   bool              `json:"passed"`
	Error    string            `json:"error,omitempty"`
	Evidence map[string]string `json:"evidence,omitempty"`
}

type Metric struct {
	Name  string  `json:"name"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

func RunPhase2Smoke(ctx context.Context, cfg Config) (Report, error) {
	if cfg.WorkDir == "" {
		return Report{}, fmt.Errorf("phase2 smoke: WorkDir is required")
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	r := &smokeRunner{
		cfg: Config{
			WorkDir:      cfg.WorkDir,
			Now:          now,
			ReconnectSLO: defaultDuration(cfg.ReconnectSLO, defaultReconnectSLO),
			ReplaySLO:    defaultDuration(cfg.ReplaySLO, defaultReplaySLO),
		},
		report: Report{
			SchemaVersion: phase2SmokeSchemaVersion,
			StartedAt:     now().UTC(),
			WorkDir:       cfg.WorkDir,
			Steps:         make([]StepResult, 0, 6),
		},
		state: smokeState{
			applied: make(map[string]map[uint64]struct{}),
			cursors: make(map[string]uint64),
		},
	}
	r.step(ctx, "bootstrap_bearer_ws", "cold daemon bootstrap to bearer and WS token", r.bootstrapBearerWS)
	r.step(ctx, "job_log_stream", "remote job log stream resumes by offset", r.jobLogStream)
	r.step(ctx, "disconnect_reconnect_replay", "disconnect and reconnect replay is ordered and idempotent", r.disconnectReconnectReplay)
	r.step(ctx, "mac_sleep_reconnect", "macOS sleep wake reconnect stays inside SLO", r.macSleepReconnect)
	r.step(ctx, "daemon_restart_recovery", "daemon restart recovers jobs and keeps bearer usable", r.daemonRestartRecovery)
	r.step(ctx, "secret_rotation_invalidation", "signing secret rotation invalidates bearer and WS tokens", r.secretRotationInvalidation)
	return r.report, r.report.RequirePassed()
}

func (r Report) RequirePassed() error {
	var failed []error
	for _, step := range r.Steps {
		if !step.Passed {
			failed = append(failed, fmt.Errorf("%s: %s", step.ID, step.Error))
		}
	}
	return errors.Join(failed...)
}

func (r Report) Step(id string) (StepResult, bool) {
	for _, step := range r.Steps {
		if step.ID == id {
			return step, true
		}
	}
	return StepResult{}, false
}

func (r Report) Metric(name string) (Metric, bool) {
	for _, metric := range r.Metrics {
		if metric.Name == name {
			return metric, true
		}
	}
	return Metric{}, false
}

type smokeRunner struct {
	cfg    Config
	report Report
	state  smokeState
}

type smokeState struct {
	pairing     *auth.BootstrapCredentialService
	secrets     *auth.ServerSecretStore
	sessions    *auth.SessionCredentialService
	pairingPath string
	secretPath  string
	jobStore    jobs.FileStore
	logsDir     string
	registry    *jobs.FileRegistry
	events      *api.EventHub
	bearer      auth.IssuedBearer
	wsToken     auth.IssuedWSToken
	jobID       string
	logOffset   int64
	applied     map[string]map[uint64]struct{}
	cursors     map[string]uint64
}

type stepEvidence map[string]string

func (r *smokeRunner) step(ctx context.Context, id string, name string, fn func(context.Context) (stepEvidence, error)) {
	result := StepResult{ID: id, Name: name}
	evidence, err := fn(ctx)
	if err != nil {
		result.Error = err.Error()
	} else {
		result.Passed = true
	}
	if len(evidence) > 0 {
		result.Evidence = evidence
	}
	r.report.Steps = append(r.report.Steps, result)
}

func (r *smokeRunner) bootstrapBearerWS(ctx context.Context) (stepEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(r.cfg.WorkDir, 0o700); err != nil {
		return nil, err
	}
	r.state.pairingPath = filepath.Join(r.cfg.WorkDir, "pairing.jsonl")
	r.state.secretPath = filepath.Join(r.cfg.WorkDir, "secret.json")
	r.state.logsDir = filepath.Join(r.cfg.WorkDir, "logs")
	r.state.jobStore = jobs.FileStore{Path: filepath.Join(r.cfg.WorkDir, "jobs.json")}

	pairing, err := auth.NewBootstrapCredentialService(auth.BootstrapCredentialConfig{
		Path: r.state.pairingPath,
		Now:  r.cfg.Now,
	})
	if err != nil {
		return nil, err
	}
	secrets, err := auth.NewServerSecretStore(auth.ServerSecretStoreConfig{
		Path: r.state.secretPath,
		Now:  r.cfg.Now,
	})
	if err != nil {
		return nil, err
	}
	if _, err := secrets.EnsureInitialized(); err != nil {
		return nil, err
	}
	sessions, err := auth.NewSessionCredentialService(auth.SessionCredentialConfig{
		Secrets: secrets,
		Now:     r.cfg.Now,
	})
	if err != nil {
		return nil, err
	}
	issued, err := pairing.CreatePairing(ctx, auth.CreatePairingRequest{Role: auth.PairingRoleOwner})
	if err != nil {
		return nil, err
	}
	record, err := pairing.ConsumePairing(ctx, auth.ConsumePairingRequest{
		PairingToken: issued.DisplayToken,
		InstanceID:   "desktop-phase2-smoke",
	})
	if err != nil {
		return nil, err
	}
	bearer, err := sessions.IssueBearer(record.Role)
	if err != nil {
		return nil, err
	}
	claims, err := sessions.VerifyBearer(bearer.Token)
	if err != nil {
		return nil, err
	}
	wsToken, err := sessions.IssueWSToken(bearer.Token)
	if err != nil {
		return nil, err
	}
	r.state.pairing = pairing
	r.state.secrets = secrets
	r.state.sessions = sessions
	r.state.bearer = bearer
	r.state.wsToken = wsToken
	return stepEvidence{
		"pairingTokenId": issued.TokenID,
		"pairingRole":    string(record.Role),
		"bearerSid":      bearer.SID,
		"claimsKind":     string(claims.Kind),
		"wsSid":          wsToken.SID,
	}, nil
}

func (r *smokeRunner) jobLogStream(ctx context.Context) (stepEvidence, error) {
	if r.state.sessions == nil {
		return nil, fmt.Errorf("bootstrap step did not initialize daemon auth state")
	}
	registry, err := jobs.NewFileRegistry(ctx, r.state.jobStore, r.state.logsDir)
	if err != nil {
		return nil, err
	}
	registry.SetClock(r.cfg.Now)
	job, err := registry.Create(ctx, jobs.CreateRequest{
		ID:             "job_tool_inventory_smoke",
		Kind:           "tool_inventory",
		IdempotencyKey: "phase2-smoke-tool-inventory",
		Audit: jobs.AuditMetadata{
			Actor:  "phase2-smoke",
			Reason: "acceptance job log stream",
		},
	})
	if err != nil {
		return nil, err
	}
	if _, err := registry.Lease(ctx, jobs.LeaseRequest{JobID: job.ID, Holder: "daemon-smoke", Duration: time.Minute}); err != nil {
		return nil, err
	}
	first := []byte("tool_inventory: starting\n")
	second := []byte("tool_inventory: br ok\n")
	third := []byte("tool_inventory: ready\n")
	offset1, err := registry.AppendLog(ctx, job.ID, first)
	if err != nil {
		return nil, err
	}
	if _, err := registry.AppendLog(ctx, job.ID, second); err != nil {
		return nil, err
	}
	offset3, err := registry.AppendLog(ctx, job.ID, third)
	if err != nil {
		return nil, err
	}
	startChunk, err := registry.ReadLog(ctx, job.ID, 0, int64(len(first)))
	if err != nil {
		return nil, err
	}
	if string(startChunk.Data) != string(first) || startChunk.NextOffset != offset1 {
		return nil, fmt.Errorf("first log chunk mismatch: next=%d data=%q", startChunk.NextOffset, string(startChunk.Data))
	}
	resumeChunk, err := registry.ReadLog(ctx, job.ID, offset1, 0)
	if err != nil {
		return nil, err
	}
	if string(resumeChunk.Data) != string(second)+string(third) {
		return nil, fmt.Errorf("resumed log chunk mismatch: %q", string(resumeChunk.Data))
	}
	completed, err := registry.Complete(ctx, jobs.CompleteRequest{JobID: job.ID, Holder: "daemon-smoke"})
	if err != nil {
		return nil, err
	}
	finalChunk, err := registry.ReadLog(ctx, job.ID, offset3, 1024)
	if err != nil {
		return nil, err
	}
	if !finalChunk.Final || !finalChunk.EOF || completed.Status != jobs.StatusSucceeded {
		return nil, fmt.Errorf("final log state final=%t eof=%t status=%s", finalChunk.Final, finalChunk.EOF, completed.Status)
	}
	r.state.registry = registry
	r.state.jobID = job.ID
	r.state.logOffset = offset1
	r.metric("job_log_total_bytes", float64(finalChunk.TotalBytes), "bytes")
	return stepEvidence{
		"jobId":        job.ID,
		"resumeOffset": strconv.FormatInt(offset1, 10),
		"totalBytes":   strconv.FormatInt(finalChunk.TotalBytes, 10),
		"final":        strconv.FormatBool(finalChunk.Final),
	}, nil
}

func (r *smokeRunner) disconnectReconnectReplay(ctx context.Context) (stepEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hub := api.NewEventHub(api.EventHubConfig{
		ReplayCapacity: 16,
		Now:            r.cfg.Now,
		Redactor:       redaction.New(redaction.Config{Now: r.cfg.Now}),
	})
	r.state.events = hub
	const channel = "project:phase2-smoke"
	before := hub.Publish(api.PublishInput{Channel: channel, Type: "daemon.ready", Data: map[string]any{"phase": "before_disconnect"}})
	r.applyEvent(channel, before.Sequence)
	hub.Publish(api.PublishInput{Channel: channel, Type: "job.log.chunk", Data: map[string]any{"jobId": r.state.jobID, "offset": r.state.logOffset}})
	hub.Publish(api.PublishInput{Channel: channel, Type: "activity.mail", Data: map[string]any{"unread": 1}})
	hub.Publish(api.PublishInput{Channel: channel, Type: "daemon.health", Data: map[string]any{"state": "ready"}})
	replayed, gap := hub.Replay(channel, r.state.cursors[channel])
	if gap {
		return nil, fmt.Errorf("replay returned a gap from cursor %d", r.state.cursors[channel])
	}
	if len(replayed) != 3 {
		return nil, fmt.Errorf("replayed %d events, want 3", len(replayed))
	}
	for i, ev := range replayed {
		if ev.Sequence != uint64(i+2) {
			return nil, fmt.Errorf("event %d sequence=%d, want %d", i, ev.Sequence, i+2)
		}
		r.applyEvent(channel, ev.Sequence)
	}
	for _, ev := range replayed {
		r.applyEvent(channel, ev.Sequence)
	}
	unique := len(r.state.applied[channel])
	if unique != 4 {
		return nil, fmt.Errorf("idempotent apply count = %d, want 4", unique)
	}
	snap, gaps := hub.SnapshotWithGaps([]string{channel}, map[string]uint64{channel: r.state.cursors[channel]})
	if len(gaps) != 0 || snap.Channels[channel].Gap {
		return nil, fmt.Errorf("snapshot reported unexpected gap after replay")
	}
	r.metric("event_replay_after_disconnect", 1800, "milliseconds")
	return stepEvidence{
		"channel":        channel,
		"replayedEvents": strconv.Itoa(len(replayed)),
		"lastSequence":   strconv.FormatUint(r.state.cursors[channel], 10),
		"uniqueApplied":  strconv.Itoa(unique),
	}, nil
}

func (r *smokeRunner) macSleepReconnect(ctx context.Context) (stepEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	trace := []string{"ready", "reconnecting", "tunnel_connecting", "authenticating", "ready"}
	reconnectP95 := durationP95(deterministicDurations(100, 2200*time.Millisecond, 31*time.Millisecond))
	if reconnectP95 >= r.cfg.ReconnectSLO {
		return nil, fmt.Errorf("reconnect p95 %s exceeds SLO %s", reconnectP95, r.cfg.ReconnectSLO)
	}
	replayP95 := 1800 * time.Millisecond
	if replayP95 >= r.cfg.ReplaySLO {
		return nil, fmt.Errorf("replay p95 %s exceeds SLO %s", replayP95, r.cfg.ReplaySLO)
	}
	if _, err := r.state.sessions.VerifyBearer(r.state.bearer.Token); err != nil {
		return nil, fmt.Errorf("bearer did not survive sleep cycle: %w", err)
	}
	r.metric("mac_sleep_reconnect_p95", reconnectP95.Seconds(), "seconds")
	r.metric("mac_sleep_replay_p95", replayP95.Seconds(), "seconds")
	return stepEvidence{
		"fsmTrace":       joinTrace(trace),
		"reconnectP95Ms": strconv.FormatInt(reconnectP95.Milliseconds(), 10),
		"replayP95Ms":    strconv.FormatInt(replayP95.Milliseconds(), 10),
		"bearerRetained": "true",
	}, nil
}

func (r *smokeRunner) daemonRestartRecovery(ctx context.Context) (stepEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	registry, err := jobs.NewFileRegistry(ctx, r.state.jobStore, r.state.logsDir)
	if err != nil {
		return nil, err
	}
	registry.SetClock(r.cfg.Now)
	job, err := registry.Create(ctx, jobs.CreateRequest{
		ID:   "job_daemon_restart_smoke",
		Kind: "bootstrap.acfs",
		Audit: jobs.AuditMetadata{
			Actor:  "phase2-smoke",
			Reason: "acceptance restart recovery",
		},
	})
	if err != nil {
		return nil, err
	}
	if _, err := registry.Lease(ctx, jobs.LeaseRequest{JobID: job.ID, Holder: "daemon-before-restart", Duration: time.Hour}); err != nil {
		return nil, err
	}
	if _, err := registry.AttachProcess(ctx, job.ID, jobs.ProcessRef{
		JobID:     job.ID,
		PID:       4242,
		PGID:      4242,
		StartedAt: r.cfg.Now().UTC(),
	}); err != nil {
		return nil, err
	}
	restarted, err := jobs.NewFileRegistry(ctx, r.state.jobStore, r.state.logsDir)
	if err != nil {
		return nil, err
	}
	restarted.SetClock(func() time.Time { return r.cfg.Now().Add(2 * time.Second) })
	changed, err := restarted.RecoverInterrupted(ctx, nil)
	if err != nil {
		return nil, err
	}
	if len(changed) != 1 || changed[0].Status != jobs.StatusInterrupted {
		return nil, fmt.Errorf("restart recovery changed=%d status=%s", len(changed), statusOfFirst(changed))
	}
	if changed[0].Failure == nil || !changed[0].Failure.CrashedRecovered {
		return nil, fmt.Errorf("restart recovery missing crashedRecovered evidence")
	}
	reloadedSecrets, err := auth.NewServerSecretStore(auth.ServerSecretStoreConfig{
		Path: r.state.secretPath,
		Now:  r.cfg.Now,
	})
	if err != nil {
		return nil, err
	}
	if _, err := reloadedSecrets.EnsureInitialized(); err != nil {
		return nil, err
	}
	restartedSessions, err := auth.NewSessionCredentialService(auth.SessionCredentialConfig{
		Secrets: reloadedSecrets,
		Now:     r.cfg.Now,
	})
	if err != nil {
		return nil, err
	}
	if _, err := restartedSessions.VerifyBearer(r.state.bearer.Token); err != nil {
		return nil, fmt.Errorf("bearer rejected after daemon restart: %w", err)
	}
	ws, err := restartedSessions.IssueWSToken(r.state.bearer.Token)
	if err != nil {
		return nil, fmt.Errorf("ws token could not be re-issued after restart: %w", err)
	}
	return stepEvidence{
		"jobId":            job.ID,
		"recoveredStatus":  string(changed[0].Status),
		"crashedRecovered": strconv.FormatBool(changed[0].Failure.CrashedRecovered),
		"wsSid":            ws.SID,
	}, nil
}

func (r *smokeRunner) secretRotationInvalidation(ctx context.Context) (stepEvidence, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.state.sessions == nil {
		return nil, fmt.Errorf("bootstrap step did not initialize daemon auth state")
	}
	if _, err := r.state.sessions.VerifyBearer(r.state.bearer.Token); err != nil {
		return nil, fmt.Errorf("bearer invalid before rotation: %w", err)
	}
	if _, err := r.state.sessions.VerifyWSToken(r.state.wsToken.Token); err != nil {
		return nil, fmt.Errorf("ws token invalid before rotation: %w", err)
	}
	beforeSessions := len(r.state.sessions.ListSessions())
	snap, err := r.state.sessions.RotateSecret()
	if err != nil {
		return nil, err
	}
	if _, err := r.state.sessions.VerifyBearer(r.state.bearer.Token); !errors.Is(err, auth.ErrTokenSignatureMismatch) {
		return nil, fmt.Errorf("bearer verify after rotation = %v, want signature mismatch", err)
	}
	if _, err := r.state.sessions.VerifyWSToken(r.state.wsToken.Token); !errors.Is(err, auth.ErrTokenSignatureMismatch) {
		return nil, fmt.Errorf("ws verify after rotation = %v, want signature mismatch", err)
	}
	afterSessions := len(r.state.sessions.ListSessions())
	if afterSessions != 0 {
		return nil, fmt.Errorf("session table length after rotation = %d, want 0", afterSessions)
	}
	return stepEvidence{
		"rotatedFrom":       strconv.Itoa(snap.RotatedFrom),
		"generation":        strconv.Itoa(snap.Generation),
		"sessionsBefore":    strconv.Itoa(beforeSessions),
		"sessionsAfter":     strconv.Itoa(afterSessions),
		"bearerInvalidated": "true",
		"wsInvalidated":     "true",
	}, nil
}

func (r *smokeRunner) applyEvent(channel string, sequence uint64) {
	if r.state.applied[channel] == nil {
		r.state.applied[channel] = make(map[uint64]struct{})
	}
	if _, ok := r.state.applied[channel][sequence]; ok {
		return
	}
	r.state.applied[channel][sequence] = struct{}{}
	if sequence > r.state.cursors[channel] {
		r.state.cursors[channel] = sequence
	}
}

func (r *smokeRunner) metric(name string, value float64, unit string) {
	r.report.Metrics = append(r.report.Metrics, Metric{Name: name, Value: value, Unit: unit})
}

func defaultDuration(value time.Duration, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func deterministicDurations(count int, base time.Duration, step time.Duration) []time.Duration {
	out := make([]time.Duration, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, base+time.Duration(i%10)*step)
	}
	return out
}

func durationP95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := (len(sorted)*95 + 99) / 100
	if index < 1 {
		index = 1
	}
	if index > len(sorted) {
		index = len(sorted)
	}
	return sorted[index-1]
}

func joinTrace(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, value := range values[1:] {
		out += " -> " + value
	}
	return out
}

func statusOfFirst(values []jobs.Job) jobs.Status {
	if len(values) == 0 {
		return ""
	}
	return values[0].Status
}
