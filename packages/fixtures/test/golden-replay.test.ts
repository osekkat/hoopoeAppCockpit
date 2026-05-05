import { describe, expect, test } from "bun:test";
import { Buffer } from "node:buffer";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, relative } from "node:path";
import {
  createMockDaemonClient,
  deriveCursors,
  fixturesRoot,
  loadTendingScenario,
  MOCK_BEARER_TOKEN,
  MOCK_FLYWHEEL_HEALTH_TIME,
  MOCK_PAIRING_TOKEN,
  startReplay,
  type LoadedScenario,
  type ReplayEvent,
} from "../src/index.ts";
import type { CapabilityRegistry } from "@hoopoe/schemas";

const SCENARIOS = [
  "healthy-hour",
  "idle-but-not-stuck",
  "wedged-pane",
  "rate-limited-no-caam",
  "rate-limited-with-caam",
  "stale-reservation",
  "commit-burst",
  "missing-tool",
  "budget-breach",
  "action-arbitration",
  "skill-drift",
  "postcondition-failure",
] as const;

const PROJECT_ID = "mock-flywheel-project";
const UPDATE_GOLDENS = process.env.HOOPOE_UPDATE_GOLDENS === "1" || process.env.UPDATE_GOLDENS === "1";

describe("Mock Flywheel scenario golden artifacts", () => {
  for (const scenarioId of SCENARIOS) {
    test(`${scenarioId}: scenario source, event stream, and mock daemon responses match goldens`, async () => {
      const scenario = loadTendingScenario(scenarioId);
      assertCapabilityRegistryShape(scenario.capabilities);

      assertGolden(
        goldenPath(scenarioId, "scenario-source.first-read.json"),
        canonicalJson(scenarioSourceGolden(scenario)),
      );
      assertGolden(
        goldenPath(scenarioId, "event-stream.ndjson"),
        await eventStreamGolden(scenario),
      );
      assertGolden(
        goldenPath(scenarioId, "mock-daemon.responses.json"),
        canonicalJson(await mockDaemonGolden(scenario)),
      );
    });
  }
});

// hp-18wq: runtime determinism guard for the golden generators. The
// existing per-scenario tests above only enforce parity with the
// committed on-disk golden — useful, but a regression that produces
// the SAME nondeterministic output the first time would still slip
// through (whoever runs HOOPOE_UPDATE_GOLDENS=1 commits whatever
// the first run produced). This block catches drift earlier by
// running each generator twice in-process and asserting byte
// identity between runs. Failures here are deterministic
// nondeterminism — Math.random / Date.now leaks, unsorted Map
// iteration, async race in startReplay, or any future generator
// that picks up wall-clock state.
describe("Mock Flywheel scenario determinism baseline", () => {
  test("repeated generation is byte-identical across runs (no Math.random / Date.now / iteration-order leaks)", async () => {
    for (const scenarioId of SCENARIOS) {
      const scenarioA = loadTendingScenario(scenarioId);
      const scenarioB = loadTendingScenario(scenarioId);

      const sourceA = canonicalJson(scenarioSourceGolden(scenarioA));
      const sourceB = canonicalJson(scenarioSourceGolden(scenarioB));
      if (sourceA !== sourceB) {
        throw new Error(
          `scenario-source nondeterministic for ${scenarioId}\n${firstDiff(sourceA, sourceB)}`,
        );
      }

      const streamA = await eventStreamGolden(scenarioA);
      const streamB = await eventStreamGolden(scenarioB);
      if (streamA !== streamB) {
        throw new Error(
          `event-stream nondeterministic for ${scenarioId}\n${firstDiff(streamA, streamB)}`,
        );
      }

      const daemonA = canonicalJson(await mockDaemonGolden(scenarioA));
      const daemonB = canonicalJson(await mockDaemonGolden(scenarioB));
      if (daemonA !== daemonB) {
        throw new Error(
          `mock-daemon nondeterministic for ${scenarioId}\n${firstDiff(daemonA, daemonB)}`,
        );
      }

      // Sanity expect so the test count reflects the per-scenario
      // assertion even when all three are equal.
      expect(sourceA).toBe(sourceB);
    }
  });
});

function scenarioSourceGolden(scenario: LoadedScenario): unknown {
  return normalizeForGolden({
    id: scenario.id,
    rootPath: scenario.rootPath,
    meta: scenario.meta,
    bvTriage: scenario.bvTriage,
    brList: scenario.brList,
    ntmSnapshot: scenario.ntmSnapshot,
    agentMailDump: scenario.agentMailDump,
    reservations: scenario.reservations,
    events: scenario.events,
    paneLogs: scenario.paneLogs,
    buildLogs: scenario.buildLogs,
    capabilities: scenario.capabilities,
    toolsDegraded: scenario.toolsDegraded,
    expectedOutcome: scenario.expectedOutcome,
  });
}

async function eventStreamGolden(scenario: LoadedScenario): Promise<string> {
  const delivered: ReplayEvent[] = [];
  const session = startReplay({
    events: scenario.events,
    speed: "instant",
    subscriber: {
      onEvent(event) {
        delivered.push(event);
      },
    },
  });
  await session.done;

  const envelopes: unknown[] = delivered.map((event, index) => ({
    kind: "event",
    index,
    event,
  }));
  envelopes.push({
    kind: "end-cursors",
    cursors: session.cursors(),
  });
  envelopes.push({
    kind: "derived-cursors",
    cursors: deriveCursors(scenario.events),
  });

  return `${envelopes.map((envelope) => canonicalJsonLine(normalizeForGolden(envelope))).join("\n")}\n`;
}

async function mockDaemonGolden(scenario: LoadedScenario): Promise<unknown> {
  const client = createMockDaemonClient({
    scenarioId: scenario.id,
    speed: "instant",
  });
  const subscribedEvents: ReplayEvent[] = [];
  const subscribeSession = client.subscribe({}, (event) => {
    subscribedEvents.push(event);
  });
  await subscribeSession.done;
  const capabilities = client.capabilities();
  assertCapabilityRegistryShape(capabilities);

  const firstBuildLog = scenario.buildLogs[0]?.runId ?? "missing-run";
  const firstPaneLog = scenario.paneLogs[0]?.agent ?? "missing-agent";

  return normalizeForGolden({
    "system.health": client.health(),
    "system.version": client.version(),
    "system.capabilities": capabilities,
    "projects.list": client.listProjects(),
    "br.list": client.getBeads(PROJECT_ID),
    "bv.triage": client.getTriage(PROJECT_ID),
    "ntm.snapshot": client.getSwarmSnapshot(PROJECT_ID),
    "agent_mail.fetch_inbox": client.getMailDump(PROJECT_ID),
    "agent_mail.reservations": client.getReservations(PROJECT_ID),
    "build_logs.first": client.getBuildLog(firstBuildLog),
    "build_logs.missing": client.getBuildLog("missing-run"),
    "pane_logs.first": client.getPaneLog(firstPaneLog),
    "pane_logs.missing": client.getPaneLog("missing-agent"),
    "auth.exchangePairingForBearer": client.exchangePairingForBearer({
      pairingToken: MOCK_PAIRING_TOKEN,
    }),
    "auth.issueWsToken": client.issueWsToken({
      bearerToken: MOCK_BEARER_TOKEN,
    }),
    "events.subscribe": {
      delivered: subscribedEvents,
      cursors: client.currentCursors(),
    },
    "scenario.id": client.scenarioId(),
  });
}

function assertCapabilityRegistryShape(registry: CapabilityRegistry): void {
  if (
    registry.schemaVersion !== 1 ||
    typeof registry.snapshotAt !== "string" ||
    typeof registry.daemonApiVersion !== "string" ||
    typeof registry.fixturesVersion !== "string" ||
    typeof registry.tools !== "object" ||
    registry.tools === null ||
    Array.isArray(registry.tools)
  ) {
    throw new Error("Mock Flywheel capability fixture is not a CapabilityRegistry");
  }

  for (const [toolId, report] of Object.entries(registry.tools)) {
    if (
      report.tool !== toolId ||
      typeof report.version !== "string" ||
      typeof report.source !== "string" ||
      typeof report.lastCheckedAt !== "string" ||
      typeof report.fixturesVersion !== "string" ||
      typeof report.capabilities !== "object" ||
      report.capabilities === null ||
      Array.isArray(report.capabilities)
    ) {
      throw new Error(`Mock Flywheel capability report is malformed for ${toolId}`);
    }
    for (const [capabilityId, capability] of Object.entries(report.capabilities)) {
      if (typeof capability.status !== "string" || capability.status.length === 0) {
        throw new Error(`Mock Flywheel capability ${toolId}.${capabilityId} is missing status`);
      }
    }
  }
}

function assertGolden(path: string, actual: string): void {
  if (UPDATE_GOLDENS) {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, actual);
    return;
  }
  if (!existsSync(path)) {
    throw new Error(
      `Golden is missing: ${relative(process.cwd(), path)}. Run scripts/fixtures/regenerate-goldens.sh.`,
    );
  }
  const expected = readFileSync(path, "utf8");
  if (actual !== expected) {
    throw new Error(
      [
        `Golden mismatch: ${relative(process.cwd(), path)}`,
        firstDiff(expected, actual),
      ].join("\n"),
    );
  }
}

function goldenPath(scenarioId: string, fileName: string): string {
  return `${fixturesRoot()}/scenarios/${scenarioId}/.goldens/${fileName}`;
}

function canonicalJson(value: unknown): string {
  return `${JSON.stringify(canonicalize(value), null, 2)}\n`;
}

function canonicalJsonLine(value: unknown): string {
  return JSON.stringify(canonicalize(value));
}

function canonicalize(value: unknown): unknown {
  if (value instanceof Uint8Array) {
    return { encoding: "base64", data: Buffer.from(value).toString("base64") };
  }
  if (Array.isArray(value)) {
    return value.map((item) => canonicalize(item));
  }
  if (value !== null && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const key of Object.keys(value).sort()) {
      out[key] = canonicalize((value as Record<string, unknown>)[key]);
    }
    return out;
  }
  return value;
}

function normalizeForGolden(value: unknown): unknown {
  if (value instanceof Uint8Array) {
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((item) => normalizeForGolden(item));
  }
  if (value !== null && typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
      out[key] = normalizeField(key, child);
    }
    return out;
  }
  return scrubString(value);
}

function normalizeField(key: string, value: unknown): unknown {
  if (key === "rootPath" && typeof value === "string") {
    return value.replace(fixturesRoot(), "<fixtures-root>");
  }
  if (key === "capturedAt" && typeof value === "string") {
    return "<scrubbed-captured-at>";
  }
  // hp-2szb: the mock-flywheel health response now uses an injectable
  // clock that defaults to MOCK_FLYWHEEL_HEALTH_TIME. We pass the fixed
  // value through so the committed golden contains the literal — proving
  // the deterministic-clock contract — but we still scrub anything else
  // under `time` as a safety net for any future surface that accidentally
  // leaks runtime state.
  if (key === "time" && typeof value === "string") {
    if (value === MOCK_FLYWHEEL_HEALTH_TIME) {
      return value;
    }
    if (isIsoTimestamp(value)) {
      return "<scrubbed-runtime-time>";
    }
  }
  return normalizeForGolden(value);
}

function scrubString(value: unknown): unknown {
  if (typeof value !== "string") return value;
  return value.replaceAll(fixturesRoot(), "<fixtures-root>");
}

function isIsoTimestamp(value: string): boolean {
  return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{3})?Z$/.test(value);
}

function firstDiff(expected: string, actual: string): string {
  const expectedLines = expected.split("\n");
  const actualLines = actual.split("\n");
  const max = Math.max(expectedLines.length, actualLines.length);
  for (let i = 0; i < max; i++) {
    if (expectedLines[i] === actualLines[i]) continue;
    return [
      `First differing line: ${i + 1}`,
      `- ${expectedLines[i] ?? "<missing>"}`,
      `+ ${actualLines[i] ?? "<missing>"}`,
    ].join("\n");
  }
  return "No line-level diff found; content may differ by trailing bytes.";
}
