// Hoopoe-owned. Wizard "Try local demo" bootstrap orchestrator (hp-lddj).
//
// This module is the seam between the wizard UI (renderer; blocked on
// hp-z1x + hp-o6q) and the Mock Flywheel substrate (this commit's
// hp-o74 + the bundled fixture catalog).
//
// Flow (per hp-lddj LD-1 → LD-4):
//   1. User clicks "Try local demo" → wizard calls `startLocalDemo(id?)`.
//   2. We resolve the catalog entry, ensure the demo state dir exists.
//   3. We construct the in-process MockDaemonClient (matches what the
//      future spawned daemon will return; no SSH/VPS/subscriptions).
//   4. We mint mock pairing/bearer/WS tokens so AuthBridge wires up
//      against the mock as if it were real.
//   5. We register the MockFlywheelClient against the IpcRegistry the
//      caller passes in.
//   6. We return a `LocalDemoSession` handle the wizard introspects to
//      render banners, manage scenario swaps, and (on user confirm)
//      tear down via `endLocalDemo`.
//
// In v1.0, the daemon binary doesn't exist yet (Phase 2). When it
// lands, this module gains a `spawnLocalDaemon` step + FD-3 envelope
// per plan.md §5.2. Until then, the bootstrap runs entirely in-process
// — which is exactly what the bead wants for the "no VPS, no
// subscriptions" promise during the v1.0 → v1.x transition.

import {
  createMockDaemonClient,
  loadTendingScenario,
  type LoadedScenario,
  type MockDaemonClient,
} from "@hoopoe/fixtures";
import {
  DEFAULT_LOCAL_DEMO_ID,
  findLocalDemo,
  type LocalDemoFixture,
} from "./LocalDemoCatalog.ts";
import {
  ensureDemoRoot,
  resolveDemoStatePaths,
  wipeDemoRoot,
  type DemoStatePaths,
  type IsolatorOptions,
} from "./LocalDemoStateIsolator.ts";
import {
  registerMockFlywheelClient,
  type RegisteredMockFlywheelClient,
} from "./MockFlywheelClient.ts";
import {
  MOCK_FLYWHEEL_AUDIT_ACTOR,
  MOCK_FLYWHEEL_AUTH_TOKENS,
} from "./MockFlywheelMode.ts";
import type { IpcRegistry } from "./IpcRegistry.ts";
import type { ReplayEvent } from "@hoopoe/fixtures";

export interface StartLocalDemoOptions {
  /** Catalog id (e.g. "healthy-hour"). Defaults to DEFAULT_LOCAL_DEMO_ID. */
  fixtureId?: string;
  /** IpcRegistry to attach the mock-flywheel commands to. Required. */
  ipcRegistry: IpcRegistry;
  /** Sink for events delivered by the mock subscription. Optional —
   *  when absent, no subscription auto-starts. */
  emitEvent?: (event: ReplayEvent) => void;
  /** Override home directory (tests). */
  homedirImpl?: () => string;
  /** Window-title formatter override (tests). */
  formatWindowTitle?: (fixture: LocalDemoFixture) => string;
}

export interface LocalDemoSession {
  /** The catalog entry the user picked. */
  readonly fixture: LocalDemoFixture;
  /** Loaded scenario data (bv triage / br list / events / capabilities). */
  readonly scenario: LoadedScenario;
  /** Path resolutions for the demo's isolated state dir. */
  readonly paths: DemoStatePaths;
  /** The mock daemon client (renderer never reaches into it directly;
   *  goes through `ipcRegistration` commands). */
  readonly client: MockDaemonClient;
  /** IpcRegistry registration handle. */
  readonly ipcRegistration: RegisteredMockFlywheelClient;
  /** Window title the wizard sets while in demo mode (per hp-lddj
   *  PERSISTENT 'DEMO' BADGE → "[DEMO] Hoopoe — {fixture-name}"). */
  readonly windowTitle: string;
  /** Audit-actor stamp for every action recorded during this demo. */
  readonly auditActor: typeof MOCK_FLYWHEEL_AUDIT_ACTOR;
  /** Mock auth tokens (pairing / bearer / WS) — the wizard hands these
   *  to AuthBridge so the auth code path is exercised end-to-end. */
  readonly authTokens: typeof MOCK_FLYWHEEL_AUTH_TOKENS;
  /** Tear down: cancel subscriptions, unregister IPC commands.
   *  Does NOT wipe demo state on disk (that's `endLocalDemo` with
   *  `wipeState: true` for the explicit switch-to-real-VPS path). */
  readonly close: () => void;
}

/** Start a local-demo session. Synchronous: the entire bootstrap runs
 *  in-process for v1.0; spawning the daemon binary lands in Phase 2. */
export function startLocalDemo(options: StartLocalDemoOptions): LocalDemoSession {
  const fixtureId = options.fixtureId ?? DEFAULT_LOCAL_DEMO_ID;
  const fixture = findLocalDemo(fixtureId);

  // Per hp-lddj PROJECT STATE ISOLATION: write only under ~/.hoopoe/demo/<id>/.
  const isolatorOptions: IsolatorOptions =
    options.homedirImpl !== undefined ? { homedirImpl: options.homedirImpl } : {};
  const paths = ensureDemoRoot(fixture.id, isolatorOptions);

  // Load the backing scenario directly (we want the resolved scenario
  // exposed on the session for the renderer's banner UI).
  const scenario = loadTendingScenario(fixture.scenarioId);

  // Construct the mock daemon client.
  const client = createMockDaemonClient({
    scenarioId: fixture.scenarioId,
  });

  // Wire the mock client onto the IPC registry. The renderer's wizard
  // closes after this and the cockpit lands on `fixture.landingStage`.
  const registerOpts: Parameters<typeof registerMockFlywheelClient>[0] = {
    ipcRegistry: options.ipcRegistry,
    client,
    initialReplaySpeed: 1,
  };
  if (options.emitEvent !== undefined) {
    registerOpts.emitEvent = options.emitEvent;
  }
  const ipcRegistration = registerMockFlywheelClient(registerOpts);

  const windowTitle = options.formatWindowTitle
    ? options.formatWindowTitle(fixture)
    : `[DEMO] Hoopoe — ${fixture.title}`;

  return {
    fixture,
    scenario,
    paths,
    client,
    ipcRegistration,
    windowTitle,
    auditActor: MOCK_FLYWHEEL_AUDIT_ACTOR,
    authTokens: MOCK_FLYWHEEL_AUTH_TOKENS,
    close: () => {
      ipcRegistration.unregister();
    },
  };
}

export interface EndLocalDemoOptions {
  session: LocalDemoSession;
  /** When true, also wipe ~/.hoopoe/demo/<id>/ — used when the user
   *  explicitly clicks "Switch to real VPS". When false (default),
   *  state survives so the user can reopen the demo later. */
  wipeState?: boolean;
  /** Override home directory (tests). */
  homedirImpl?: () => string;
}

export interface EndLocalDemoResult {
  /** Whether the on-disk demo state was wiped. */
  readonly wiped: boolean;
  /** Path that was wiped (or would have been wiped if `wipeState=false`). */
  readonly demoRoot: string;
}

/** End a local-demo session. Optionally wipes ~/.hoopoe/demo/<id>/.
 *  Per hp-lddj UNIT TEST #7 ("Switch-to-real-VPS confirmation"). */
export function endLocalDemo(options: EndLocalDemoOptions): EndLocalDemoResult {
  options.session.close();
  if (!options.wipeState) {
    return { wiped: false, demoRoot: options.session.paths.demoRoot };
  }
  const wipeOpts: IsolatorOptions =
    options.homedirImpl !== undefined ? { homedirImpl: options.homedirImpl } : {};
  const result = wipeDemoRoot(options.session.fixture.id, wipeOpts);
  return { wiped: result.existed, demoRoot: result.demoRoot };
}

/** Switch the active demo to a different fixture. Cleanly tears down
 *  the current session and starts a fresh one with the new fixture's
 *  scenario data. Demo state directories are NOT wiped on swap (each
 *  fixture has its own dir under ~/.hoopoe/demo/). Per hp-lddj UNIT
 *  TEST #8 ("Fixture switching"). */
export function swapLocalDemoFixture(
  session: LocalDemoSession,
  newFixtureId: string,
  options: Omit<StartLocalDemoOptions, "fixtureId">,
): LocalDemoSession {
  session.close();
  return startLocalDemo({ ...options, fixtureId: newFixtureId });
}

/** For diagnostics: synthesize the LocalDemoSession's "summary card"
 *  payload — used by the renderer to render the persistent DEMO banner.
 *  Renderer never reaches into the session's `client` directly; it
 *  uses this card + the IPC commands. */
export function describeLocalDemoSession(session: LocalDemoSession): {
  fixtureId: string;
  fixtureTitle: string;
  fixtureDescription: string;
  scenarioId: string;
  landingStage: LocalDemoFixture["landingStage"];
  windowTitle: string;
  demoRoot: string;
  audit: { kind: string; source: string };
} {
  return {
    fixtureId: session.fixture.id,
    fixtureTitle: session.fixture.title,
    fixtureDescription: session.fixture.description,
    scenarioId: session.fixture.scenarioId,
    landingStage: session.fixture.landingStage,
    windowTitle: session.windowTitle,
    demoRoot: session.paths.demoRoot,
    audit: { kind: session.auditActor.kind, source: session.auditActor.source },
  };
}
