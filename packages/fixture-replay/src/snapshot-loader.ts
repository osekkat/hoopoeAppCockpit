// `@hoopoe/fixture-replay` — Phase 0 fixture loader (hp-q3t).
//
// Reads the on-disk shape produced by `scripts/research-spike/snapshot.sh`:
//   <root>/snapshot.json      — top-level meta + per-tool captures
//   <root>/adapter-index.json — declared adapter list
//   <root>/adapters/<tool>.json — per-tool snapshot (mirror of snapshot.json's
//                                 captures.<tool>)
//   <root>/prepare/{command.txt,transcript.txt,status.json}
//   <root>/snapshot.{stdout,stderr}
//
// This loader is read-only and side-effect-free. The harness in `boot.ts`
// holds the loaded scenario in memory once and re-uses it for the lifetime
// of the test.

import { readFileSync, statSync } from "node:fs";
import { resolve } from "node:path";
import {
  PHASE0_SCENARIOS,
  phase0ScenarioPath,
  type Phase0ScenarioId,
} from "@hoopoe/fixtures";

export class SnapshotLoadError extends Error {
  override readonly name = "SnapshotLoadError";
  readonly scenarioId: string;
  readonly path: string;
  override readonly cause: unknown;
  constructor(scenarioId: string, path: string, message: string, cause?: unknown) {
    super(`Phase 0 scenario '${scenarioId}' (${path}): ${message}`);
    this.scenarioId = scenarioId;
    this.path = path;
    this.cause = cause;
  }
}

/** A single CLI invocation captured by snapshot.sh. */
export interface InvocationEnvelope {
  argv: readonly string[];
  exit: number;
  durationMs: number;
  stdoutBytes: number;
  stderrBytes: number;
  stdoutText?: string;
  stderrText?: string;
  stdoutJson?: unknown;
  truncated?: boolean;
  redacted?: boolean;
  tags?: readonly string[];
}

/** Capability declaration the adapter reports for one feature. */
export interface CapabilityDescriptor {
  status: "ok" | "degraded" | "missing" | "blocked-by-policy" | "untested";
  notes?: string;
  fallback?: string;
}

/** Snapshot of one tool: was it present, what version, what invocations were
 *  captured, what capabilities does it declare. Mirrors `captures.<tool>` in
 *  snapshot.json. */
export interface ToolCapture {
  tool: string;
  present: boolean;
  binPath?: string;
  version?: string;
  capabilities?: Record<string, CapabilityDescriptor>;
  captures: Record<string, InvocationEnvelope>;
  errors?: readonly string[];
  skipReason?: string;
  capturedAt?: string;
}

/** Top-level meta block. */
export interface SnapshotMeta {
  snapshotVersion: string;
  snapshotSchemaUrl: string;
  capturedAt: string;
  vpsId: string;
  scenario: Phase0ScenarioId | string;
  fixturesVersion: string;
  host: {
    uname?: string;
    lsbRelease?: string;
    kernel?: string;
    cpuCount?: number;
    memTotalKb?: number;
    diskFree?: string;
  };
  toolVersions: Record<string, string | null>;
  captureDurationMs?: number;
}

/** Top-level snapshot.json shape. */
export interface Snapshot {
  meta: SnapshotMeta;
  captures: Record<string, ToolCapture>;
}

/** Top-level adapter-index.json shape. */
export interface AdapterIndex {
  scenario: string;
  mode: "real-vps" | "synthetic" | string;
  adapters: readonly string[];
}

/** Status of the prepare/ subdirectory (`prepare/status.json`). */
export interface PrepareStatus {
  label: string;
  capturedAt: string;
  skipped: boolean;
  reason?: string;
}

/** A loaded Phase 0 scenario. Held in memory by the harness; tests should
 *  treat it as immutable. */
export interface LoadedPhase0Scenario {
  scenarioId: Phase0ScenarioId;
  rootPath: string;
  snapshot: Snapshot;
  adapterIndex: AdapterIndex;
  prepareStatus: PrepareStatus | null;
}

export function isPhase0ScenarioId(value: string): value is Phase0ScenarioId {
  return (PHASE0_SCENARIOS as readonly string[]).includes(value);
}

function readJson<T>(scenarioId: string, path: string): T {
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    throw new SnapshotLoadError(scenarioId, path, `failed to read file`, err);
  }
  try {
    return JSON.parse(text) as T;
  } catch (err) {
    throw new SnapshotLoadError(scenarioId, path, `failed to parse JSON`, err);
  }
}

function readJsonOptional<T>(scenarioId: string, path: string): T | null {
  try {
    statSync(path);
  } catch {
    return null;
  }
  return readJson<T>(scenarioId, path);
}

function assertSnapshotShape(scenarioId: string, path: string, parsed: unknown): asserts parsed is Snapshot {
  if (typeof parsed !== "object" || parsed === null) {
    throw new SnapshotLoadError(scenarioId, path, "snapshot.json is not an object");
  }
  const v = parsed as Record<string, unknown>;
  if (typeof v.meta !== "object" || v.meta === null) {
    throw new SnapshotLoadError(scenarioId, path, "snapshot.json missing meta");
  }
  if (typeof v.captures !== "object" || v.captures === null) {
    throw new SnapshotLoadError(scenarioId, path, "snapshot.json missing captures");
  }
  const meta = v.meta as Record<string, unknown>;
  if (typeof meta.scenario !== "string" || meta.scenario.length === 0) {
    throw new SnapshotLoadError(scenarioId, path, "snapshot.meta.scenario missing");
  }
  if (meta.scenario !== scenarioId) {
    throw new SnapshotLoadError(
      scenarioId,
      path,
      `snapshot.meta.scenario='${meta.scenario}' does not match expected '${scenarioId}'`,
    );
  }
}

function assertAdapterIndexShape(scenarioId: string, path: string, parsed: unknown): asserts parsed is AdapterIndex {
  if (typeof parsed !== "object" || parsed === null) {
    throw new SnapshotLoadError(scenarioId, path, "adapter-index.json is not an object");
  }
  const v = parsed as Record<string, unknown>;
  if (!Array.isArray(v.adapters) || v.adapters.some((a) => typeof a !== "string")) {
    throw new SnapshotLoadError(scenarioId, path, "adapter-index.adapters must be string[]");
  }
}

export interface LoadPhase0Options {
  /** Override the scenario directory. By default uses `phase0ScenarioPath(id)`
   *  from `@hoopoe/fixtures`, which resolves under the corpus's
   *  `phase0-2026-05-02/scenarios/<id>/`. */
  rootPath?: string;
}

/** Load a Phase 0 scenario (`fresh` | `active` | `failure`). */
export function loadPhase0Snapshot(
  scenarioId: Phase0ScenarioId,
  options: LoadPhase0Options = {},
): LoadedPhase0Scenario {
  if (!isPhase0ScenarioId(scenarioId)) {
    throw new SnapshotLoadError(
      scenarioId,
      "<unresolved>",
      `unknown Phase 0 scenario; expected one of ${PHASE0_SCENARIOS.join(", ")}`,
    );
  }
  const rootPath = options.rootPath ?? phase0ScenarioPath(scenarioId);
  let stat;
  try {
    stat = statSync(rootPath);
  } catch (err) {
    throw new SnapshotLoadError(scenarioId, rootPath, "scenario directory not found", err);
  }
  if (!stat.isDirectory()) {
    throw new SnapshotLoadError(scenarioId, rootPath, "scenario path is not a directory");
  }

  const snapshotPath = resolve(rootPath, "snapshot.json");
  const adapterIndexPath = resolve(rootPath, "adapter-index.json");
  const preparePath = resolve(rootPath, "prepare", "status.json");

  const snapshotRaw = readJson<unknown>(scenarioId, snapshotPath);
  assertSnapshotShape(scenarioId, snapshotPath, snapshotRaw);

  const adapterIndexRaw = readJson<unknown>(scenarioId, adapterIndexPath);
  assertAdapterIndexShape(scenarioId, adapterIndexPath, adapterIndexRaw);

  const prepareStatus = readJsonOptional<PrepareStatus>(scenarioId, preparePath);

  return {
    scenarioId,
    rootPath,
    snapshot: snapshotRaw,
    adapterIndex: adapterIndexRaw,
    prepareStatus,
  };
}
