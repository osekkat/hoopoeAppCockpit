// hp-l5d — Planning stage data loader.
//
// Plans are canonical markdown files under `.hoopoe/plans/<plan-id>/` per
// plan.md §1.1 and AGENTS.md. The renderer must NOT treat fixture-bundled
// plan content as canonical — that would make this module a parallel
// source of truth for plans alongside the repo files. So:
//
//   1. Fixture content is wired in only for explicit Mock Flywheel
//      projects (`local-demo`, `mock-flywheel-project`). The transport
//      stamp is `fixture-fallback` so callers can show a fallback pill.
//   2. Real projects throw `loadPlanStageData(...)` with a message
//      pointing at the missing canonical-plans daemon RPC. Saves on
//      real projects throw for the same reason — no silent local
//      writes.
//   3. The daemon-side `plans.list` / `plans.read` / `plans.save` RPCs
//      that resolve `.hoopoe/plans/<plan-id>/` paths are not wired yet
//      (tracked under hp-l5d residual + Phase 5 plan-API work).
//
// This is the same pattern stage-data.ts uses for hp-no7e: keep the
// fixture-fallback path explicit and refuse to fabricate canonical data
// for non-mock projects.

import { useQuery } from "@tanstack/react-query";
import healthyHourPlanMeta from "../../../../../packages/fixtures/scenarios/healthy-hour/plans/meta.json" with {
  type: "json",
};
import healthyHourPlan001 from "../../../../../packages/fixtures/scenarios/healthy-hour/plans/plan-001.bundle.json" with {
  type: "json",
};
import healthyHourPlan002 from "../../../../../packages/fixtures/scenarios/healthy-hour/plans/plan-002.bundle.json" with {
  type: "json",
};
import type { StageFixtureSource } from "./stage-data.ts";

const MOCK_STAGE_PROJECT_IDS = new Set(["local-demo", "mock-flywheel-project"]);

const PLAN_STAGE_QUERY_STALE_MS = 30_000;

// hp-l5d: error surface for non-mock projects. Stable string so callers
// can pattern-match (e.g. error UX surfaces) and tests can pin it.
export const PLAN_STAGE_DAEMON_RPC_PENDING =
  "Hoopoe planning data is not available for this project: the canonical .hoopoe/plans/* daemon RPC family (plans.list / plans.read / plans.save) is not yet wired. Mock Flywheel mode is the only path that returns plan artifacts today.";

export type PlanLockState = "draft" | "locked" | "archived";

export type PlanArtifactStatus =
  | "queued"
  | "running"
  | "completed"
  | "failed"
  | "skipped";

export type PlanArtifactKind =
  | "plan"
  | "rough-idea"
  | "candidate"
  | "comparative-matrix"
  | "synthesis"
  | "fresh-eyes-critique"
  | "refinement-round"
  | "unresolved-decisions";

export interface PlanSummary {
  readonly planId: string;
  readonly title: string;
  readonly version: number;
  readonly lockState: PlanLockState;
  readonly lockedAt: string | null;
  readonly branch: string;
  readonly active: boolean;
  readonly summary?: string;
}

export interface PlanArtifact {
  readonly path: string;
  readonly kind: PlanArtifactKind;
  readonly status: PlanArtifactStatus;
  readonly label: string;
  readonly content: string;
  readonly model?: string;
  readonly harness?: string;
  readonly caamAccount?: string;
  readonly latencyMs?: number;
  readonly round?: number;
}

export type PlanHistoryKind =
  | "plan_created"
  | "candidate_generated"
  | "comparative_matrix_built"
  | "synthesis_run"
  | "fresh_eyes_critique"
  | "refinement_round_complete"
  | "plan_locked"
  | "plan_unlocked";

export interface PlanHistoryEntry {
  readonly ts: string;
  readonly kind: PlanHistoryKind;
  readonly actor: string;
  readonly summary?: string;
  readonly artifact?: string;
  readonly latencyMs?: number;
  readonly round?: number;
  readonly version?: number;
}

export interface PlanBundle {
  readonly planId: string;
  readonly title: string;
  readonly version: number;
  readonly lockState: PlanLockState;
  readonly lockedAt: string | null;
  readonly branch: string;
  readonly active: boolean;
  readonly artifacts: readonly PlanArtifact[];
  readonly history: readonly PlanHistoryEntry[];
}

export interface PlanStageData {
  readonly projectId: string;
  readonly source: StageFixtureSource;
  readonly plans: readonly PlanSummary[];
  readonly bundles: Readonly<Record<string, PlanBundle>>;
}

export function isPlanProjectId(projectId: string): boolean {
  return MOCK_STAGE_PROJECT_IDS.has(projectId);
}

function buildPlanStageData(projectId: string): PlanStageData {
  const meta = healthyHourPlanMeta as {
    readonly scenarioId: string;
    readonly fixturesVersion: string;
    readonly capturedAt: string;
    readonly vpsId: string;
    readonly plans: readonly PlanSummary[];
  };

  const bundles: Record<string, PlanBundle> = {};
  for (const bundle of [healthyHourPlan001, healthyHourPlan002]) {
    bundles[bundle.planId] = bundle as unknown as PlanBundle;
  }

  return {
    projectId,
    source: {
      scenarioId: meta.scenarioId,
      fixturesVersion: meta.fixturesVersion,
      capturedAt: meta.capturedAt,
      vpsId: meta.vpsId,
      transport: "fixture-fallback",
    },
    plans: meta.plans,
    bundles,
  };
}

/** Resolve plan stage data for a given project. Throws for non-mock
 *  projects — the canonical `.hoopoe/plans/*` daemon RPC family is not
 *  wired yet (hp-l5d), and the renderer must not pretend fixture content
 *  is canonical truth for real VPS projects (Guardrail 4 + plan.md
 *  §1.1). Mock Flywheel projects return fixture-fallback data so the
 *  Planning stage demo path still works. */
export function loadPlanStageData(projectId: string): PlanStageData {
  if (!isPlanProjectId(projectId)) {
    throw new Error(PLAN_STAGE_DAEMON_RPC_PENDING);
  }
  return buildPlanStageData(projectId);
}

/** Save a plan artifact. Stub: throws for every project until the
 *  canonical `plans.save` daemon RPC lands (hp-l5d). Saves must NOT
 *  silently mutate fixture state — fixture bundles are imported as
 *  read-only JSON modules, and a fake "save" path would mislead users
 *  into thinking their edits persisted. */
export function savePlanArtifact(_projectId: string, _planId: string, _artifact: PlanArtifact): never {
  throw new Error(
    `Hoopoe plan artifact save is not available: the canonical plans.save daemon RPC is not yet wired (hp-l5d).`,
  );
}

export function usePlanStageQuery(projectId: string) {
  return useQuery<PlanStageData>({
    queryKey: ["plan-stage", projectId],
    queryFn: () => loadPlanStageData(projectId),
    enabled: isPlanProjectId(projectId),
    staleTime: PLAN_STAGE_QUERY_STALE_MS,
  });
}

export function planStatusToneClass(status: PlanArtifactStatus): string {
  switch (status) {
    case "running":
      return "hh-plan-status-running";
    case "completed":
      return "hh-plan-status-completed";
    case "failed":
      return "hh-plan-status-failed";
    case "skipped":
      return "hh-plan-status-skipped";
    case "queued":
    default:
      return "hh-plan-status-queued";
  }
}

export function planLockStateLabel(state: PlanLockState): string {
  switch (state) {
    case "locked":
      return "LOCKED";
    case "archived":
      return "ARCHIVED";
    case "draft":
    default:
      return "DRAFT";
  }
}

export function findActivePlan(plans: readonly PlanSummary[]): PlanSummary | null {
  return plans.find((p) => p.active) ?? plans[0] ?? null;
}

export function selectArtifact(
  bundle: PlanBundle | undefined,
  preferredPath?: string,
): PlanArtifact | null {
  if (!bundle) return null;
  if (preferredPath) {
    const match = bundle.artifacts.find((a) => a.path === preferredPath);
    if (match) return match;
  }
  const planEntry = bundle.artifacts.find((a) => a.kind === "plan");
  if (planEntry) return planEntry;
  return bundle.artifacts[0] ?? null;
}

export function selectCandidates(bundle: PlanBundle | undefined): readonly PlanArtifact[] {
  if (!bundle) return [];
  return bundle.artifacts.filter((a) => a.kind === "candidate");
}
