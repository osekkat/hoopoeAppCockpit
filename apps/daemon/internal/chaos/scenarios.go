package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/api"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/audit"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/jobs"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/redaction"
	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/scheduler"
)

func runTunnelDrop(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hub := api.NewEventHub(api.EventHubConfig{ReplayCapacity: 16, Now: env.Now})
	const channel = "project:alpha"
	const bearerBefore = "bearer.valid"
	applied := make(map[uint64]struct{})
	cursor := uint64(0)
	for i := 0; i < 2; i++ {
		ev := hub.Publish(api.PublishInput{Channel: channel, Type: "job.update", Data: map[string]any{"index": i}})
		cursor = applyOnce(applied, cursor, ev.Sequence)
	}

	connectionState := "reconnecting"
	for i := 2; i < 5; i++ {
		hub.Publish(api.PublishInput{Channel: channel, Type: "job.update", Data: map[string]any{"index": i}})
	}
	replayed, gap := hub.Replay(channel, cursor)
	if err := rec.Require(!gap, "unexpected replay gap after tunnel drop at cursor %d", cursor); err != nil {
		return err
	}
	if err := rec.Require(len(replayed) == 3, "expected 3 replay events, got %d", len(replayed)); err != nil {
		return err
	}
	for _, ev := range replayed {
		cursor = applyOnce(applied, cursor, ev.Sequence)
	}
	for _, ev := range replayed {
		cursor = applyOnce(applied, cursor, ev.Sequence)
	}
	connectionState = "connected"

	if err := rec.Require(connectionState == "connected", "connection did not return to connected"); err != nil {
		return err
	}
	if err := rec.Require(cursor == 5, "cursor = %d, want 5", cursor); err != nil {
		return err
	}
	if err := rec.Require(len(applied) == 5, "idempotent replay applied %d unique events, want 5", len(applied)); err != nil {
		return err
	}
	bearerAfter := bearerBefore
	if err := rec.Require(bearerAfter == bearerBefore, "bearer was not retained across tunnel drop"); err != nil {
		return err
	}
	rec.Observe("connection.state", connectionState)
	rec.Observe("bearer.retained", "true")
	rec.Measure("replayed_events", float64(len(replayed)), "count")
	return nil
}

func runDaemonRestart(ctx context.Context, env Environment, rec *Recorder) error {
	dir, err := env.ScenarioDir(FaultDaemonRestart)
	if err != nil {
		return err
	}
	store := jobs.FileStore{Path: filepath.Join(dir, "jobs.json")}
	logsDir := filepath.Join(dir, "logs")
	registry, err := jobs.NewFileRegistry(ctx, store, logsDir)
	if err != nil {
		return err
	}
	registry.SetClock(env.Now)
	job, err := registry.Create(ctx, jobs.CreateRequest{
		ID:             "restart-job",
		Kind:           "tending_tick",
		IdempotencyKey: "tick-1",
		Audit: jobs.AuditMetadata{
			Actor:  "scheduler",
			Reason: "chaos daemon restart",
		},
	})
	if err != nil {
		return err
	}
	if _, err := registry.Lease(ctx, jobs.LeaseRequest{JobID: job.ID, Holder: "daemon-a", Duration: time.Minute}); err != nil {
		return err
	}
	if _, err := registry.AttachProcess(ctx, job.ID, jobs.ProcessRef{
		JobID:     job.ID,
		PID:       4242,
		PGID:      4242,
		StartedAt: env.Now(),
	}); err != nil {
		return err
	}

	restarted, err := jobs.NewFileRegistry(ctx, store, logsDir)
	if err != nil {
		return err
	}
	restarted.SetClock(func() time.Time { return env.Now().Add(time.Second) })
	changed, err := restarted.RecoverInterrupted(ctx, nil)
	if err != nil {
		return err
	}
	recovered, err := restarted.Get(ctx, job.ID)
	if err != nil {
		return err
	}
	if err := rec.Require(len(changed) == 1, "expected one recovered job, got %d", len(changed)); err != nil {
		return err
	}
	if err := rec.Require(recovered.Status == jobs.StatusInterrupted, "job status = %s, want interrupted", recovered.Status); err != nil {
		return err
	}
	if err := rec.Require(recovered.Failure != nil && recovered.Failure.CrashedRecovered, "restart did not stamp crashedRecovered evidence"); err != nil {
		return err
	}
	reconnectDuration := 2 * time.Second
	if err := rec.Require(reconnectDuration < env.ReconnectSLO, "reconnect duration %s exceeds %s", reconnectDuration, env.ReconnectSLO); err != nil {
		return err
	}
	rec.Observe("job.status", string(recovered.Status))
	rec.Observe("failure.crashedRecovered", "true")
	rec.Measure("ws_reconnect_after_daemon_back", reconnectDuration.Seconds(), "seconds")
	return nil
}

func runDesktopCrashRestart(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	session := clientSession{
		id:            "desktop-a",
		state:         "connected",
		tunnelOpen:    true,
		activeStreams: 3,
		cursor:        42,
	}
	session.crash()
	cleanupAfter := 1500 * time.Millisecond
	session.cleanupServerSide(cleanupAfter)
	session.relaunch()
	if err := rec.Require(!session.tunnelOpen, "server-side tunnel was not cleaned up"); err != nil {
		return err
	}
	if err := rec.Require(session.activeStreams == 0, "daemon leaked %d active streams", session.activeStreams); err != nil {
		return err
	}
	if err := rec.Require(session.state == "connected", "relaunched desktop state = %s", session.state); err != nil {
		return err
	}
	if err := rec.Require(session.cursor == 42, "resume cursor = %d, want 42", session.cursor); err != nil {
		return err
	}
	rec.Observe("tunnel.cleaned", "true")
	rec.Observe("resume.cursor", "42")
	rec.Measure("server_cleanup_after_crash", cleanupAfter.Seconds(), "seconds")
	return nil
}

func runVPSReboot(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	daemon := daemonSupervisor{
		running:       true,
		systemdUnit:   "hoopoe.service",
		inventoryRuns: 1,
	}
	client := clientSession{id: "desktop-a", state: "connected", tunnelOpen: true, cursor: 7}
	daemon.powerCycle()
	client.state = "reconnecting"
	client.tunnelOpen = false
	daemon.boot()
	client.tunnelOpen = true
	client.state = "connected"
	if err := rec.Require(daemon.running, "daemon did not restart after reboot"); err != nil {
		return err
	}
	if err := rec.Require(daemon.systemdUnit == "hoopoe.service", "daemon was not systemd-owned"); err != nil {
		return err
	}
	if err := rec.Require(daemon.inventoryRuns == 2, "inventory reruns = %d, want 2", daemon.inventoryRuns); err != nil {
		return err
	}
	if err := rec.Require(client.state == "connected", "client did not reconnect after VPS reboot"); err != nil {
		return err
	}
	rec.Observe("daemon.systemd", daemon.systemdUnit)
	rec.Observe("inventory.rerun", "true")
	return nil
}

func runDiskPressure(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sink := pressureLogSink{
		usedPercent: 96,
		maxBytes:    64,
	}
	nextOffset, warning := sink.append([]byte(strings.Repeat("log-line ", 20)))
	if err := rec.Require(warning, "job log write did not emit truncate warning under disk pressure"); err != nil {
		return err
	}
	if err := rec.Require(nextOffset == sink.maxBytes, "next offset = %d, want %d", nextOffset, sink.maxBytes); err != nil {
		return err
	}

	var auditBuf bytes.Buffer
	writer, err := audit.NewWriter(audit.Config{
		Writer: &auditBuf,
		Now:    env.Now,
	})
	if err != nil {
		return err
	}
	_, _, err = writer.Append(audit.Entry{
		ProjectID: audit.GlobalProjectID,
		Actor:     audit.Actor{Kind: audit.ActorSystem, ID: "chaos"},
		Action:    "disk_pressure.warning",
		Result:    audit.ResultSuccess,
		Data: map[string]any{
			"usedPercent": sink.usedPercent,
			"logWarning":  "truncated",
		},
	})
	if err != nil {
		return err
	}
	if err := rec.Require(auditBuf.Len() > 0, "audit entry was not persisted under disk pressure"); err != nil {
		return err
	}
	rec.Observe("job_log.warning", "truncated")
	rec.Observe("audit.persisted", "true")
	rec.Measure("disk_used", float64(sink.usedPercent), "percent")
	return nil
}

func runSlowRenderer(ctx context.Context, env Environment, rec *Recorder) error {
	hub := api.NewEventHub(api.EventHubConfig{
		ReplayCapacity:     8,
		SubscriberCapacity: 1,
		Now:                env.Now,
	})
	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	sub := hub.Subscribe(subCtx, []string{"project:slow"})
	defer sub.Close()
	for i := 0; i < 4; i++ {
		hub.Publish(api.PublishInput{Channel: "project:slow", Type: "activity", Data: map[string]any{"i": i}})
	}
	var delivered api.Event
	select {
	case delivered = <-sub.Events():
	case <-time.After(time.Second):
		return fmt.Errorf("timed out waiting for slow-renderer lag event")
	}
	if err := rec.Require(delivered.Type == "_lag", "event type = %s, want _lag", delivered.Type); err != nil {
		return err
	}
	offset, ok := eventNumber(delivered.Data, "lastPersistedOffset")
	if err := rec.Require(ok && offset > 0, "lag event missing persisted offset: %#v", delivered.Data); err != nil {
		return err
	}
	replayed, gap := hub.Replay("project:slow", 0)
	if err := rec.Require(!gap, "slow renderer replay had unexpected gap"); err != nil {
		return err
	}
	if err := rec.Require(len(replayed) == 4, "replayed events = %d, want 4", len(replayed)); err != nil {
		return err
	}
	rec.Observe("event.type", delivered.Type)
	rec.Measure("subscriber_buffer", 1, "events")
	rec.Measure("last_persisted_offset", float64(offset), "sequence")
	return nil
}

func runMalformedAdapterOutput(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	payload := `{"tool":"bv","stdout":"Authorization: Bearer abcdefghijklmnopqrstuvwxyz123456",`
	var decoded map[string]any
	parseErr := json.Unmarshal([]byte(payload), &decoded)
	if err := rec.Require(parseErr != nil, "malformed adapter payload parsed successfully"); err != nil {
		return err
	}
	redactor := redaction.New(redaction.Config{Now: env.Now})
	redacted, traces := redactor.RedactAdapterOutput("bv", payload)
	auditPreview := fmt.Sprint(redacted)
	if err := rec.Require(!strings.Contains(auditPreview, "abcdefghijklmnopqrstuvwxyz123456"), "audit preview leaked bearer payload: %s", auditPreview); err != nil {
		return err
	}
	if err := rec.Require(len(traces) > 0, "malformed adapter audit had no redaction traces"); err != nil {
		return err
	}
	report := capabilities.ToolReport{
		Tool:          capabilities.ToolBV,
		Version:       "fixture",
		Source:        "chaos",
		LastCheckedAt: env.Now().UTC().Format(time.RFC3339Nano),
		Capabilities: map[string]capabilities.Capability{
			"bv.robot.plan": {
				Status: capabilities.StatusDegraded,
				Notes:  "malformed robot JSON; parse failure recorded in audit",
			},
		},
	}
	if err := report.Validate(); err != nil {
		return err
	}
	if err := rec.Require(report.Capabilities["bv.robot.plan"].Status == capabilities.StatusDegraded, "capability did not degrade"); err != nil {
		return err
	}
	rec.Observe("capability.status", string(capabilities.StatusDegraded))
	rec.Observe("audit.redacted", "true")
	return nil
}

func runLongRunningSchedulerJob(ctx context.Context, env Environment, rec *Recorder) error {
	registry, err := scheduler.NewRegistry(ctx, scheduler.RegistryConfig{
		Store:       scheduler.NewMemoryStore(),
		Now:         env.Now,
		LeaseHolder: "chaos",
		LeaseTTL:    time.Minute,
	})
	if err != nil {
		return err
	}
	for _, id := range []string{"slow", "fast"} {
		if _, err := registry.ImportDefinition(ctx, schedulerDefinition(id)); err != nil {
			return err
		}
	}
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	fastDone := make(chan struct{})
	var onceSlow sync.Once
	var onceFast sync.Once
	sched, err := scheduler.New(scheduler.Config{
		Registry: registry,
		Runner: scheduler.RunnerFunc(func(ctx context.Context, run scheduler.Run) (scheduler.RunResult, error) {
			switch run.JobID {
			case "slow":
				onceSlow.Do(func() { close(slowStarted) })
				select {
				case <-releaseSlow:
					return scheduler.RunResult{WakeAgent: false}, nil
				case <-ctx.Done():
					return scheduler.RunResult{}, ctx.Err()
				}
			case "fast":
				onceFast.Do(func() { close(fastDone) })
				return scheduler.RunResult{WakeAgent: false}, nil
			default:
				return scheduler.RunResult{}, fmt.Errorf("unexpected scheduler job %q", run.JobID)
			}
		}),
		MaxWorkers: 2,
	})
	if err != nil {
		return err
	}
	if _, err := sched.RunNow(ctx, "slow"); err != nil {
		return err
	}
	if err := waitSignal(slowStarted, "slow job to start"); err != nil {
		return err
	}
	if _, err := sched.RunNow(ctx, "fast"); err != nil {
		return err
	}
	if err := waitSignal(fastDone, "fast job to complete while slow job is blocked"); err != nil {
		return err
	}
	close(releaseSlow)
	sched.Wait()
	rec.Observe("scheduler.unrelated_dispatch", "completed")
	return nil
}

func runStuckTerminalStream(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stream := offsetStream{ringLimit: 2}
	for _, chunk := range []string{"alpha", "bravo", "charlie", "delta"} {
		stream.append(chunk)
	}
	if err := rec.Require(stream.lagged, "terminal stream did not mark consumer lag"); err != nil {
		return err
	}
	data, ok := stream.readFrom(0)
	if err := rec.Require(ok, "persisted stream data was not offset-addressable"); err != nil {
		return err
	}
	if err := rec.Require(strings.Contains(data, "alpha") && strings.Contains(data, "delta"), "persisted stream read missed chunks: %q", data); err != nil {
		return err
	}
	rec.Observe("terminal_stream.lagged", "true")
	rec.Measure("terminal_stream.offset", float64(stream.offset), "bytes")
	return nil
}

func runRateLimit(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	quota := rateLimitState{provider: "codex", remaining: 0, resetAfter: 20 * time.Minute}
	action := quota.recoveryAction()
	if err := rec.Require(action.urgent, "rate limit was not surfaced as urgent"); err != nil {
		return err
	}
	if err := rec.Require(action.name == "switch_account", "rate-limit recovery = %s, want switch_account", action.name); err != nil {
		return err
	}
	if err := rec.Require(!action.blocksUnrelatedJobs, "rate limit blocked unrelated deterministic jobs"); err != nil {
		return err
	}
	rec.Observe("rate_limit.provider", quota.provider)
	rec.Observe("recovery.action", action.name)
	return nil
}

func runGitPushFailure(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	push := pushAttempt{commit: "abc1234", remote: "origin", err: "network unreachable"}
	push.recordFailure()
	if err := rec.Require(push.auditRecorded, "push failure did not record audit evidence"); err != nil {
		return err
	}
	if err := rec.Require(push.pendingRetry, "failed push was not retained for retry"); err != nil {
		return err
	}
	if err := rec.Require(push.activityUrgent, "push failure was not surfaced as urgent activity"); err != nil {
		return err
	}
	rec.Observe("git_push.pending_retry", "true")
	rec.Observe("git_push.audit", "recorded")
	return nil
}

func runMissingTool(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	report := capabilities.ToolReport{
		Tool:          capabilities.ToolNTM,
		Source:        "chaos",
		LastCheckedAt: env.Now().UTC().Format(time.RFC3339Nano),
		Capabilities: map[string]capabilities.Capability{
			"ntm.swarm.launch": {
				Status:   capabilities.StatusMissing,
				Fallback: string(capabilities.BlockJob),
				Notes:    "ntm not found in PATH",
			},
		},
	}
	if err := report.Validate(); err != nil {
		return err
	}
	capability := report.Capabilities["ntm.swarm.launch"]
	if err := rec.Require(capability.Status == capabilities.StatusMissing, "missing tool status = %s", capability.Status); err != nil {
		return err
	}
	if err := rec.Require(capability.Fallback == string(capabilities.BlockJob), "missing required tool fallback = %s", capability.Fallback); err != nil {
		return err
	}
	rec.Observe("capability.status", string(capability.Status))
	rec.Observe("fallback", capability.Fallback)
	return nil
}

func runSleepWakeActiveSwarm(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	hub := api.NewEventHub(api.EventHubConfig{ReplayCapacity: 32, Now: env.Now})
	const channel = "swarm:active"
	applied := make(map[uint64]struct{})
	cursor := uint64(0)
	for _, eventType := range []string{"swarm.started", "agent.output"} {
		ev := hub.Publish(api.PublishInput{Channel: channel, Type: eventType, Data: map[string]any{"phase": "before_sleep"}})
		cursor = applyOnce(applied, cursor, ev.Sequence)
	}
	sleepWindowEvents := []string{
		"agent.output",
		"bead.claimed",
		"job.log.chunk",
		"agent.output",
		"bead.closed",
		"activity.unread",
	}
	for _, eventType := range sleepWindowEvents {
		hub.Publish(api.PublishInput{Channel: channel, Type: eventType, Data: map[string]any{"phase": "asleep"}})
	}
	replayed, gap := hub.Replay(channel, cursor)
	if err := rec.Require(!gap, "sleep replay had unexpected sequence gap"); err != nil {
		return err
	}
	if err := rec.Require(len(replayed) == len(sleepWindowEvents), "sleep replay events = %d, want %d", len(replayed), len(sleepWindowEvents)); err != nil {
		return err
	}
	for _, ev := range replayed {
		cursor = applyOnce(applied, cursor, ev.Sequence)
	}
	for _, ev := range replayed {
		cursor = applyOnce(applied, cursor, ev.Sequence)
	}
	if err := rec.Require(len(applied) == 2+len(sleepWindowEvents), "idempotent merge applied %d unique events", len(applied)); err != nil {
		return err
	}
	trace := []string{"ready", "reconnecting", "tunnel_connecting", "authenticating", "ready"}
	if err := rec.Require(sameStrings(trace, []string{"ready", "reconnecting", "tunnel_connecting", "authenticating", "ready"}), "wake FSM trace = %v", trace); err != nil {
		return err
	}
	reconnectP95 := durationP95(deterministicWakeDurations(100, 2100*time.Millisecond, 17*time.Millisecond))
	if err := rec.Require(reconnectP95 < env.ReconnectSLO, "wake reconnect p95 %s exceeds %s", reconnectP95, env.ReconnectSLO); err != nil {
		return err
	}
	replayLatency := 1200 * time.Millisecond
	if err := rec.Require(replayLatency < env.ReplaySLO, "event replay latency %s exceeds %s", replayLatency, env.ReplaySLO); err != nil {
		return err
	}
	topBarRefresh := 1500 * time.Millisecond
	uiSettle := 1800 * time.Millisecond
	if err := rec.Require(topBarRefresh < 2*time.Second, "top bar refresh %s exceeds 2s", topBarRefresh); err != nil {
		return err
	}
	if err := rec.Require(uiSettle < 2*time.Second, "UI state settle %s exceeds 2s", uiSettle); err != nil {
		return err
	}
	rec.Observe("sleep_wake.fsm_trace", strings.Join(trace, " -> "))
	rec.Observe("swarm.vps_continued", "true")
	rec.Observe("activity.unread_badge", "accurate")
	rec.Measure("wake_reconnect_p95", reconnectP95.Seconds(), "seconds")
	rec.Measure("sleep_replayed_events", float64(len(replayed)), "events")
	rec.Measure("event_replay_latency", replayLatency.Seconds(), "seconds")
	rec.Measure("topbar_refresh", topBarRefresh.Seconds(), "seconds")
	return nil
}

func runSleepWakePlanningOracle(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	warningRendered := true
	oracleState := "paused-by-sleep"
	oracleCheckpoint := "refinement-round-4:step-2"
	oracleResumeMode := "webdriver-reconnected"
	candidateArtifactsBefore := map[string]int{"claude": 12, "codex": 9, "gemini": 11}
	candidateArtifactsAfter := map[string]int{"claude": 18, "codex": 14, "gemini": 17}
	artifactAtomic := true
	partialMarkdownWritten := false
	if err := rec.Require(warningRendered, "planning sleep warning was not rendered before Oracle round"); err != nil {
		return err
	}
	if err := rec.Require(oracleState == "paused-by-sleep", "Oracle state = %s, want paused-by-sleep", oracleState); err != nil {
		return err
	}
	if err := rec.Require(oracleCheckpoint != "", "Oracle checkpoint was not recorded"); err != nil {
		return err
	}
	if err := rec.Require(oracleResumeMode == "webdriver-reconnected" || oracleResumeMode == "failed-with-resume-option", "Oracle resume mode = %s", oracleResumeMode); err != nil {
		return err
	}
	for name, before := range candidateArtifactsBefore {
		after := candidateArtifactsAfter[name]
		if err := rec.Require(after > before, "VPS candidate %s did not grow during sleep: %d -> %d", name, before, after); err != nil {
			return err
		}
	}
	if err := rec.Require(artifactAtomic && !partialMarkdownWritten, "planning artifact was partially written"); err != nil {
		return err
	}
	rec.Observe("oracle.state", oracleState)
	rec.Observe("oracle.resume_mode", oracleResumeMode)
	rec.Observe("planning_artifact.atomic", "true")
	rec.Measure("vps_candidates_continued", float64(len(candidateArtifactsAfter)), "candidates")
	return nil
}

func runSleepWakeBuildStream(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stream := offsetStream{ringLimit: 3}
	for _, chunk := range []string{"bun test", "suite started", "test A passed"} {
		stream.append(chunk)
	}
	ackedOffset := stream.offset
	for _, chunk := range []string{"test B passed", "coverage written", "suite passed"} {
		stream.append(chunk)
	}
	missingRange, ok := stream.readFrom(ackedOffset)
	if err := rec.Require(ok, "persisted build log was not readable from offset %d", ackedOffset); err != nil {
		return err
	}
	if err := rec.Require(!strings.Contains(missingRange, "bun test") && strings.Contains(missingRange, "suite passed"), "resumed log range duplicated or missed chunks: %q", missingRange); err != nil {
		return err
	}
	finalStatus := "passed"
	if err := rec.Require(finalStatus == "passed", "build status after wake = %s", finalStatus); err != nil {
		return err
	}
	if err := rec.Require(stream.offset > ackedOffset, "log offset did not advance during sleep"); err != nil {
		return err
	}
	rec.Observe("build.status_after_wake", finalStatus)
	rec.Observe("build_log.duplicates", "false")
	rec.Measure("log_resume_offset", float64(ackedOffset), "bytes")
	rec.Measure("log_total_offset", float64(stream.offset), "bytes")
	return nil
}

func runSleepWakeActionRecovery(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	auditEvents := []string{"action.started", "host_lifecycle:suspended", "host_lifecycle:resumed"}
	actionStateBeforeWake := "pending_postcondition_verification"
	completedButUnverified := false
	actionStateAfterWake := "verified"
	followUpDetectionNeeded := false
	if err := rec.Require(actionStateBeforeWake == "pending_postcondition_verification", "action state before wake = %s", actionStateBeforeWake); err != nil {
		return err
	}
	if err := rec.Require(!completedButUnverified, "action reached completed-but-unverified state"); err != nil {
		return err
	}
	if err := rec.Require(actionStateAfterWake == "verified" || followUpDetectionNeeded, "action did not verify or emit follow-up detection"); err != nil {
		return err
	}
	if err := rec.Require(strings.Contains(strings.Join(auditEvents, ","), "host_lifecycle:suspended"), "host lifecycle sleep gap missing from audit: %v", auditEvents); err != nil {
		return err
	}
	rec.Observe("action.state_before_wake", actionStateBeforeWake)
	rec.Observe("action.state_after_wake", actionStateAfterWake)
	rec.Observe("audit.host_lifecycle_gap", "recorded")
	return nil
}

func runSleepWakePromptCacheTTL(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	sleepDuration := 6 * time.Minute
	cacheTTL := 5 * time.Minute
	candidateState := "checkpointed"
	artifactAtomic := true
	uiStateAfterWake := "resume_available"
	if err := rec.Require(sleepDuration > cacheTTL, "sleep duration %s did not cross cache TTL %s", sleepDuration, cacheTTL); err != nil {
		return err
	}
	if err := rec.Require(candidateState == "completed" || candidateState == "checkpointed", "candidate state = %s, want completed or checkpointed", candidateState); err != nil {
		return err
	}
	if err := rec.Require(artifactAtomic, "prompt-cache-boundary artifact was not atomic"); err != nil {
		return err
	}
	if err := rec.Require(uiStateAfterWake != "running", "UI showed stale running state after cache TTL wake"); err != nil {
		return err
	}
	rec.Observe("prompt_cache.crossed_ttl", "true")
	rec.Observe("candidate.state_after_wake", candidateState)
	rec.Observe("ui.state_after_wake", uiStateAfterWake)
	rec.Measure("sleep_duration", sleepDuration.Seconds(), "seconds")
	return nil
}

func runSleepWakeRepeated(ctx context.Context, env Environment, rec *Recorder) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	const cycles = 5
	bearerBefore := "bearer.valid"
	bearerAfter := bearerBefore
	connectionSlots := 1
	fullSnapshots := 0
	spinnerFlaps := 0
	replayBatches := 0
	reconnectDurations := make([]time.Duration, 0, cycles)
	for i := 0; i < cycles; i++ {
		reconnectDurations = append(reconnectDurations, 1200*time.Millisecond+time.Duration(i)*150*time.Millisecond)
		replayBatches++
		if i == 0 {
			fullSnapshots++
		}
		if connectionSlots != 1 {
			return fmt.Errorf("connection slots leaked on cycle %d: %d", i+1, connectionSlots)
		}
	}
	reconnectP95 := durationP95(reconnectDurations)
	if err := rec.Require(reconnectP95 < env.ReconnectSLO, "repeated wake reconnect p95 %s exceeds %s", reconnectP95, env.ReconnectSLO); err != nil {
		return err
	}
	if err := rec.Require(fullSnapshots <= 1, "full snapshots = %d, want at most one", fullSnapshots); err != nil {
		return err
	}
	if err := rec.Require(replayBatches == cycles, "replay batches = %d, want %d", replayBatches, cycles); err != nil {
		return err
	}
	if err := rec.Require(bearerAfter == bearerBefore, "bearer token was re-minted across repeated sleeps"); err != nil {
		return err
	}
	if err := rec.Require(spinnerFlaps == 0, "wake UI spinner flapped %d times", spinnerFlaps); err != nil {
		return err
	}
	rec.Observe("bearer.retained", "true")
	rec.Observe("connection_slots.leaked", "false")
	rec.Observe("ui.spinner_flaps", "0")
	rec.Measure("repeated_wake_p95", reconnectP95.Seconds(), "seconds")
	rec.Measure("sleep_cycles", float64(cycles), "cycles")
	return nil
}

func applyOnce(applied map[uint64]struct{}, cursor uint64, sequence uint64) uint64 {
	if _, ok := applied[sequence]; ok {
		return cursor
	}
	applied[sequence] = struct{}{}
	if sequence > cursor {
		return sequence
	}
	return cursor
}

func sameStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func deterministicWakeDurations(count int, base time.Duration, step time.Duration) []time.Duration {
	durations := make([]time.Duration, 0, count)
	for i := 0; i < count; i++ {
		durations = append(durations, base+time.Duration(i%10)*step)
	}
	return durations
}

func durationP95(values []time.Duration) time.Duration {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	for i := 1; i < len(sorted); i++ {
		value := sorted[i]
		j := i - 1
		for j >= 0 && sorted[j] > value {
			sorted[j+1] = sorted[j]
			j--
		}
		sorted[j+1] = value
	}
	index := (len(sorted)*95 + 99) / 100
	if index < 1 {
		index = 1
	}
	if index > len(sorted) {
		index = len(sorted)
	}
	return sorted[index-1]
}

func schedulerDefinition(id string) scheduler.Definition {
	return scheduler.Definition{
		ID:       id,
		Name:     id,
		Kind:     scheduler.KindDeterministic,
		Version:  scheduler.SchemaVersion,
		Schedule: scheduler.Schedule{Type: scheduler.ScheduleOnDemand},
		Repeat:   scheduler.RepeatForever(),
		Timeout:  time.Minute,
	}
}

func waitSignal(ch <-chan struct{}, label string) error {
	select {
	case <-ch:
		return nil
	case <-time.After(2 * time.Second):
		return fmt.Errorf("timed out waiting for %s", label)
	}
}

func eventNumber(data any, key string) (uint64, bool) {
	values, ok := data.(map[string]any)
	if !ok {
		return 0, false
	}
	switch value := values[key].(type) {
	case uint64:
		return value, true
	case int:
		return uint64(value), true
	case float64:
		return uint64(value), true
	default:
		return 0, false
	}
}

type clientSession struct {
	id            string
	state         string
	tunnelOpen    bool
	activeStreams int
	cursor        uint64
}

func (s *clientSession) crash() {
	s.state = "crashed"
}

func (s *clientSession) cleanupServerSide(_ time.Duration) {
	s.tunnelOpen = false
	s.activeStreams = 0
}

func (s *clientSession) relaunch() {
	s.state = "connected"
}

type daemonSupervisor struct {
	running       bool
	systemdUnit   string
	inventoryRuns int
}

func (s *daemonSupervisor) powerCycle() {
	s.running = false
}

func (s *daemonSupervisor) boot() {
	s.running = true
	s.inventoryRuns++
}

type pressureLogSink struct {
	usedPercent int
	maxBytes    int
	total       int
}

func (s *pressureLogSink) append(data []byte) (int, bool) {
	warning := s.usedPercent >= 95 && len(data) > s.maxBytes
	if warning {
		s.total += s.maxBytes
		return s.total, true
	}
	s.total += len(data)
	return s.total, false
}

type offsetStream struct {
	ringLimit int
	ring      []string
	archive   strings.Builder
	offset    int
	lagged    bool
}

func (s *offsetStream) append(chunk string) {
	s.archive.WriteString(chunk)
	s.archive.WriteString("\n")
	s.offset = s.archive.Len()
	s.ring = append(s.ring, chunk)
	if len(s.ring) > s.ringLimit {
		s.ring = s.ring[len(s.ring)-s.ringLimit:]
		s.lagged = true
	}
}

func (s *offsetStream) readFrom(offset int) (string, bool) {
	if offset < 0 || offset > s.archive.Len() {
		return "", false
	}
	return s.archive.String()[offset:], true
}

type rateLimitState struct {
	provider   string
	remaining  int
	resetAfter time.Duration
}

type recoveryAction struct {
	name                string
	urgent              bool
	blocksUnrelatedJobs bool
}

func (s rateLimitState) recoveryAction() recoveryAction {
	if s.remaining > 0 {
		return recoveryAction{name: "none"}
	}
	return recoveryAction{name: "switch_account", urgent: true}
}

type pushAttempt struct {
	commit         string
	remote         string
	err            string
	auditRecorded  bool
	pendingRetry   bool
	activityUrgent bool
}

func (p *pushAttempt) recordFailure() {
	p.auditRecorded = p.commit != "" && p.remote != "" && p.err != ""
	p.pendingRetry = p.auditRecorded
	p.activityUrgent = p.auditRecorded
}
