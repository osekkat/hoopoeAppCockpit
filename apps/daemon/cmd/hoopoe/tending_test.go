package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/scheduler"
)

func newTestTendingIO(t *testing.T) *tendingIO {
	t.Helper()
	dir := t.TempDir()
	return &tendingIO{
		Stdout:         new(bytes.Buffer),
		Stderr:         new(bytes.Buffer),
		Stdin:          strings.NewReader(""),
		Now:            func() time.Time { return time.Date(2026, 5, 4, 2, 0, 0, 0, time.UTC) },
		StatePath:      filepath.Join(dir, "scheduler-state.json"),
		DefinitionsDir: filepath.Join(dir, "jobs.d"),
		AuditPath:      filepath.Join(dir, "audit.jsonl"),
	}
}

func resetTendingStdout(io *tendingIO) {
	io.Stdout = new(bytes.Buffer)
}

func tendingStdout(io *tendingIO) string {
	return io.Stdout.(*bytes.Buffer).String()
}

func TestTendingCreateListAndStatusJSON(t *testing.T) {
	io := newTestTendingIO(t)
	ctx := context.Background()

	if err := runTending(ctx, []string{"create", "snapshot-health", "--schedule", "every 5m", "--skills", "ntm,vibing-with-ntm", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	var created tendingJobView
	if err := json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &created); err != nil {
		t.Fatalf("decode create JSON: %v\nout=%q", err, tendingStdout(io))
	}
	if created.JobID != "snapshot-health" || created.Status != "active" || created.Schedule != "every 5m" {
		t.Fatalf("unexpected created view: %#v", created)
	}
	if _, err := os.Stat(filepath.Join(io.DefinitionsDir, "snapshot-health.json")); err != nil {
		t.Fatalf("definition file missing: %v", err)
	}

	resetTendingStdout(io)
	if err := runTending(ctx, []string{"list", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	var jobs []tendingJobView
	if err := json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &jobs); err != nil {
		t.Fatalf("decode list JSON: %v\nout=%q", err, tendingStdout(io))
	}
	if len(jobs) != 1 || jobs[0].JobID != "snapshot-health" {
		t.Fatalf("expected listed snapshot-health job, got %#v", jobs)
	}

	resetTendingStdout(io)
	if err := runTending(ctx, []string{"status", "snapshot-health", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	var status tendingJobView
	if err := json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &status); err != nil {
		t.Fatalf("decode status JSON: %v\nout=%q", err, tendingStdout(io))
	}
	if status.JobID != "snapshot-health" || status.Status != "active" {
		t.Fatalf("unexpected status: %#v", status)
	}
}

func TestTendingPauseResumePersistsThroughCLIState(t *testing.T) {
	io := newTestTendingIO(t)
	ctx := context.Background()
	if err := runTending(ctx, []string{"create", "tend-swarm", "--schedule", "on demand"}, io); err != nil {
		t.Fatal(err)
	}

	resetTendingStdout(io)
	if err := runTending(ctx, []string{"pause", "tend-swarm", "--actor", "tester", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	var paused tendingJobView
	if err := json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &paused); err != nil {
		t.Fatalf("decode pause JSON: %v", err)
	}
	if paused.Status != "paused" {
		t.Fatalf("expected paused status, got %#v", paused)
	}

	restarted := *io
	restarted.Stdout = new(bytes.Buffer)
	restarted.Stderr = new(bytes.Buffer)
	if err := runTending(ctx, []string{"status", "tend-swarm", "--json"}, &restarted); err != nil {
		t.Fatal(err)
	}
	var afterRestart tendingJobView
	if err := json.Unmarshal(restarted.Stdout.(*bytes.Buffer).Bytes(), &afterRestart); err != nil {
		t.Fatalf("decode restarted status JSON: %v", err)
	}
	if afterRestart.Status != "paused" {
		t.Fatalf("expected paused status after reopening state, got %#v", afterRestart)
	}

	restarted.Stdout = new(bytes.Buffer)
	if err := runTending(ctx, []string{"resume", "tend-swarm", "--actor", "tester", "--json"}, &restarted); err != nil {
		t.Fatal(err)
	}
	var resumed tendingJobView
	if err := json.Unmarshal(restarted.Stdout.(*bytes.Buffer).Bytes(), &resumed); err != nil {
		t.Fatalf("decode resume JSON: %v", err)
	}
	if resumed.Status != "active" {
		t.Fatalf("expected active status after resume, got %#v", resumed)
	}
}

func TestTendingRunRemoveAndAudit(t *testing.T) {
	io := newTestTendingIO(t)
	ctx := context.Background()
	if err := runTending(ctx, []string{"create", "manual-scan", "--schedule", "on demand"}, io); err != nil {
		t.Fatal(err)
	}

	resetTendingStdout(io)
	if err := runTending(ctx, []string{"run", "manual-scan", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	var decision scheduler.Decision
	if err := json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &decision); err != nil {
		t.Fatalf("decode run decision JSON: %v\nout=%q", err, tendingStdout(io))
	}
	if decision.JobID != "manual-scan" || decision.Outcome != scheduler.OutcomeStarted {
		t.Fatalf("unexpected run decision: %#v", decision)
	}

	resetTendingStdout(io)
	if err := runTending(ctx, []string{"remove", "manual-scan", "--yes", "--actor", "tester", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	resetTendingStdout(io)
	if err := runTending(ctx, []string{"list", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	var jobs []tendingJobView
	if err := json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &jobs); err != nil {
		t.Fatalf("decode list JSON: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected removed job to be absent from runtime list, got %#v", jobs)
	}

	audit, err := os.ReadFile(io.AuditPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tending.job.created", "tending.job.run_now", "tending.job.removed"} {
		if !strings.Contains(string(audit), want) {
			t.Fatalf("audit log missing %s: %s", want, audit)
		}
	}
}

func TestTendingRunInvokesPreScript(t *testing.T) {
	io := newTestTendingIO(t)
	ctx := context.Background()
	scriptPath, markerPath := writeTendingPreScript(t)
	if err := runTending(ctx, []string{"create", "scripted-run", "--schedule", "on demand", "--script", scriptPath}, io); err != nil {
		t.Fatal(err)
	}

	resetTendingStdout(io)
	if err := runTending(ctx, []string{"run", "scripted-run", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	var decision scheduler.Decision
	if err := json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &decision); err != nil {
		t.Fatalf("decode run decision JSON: %v\nout=%q", err, tendingStdout(io))
	}
	if decision.JobID != "scripted-run" || decision.Outcome != scheduler.OutcomeStarted {
		t.Fatalf("unexpected run decision: %#v", decision)
	}

	assertTendingScriptInput(t, markerPath, "scripted-run")
	assertTendingRunSucceeded(t, io, decision.RunID)
}

func TestTendingTickInvokesDuePreScript(t *testing.T) {
	current := time.Date(2026, 5, 4, 3, 0, 0, 0, time.UTC)
	io := newTestTendingIO(t)
	io.Now = func() time.Time { return current }
	ctx := context.Background()
	scriptPath, markerPath := writeTendingPreScript(t)
	if err := runTending(ctx, []string{"create", "scripted-tick", "--schedule", "every 1s", "--script", scriptPath}, io); err != nil {
		t.Fatal(err)
	}

	current = current.Add(time.Second)
	resetTendingStdout(io)
	if err := runTending(ctx, []string{"tick", "--json"}, io); err != nil {
		t.Fatal(err)
	}
	var decisions []scheduler.Decision
	if err := json.Unmarshal(io.Stdout.(*bytes.Buffer).Bytes(), &decisions); err != nil {
		t.Fatalf("decode tick decisions JSON: %v\nout=%q", err, tendingStdout(io))
	}
	if len(decisions) != 1 || decisions[0].JobID != "scripted-tick" || decisions[0].Outcome != scheduler.OutcomeStarted {
		t.Fatalf("unexpected tick decisions: %#v", decisions)
	}

	assertTendingScriptInput(t, markerPath, "scripted-tick")
	assertTendingRunSucceeded(t, io, decisions[0].RunID)
}

func TestTendingEditRequiresEditor(t *testing.T) {
	t.Setenv("EDITOR", "")
	io := newTestTendingIO(t)
	err := runTending(context.Background(), []string{"edit", "missing-job"}, io)
	if err == nil || !strings.Contains(err.Error(), "EDITOR is required") {
		t.Fatalf("expected editor requirement, got %v", err)
	}
}

func writeTendingPreScript(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "pre-script.sh")
	markerPath := filepath.Join(dir, "pre-script-input.json")
	body := fmt.Sprintf(`#!/bin/sh
set -eu
cat > %q
printf 'pre-script ok\n'
printf '%%s\n' '{"wakeAgent":false,"context":{"scriptRan":true}}'
`, markerPath)
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatalf("write pre-script: %v", err)
	}
	return scriptPath, markerPath
}

func assertTendingScriptInput(t *testing.T, markerPath string, jobID string) {
	t.Helper()
	data, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("pre-script marker missing: %v", err)
	}
	var input struct {
		SchemaVersion int `json:"schemaVersion"`
		Job           struct {
			ID string `json:"id"`
		} `json:"job"`
		Run struct {
			JobID string `json:"jobId"`
		} `json:"run"`
		Canonical    map[string]json.RawMessage `json:"canonical"`
		Capabilities map[string]bool            `json:"capabilities"`
	}
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatalf("decode pre-script input: %v\ninput=%s", err, data)
	}
	if input.SchemaVersion == 0 || input.Job.ID != jobID || input.Run.JobID != jobID {
		t.Fatalf("unexpected pre-script input envelope: %#v", input)
	}
	if len(input.Canonical["scheduler"]) == 0 {
		t.Fatalf("pre-script input missing scheduler snapshot: %#v", input.Canonical)
	}
	if !input.Capabilities["tending.prescript.exec"] || !input.Capabilities["tending.action_executor"] {
		t.Fatalf("pre-script input missing runner capabilities: %#v", input.Capabilities)
	}
}

func assertTendingRunSucceeded(t *testing.T, io *tendingIO, runID string) {
	t.Helper()
	registry, err := openTendingRegistry(context.Background(), io)
	if err != nil {
		t.Fatalf("open registry: %v", err)
	}
	run, err := registry.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("get run %s: %v", runID, err)
	}
	if run.Status != scheduler.RunStatusSucceeded {
		t.Fatalf("run %s status = %s, error=%q", runID, run.Status, run.Error)
	}
	if run.Result == nil || run.Result.Context["scriptRan"] != true {
		t.Fatalf("run %s missing pre-script result context: %#v", runID, run.Result)
	}
	if _, ok := run.Context["preScript"]; !ok {
		t.Fatalf("run %s missing preScript metadata: %#v", runID, run.Context)
	}
}
