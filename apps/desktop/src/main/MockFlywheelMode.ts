// Hoopoe-owned. Main-process entrypoint for Mock Flywheel Mode (hp-o74).
//
// What this does:
//   - Parses `--mock-flywheel` and `--fixture-path` argv flags.
//   - When mock mode is on, swaps the daemon-RPC client for the
//     fixture-replayer-backed `MockDaemonClient` from `@hoopoe/fixtures`.
//   - Issues mock pairing tokens / mock bearers / mock WS tokens so the
//     real `AuthBridge` code path is exercised end-to-end (per the bead's
//     "Mock Flywheel must NOT replace daemon authentication" rule).
//   - Exposes a tiny "are we in mock mode?" predicate the rest of main
//     can read to gate behavior (top-bar 'MOCK MODE' indicator, scenario
//     picker in Diagnostics, audit-actor stamping).
//
// What this does NOT do:
//   - Render the 'MOCK MODE' top-bar pill (renderer concern; lives in
//     @hoopoe/design-system + the Phase 1 four-stage shell hp-z1x).
//   - Implement the Diagnostics scenario picker UI (renderer concern).
//   - Wire IPC handlers — that lives in `MockFlywheelClient.ts` so the
//     IpcRegistry boundary is unit-testable separately.
//
// Cross-references:
//   - bead hp-o74 (this bead's spec)
//   - bead hp-lddj (first-run wizard's "Try local demo" CTA — calls
//     `parseArgvForMockFlywheel` to detect mock-mode after spawn)
//   - bead hp-dr8 (cross-cutting Mock Flywheel epic; this is the desktop
//     side; daemon side TBD)
//   - plan.md §13 "Must include for development and demos"
//   - plan.md §6.1 "Local-demo path"
//
// Hard guardrails honored:
//   - No provider SDK reach (Guardrail 11) — the mock daemon never
//     touches any real model provider.
//   - The 'MOCK MODE' indicator is unmistakable in the audit log:
//     audit entries get `actor.kind = 'mock'` so a fixture run can never
//     be confused with a real swarm action (per `hp-lddj` UX spec).
//   - Activity-panel state for mock runs is namespaced under
//     ~/.hoopoe/demo/<fixture-id>/ (per `hp-lddj` "PROJECT STATE
//     ISOLATION") — concretely written by the daemon side; the desktop
//     side declares the path here so renderers can introspect it.

import { resolve } from "node:path";
import { homedir } from "node:os";
import {
  createMockDaemonClient,
  MOCK_BEARER_TOKEN,
  MOCK_PAIRING_TOKEN,
  MOCK_WS_TOKEN,
  type MockDaemonClient,
} from "@hoopoe/fixtures";

/** Argv flags we recognize. Passed through unchanged to the rest of main. */
export const MOCK_FLYWHEEL_FLAG = "--mock-flywheel";
export const FIXTURE_PATH_FLAG = "--fixture-path";
export const SCENARIO_FLAG = "--scenario";

export type MockFlywheelMode =
  | { enabled: false }
  | {
      enabled: true;
      /** Either a directory under packages/fixtures/scenarios/ or, when
       *  `isAbsolutePath`, a complete path to a scenario directory. */
      scenario: string;
      isAbsolutePath: boolean;
      /** ~/.hoopoe/demo/<fixture-id>/ — the daemon-side state directory.
       *  Renderers read this to display the "namespaced demo state"
       *  banner; the daemon writes here. */
      demoStateDir: string;
    };

export interface ParseArgvOptions {
  /** `process.argv` slice; default `process.argv`. Override for tests. */
  argv?: readonly string[];
  /** Override `os.homedir()` for tests. */
  homedirImpl?: () => string;
}

/** Parse argv for mock-mode flags. Returns a discriminated union the rest
 *  of main can branch on without duplicating flag-parsing logic. */
export function parseArgvForMockFlywheel(options: ParseArgvOptions = {}): MockFlywheelMode {
  const argv = options.argv ?? process.argv;
  const enabled = argv.includes(MOCK_FLYWHEEL_FLAG);
  if (!enabled) {
    return { enabled: false };
  }
  const fixtureIdx = argv.indexOf(FIXTURE_PATH_FLAG);
  const scenarioIdx = argv.indexOf(SCENARIO_FLAG);
  let scenario: string;
  let isAbsolutePath: boolean;
  if (fixtureIdx >= 0 && fixtureIdx + 1 < argv.length) {
    scenario = argv[fixtureIdx + 1] as string;
    isAbsolutePath = true;
  } else if (scenarioIdx >= 0 && scenarioIdx + 1 < argv.length) {
    scenario = argv[scenarioIdx + 1] as string;
    isAbsolutePath = false;
  } else {
    // Default scenario per the bead: 'healthy-hour' is what `bun run
    // dev:mock` boots into.
    scenario = "healthy-hour";
    isAbsolutePath = false;
  }
  const home = (options.homedirImpl ?? homedir)();
  // Demo-state isolation per hp-lddj "PROJECT STATE ISOLATION":
  //   ~/.hoopoe/demo/<fixture-id>/
  // For absolute paths, derive the fixture id from the directory basename.
  const fixtureId = isAbsolutePath ? scenario.split("/").filter(Boolean).pop() ?? "scenario" : scenario;
  const demoStateDir = resolve(home, ".hoopoe", "demo", fixtureId);
  return {
    enabled: true,
    scenario,
    isAbsolutePath,
    demoStateDir,
  };
}

/** Build the mock daemon client when mock mode is enabled. Throws if mode
 *  is not enabled — callers must check first. */
export function createMockFlywheelDaemon(mode: MockFlywheelMode): MockDaemonClient {
  if (!mode.enabled) {
    throw new Error(
      "MockFlywheelMode: createMockFlywheelDaemon called while mode.enabled=false. " +
        "Check parseArgvForMockFlywheel() result first.",
    );
  }
  return createMockDaemonClient({
    scenarioId: mode.scenario,
    isAbsolutePath: mode.isAbsolutePath,
  });
}

/** Token triple the AuthBridge accepts when running against a mock daemon.
 *  Exported so the wizard's local-demo bootstrap (hp-lddj) can hand them to
 *  AuthBridge without round-tripping through HTTP. */
export const MOCK_FLYWHEEL_AUTH_TOKENS = {
  pairingToken: MOCK_PAIRING_TOKEN,
  bearerToken: MOCK_BEARER_TOKEN,
  wsToken: MOCK_WS_TOKEN,
} as const;

/** Audit-actor stamp every mock-mode action receives so the audit log can
 *  never confuse a fixture replay with a real swarm action.
 *  See hp-lddj "PERSISTENT 'DEMO' BADGE" + plan.md §10 audit log. */
export const MOCK_FLYWHEEL_AUDIT_ACTOR = {
  kind: "mock" as const,
  source: "mock-flywheel",
} as const;
