// `@hoopoe/fixture-replay` — assertion helpers (hp-q3t).
//
// Each helper throws an `AssertionError`-shaped exception with a precise
// message so test runners (`bun:test`, `vitest`) surface the cause without
// further drilling. Helpers are pure with respect to the client (they
// inspect `recordedCalls()` / `reachedStages()` / `emittedEvents()` —
// they never mutate state).

import type { ReplayEvent } from "@hoopoe/fixtures";
import type { ReplayClient } from "./client.ts";
import { isStageId, type StageId } from "./stages.ts";
import { scanEventsForSecrets, type SecretScanResult } from "./secret-scan.ts";

export class FixtureReplayAssertionError extends Error {
  override readonly name = "FixtureReplayAssertionError";
  constructor(message: string) {
    super(message);
  }
}

/** Assert the client has reached `stage` (via either `callAdapter` mapping
 *  or an explicit `markStageReached`). */
export function expectStageReached(client: ReplayClient, stage: StageId | string): void {
  if (!isStageId(stage)) {
    throw new FixtureReplayAssertionError(
      `expectStageReached: '${stage}' is not a known stage id`,
    );
  }
  const reached = client.reachedStages();
  if (!reached.includes(stage)) {
    const calls = client
      .recordedCalls()
      .map((c) => `${c.adapter}.${c.method}`)
      .join(", ");
    throw new FixtureReplayAssertionError(
      `expectStageReached: stage '${stage}' not reached for scenario '${client.scenarioId()}'. ` +
        `Reached stages: [${reached.join(", ") || "none"}]. ` +
        `Recorded calls: [${calls || "none"}].`,
    );
  }
}

/** Assert that `client.callAdapter(adapter, method)` was invoked at least
 *  once. Method is matched exactly. */
export function expectAdapterCalled(
  client: ReplayClient,
  adapter: string,
  method: string,
): void {
  const calls = client.recordedCalls();
  for (const c of calls) {
    if (c.adapter === adapter && c.method === method) return;
  }
  const seen = calls.map((c) => `${c.adapter}.${c.method}`).join(", ");
  throw new FixtureReplayAssertionError(
    `expectAdapterCalled: '${adapter}.${method}' not called for scenario '${client.scenarioId()}'. ` +
      `Recorded calls: [${seen || "none"}].`,
  );
}

/** Assert that `client.callAdapter(adapter, method)` was NOT invoked. */
export function expectAdapterNotCalled(
  client: ReplayClient,
  adapter: string,
  method: string,
): void {
  const calls = client.recordedCalls();
  for (const c of calls) {
    if (c.adapter === adapter && c.method === method) {
      throw new FixtureReplayAssertionError(
        `expectAdapterNotCalled: '${adapter}.${method}' was called (tickMs=${c.tickMs}, ` +
          `present=${c.toolPresent}) for scenario '${client.scenarioId()}'.`,
      );
    }
  }
}

/** Get the events the client has emitted (baseline + test-driven). */
export function getEmittedEvents(client: ReplayClient): readonly ReplayEvent[] {
  return client.emittedEvents();
}

/** Run the secret-shape scanner over the events and throw if any unredacted
 *  secret-like substring is found. Allow-listed mock literals
 *  (`MOCKMOCKMOCK`, etc.) are excluded; see `secret-scan.ts`. */
export function assertNoUnredactedSecrets(events: readonly ReplayEvent[]): SecretScanResult {
  const result = scanEventsForSecrets(events);
  if (result.findings.length > 0) {
    const summary = result.findings
      .slice(0, 5)
      .map((f) => `${f.eventRef} :: ${f.rule} :: ${f.evidence}`)
      .join(" | ");
    const more = result.findings.length > 5 ? ` (+${result.findings.length - 5} more)` : "";
    throw new FixtureReplayAssertionError(
      `assertNoUnredactedSecrets: scanned ${result.events} events, found ${result.findings.length} ` +
        `unredacted secret-shape match(es): ${summary}${more}`,
    );
  }
  return result;
}
