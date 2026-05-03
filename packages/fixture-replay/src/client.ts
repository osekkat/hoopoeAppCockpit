// `@hoopoe/fixture-replay` — instrumented daemon-like client (hp-q3t).
//
// Wraps a loaded Phase 0 scenario in a thin RPC-shaped surface that
// records every adapter call + lets tests append synthesized events to
// the same in-memory event log.
//
// Renderer parity: the methods exposed here mirror the daemon RPC the
// production renderer talks to (plan.md §2.6 seed contract:
// `/v1/health`, `/v1/capabilities`, etc.). Adapter calls are routed
// through `callAdapter(adapter, method)` so the test asserts on
// _adapter intent_ instead of digging into invocation envelope names.
// `getAdapterInvocation` is the lower-level escape hatch when a test
// genuinely needs the captured argv/exit/stdout from snapshot.json.

import type { ReplayEvent } from "@hoopoe/fixtures";
import type {
  CapabilityDescriptor,
  InvocationEnvelope,
  LoadedPhase0Scenario,
  ToolCapture,
} from "./snapshot-loader.ts";
import type { StageId } from "./stages.ts";
import { stageForAdapter } from "./stages.ts";
import { synthesizeBaselineEvents } from "./events.ts";

/** AdapterId accepts the canonical AdapterSlug union plus any string the
 *  scenario's adapter-index lists (so future tools added without an
 *  AdapterSlug enum bump are still callable). */
export type AdapterId = string;

export interface AdapterCallRecord {
  adapter: AdapterId;
  /** Logical method name the test passed (e.g. "list", "triage", "snapshot"). */
  method: string;
  /** Wall-clock ms since boot. Used for stable ordering, not assertions. */
  tickMs: number;
  /** Index into emittedEvents() that was emitted for this call (if any). */
  emittedEventSeq: number | null;
  /** Whether the underlying tool was present in the snapshot. Calls against
   *  absent tools still record (so tests can assert on missing-tool flow),
   *  but the harness emits `adapter.degraded` instead of `adapter.invoked`. */
  toolPresent: boolean;
}

export interface ReplayClient {
  /** Scenario id (e.g. "fresh"). */
  scenarioId: () => string;
  /** Loaded scenario root for low-level introspection. */
  scenario: () => LoadedPhase0Scenario;
  /** Daemon-shaped health probe. Always reports "mock-flywheel" environment
   *  so renderer code paths that assert on environment do the right thing. */
  health: () => { status: "ok"; environment: "mock-flywheel"; time: string };
  /** Per-adapter capability map flattened to the daemon's `/v1/capabilities`
   *  shape: `{ "<adapter>.<feature>": CapabilityDescriptor }`. */
  capabilities: () => Record<string, CapabilityDescriptor>;
  /** Map of adapter slug → was the binary present at capture time. */
  toolPresence: () => Record<string, boolean>;
  /** Adapters declared in adapter-index.json (the contract the scenario
   *  promises to exercise). */
  declaredAdapters: () => readonly string[];
  /** Record an adapter call. Returns the ToolCapture if the adapter was
   *  present, or `null` if absent. Either way the call is recorded. */
  callAdapter: (adapter: AdapterId, method: string) => ToolCapture | null;
  /** Look up a single captured invocation by name (e.g. "status_porcelain"
   *  for `git`). Returns `null` if either the tool or the invocation is
   *  missing in the snapshot. Does NOT record a call — pair it with
   *  `callAdapter` if you want both invocation data and call recording. */
  getAdapterInvocation: (adapter: AdapterId, invocation: string) => InvocationEnvelope | null;
  /** Force a specific stage to be marked as reached without triggering an
   *  adapter call (e.g. for renderer-only flows that don't go through an
   *  adapter). */
  markStageReached: (stage: StageId, reason?: string) => void;
  /** Stages that have been reached (via callAdapter or markStageReached). */
  reachedStages: () => readonly StageId[];
  /** Append an event to the in-memory event log. Tests use this to feed
   *  synthesized events into `assertNoUnredactedSecrets` etc. */
  emit: (event: ReplayEvent) => void;
  /** All events emitted so far, in emission order. Includes the baseline
   *  set synthesized at boot. */
  emittedEvents: () => readonly ReplayEvent[];
  /** Every adapter call recorded so far. */
  recordedCalls: () => readonly AdapterCallRecord[];
  /** Idempotent teardown. Future calls throw. */
  close: () => void;
}

interface ClientState {
  closed: boolean;
  bootMs: number;
  baselineEventCount: number;
  events: ReplayEvent[];
  calls: AdapterCallRecord[];
  reachedStages: Set<StageId>;
  stageMarkers: Array<{ stage: StageId; reason: string; tickMs: number }>;
  scenario: LoadedPhase0Scenario;
}

function flattenCapabilities(scenario: LoadedPhase0Scenario): Record<string, CapabilityDescriptor> {
  const out: Record<string, CapabilityDescriptor> = {};
  for (const [toolSlug, capture] of Object.entries(scenario.snapshot.captures)) {
    if (!capture.capabilities) continue;
    for (const [featureKey, descriptor] of Object.entries(capture.capabilities)) {
      // Capability keys in the snapshot are already namespaced
      // ("git.status.read"), but enforce the prefix so renderers don't have
      // to handle two shapes.
      const key = featureKey.startsWith(`${toolSlug}.`) ? featureKey : `${toolSlug}.${featureKey}`;
      out[key] = descriptor;
    }
  }
  return out;
}

function toolPresenceMap(scenario: LoadedPhase0Scenario): Record<string, boolean> {
  const out: Record<string, boolean> = {};
  for (const [toolSlug, capture] of Object.entries(scenario.snapshot.captures)) {
    out[toolSlug] = capture.present;
  }
  // Adapters listed in adapter-index but missing from captures (e.g. `rch`
  // in the current Phase 0 corpus) report as absent rather than throwing —
  // the contract is "every declared adapter has a known presence answer".
  for (const adapter of scenario.adapterIndex.adapters) {
    if (!(adapter in out)) {
      out[adapter] = false;
    }
  }
  return out;
}

interface CreateReplayClientOptions {
  /** Override the boot wall clock (for tests that pin time). Default: Date.now(). */
  now?: () => number;
}

export function createReplayClient(
  scenario: LoadedPhase0Scenario,
  options: CreateReplayClientOptions = {},
): ReplayClient {
  const now = options.now ?? (() => Date.now());
  const bootMs = now();
  const baseline = synthesizeBaselineEvents(scenario);
  const state: ClientState = {
    closed: false,
    bootMs,
    baselineEventCount: baseline.length,
    events: [...baseline],
    calls: [],
    reachedStages: new Set<StageId>(),
    stageMarkers: [],
    scenario,
  };

  function ensureOpen(method: string): void {
    if (state.closed) {
      throw new Error(`ReplayClient.${method} called after close()`);
    }
  }

  function tickMs(): number {
    return now() - state.bootMs;
  }

  function nextSeq(): number {
    return state.events.length === 0 ? 1 : (state.events.at(-1)?.seq ?? 0) + 1;
  }

  function emitInternal(event: ReplayEvent): void {
    state.events.push(event);
  }

  function recordStagesForAdapter(adapter: AdapterId): void {
    // stageForAdapter is typed against AdapterSlug; cast is safe at runtime —
    // adapters not in the canonical slug set simply don't map to any stage.
    const stages = stageForAdapter(adapter as Parameters<typeof stageForAdapter>[0]);
    for (const stage of stages) {
      if (!state.reachedStages.has(stage)) {
        state.reachedStages.add(stage);
        state.stageMarkers.push({ stage, reason: `adapter:${adapter}`, tickMs: tickMs() });
      }
    }
  }

  return {
    scenarioId: () => scenario.scenarioId,
    scenario: () => scenario,
    health: () => ({
      status: "ok" as const,
      environment: "mock-flywheel" as const,
      time: new Date(now()).toISOString(),
    }),
    capabilities: () => flattenCapabilities(scenario),
    toolPresence: () => toolPresenceMap(scenario),
    declaredAdapters: () => scenario.adapterIndex.adapters,

    callAdapter: (adapter, method) => {
      ensureOpen("callAdapter");
      const capture = scenario.snapshot.captures[adapter] ?? null;
      const present = capture?.present === true;
      const seq = nextSeq();
      const event: ReplayEvent = {
        channel: "adapter",
        seq,
        ts: scenario.snapshot.meta.capturedAt,
        type: present ? "adapter.invoked" : "adapter.degraded",
        payload: present
          ? { tool: adapter, method }
          : { tool: adapter, method, reason: capture?.skipReason ?? "tool not present" },
      };
      emitInternal(event);
      state.calls.push({
        adapter,
        method,
        tickMs: tickMs(),
        emittedEventSeq: seq,
        toolPresent: present,
      });
      recordStagesForAdapter(adapter);
      return capture;
    },

    getAdapterInvocation: (adapter, invocation) => {
      ensureOpen("getAdapterInvocation");
      const capture = scenario.snapshot.captures[adapter];
      if (!capture || !capture.present) return null;
      return capture.captures[invocation] ?? null;
    },

    markStageReached: (stage, reason) => {
      ensureOpen("markStageReached");
      if (state.reachedStages.has(stage)) return;
      state.reachedStages.add(stage);
      state.stageMarkers.push({
        stage,
        reason: reason ?? "explicit",
        tickMs: tickMs(),
      });
    },

    reachedStages: () => Array.from(state.reachedStages),

    emit: (event) => {
      ensureOpen("emit");
      // Defensive: enforce monotonic seq within the harness so test-emitted
      // events don't collide with synthesized ones.
      const expected = nextSeq();
      const adjusted: ReplayEvent = event.seq < expected
        ? { ...event, seq: expected }
        : event;
      emitInternal(adjusted);
    },

    emittedEvents: () => state.events.slice(),
    recordedCalls: () => state.calls.slice(),

    close: () => {
      if (state.closed) return;
      state.closed = true;
    },
  };
}
