// `@hoopoe/fixture-replay` — public boot entrypoint (hp-q3t).
//
// `bootMockFlywheel({scenario})` is the one-liner tests use to spin up a
// deterministic Phase 0 client. Same scenario id + same fixture corpus =
// byte-identical client state at boot, by construction:
//   - The snapshot is read once and held in memory (immutable).
//   - The baseline event list is derived deterministically from the
//     snapshot's tool captures (sorted alphabetically; see `events.ts`).
//   - The wall clock is exposed via the optional `now` override so tests
//     can assert on `tickMs` deltas without wall-clock flakiness.
//
// `bootScenarioLibrary()` returns one client per Phase 0 scenario, useful
// for parameterized tests that exercise the same flow across fresh /
// active / failure.

import { PHASE0_SCENARIOS, type Phase0ScenarioId } from "@hoopoe/fixtures";
import { createReplayClient, type ReplayClient } from "./client.ts";
import { loadPhase0Snapshot, type LoadPhase0Options } from "./snapshot-loader.ts";

export interface BootMockFlywheelOptions extends LoadPhase0Options {
  scenario: Phase0ScenarioId;
  /** Override the boot clock (default `Date.now`). */
  now?: () => number;
}

export function bootMockFlywheel(options: BootMockFlywheelOptions): ReplayClient {
  const { scenario, now, ...loadOptions } = options;
  const loaded = loadPhase0Snapshot(scenario, loadOptions);
  return createReplayClient(loaded, now !== undefined ? { now } : {});
}

export interface BootScenarioLibraryOptions extends LoadPhase0Options {
  /** Restrict to a subset of Phase 0 scenarios. Default: all three. */
  scenarios?: readonly Phase0ScenarioId[];
  /** Shared boot clock applied to every client. */
  now?: () => number;
}

export function bootScenarioLibrary(
  options: BootScenarioLibraryOptions = {},
): ReadonlyArray<{ scenario: Phase0ScenarioId; client: ReplayClient }> {
  const ids = options.scenarios ?? PHASE0_SCENARIOS;
  const out: Array<{ scenario: Phase0ScenarioId; client: ReplayClient }> = [];
  for (const scenario of ids) {
    const bootOptions: BootMockFlywheelOptions = { scenario };
    if (options.rootPath !== undefined) bootOptions.rootPath = options.rootPath;
    if (options.now !== undefined) bootOptions.now = options.now;
    out.push({ scenario, client: bootMockFlywheel(bootOptions) });
  }
  return out;
}
