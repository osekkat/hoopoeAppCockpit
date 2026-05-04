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

export function usePlanStageQuery(projectId: string) {
  return useQuery<PlanStageData>({
    queryKey: ["plan-stage", projectId],
    queryFn: () => buildPlanStageData(projectId),
    enabled: isPlanProjectId(projectId),
    staleTime: 5_000,
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
