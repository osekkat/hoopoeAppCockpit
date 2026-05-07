import type { ComponentType } from "react";
import {
  Activity,
  Bug,
  CircleDot,
  LayoutDashboard,
  ListChecks,
} from "lucide-react";
import type { FeatureId } from "../capabilities/index.ts";

export type StageId = "plan" | "bead" | "swarm" | "harden";
export type ShellRouteId = StageId | "diag";

export interface StageDefinition {
  readonly id: ShellRouteId;
  readonly number: "01" | "02" | "03" | "04" | "DX";
  readonly verb: string;
  readonly label: string;
  readonly routeSegment: string;
  readonly routeTo:
    | "/$projectId/plan"
    | "/$projectId/bead"
    | "/$projectId/swarm"
    | "/$projectId/harden"
    | "/$projectId/diag";
  readonly icon: ComponentType<{ readonly size?: number; readonly strokeWidth?: number }>;
  /** FEATURE_CATALOG ids whose decision gates the stage. The renderer
   *  picks the worst-of resolution across this list (per plan.md §2.8 +
   *  hp-hle): blocked-by-policy > unavailable > degraded > available.
   *  An empty list means the stage is always available — Planning is
   *  pure local UI; Diagnostics is the inspection surface for missing
   *  capabilities and must always render. */
  readonly requiredFeatureIds: readonly FeatureId[];
}

export const stageDefinitions = [
  {
    id: "plan",
    number: "01",
    verb: "PLAN",
    label: "Planning",
    routeSegment: "plan",
    routeTo: "/$projectId/plan",
    icon: CircleDot,
    requiredFeatureIds: [],
  },
  {
    id: "bead",
    number: "02",
    verb: "BEAD",
    label: "Beads",
    routeSegment: "bead",
    routeTo: "/$projectId/bead",
    icon: ListChecks,
    requiredFeatureIds: ["bead.kanban.refresh"],
  },
  {
    id: "swarm",
    number: "03",
    verb: "SWARM",
    label: "Swarm",
    routeSegment: "swarm",
    routeTo: "/$projectId/swarm",
    icon: LayoutDashboard,
    requiredFeatureIds: ["swarm.dashboard.live", "swarm.bead.push-branch"],
  },
  {
    id: "harden",
    number: "04",
    verb: "HARDEN",
    label: "Hardening",
    routeSegment: "harden",
    routeTo: "/$projectId/harden",
    icon: Bug,
    requiredFeatureIds: ["bead.kanban.refresh"],
  },
  {
    id: "diag",
    number: "DX",
    verb: "DIAGNOSTICS",
    label: "Diagnostics",
    routeSegment: "diag",
    routeTo: "/$projectId/diag",
    icon: Activity,
    requiredFeatureIds: [],
  },
] as const satisfies readonly StageDefinition[];

export const defaultProjectId = "local-demo";

export function getStageDefinition(id: ShellRouteId): StageDefinition {
  const stage = stageDefinitions.find((candidate) => candidate.id === id);
  if (!stage) {
    throw new Error(`Unknown Hoopoe stage: ${id}`);
  }
  return stage;
}

export function stageForPathname(pathname: string): StageDefinition | undefined {
  return stageDefinitions.find((stage) => pathname.endsWith(`/${stage.routeSegment}`));
}

export function projectDisplayName(projectId: string | undefined): string {
  if (!projectId) return "No project selected";
  if (projectId === defaultProjectId) return "Local demo";
  return projectId.replaceAll("-", " ");
}

