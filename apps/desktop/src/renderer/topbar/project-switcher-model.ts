import type { ShellProjectSummary } from "../store.ts";
import type { ShellRouteId } from "../stages.ts";

export interface ProjectSections {
  readonly pinned: readonly ShellProjectSummary[];
  readonly recent: readonly ShellProjectSummary[];
}

export function splitProjectSections(
  projects: readonly ShellProjectSummary[],
  searchTerm: string,
): ProjectSections {
  const matching = projects.filter((project) => projectMatchesSearch(project, searchTerm));
  return {
    pinned: matching.filter((project) => project.pinned),
    recent: matching
      .filter((project) => !project.pinned)
      .toSorted((a, b) => Date.parse(b.lastActivatedAt) - Date.parse(a.lastActivatedAt)),
  };
}

export function projectMatchesSearch(
  project: ShellProjectSummary,
  searchTerm: string,
): boolean {
  const query = normalizeSearchText(searchTerm);
  if (query.length === 0) return true;

  const haystack = normalizeSearchText(
    [
      project.name,
      project.slug,
      project.repoUrl,
      project.rootPath,
      project.branch,
    ].join(" "),
  );
  const compactQuery = query.replaceAll(" ", "");
  const compactHaystack = haystack.replaceAll(" ", "");

  return (
    haystack.includes(query) ||
    compactHaystack.includes(compactQuery) ||
    isSubsequence(compactQuery, compactHaystack)
  );
}

export function isProjectSwarmRunning(project: ShellProjectSummary | undefined): boolean {
  return project?.swarm.status === "running" && project.swarm.activeAgents > 0;
}

export function routeForStage(stageId: ShellRouteId):
  | "/$projectId/plan"
  | "/$projectId/bead"
  | "/$projectId/swarm"
  | "/$projectId/harden"
  | "/$projectId/diag" {
  return stageRouteById[stageId];
}

const stageRouteById = {
  plan: "/$projectId/plan",
  bead: "/$projectId/bead",
  swarm: "/$projectId/swarm",
  harden: "/$projectId/harden",
  diag: "/$projectId/diag",
} as const satisfies Record<
  ShellRouteId,
  | "/$projectId/plan"
  | "/$projectId/bead"
  | "/$projectId/swarm"
  | "/$projectId/harden"
  | "/$projectId/diag"
>;

export function formatRelativeActivation(isoTime: string, now = Date.now()): string {
  const deltaMs = Math.max(0, now - Date.parse(isoTime));
  const minute = 60 * 1000;
  const hour = 60 * minute;
  const day = 24 * hour;

  if (deltaMs < minute) return "just now";
  if (deltaMs < hour) return `${Math.round(deltaMs / minute)}m ago`;
  if (deltaMs < day) return `${Math.round(deltaMs / hour)}h ago`;
  return `${Math.round(deltaMs / day)}d ago`;
}

export function normalizeSearchText(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, " ")
    .trim()
    .replace(/\s+/g, " ");
}

function isSubsequence(needle: string, haystack: string): boolean {
  if (needle.length === 0) return true;
  let nextNeedleIndex = 0;
  for (const char of haystack) {
    if (char === needle[nextNeedleIndex]) {
      nextNeedleIndex += 1;
      if (nextNeedleIndex === needle.length) return true;
    }
  }
  return false;
}
