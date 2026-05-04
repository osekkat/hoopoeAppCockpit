// hp-o6q — wizard state IO + ReplaySink tests.

import { expect, test } from "bun:test";
import {
  WIZARD_STATE_SCHEMA_VERSION,
  WizardReplaySink,
  WizardStateError,
  appendCheckpoint,
  fromStateFile,
  recordPath,
  startRun,
  toStateFile,
} from "./index.ts";

test("startRun: requires non-empty runId + records the start timestamp", () => {
  expect(() => startRun({ runId: "" })).toThrow(/empty_run_id/);
  expect(() => startRun({ runId: "   " })).toThrow(/empty_run_id/);
  const run = startRun({ runId: "r1", now: () => new Date("2026-05-04T00:00:00Z") });
  expect(run.runId).toBe("r1");
  expect(run.startedAt).toBe("2026-05-04T00:00:00.000Z");
  expect(run.checkpoints).toEqual([]);
  expect(run.path).toBeNull();
});

test("appendCheckpoint: returns new run with the event tacked on", () => {
  const run = startRun({ runId: "r1" });
  const next = appendCheckpoint(run, {
    stepId: "path",
    outcome: "completed",
    data: { path: "existing_vps" },
    now: () => new Date("2026-05-04T01:23:45Z"),
  });
  expect(next).not.toBe(run); // immutable update
  expect(run.checkpoints.length).toBe(0);
  expect(next.checkpoints.length).toBe(1);
  expect(next.checkpoints[0]).toEqual({
    stepId: "path",
    outcome: "completed",
    recordedAt: "2026-05-04T01:23:45.000Z",
    data: { path: "existing_vps" },
  });
});

test("appendCheckpoint: refuses unknown outcomes", () => {
  const run = startRun({ runId: "r" });
  expect(() =>
    appendCheckpoint(run, { stepId: "path", outcome: "exploded" as never }),
  ).toThrow(/invalid_outcome/);
});

test("appendCheckpoint: persists the failure block when present", () => {
  const run = startRun({ runId: "r" });
  const next = appendCheckpoint(run, {
    stepId: "preflight",
    outcome: "failed",
    failure: { code: "ssh_timeout", message: "ssh handshake timed out after 10s" },
  });
  expect(next.checkpoints[0]?.failure).toEqual({
    code: "ssh_timeout",
    message: "ssh handshake timed out after 10s",
  });
});

test("recordPath: returns a new run with the path set", () => {
  const run = startRun({ runId: "r" });
  const next = recordPath(run, "local_demo");
  expect(run.path).toBeNull();
  expect(next.path).toBe("local_demo");
});

test("toStateFile + fromStateFile: round-trip", () => {
  const r1 = appendCheckpoint(startRun({ runId: "alpha" }), {
    stepId: "path",
    outcome: "completed",
  });
  const r2 = startRun({ runId: "beta" });
  const file = toStateFile([r1, r2]);
  expect(file.schemaVersion).toBe(WIZARD_STATE_SCHEMA_VERSION);
  expect(file.runs.length).toBe(2);
  const parsed = fromStateFile(JSON.parse(JSON.stringify(file)));
  expect(parsed.length).toBe(2);
  expect(parsed[0]?.runId).toBe("alpha");
  expect(parsed[1]?.runId).toBe("beta");
});

test("fromStateFile: rejects schema mismatch + non-array runs", () => {
  expect(() => fromStateFile(null)).toThrow(/not_object/);
  expect(() => fromStateFile({ schemaVersion: 99, runs: [] })).toThrow(/schema_mismatch/);
  expect(() => fromStateFile({ schemaVersion: WIZARD_STATE_SCHEMA_VERSION, runs: "no" })).toThrow(/not_array/);
});

test("WizardReplaySink: beginRun + recordCheckpoint + active()", () => {
  const sink = new WizardReplaySink();
  expect(sink.active()).toBeNull();
  sink.beginRun({ runId: "rs1" });
  expect(sink.active()?.runId).toBe("rs1");
  sink.recordCheckpoint({ stepId: "path", outcome: "completed" });
  expect(sink.active()?.checkpoints.length).toBe(1);
  expect(sink.list().length).toBe(1);
});

test("WizardReplaySink: starting a second run preserves history", () => {
  const sink = new WizardReplaySink();
  sink.beginRun({ runId: "rs1" });
  sink.recordCheckpoint({ stepId: "path", outcome: "completed" });
  sink.beginRun({ runId: "rs2" });
  expect(sink.list().length).toBe(2);
  expect(sink.list()[0]?.runId).toBe("rs1");
  expect(sink.list()[1]?.runId).toBe("rs2");
  expect(sink.active()?.runId).toBe("rs2");
});

test("WizardReplaySink: recordActivePath + recordCheckpoint refuse without an active run", () => {
  const sink = new WizardReplaySink();
  expect(() => sink.recordCheckpoint({ stepId: "path", outcome: "completed" })).toThrow(
    /no_active_run/,
  );
  expect(() => sink.recordActivePath("local_demo")).toThrow(/no_active_run/);
});

test("WizardReplaySink: toFile reflects the current runs", () => {
  const sink = new WizardReplaySink();
  sink.beginRun({ runId: "rs" });
  sink.recordActivePath("existing_vps");
  sink.recordCheckpoint({ stepId: "path", outcome: "completed" });
  const file = sink.toFile();
  expect(file.runs[0]?.path).toBe("existing_vps");
  expect(file.runs[0]?.checkpoints[0]?.stepId).toBe("path");
});

test("WizardStateError: stable name + carries code", () => {
  const err = new WizardStateError("invalid_outcome", "test");
  expect(err.name).toBe("WizardStateError");
  expect(err.code).toBe("invalid_outcome");
  expect(err.message).toContain("invalid_outcome");
});
