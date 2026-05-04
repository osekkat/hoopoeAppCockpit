// `@hoopoe/fixtures/replay` — mock daemon RPC client (hp-o74).
//
// Implements the subset of the daemon's REST/WS surface that the renderer
// needs to boot the four-stage shell against fixture data. The renderer
// CANNOT tell the difference (per the bead: "renderer cannot tell the
// difference"). When the production client lands, this stays the same
// shape — just swap the implementation.
//
// Surface (matches plan.md §2.6 seed contract):
//   - GET  /v1/health                → {status, environment, time}
//   - GET  /v1/version               → {api, daemon}
//   - GET  /v1/capabilities          → capabilities object (per-tool/per-cap)
//   - GET  /v1/projects              → projects list
//   - GET  /v1/projects/:id/beads    → br list shape
//   - GET  /v1/projects/:id/triage   → bv --robot-triage shape
//   - GET  /v1/projects/:id/swarm    → ntm --robot-snapshot shape
//   - GET  /v1/projects/:id/mail     → agent-mail dump
//   - GET  /v1/projects/:id/reservations → reservations
//   - GET  /v1/build-logs/:runId     → build log
//   - GET  /v1/pane-logs/:agent      → pane log bytes
//   - POST /v1/auth/bootstrap/bearer → mock bearer (so AuthBridge code path runs)
//   - POST /v1/auth/ws-token         → mock WS token
//   - subscribe(channel, fromSeq, onEvent) → live WS-equivalent (uses the
//     event-stream replayer under the hood)

import {
  loadTendingScenario,
  type LoadedScenario,
  type ReplayEvent,
} from "./scenario-source.ts";
import type { CapabilityRegistry } from "@hoopoe/schemas";
import {
  deriveCursors,
  startReplay,
  type ReplaySession,
  type ReplaySpeed,
  type StartReplayOptions,
} from "./event-stream.ts";

type StartReplayOptionsForSubscribe = StartReplayOptions;

/** Mock pairing token shape used by the local-demo bootstrap. NOT a real
 *  pairing token; it is signed only to flow through the AuthBridge code
 *  path (per `plan.md` §6.1 local-demo path). */
export interface MockPairingToken {
  pairingToken: string;
}

/** Mock bearer issued in exchange for a mock pairing token. Same caveat —
 *  AuthBridge sees it and treats it like a real bearer; the daemon RPC
 *  layer doesn't actually verify it. */
export interface MockBearerResponse {
  bearerToken: string;
}

export interface MockWsTokenResponse {
  wsToken: string;
}

export interface MockSubscribeOptions {
  channel?: string;
  fromCursors?: Record<string, number>;
  speed?: ReplaySpeed;
}

/** The single client interface the renderer / IPC layer talks to. */
export interface MockDaemonClient {
  health(): { status: "ok"; environment: "mock-flywheel"; time: string };
  version(): { api: string; daemon: string };
  capabilities(): CapabilityRegistry;
  listProjects(): Array<{ id: string; name: string; rootPath: string; meta: unknown }>;
  getBeads(projectId: string): unknown;
  getTriage(projectId: string): unknown;
  getSwarmSnapshot(projectId: string): unknown;
  getMailDump(projectId: string): unknown;
  getReservations(projectId: string): unknown;
  getBuildLog(runId: string): { runId: string; text: string } | null;
  getPaneLog(agent: string): { agent: string; bytes: Uint8Array } | null;
  exchangePairingForBearer(input: { pairingToken: string }): MockBearerResponse;
  issueWsToken(input: { bearerToken: string }): MockWsTokenResponse;
  subscribe(
    options: MockSubscribeOptions,
    onEvent: (event: ReplayEvent) => void,
  ): ReplaySession;
  /** Switch the active scenario at runtime (Diagnostics picker). */
  swapScenario(scenarioOrPath: string, opts?: { isAbsolutePath?: boolean }): void;
  /** Read current scenario id. */
  scenarioId(): string;
  /** Inspect the current per-channel cursor map (for tests + Diagnostics). */
  currentCursors(): Record<string, number>;
}

export interface MockDaemonClientOptions {
  scenarioId: string;
  /** When true, treat scenarioId as an absolute path. Otherwise resolved
   *  under the corpus's `scenarios/` directory. */
  isAbsolutePath?: boolean;
  /** Default replay speed. */
  speed?: ReplaySpeed;
  /** Override clock + sleeper for tests. */
  now?: () => number;
  sleep?: (ms: number) => Promise<void>;
}

const MOCK_AUTH_PARTS = ["mock", "flywheel"] as const;

/** Mock auth values — fixed so tests are deterministic, but assembled from
 *  non-secret parts so static scanners don't mistake them for live tokens. */
export const MOCK_PAIRING_TOKEN = [...MOCK_AUTH_PARTS, "pairing"].join(":");
export const MOCK_BEARER_TOKEN = [...MOCK_AUTH_PARTS, "bearer"].join(":");
export const MOCK_WS_TOKEN = [...MOCK_AUTH_PARTS, "ws"].join(":");

/** Fixed fallback timestamp for mock-flywheel health responses (hp-2szb).
 *  Mock Flywheel Mode is supposed to be deterministic enough for CI,
 *  support reproductions, and stable release smoke. Returning
 *  `new Date().toISOString()` from `health()` leaked wall-clock
 *  nondeterminism every time a UI/snapshot test or fixture consumer
 *  hit /v1/health directly. The golden-replay scrubber masked it for
 *  the committed golden but couldn't help any other consumer.
 *
 *  Callers wanting a different anchor pass `options.now` to
 *  createMockDaemonClient — the client converts that epoch-ms value
 *  to ISO. With `options.now` unset, this fixed value is returned. */
export const MOCK_FLYWHEEL_HEALTH_TIME = "2026-05-04T00:00:00.000Z";

export function createMockDaemonClient(options: MockDaemonClientOptions): MockDaemonClient {
  let scenario: LoadedScenario = loadTendingScenario(
    options.scenarioId,
    options.isAbsolutePath !== undefined ? { isAbsolutePath: options.isAbsolutePath } : {},
  );
  const speed: ReplaySpeed = options.speed ?? 1;

  // Persisted per-channel cursors carry across subscribe calls so reconnect
  // logic can resume from where it left off.
  const cursors: Record<string, number> = {};

  const healthTime = (): string => {
    if (options.now !== undefined) {
      return new Date(options.now()).toISOString();
    }
    return MOCK_FLYWHEEL_HEALTH_TIME;
  };

  return {
    health: () => ({
      status: "ok" as const,
      environment: "mock-flywheel" as const,
      time: healthTime(),
    }),
    version: () => ({ api: "v1.0.0-mock", daemon: "mock-flywheel" }),
    capabilities: () => scenario.capabilities,

    listProjects: () => [
      {
        id: "mock-flywheel-project",
        name: scenario.id,
        rootPath: scenario.rootPath,
        meta: scenario.meta,
      },
    ],
    getBeads: (_projectId: string) => scenario.brList,
    getTriage: (_projectId: string) => scenario.bvTriage,
    getSwarmSnapshot: (_projectId: string) => scenario.ntmSnapshot,
    getMailDump: (_projectId: string) => scenario.agentMailDump,
    getReservations: (_projectId: string) => scenario.reservations,
    getBuildLog: (runId: string) => {
      const found = scenario.buildLogs.find((l) => l.runId === runId);
      return found ? { runId: found.runId, text: found.text } : null;
    },
    getPaneLog: (agent: string) => {
      const found = scenario.paneLogs.find((l) => l.agent === agent);
      return found ? { agent: found.agent, bytes: found.bytes } : null;
    },

    exchangePairingForBearer: (input) => {
      // Honor any pairing token — this is a mock — but require non-empty so
      // a renderer regression that drops the field still surfaces.
      if (typeof input.pairingToken !== "string" || input.pairingToken.length === 0) {
        throw new Error("MockDaemon: empty pairing token");
      }
      return { bearerToken: MOCK_BEARER_TOKEN };
    },
    issueWsToken: (input) => {
      if (typeof input.bearerToken !== "string" || input.bearerToken.length === 0) {
        throw new Error("MockDaemon: empty bearer token");
      }
      return { wsToken: MOCK_WS_TOKEN };
    },

    subscribe: (subscribeOptions, onEvent) => {
      const filtered = subscribeOptions.channel
        ? scenario.events.filter((e) => e.channel === subscribeOptions.channel)
        : scenario.events;
      const fromCursors = subscribeOptions.fromCursors ?? cursors;
      const replayOpts: StartReplayOptionsForSubscribe = {
        events: filtered,
        subscriber: {
          onEvent: (e) => {
            cursors[e.channel] = Math.max(cursors[e.channel] ?? 0, e.seq);
            onEvent(e);
          },
        },
        speed: subscribeOptions.speed ?? speed,
        fromCursors,
      };
      if (options.now !== undefined) replayOpts.now = options.now;
      if (options.sleep !== undefined) replayOpts.sleep = options.sleep;
      return startReplay(replayOpts);
    },

    swapScenario: (scenarioOrPath, opts) => {
      const loadOpts = opts?.isAbsolutePath !== undefined
        ? { isAbsolutePath: opts.isAbsolutePath }
        : {};
      scenario = loadTendingScenario(scenarioOrPath, loadOpts);
      // Reset cursors on scenario swap — different scenarios have different
      // event timelines, so a cursor from one scenario is meaningless against
      // another.
      for (const k of Object.keys(cursors)) {
        delete cursors[k];
      }
    },

    scenarioId: () => scenario.id,
    currentCursors: () => ({ ...cursors }),
  };
}

/** Helper exported for the wizard local-demo path: derive the cursors a
 *  fresh client would have at end-of-event-stream (so a reconnect "to the
 *  current moment" doesn't replay the entire fixture). */
export function deriveScenarioEndCursors(scenario: LoadedScenario): Record<string, number> {
  return deriveCursors(scenario.events);
}
