// `@hoopoe/fixture-replay` — event synthesis from snapshot captures (hp-q3t).
//
// Phase 0 fixtures don't ship a canonical events.jsonl (that's a §8.8
// tending-scenario concern). For renderer-shaped consumers we synthesize
// `adapter.captured` events deterministically from the per-tool invocation
// envelopes in snapshot.json:
//
//   - One event per (tool, invocation) pair.
//   - `seq` increments globally across (tool, invocation) tuples sorted
//     alphabetically — same fixture always produces the same event order.
//   - `ts` is the snapshot's capturedAt; events are not rate-limited.
//   - `payload` carries argv + exit code + byte counts (NOT stdout text,
//     since downstream consumers should fetch the full envelope explicitly
//     to keep event payloads small + redact-friendly).
//
// `synthesizeBaselineEvents` is what `bootMockFlywheel` calls at boot to
// pre-populate the emittedEvents() list so tests have something to inspect
// before they make their own adapter calls.

import type { ReplayEvent } from "@hoopoe/fixtures";
import type { LoadedPhase0Scenario, ToolCapture } from "./snapshot-loader.ts";

export interface BaselineEventPayload {
  tool: string;
  invocation: string;
  argv: readonly string[];
  exit: number;
  durationMs: number;
  stdoutBytes: number;
  stderrBytes: number;
  redacted: boolean;
  truncated: boolean;
  tags: readonly string[];
}

function captureToolList(scenario: LoadedPhase0Scenario): Array<readonly [string, ToolCapture]> {
  const entries = Object.entries(scenario.snapshot.captures);
  // Sort by tool slug for determinism.
  entries.sort((a, b) => a[0].localeCompare(b[0]));
  return entries;
}

export function synthesizeBaselineEvents(scenario: LoadedPhase0Scenario): ReplayEvent[] {
  const events: ReplayEvent[] = [];
  const ts = scenario.snapshot.meta.capturedAt;
  let seq = 1;
  for (const [toolSlug, capture] of captureToolList(scenario)) {
    if (!capture.present) {
      // Emit a single tool.absent event so renderers (and tests) can react
      // to capability-missing without having to infer it from absence.
      const payload: { tool: string; reason: string } = {
        tool: toolSlug,
        reason: capture.skipReason ?? "tool not present",
      };
      events.push({
        channel: "capabilities",
        seq,
        ts,
        type: "tool.absent",
        payload,
      });
      seq += 1;
      continue;
    }
    const invocations = Object.entries(capture.captures).sort((a, b) => a[0].localeCompare(b[0]));
    for (const [invocationName, envelope] of invocations) {
      const payload: BaselineEventPayload = {
        tool: toolSlug,
        invocation: invocationName,
        argv: envelope.argv,
        exit: envelope.exit,
        durationMs: envelope.durationMs,
        stdoutBytes: envelope.stdoutBytes,
        stderrBytes: envelope.stderrBytes,
        redacted: envelope.redacted ?? false,
        truncated: envelope.truncated ?? false,
        tags: envelope.tags ?? [],
      };
      events.push({
        channel: "adapter",
        seq,
        ts,
        type: "adapter.captured",
        payload,
      });
      seq += 1;
    }
  }
  return events;
}
