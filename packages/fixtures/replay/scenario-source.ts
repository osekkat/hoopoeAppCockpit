// `@hoopoe/fixtures/replay` — scenario source loader (hp-o74).
//
// Loads a scenario directory from the corpus and returns typed views of
// its contents. The replayer + mock daemon client consume this; tests can
// substitute a synthetic ScenarioSource.
//
// Design notes:
// - Pure functions (no I/O caching). Callers cache if needed.
// - Reads happen at scenario-load time, not at every adapter call —
//   payloads are held in memory once loaded.
// - Strict bounds: a malformed scenario throws ScenarioLoadError early
//   rather than silently dropping fields.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, resolve } from "node:path";
import {
  type FixtureMeta,
  type TendingScenarioId,
  type Phase0ScenarioId,
} from "../src/kinds.ts";
import { fixturesRoot, phase0ScenarioPath, scenarioPath } from "../src/loader.ts";
import type {
  Capability,
  CapabilityRegistry,
  CapabilityStatus,
  ToolId,
  ToolReport,
} from "@hoopoe/schemas";

export class ScenarioLoadError extends Error {
  override readonly name = "ScenarioLoadError";
  readonly scenarioId: string;
  override readonly cause: unknown;
  constructor(scenarioId: string, message: string, cause?: unknown) {
    super(`Scenario '${scenarioId}': ${message}`);
    this.scenarioId = scenarioId;
    this.cause = cause;
  }
}

/** Replayable WS event envelope. Mirrors the shape the daemon emits per
 *  `plan.md` §2.6. Adapter consumers MUST use `seq` + `channel` to
 *  reconstruct order on reconnect. */
export interface ReplayEvent {
  channel: string;
  seq: number;
  ts: string;
  type: string;
  payload?: unknown;
}

export interface PaneLog {
  agent: string;
  bytes: Uint8Array;
}

export interface BuildLog {
  runId: string;
  text: string;
}

/** Loaded scenario state — the replayer consumes this. */
export interface LoadedScenario {
  id: string;
  rootPath: string;
  meta: FixtureMeta;
  bvTriage: unknown;
  brList: unknown;
  ntmSnapshot: unknown;
  agentMailDump: unknown;
  reservations: unknown;
  events: ReplayEvent[];
  paneLogs: PaneLog[];
  buildLogs: BuildLog[];
  capabilities: CapabilityRegistry;
  toolsDegraded: Record<string, unknown> | null;
  expectedOutcome: unknown;
}

const TOOL_ID_ALIASES: Record<string, ToolId> = {
  health: "health_generic",
};

const TOOL_IDS = new Set<ToolId>([
  "ntm",
  "br",
  "bv",
  "agent_mail",
  "git",
  "ru",
  "caam",
  "caut",
  "dcg",
  "casr",
  "pt",
  "srp",
  "sbh",
  "ubs",
  "jsm",
  "jfp",
  "oracle",
  "rch",
  "rano",
  "health_ts",
  "health_py",
  "health_rs",
  "health_go",
  "health_generic",
]);

const CAPABILITY_STATUSES = new Set<CapabilityStatus>([
  "ok",
  "degraded",
  "missing",
  "blocked-by-policy",
  "untested",
]);

const CAPABILITY_TRANSPORTS = new Set<NonNullable<Capability["transport"]>>([
  "websocket",
  "sse",
  "http",
  "stdio",
  "fixture",
]);

function readJson<T = unknown>(scenarioId: string, path: string, optional: false): T;
function readJson<T = unknown>(scenarioId: string, path: string, optional: true): T | null;
function readJson<T = unknown>(scenarioId: string, path: string, optional: boolean): T | null {
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    if (optional) return null;
    throw new ScenarioLoadError(scenarioId, `failed to read ${path}`, err);
  }
  try {
    return JSON.parse(text) as T;
  } catch (err) {
    throw new ScenarioLoadError(scenarioId, `failed to parse JSON at ${path}`, err);
  }
}

function readNdjson(scenarioId: string, path: string): ReplayEvent[] {
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    throw new ScenarioLoadError(scenarioId, `failed to read events.jsonl at ${path}`, err);
  }
  const events: ReplayEvent[] = [];
  const lines = text.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i] ?? "";
    const line = raw.trim();
    if (line.length === 0) continue;
    let parsed: unknown;
    try {
      parsed = JSON.parse(line);
    } catch (err) {
      throw new ScenarioLoadError(
        scenarioId,
        `events.jsonl line ${i + 1} did not parse as JSON`,
        err,
      );
    }
    if (
      typeof parsed !== "object" ||
      parsed === null ||
      typeof (parsed as ReplayEvent).channel !== "string" ||
      typeof (parsed as ReplayEvent).seq !== "number" ||
      typeof (parsed as ReplayEvent).ts !== "string" ||
      typeof (parsed as ReplayEvent).type !== "string"
    ) {
      throw new ScenarioLoadError(
        scenarioId,
        `events.jsonl line ${i + 1} missing channel/seq/ts/type`,
      );
    }
    events.push(parsed as ReplayEvent);
  }
  return events;
}

function readPaneLogs(scenarioId: string, dir: string): PaneLog[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return [];
  }
  const logs: PaneLog[] = [];
  for (const name of entries) {
    if (!name.endsWith(".bin")) continue;
    const fp = join(dir, name);
    try {
      const buf = readFileSync(fp);
      logs.push({ agent: name.replace(/\.bin$/, ""), bytes: new Uint8Array(buf) });
    } catch (err) {
      throw new ScenarioLoadError(scenarioId, `failed to read pane-log ${fp}`, err);
    }
  }
  return logs.sort((a, b) => a.agent.localeCompare(b.agent));
}

function readBuildLogs(scenarioId: string, dir: string): BuildLog[] {
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return [];
  }
  const logs: BuildLog[] = [];
  for (const name of entries) {
    if (!name.endsWith(".txt")) continue;
    const fp = join(dir, name);
    try {
      logs.push({ runId: name.replace(/\.txt$/, ""), text: readFileSync(fp, "utf8") });
    } catch (err) {
      throw new ScenarioLoadError(scenarioId, `failed to read build-log ${fp}`, err);
    }
  }
  return logs.sort((a, b) => a.runId.localeCompare(b.runId));
}

function normalizeToolId(scenarioId: string, rawToolId: string): ToolId {
  const toolId = TOOL_ID_ALIASES[rawToolId] ?? rawToolId;
  if (TOOL_IDS.has(toolId as ToolId)) {
    return toolId as ToolId;
  }
  throw new ScenarioLoadError(
    scenarioId,
    `capabilities.json contains unknown tool id '${rawToolId}'`,
  );
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function readStringField(value: Record<string, unknown>, key: string): string | undefined {
  const field = value[key];
  return typeof field === "string" ? field : undefined;
}

function normalizeCapability(
  scenarioId: string,
  toolId: ToolId,
  capId: string,
  raw: unknown,
): Capability {
  if (!isObject(raw)) {
    throw new ScenarioLoadError(
      scenarioId,
      `capabilities.json ${toolId}.${capId} must be an object`,
    );
  }
  const rawStatus = raw.status;
  if (typeof rawStatus !== "string" || !CAPABILITY_STATUSES.has(rawStatus as CapabilityStatus)) {
    throw new ScenarioLoadError(
      scenarioId,
      `capabilities.json ${toolId}.${capId}.status must be a CapabilityStatus`,
    );
  }

  const capability: Capability = { status: rawStatus as CapabilityStatus };
  const fallback = readStringField(raw, "fallback");
  const transport = readStringField(raw, "transport");
  const notes = readStringField(raw, "notes");
  if (fallback !== undefined) capability.fallback = fallback;
  if (transport !== undefined) {
    if (!CAPABILITY_TRANSPORTS.has(transport as NonNullable<Capability["transport"]>)) {
      throw new ScenarioLoadError(
        scenarioId,
        `capabilities.json ${toolId}.${capId}.transport must be a valid transport`,
      );
    }
    capability.transport = transport as NonNullable<Capability["transport"]>;
  }
  if (notes !== undefined) capability.notes = notes;
  return capability;
}

function isToolReport(raw: Record<string, unknown>): raw is ToolReport {
  return (
    typeof raw.tool === "string" &&
    typeof raw.version === "string" &&
    typeof raw.source === "string" &&
    isObject(raw.capabilities) &&
    typeof raw.lastCheckedAt === "string" &&
    typeof raw.fixturesVersion === "string"
  );
}

function normalizeToolReport(
  scenarioId: string,
  rawToolId: string,
  raw: unknown,
  meta: FixtureMeta,
): ToolReport {
  const toolId = normalizeToolId(scenarioId, rawToolId);
  if (!isObject(raw)) {
    throw new ScenarioLoadError(
      scenarioId,
      `capabilities.json entry for '${rawToolId}' must be an object`,
    );
  }

  if (isToolReport(raw)) {
    if (raw.tool !== toolId) {
      throw new ScenarioLoadError(
        scenarioId,
        `capabilities.json report key '${rawToolId}' disagrees with tool '${raw.tool}'`,
      );
    }
    return {
      ...raw,
      capabilities: normalizeCapabilitiesMap(scenarioId, toolId, raw.capabilities),
    };
  }

  return {
    tool: toolId,
    version: "",
    source: "fixture",
    capabilities: normalizeCapabilitiesMap(scenarioId, toolId, raw),
    lastCheckedAt: meta.capturedAt,
    fixturesVersion: meta.fixturesVersion,
  };
}

function normalizeCapabilitiesMap(
  scenarioId: string,
  toolId: ToolId,
  rawCapabilities: Record<string, unknown>,
): Record<string, Capability> {
  const capabilities: Record<string, Capability> = {};
  for (const [capId, rawCapability] of Object.entries(rawCapabilities)) {
    capabilities[capId] = normalizeCapability(scenarioId, toolId, capId, rawCapability);
  }
  return capabilities;
}

function normalizeCapabilityRegistry(
  scenarioId: string,
  raw: unknown,
  meta: FixtureMeta,
): CapabilityRegistry {
  if (!isObject(raw)) {
    throw new ScenarioLoadError(scenarioId, "capabilities.json must be a JSON object");
  }

  if (
    raw.schemaVersion === 1 &&
    typeof raw.snapshotAt === "string" &&
    typeof raw.daemonApiVersion === "string" &&
    typeof raw.fixturesVersion === "string" &&
    isObject(raw.tools)
  ) {
    const tools: Record<string, ToolReport> = {};
    for (const [rawToolId, rawReport] of Object.entries(raw.tools)) {
      tools[normalizeToolId(scenarioId, rawToolId)] = normalizeToolReport(
        scenarioId,
        rawToolId,
        rawReport,
        meta,
      );
    }
    return {
      schemaVersion: 1,
      snapshotAt: raw.snapshotAt,
      daemonApiVersion: raw.daemonApiVersion,
      fixturesVersion: raw.fixturesVersion,
      tools,
    };
  }

  const tools: Record<string, ToolReport> = {};
  for (const [rawToolId, rawReport] of Object.entries(raw)) {
    tools[normalizeToolId(scenarioId, rawToolId)] = normalizeToolReport(
      scenarioId,
      rawToolId,
      rawReport,
      meta,
    );
  }
  return {
    schemaVersion: 1,
    snapshotAt: meta.capturedAt,
    daemonApiVersion: "0.1.0",
    fixturesVersion: meta.fixturesVersion,
    tools,
  };
}

export interface LoadScenarioOptions {
  /** Override the corpus root. Defaults to `fixturesRoot()`. */
  corpusRoot?: string;
  /** When true, treat the scenarioPath as a complete path rather than a name
   *  to resolve under `corpusRoot/scenarios/`. */
  isAbsolutePath?: boolean;
}

/** Load a §8.8 tending scenario by id. */
export function loadTendingScenario(
  id: TendingScenarioId | string,
  options: LoadScenarioOptions = {},
): LoadedScenario {
  const rootPath = options.isAbsolutePath
    ? resolve(id)
    : options.corpusRoot
      ? resolve(options.corpusRoot, "scenarios", id)
      : scenarioPath(id as TendingScenarioId);
  return loadScenarioFromPath(id, rootPath);
}

/** Load a Phase 0 real-VPS scenario by id (fresh / active / failure). */
export function loadPhase0Scenario(
  id: Phase0ScenarioId,
  options: LoadScenarioOptions = {},
): LoadedScenario {
  const rootPath = options.corpusRoot
    ? resolve(options.corpusRoot, "phase0-2026-05-02", "scenarios", id)
    : phase0ScenarioPath(id);
  return loadScenarioFromPath(id, rootPath);
}

function loadScenarioFromPath(id: string, rootPath: string): LoadedScenario {
  let stat;
  try {
    stat = statSync(rootPath);
  } catch (err) {
    throw new ScenarioLoadError(id, `directory not found at ${rootPath}`, err);
  }
  if (!stat.isDirectory()) {
    throw new ScenarioLoadError(id, `path is not a directory: ${rootPath}`);
  }

  const meta = readJson<FixtureMeta>(id, join(rootPath, "meta.json"), false);
  const rawCapabilities = readJson<unknown>(id, join(rootPath, "capabilities.json"), false);
  return {
    id,
    rootPath,
    meta,
    bvTriage: readJson<unknown>(id, join(rootPath, "bv-triage.json"), false),
    brList: readJson<unknown>(id, join(rootPath, "br-list.json"), false),
    ntmSnapshot: readJson<unknown>(id, join(rootPath, "ntm-snapshot.json"), false),
    agentMailDump: readJson<unknown>(id, join(rootPath, "agent-mail-dump.json"), false),
    reservations: readJson<unknown>(id, join(rootPath, "reservations.json"), false),
    events: readNdjson(id, join(rootPath, "events.jsonl")),
    paneLogs: readPaneLogs(id, join(rootPath, "pane-logs")),
    buildLogs: readBuildLogs(id, join(rootPath, "build-logs")),
    capabilities: normalizeCapabilityRegistry(id, rawCapabilities, meta),
    toolsDegraded: readJson<Record<string, unknown>>(id, join(rootPath, "tools-degraded.json"), true),
    expectedOutcome: readJson<unknown>(id, join(rootPath, "expected-outcome.json"), false),
  };
}

/** List the scenario IDs that exist on disk under `<corpusRoot>/scenarios/`.
 *  Used by the Diagnostics scenario picker (`hp-o74` UI). */
export function listAvailableScenarios(corpusRoot?: string): string[] {
  const root = resolve(corpusRoot ?? fixturesRoot(), "scenarios");
  let entries: string[];
  try {
    entries = readdirSync(root);
  } catch {
    return [];
  }
  return entries
    .filter((name) => {
      const p = join(root, name, "meta.json");
      try {
        return statSync(p).isFile();
      } catch {
        return false;
      }
    })
    .sort();
}
