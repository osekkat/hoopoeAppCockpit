import type { ComponentType } from "react";
import {
  Activity,
  Bug,
  CircleDot,
  LayoutDashboard,
  ListChecks,
} from "lucide-react";

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
  },
  {
    id: "bead",
    number: "02",
    verb: "BEAD",
    label: "Beads",
    routeSegment: "bead",
    routeTo: "/$projectId/bead",
    icon: ListChecks,
  },
  {
    id: "swarm",
    number: "03",
    verb: "SWARM",
    label: "Swarm",
    routeSegment: "swarm",
    routeTo: "/$projectId/swarm",
    icon: LayoutDashboard,
  },
  {
    id: "harden",
    number: "04",
    verb: "HARDEN",
    label: "Hardening",
    routeSegment: "harden",
    routeTo: "/$projectId/harden",
    icon: Bug,
  },
  {
    id: "diag",
    number: "DX",
    verb: "DIAGNOSTICS",
    label: "Diagnostics",
    routeSegment: "diag",
    routeTo: "/$projectId/diag",
    icon: Activity,
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

