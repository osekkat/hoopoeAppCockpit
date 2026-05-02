// Hoopoe-owned. Bundled local-demo fixture catalog (hp-lddj).
//
// The first-run wizard's "Try local demo" path (§6.1) renders these as
// row choices in step LD-1 ("Pick fixture"). Each entry maps to a
// scenario directory under packages/fixtures/scenarios/.
//
// New fixtures get added here AND under packages/fixtures/scenarios/
// (with the seeder script regenerating the scenario files). Keep the
// catalog small + curated — this is the user-facing first impression.
//
// The wizard UI itself (CTAs, thumbnails, "Start demo" button) lives in
// the renderer and is blocked on hp-z1x (four-stage shell) + hp-o6q
// (wizard UI). This module just exposes the data + a typed lookup.

import type { TendingScenarioId } from "@hoopoe/fixtures";

export interface LocalDemoFixture {
  /** Stable id used in argv (`--scenario <id>`) and demoStateDir naming. */
  readonly id: string;
  /** User-facing one-line title shown in the wizard row. */
  readonly title: string;
  /** Brief copy under the title — what's interesting about this scenario. */
  readonly description: string;
  /** Stage the cockpit lands on after MFM bootstrap (which stage is most
   *  interesting for this fixture). */
  readonly landingStage: "planning" | "beads" | "swarm" | "hardening";
  /** Approx wall-clock to interesting state at 1× replay (informational). */
  readonly approxRuntimeS: number;
  /** Scenario id under packages/fixtures/scenarios/ that backs this demo. */
  readonly scenarioId: TendingScenarioId;
  /** Optional thumbnail asset path (renderer-relative). Empty when no
   *  asset exists yet — the wizard renders a generated SVG placeholder. */
  readonly thumbnailPath?: string;
}

/** v1 catalog: 4 curated demos per hp-lddj LD-1 spec. Ordered for the
 *  wizard's row layout (top = most relatable). */
export const LOCAL_DEMO_CATALOG: readonly LocalDemoFixture[] = [
  {
    id: "healthy-hour",
    title: "Healthy hour",
    description:
      "A small swarm humming along: 4 active panes, no wedging, mail quiet. Use this to see the cockpit's resting state.",
    landingStage: "swarm",
    approxRuntimeS: 5,
    scenarioId: "healthy-hour",
  },
  {
    id: "wedged-pane",
    title: "Wedged pane",
    description:
      "An agent stuck mid-tool-call for 18 minutes, holding a file reservation. Watch tend-swarm propose ask_status → kill → force_release.",
    landingStage: "swarm",
    approxRuntimeS: 8,
    scenarioId: "wedged-pane",
  },
  {
    id: "rate-limited-with-caam",
    title: "Rate-limited with CAAM alternative",
    description:
      "Claude rate-limited mid-task; CAAM has a backup account at 12% usage. Watch the switch_account ActionPlan + agent.resume.",
    landingStage: "swarm",
    approxRuntimeS: 6,
    scenarioId: "rate-limited-with-caam",
  },
  {
    id: "rate-limited-no-caam",
    title: "Rate-limited (no alternative)",
    description:
      "Claude rate-limited; no CAAM alternative configured. Pre-script proposes pause + surfaces retry_after countdown.",
    landingStage: "swarm",
    approxRuntimeS: 6,
    scenarioId: "rate-limited-no-caam",
  },
];

export class UnknownLocalDemoError extends Error {
  override readonly name = "UnknownLocalDemoError";
  readonly demoId: string;
  constructor(demoId: string) {
    super(`Unknown local-demo fixture: ${demoId}`);
    this.demoId = demoId;
  }
}

/** Look up a catalog entry by id. Throws UnknownLocalDemoError if missing
 *  (typo in argv / persisted preference). */
export function findLocalDemo(id: string): LocalDemoFixture {
  const found = LOCAL_DEMO_CATALOG.find((d) => d.id === id);
  if (!found) {
    throw new UnknownLocalDemoError(id);
  }
  return found;
}

/** All catalog ids (used by argv parsing + tests). */
export const LOCAL_DEMO_IDS: readonly string[] = LOCAL_DEMO_CATALOG.map((d) => d.id);

/** Default demo when the user clicks "Try local demo" without a previous
 *  selection — matches the bead's "default scenario is healthy-hour". */
export const DEFAULT_LOCAL_DEMO_ID = "healthy-hour";
