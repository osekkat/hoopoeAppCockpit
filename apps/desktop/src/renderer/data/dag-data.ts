import { useQuery } from "@tanstack/react-query";
import beadGraphFixture from "../../../../../packages/fixtures/scenarios/healthy-hour/dag/bead-graph.json" with {
  type: "json",
};
import {
  buildBeadGraph,
  buildBeadDagLayout,
  computeCriticalPath,
  computeReadyFrontier,
  type BeadInput,
  type DagGraph,
  type BeadDagLayout,
} from "../stages/Beads/dag-layout.ts";
import type { BeadDependency, BeadStageItem, StageFixtureSource } from "./stage-data.ts";

// hp-qpu source-of-truth boundary: this fixture-fallback renderer
// path is gated to Mock-Flywheel project IDs only. Production project
// IDs must source bead-graph intelligence (cycles / criticalPath /
// readyFrontier / PageRank / betweenness) from the daemon's
// `/v1/projects/{id}/beads/graph` endpoint backed by `bv --robot-*`,
// not from the local computeCriticalPath / computeReadyFrontier
// helpers below. dag-data.test.ts guards that gate.
const MOCK_STAGE_PROJECT_IDS = new Set(["local-demo", "mock-flywheel-project"]);

interface FixtureBead {
  readonly id: string;
  readonly title: string;
  readonly status: string;
  readonly priority: number;
  readonly blockedBy?: readonly string[];
  readonly soft?: readonly string[];
  readonly issueType?: string;
  readonly updatedAt?: string;
}

interface FixtureFile {
  readonly scenarioId: string;
  readonly fixturesVersion: string;
  readonly capturedAt: string;
  readonly vpsId: string;
  readonly beads: readonly FixtureBead[];
}

export interface DagStageData {
  readonly projectId: string;
  readonly source: StageFixtureSource;
  readonly graph: DagGraph;
  readonly criticalPath: readonly string[];
  readonly readyFrontier: readonly string[];
  readonly layout: BeadDagLayout;
}

export function isDagProjectId(projectId: string): boolean {
  return MOCK_STAGE_PROJECT_IDS.has(projectId);
}

function fixtureBeadToStageItem(bead: FixtureBead): BeadStageItem {
  const dependencies: BeadDependency[] = [];
  for (const id of bead.blockedBy ?? []) {
    dependencies.push({
      id,
      title: "",
      status: "unknown",
      priority: null,
      type: "blocks",
      addedBy: "fixture",
      addedAt: "",
      reason: "",
    });
  }
  for (const id of bead.soft ?? []) {
    dependencies.push({
      id,
      title: "",
      status: "unknown",
      priority: null,
      type: "soft",
      addedBy: "fixture",
      addedAt: "",
      reason: "",
    });
  }
  return {
    id: bead.id,
    title: bead.title,
    status: bead.status,
    priority: bead.priority,
    issueType: bead.issueType ?? "task",
    updatedAt: bead.updatedAt ?? "",
    descriptionSnippet: "",
    dependencyCount: dependencies.length,
    dependencies,
  };
}

export function buildDagStageData(projectId: string): DagStageData {
  const fixture = beadGraphFixture as unknown as FixtureFile;
  const beads = fixture.beads.map(fixtureBeadToStageItem);
  const graph = buildBeadGraph(fixture.beads.map(fixtureBeadToInput));
  const layout = buildBeadDagLayout(beads);
  return {
    projectId,
    source: {
      scenarioId: fixture.scenarioId,
      fixturesVersion: fixture.fixturesVersion,
      capturedAt: fixture.capturedAt,
      vpsId: fixture.vpsId,
      transport: "fixture-fallback",
    },
    graph,
    criticalPath: computeCriticalPath(graph),
    readyFrontier: computeReadyFrontier(graph),
    layout,
  };
}

function fixtureBeadToInput(bead: FixtureBead): BeadInput {
  return {
    id: bead.id,
    title: bead.title,
    status: bead.status,
    priority: bead.priority,
    blockedBy: bead.blockedBy ?? [],
    soft: bead.soft ?? [],
  };
}

export function useDagStageQuery(projectId: string) {
  return useQuery<DagStageData>({
    queryKey: ["dag-stage", projectId],
    queryFn: () => buildDagStageData(projectId),
    enabled: isDagProjectId(projectId),
    staleTime: 5_000,
  });
}
