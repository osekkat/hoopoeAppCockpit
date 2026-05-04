import type { BeadStageItem, BeadDependency } from "../../data/stage-data.ts";

export interface BeadDagLayoutOptions {
  readonly clusterThreshold?: number;
  readonly expandedClusterIds?: ReadonlySet<string>;
}

export interface BeadDagLayout {
  readonly nodes: readonly BeadDagNode[];
  readonly edges: readonly BeadDagEdge[];
  readonly criticalPathIds: readonly string[];
  readonly readyFrontierIds: readonly string[];
  readonly cycles: readonly (readonly string[])[];
  readonly visibleBeadCount: number;
  readonly totalBeadCount: number;
  readonly clustered: boolean;
  readonly width: number;
  readonly height: number;
}

export interface BeadDagNode {
  readonly id: string;
  readonly beadIds: readonly string[];
  readonly label: string;
  readonly title: string;
  readonly status: string;
  readonly priority: number;
  readonly issueType: string;
  readonly x: number;
  readonly y: number;
  readonly layer: number;
  readonly isCluster: boolean;
  readonly isCriticalPath: boolean;
  readonly isReadyFrontier: boolean;
  readonly readyFrontierCount: number;
  readonly statusCounts: readonly StatusCount[];
}

export interface BeadDagEdge {
  readonly id: string;
  readonly source: string;
  readonly target: string;
  readonly dependencyIds: readonly string[];
  readonly type: BeadDependency["type"];
  readonly count: number;
  readonly isCriticalPath: boolean;
  readonly isCycle: boolean;
  readonly label: string;
  readonly addedBy: string;
  readonly addedAt: string;
  readonly reason: string;
}

export interface StatusCount {
  readonly status: string;
  readonly count: number;
}

interface VisibleItem {
  readonly id: string;
  readonly beadIds: readonly string[];
  readonly label: string;
  readonly title: string;
  readonly status: string;
  readonly priority: number;
  readonly issueType: string;
  readonly isCluster: boolean;
  readonly statusCounts: readonly StatusCount[];
}

interface PositionedItem extends VisibleItem {
  readonly x: number;
  readonly y: number;
  readonly layer: number;
}

const NODE_WIDTH = 220;
const NODE_HEIGHT = 82;
const GAP_X = 42;
const GAP_Y = 72;
const PAD_X = 32;
const PAD_Y = 30;

const DEFAULT_CLUSTER_THRESHOLD = 500;

export function buildBeadDagLayout(
  beads: readonly BeadStageItem[],
  options: BeadDagLayoutOptions = {},
): BeadDagLayout {
  const beadById = new Map(beads.map((bead) => [bead.id, bead]));
  const blockingDependencyIdsByBead = new Map<string, readonly string[]>();
  const allDependencyIdsByBead = new Map<string, readonly string[]>();
  for (const bead of beads) {
    const knownDependencies = bead.dependencies.filter((dependency) => beadById.has(dependency.id));
    allDependencyIdsByBead.set(bead.id, knownDependencies.map((dependency) => dependency.id));
    blockingDependencyIdsByBead.set(
      bead.id,
      knownDependencies
        .filter((dependency) => dependency.type !== "soft")
        .map((dependency) => dependency.id),
    );
  }

  const cycles = detectCycles(beads, allDependencyIdsByBead);
  const cycleEdgeKeys = cycleEdges(cycles);
  const criticalPathIds = criticalPath(beads, allDependencyIdsByBead);
  const criticalPathSet = new Set(criticalPathIds);
  const criticalPathEdgeKeys = pathEdges(criticalPathIds);
  const readyFrontierIds = readyFrontier(beads, blockingDependencyIdsByBead, beadById);
  const readyFrontierSet = new Set(readyFrontierIds);

  const clusterThreshold = options.clusterThreshold ?? DEFAULT_CLUSTER_THRESHOLD;
  const expandedClusterIds = options.expandedClusterIds ?? new Set<string>();
  const { items, beadToVisibleId, clustered } = visibleItems(
    beads,
    clusterThreshold,
    expandedClusterIds,
    readyFrontierSet,
    criticalPathSet,
  );
  const edges = visibleEdges(beads, beadToVisibleId, cycleEdgeKeys, criticalPathEdgeKeys);
  const positioned = positionItems(items, edges);
  const positionById = new Map(positioned.map((node) => [node.id, node]));
  const nodes = positioned.map((node) => ({
    ...node,
    isCriticalPath: node.beadIds.some((id) => criticalPathSet.has(id)),
    isReadyFrontier: node.beadIds.some((id) => readyFrontierSet.has(id)),
    readyFrontierCount: node.beadIds.filter((id) => readyFrontierSet.has(id)).length,
  }));

  return {
    nodes,
    edges,
    criticalPathIds,
    readyFrontierIds,
    cycles,
    visibleBeadCount: nodes.reduce((sum, node) => sum + node.beadIds.length, 0),
    totalBeadCount: beads.length,
    clustered,
    width: graphWidth(positionById),
    height: graphHeight(positionById),
  };
}

function visibleItems(
  beads: readonly BeadStageItem[],
  clusterThreshold: number,
  expandedClusterIds: ReadonlySet<string>,
  readyFrontierSet: ReadonlySet<string>,
  criticalPathSet: ReadonlySet<string>,
): {
  readonly items: readonly VisibleItem[];
  readonly beadToVisibleId: ReadonlyMap<string, string>;
  readonly clustered: boolean;
} {
  if (beads.length <= clusterThreshold) {
    const items = beads.map((bead) => visibleBead(bead));
    return { items, beadToVisibleId: new Map(items.map((item) => [item.beadIds[0]!, item.id])), clustered: false };
  }

  const groups = new Map<string, BeadStageItem[]>();
  for (const bead of beads) {
    const key = clusterKey(bead, readyFrontierSet, criticalPathSet);
    const group = groups.get(key) ?? [];
    group.push(bead);
    groups.set(key, group);
  }

  const items: VisibleItem[] = [];
  const beadToVisibleId = new Map<string, string>();
  for (const [key, group] of Array.from(groups.entries()).sort(([a], [b]) => a.localeCompare(b))) {
    const clusterId = clusterIdFor(key);
    if (group.length === 1 || expandedClusterIds.has(clusterId)) {
      for (const bead of group) {
        const item = visibleBead(bead);
        items.push(item);
        beadToVisibleId.set(bead.id, item.id);
      }
      continue;
    }

    const sortedGroup = [...group].sort(compareBeads);
    const cluster = visibleCluster(clusterId, key, sortedGroup);
    items.push(cluster);
    for (const bead of sortedGroup) {
      beadToVisibleId.set(bead.id, cluster.id);
    }
  }

  return { items, beadToVisibleId, clustered: true };
}

function visibleBead(bead: BeadStageItem): VisibleItem {
  return {
    id: bead.id,
    beadIds: [bead.id],
    label: bead.id,
    title: bead.title,
    status: bead.status,
    priority: bead.priority,
    issueType: bead.issueType,
    isCluster: false,
    statusCounts: [{ status: bead.status, count: 1 }],
  };
}

function visibleCluster(id: string, key: string, beads: readonly BeadStageItem[]): VisibleItem {
  const lead = beads[0]!;
  return {
    id,
    beadIds: beads.map((bead) => bead.id),
    label: `${beads.length} beads`,
    title: key,
    status: "cluster",
    priority: Math.min(...beads.map((bead) => bead.priority)),
    issueType: lead.issueType,
    isCluster: true,
    statusCounts: statusCounts(beads),
  };
}

function visibleEdges(
  beads: readonly BeadStageItem[],
  beadToVisibleId: ReadonlyMap<string, string>,
  cycleEdgeKeys: ReadonlySet<string>,
  criticalPathEdgeKeys: ReadonlySet<string>,
): readonly BeadDagEdge[] {
  const edgeMap = new Map<string, BeadDagEdge>();
  for (const bead of beads) {
    const target = beadToVisibleId.get(bead.id);
    if (!target) continue;
    for (const dependency of bead.dependencies) {
      const source = beadToVisibleId.get(dependency.id);
      if (!source || source === target) continue;
      const key = `${source}->${target}:${dependency.type}`;
      const originalEdgeKey = edgeKey(dependency.id, bead.id);
      const existing = edgeMap.get(key);
      if (existing) {
        edgeMap.set(key, {
          ...existing,
          dependencyIds: [...existing.dependencyIds, dependency.id],
          count: existing.count + 1,
          isCycle: existing.isCycle || cycleEdgeKeys.has(originalEdgeKey),
          isCriticalPath: existing.isCriticalPath || criticalPathEdgeKeys.has(originalEdgeKey),
        });
        continue;
      }

      edgeMap.set(key, {
        id: key,
        source,
        target,
        dependencyIds: [dependency.id],
        type: dependency.type,
        count: 1,
        isCriticalPath: criticalPathEdgeKeys.has(originalEdgeKey),
        isCycle: cycleEdgeKeys.has(originalEdgeKey),
        label: dependency.type === "soft" ? "soft" : dependency.type === "cycle" ? "cycle" : "blocks",
        addedBy: dependency.addedBy,
        addedAt: dependency.addedAt,
        reason: dependency.reason,
      });
    }
  }
  return Array.from(edgeMap.values()).sort((a, b) => a.id.localeCompare(b.id));
}

function positionItems(
  items: readonly VisibleItem[],
  edges: readonly BeadDagEdge[],
): readonly PositionedItem[] {
  const incoming = new Map<string, string[]>();
  const outgoing = new Map<string, string[]>();
  for (const item of items) {
    incoming.set(item.id, []);
    outgoing.set(item.id, []);
  }
  for (const edge of edges) {
    incoming.get(edge.target)?.push(edge.source);
    outgoing.get(edge.source)?.push(edge.target);
  }

  const visiting = new Set<string>();
  const layerMemo = new Map<string, number>();
  const layerOf = (id: string): number => {
    const cached = layerMemo.get(id);
    if (cached !== undefined) return cached;
    if (visiting.has(id)) return 0;
    visiting.add(id);
    const parents = incoming.get(id) ?? [];
    const layer = parents.length === 0 ? 0 : 1 + Math.max(...parents.map(layerOf));
    visiting.delete(id);
    layerMemo.set(id, layer);
    return layer;
  };
  for (const item of items) layerOf(item.id);

  const byLayer = new Map<number, VisibleItem[]>();
  for (const item of items) {
    const layer = layerMemo.get(item.id) ?? 0;
    const row = byLayer.get(layer) ?? [];
    row.push(item);
    byLayer.set(layer, row);
  }

  const positioned = new Map<string, PositionedItem>();
  for (const layer of Array.from(byLayer.keys()).sort((a, b) => a - b)) {
    const row = [...(byLayer.get(layer) ?? [])].sort((a, b) => {
      if (layer === 0) return compareVisibleItems(a, b);
      const ax = averageParentX(a.id, incoming, positioned);
      const bx = averageParentX(b.id, incoming, positioned);
      if (ax !== bx) return ax - bx;
      return compareVisibleItems(a, b);
    });
    const totalWidth = row.length * NODE_WIDTH + Math.max(0, row.length - 1) * GAP_X;
    const startX = PAD_X + Math.max(0, (widestLayerWidth(byLayer) - totalWidth) / 2);
    row.forEach((item, index) => {
      positioned.set(item.id, {
        ...item,
        x: startX + index * (NODE_WIDTH + GAP_X),
        y: PAD_Y + layer * (NODE_HEIGHT + GAP_Y),
        layer,
      });
    });
  }

  return Array.from(positioned.values()).sort((a, b) => a.layer - b.layer || a.x - b.x);
}

function detectCycles(
  beads: readonly BeadStageItem[],
  dependencyIdsByBead: ReadonlyMap<string, readonly string[]>,
): readonly (readonly string[])[] {
  const ids = new Set(beads.map((bead) => bead.id));
  const visiting = new Set<string>();
  const visited = new Set<string>();
  const stack: string[] = [];
  const cycleKeys = new Set<string>();
  const cycles: string[][] = [];

  const visit = (id: string): void => {
    if (visited.has(id)) return;
    if (visiting.has(id)) {
      const start = stack.indexOf(id);
      if (start >= 0) {
        const cycle = [...stack.slice(start), id];
        const key = canonicalCycleKey(cycle);
        if (!cycleKeys.has(key)) {
          cycleKeys.add(key);
          cycles.push(cycle);
        }
      }
      return;
    }
    visiting.add(id);
    stack.push(id);
    for (const depId of dependencyIdsByBead.get(id) ?? []) {
      if (ids.has(depId)) visit(depId);
    }
    stack.pop();
    visiting.delete(id);
    visited.add(id);
  };

  for (const bead of beads) visit(bead.id);
  return cycles;
}

function cycleEdges(cycles: readonly (readonly string[])[]): ReadonlySet<string> {
  const keys = new Set<string>();
  for (const cycle of cycles) {
    for (let index = 0; index < cycle.length - 1; index += 1) {
      const beadId = cycle[index]!;
      const dependencyId = cycle[index + 1]!;
      keys.add(edgeKey(dependencyId, beadId));
    }
  }
  return keys;
}

function pathEdges(path: readonly string[]): ReadonlySet<string> {
  const keys = new Set<string>();
  for (let index = 0; index < path.length - 1; index += 1) {
    keys.add(edgeKey(path[index]!, path[index + 1]!));
  }
  return keys;
}

function criticalPath(
  beads: readonly BeadStageItem[],
  dependencyIdsByBead: ReadonlyMap<string, readonly string[]>,
): readonly string[] {
  const beadById = new Map(beads.map((bead) => [bead.id, bead]));
  const dependents = new Map<string, string[]>();
  for (const bead of beads) {
    for (const depId of dependencyIdsByBead.get(bead.id) ?? []) {
      const values = dependents.get(depId) ?? [];
      values.push(bead.id);
      dependents.set(depId, values);
    }
  }

  const openLeaves = beads.filter(
    (bead) => !isClosedStatus(bead.status) && (dependents.get(bead.id) ?? []).length === 0,
  );
  const candidates = openLeaves.length > 0 ? openLeaves : beads.filter((bead) => !isClosedStatus(bead.status));
  const leaf = [...candidates].sort((a, b) => {
    if (a.priority !== b.priority) return a.priority - b.priority;
    return pathDepth(b.id, dependencyIdsByBead, new Set()) - pathDepth(a.id, dependencyIdsByBead, new Set());
  })[0];
  if (!leaf) return [];

  const path = [leaf.id];
  let current = leaf.id;
  const seen = new Set<string>([current]);
  while (true) {
    const next = (dependencyIdsByBead.get(current) ?? [])
      .filter((id) => beadById.has(id) && !seen.has(id))
      .sort((a, b) => {
        const beadA = beadById.get(a)!;
        const beadB = beadById.get(b)!;
        const depthDiff =
          pathDepth(b, dependencyIdsByBead, new Set()) -
          pathDepth(a, dependencyIdsByBead, new Set());
        if (depthDiff !== 0) return depthDiff;
        return beadA.priority - beadB.priority;
      })[0];
    if (!next) break;
    path.push(next);
    seen.add(next);
    current = next;
  }
  return path.reverse();
}

function readyFrontier(
  beads: readonly BeadStageItem[],
  blockingDependencyIdsByBead: ReadonlyMap<string, readonly string[]>,
  beadById: ReadonlyMap<string, BeadStageItem>,
): readonly string[] {
  return beads
    .filter((bead) => {
      if (isClosedStatus(bead.status)) return false;
      return (blockingDependencyIdsByBead.get(bead.id) ?? []).every((depId) =>
        isClosedStatus(beadById.get(depId)?.status ?? "unknown"),
      );
    })
    .sort(compareBeads)
    .map((bead) => bead.id);
}

function pathDepth(
  id: string,
  dependencyIdsByBead: ReadonlyMap<string, readonly string[]>,
  seen: Set<string>,
): number {
  if (seen.has(id)) return 0;
  seen.add(id);
  const deps = dependencyIdsByBead.get(id) ?? [];
  if (deps.length === 0) return 0;
  return 1 + Math.max(...deps.map((depId) => pathDepth(depId, dependencyIdsByBead, new Set(seen))));
}

export function isClosedStatus(status: string): boolean {
  const normalized = status.toLowerCase();
  return normalized === "closed" || normalized === "done" || normalized === "complete" || normalized === "completed";
}

function clusterKey(
  bead: BeadStageItem,
  readyFrontierSet: ReadonlySet<string>,
  criticalPathSet: ReadonlySet<string>,
): string {
  if (criticalPathSet.has(bead.id)) return "Critical path";
  if (readyFrontierSet.has(bead.id)) return "Ready frontier";
  const phase = /\bPhase\s+\d+(?:\.\d+)?/i.exec(bead.title)?.[0];
  if (phase) return `${phase} / ${bead.issueType} / ${bead.status}`;
  return `${bead.issueType} / ${bead.status} / P${bead.priority}`;
}

export function clusterIdFor(key: string): string {
  return `cluster:${encodeURIComponent(key.toLowerCase().replace(/\s+/g, "-"))}`;
}

function statusCounts(beads: readonly BeadStageItem[]): readonly StatusCount[] {
  const counts = new Map<string, number>();
  for (const bead of beads) counts.set(bead.status, (counts.get(bead.status) ?? 0) + 1);
  return Array.from(counts.entries())
    .map(([status, count]) => ({ status, count }))
    .sort((a, b) => a.status.localeCompare(b.status));
}

function compareBeads(a: BeadStageItem, b: BeadStageItem): number {
  if (a.priority !== b.priority) return a.priority - b.priority;
  return a.id.localeCompare(b.id);
}

function compareVisibleItems(a: VisibleItem, b: VisibleItem): number {
  if (a.priority !== b.priority) return a.priority - b.priority;
  return a.label.localeCompare(b.label);
}

function averageParentX(
  id: string,
  incoming: ReadonlyMap<string, readonly string[]>,
  positioned: ReadonlyMap<string, PositionedItem>,
): number {
  const parents = incoming.get(id) ?? [];
  const xs = parents
    .map((parent) => positioned.get(parent)?.x)
    .filter((x): x is number => typeof x === "number");
  if (xs.length === 0) return 0;
  return xs.reduce((sum, x) => sum + x, 0) / xs.length;
}

function widestLayerWidth(byLayer: ReadonlyMap<number, readonly VisibleItem[]>): number {
  let widest = 0;
  for (const row of byLayer.values()) {
    widest = Math.max(widest, row.length * NODE_WIDTH + Math.max(0, row.length - 1) * GAP_X);
  }
  return widest;
}

function graphWidth(positionById: ReadonlyMap<string, PositionedItem>): number {
  if (positionById.size === 0) return PAD_X * 2;
  return Math.max(...Array.from(positionById.values()).map((node) => node.x + NODE_WIDTH)) + PAD_X;
}

function graphHeight(positionById: ReadonlyMap<string, PositionedItem>): number {
  if (positionById.size === 0) return PAD_Y * 2;
  return Math.max(...Array.from(positionById.values()).map((node) => node.y + NODE_HEIGHT)) + PAD_Y;
}

function canonicalCycleKey(cycle: readonly string[]): string {
  const body = cycle.slice(0, -1);
  const rotations = body.map((_, index) => [...body.slice(index), ...body.slice(0, index)].join(">"));
  return rotations.sort()[0] ?? body.join(">");
}

function edgeKey(source: string, target: string): string {
  return `${source}->${target}`;
}

export interface BeadInput {
  readonly id: string;
  readonly title: string;
  readonly status: string;
  readonly priority: number;
  readonly blockedBy: readonly string[];
  readonly soft?: readonly string[];
}

export interface DagNode {
  readonly id: string;
  readonly label: string;
  readonly title: string;
  readonly status: string;
  readonly priority: number;
  readonly layer: number;
  readonly column: number;
  readonly inCycle: boolean;
  readonly isReadyFrontier: boolean;
  readonly readyFrontierCount: number;
}

export interface DagEdge {
  readonly id: string;
  readonly source: string;
  readonly target: string;
  readonly kind: "blocking" | "soft" | "cycle";
}

export interface DagCycle {
  readonly nodes: readonly string[];
}

export interface DagGraph {
  readonly nodes: readonly DagNode[];
  readonly edges: readonly DagEdge[];
  readonly blocks: ReadonlyMap<string, readonly string[]>;
  readonly blockedBy: ReadonlyMap<string, readonly string[]>;
  readonly cycles: readonly DagCycle[];
}

export function buildBeadGraph(inputs: readonly BeadInput[]): DagGraph {
  const ids = new Set(inputs.map((input) => input.id));
  const beadById = new Map(inputs.map((input) => [input.id, input]));
  const blockedBy = new Map<string, string[]>();
  const blocks = new Map<string, string[]>();
  const rawEdges: DagEdge[] = [];

  for (const input of inputs) {
    const blockers = input.blockedBy.filter((id) => ids.has(id));
    blockedBy.set(input.id, blockers);
    for (const blocker of blockers) {
      const targets = blocks.get(blocker) ?? [];
      targets.push(input.id);
      blocks.set(blocker, targets);
      rawEdges.push({ id: edgeKey(blocker, input.id), source: blocker, target: input.id, kind: "blocking" });
    }
    for (const blocker of input.soft ?? []) {
      if (!ids.has(blocker)) continue;
      rawEdges.push({ id: `soft:${edgeKey(blocker, input.id)}`, source: blocker, target: input.id, kind: "soft" });
    }
  }

  const cycleList = inputCycles(inputs, blockedBy);
  const cycleNodeIds = new Set(cycleList.flatMap((cycle) => cycle.nodes));
  const cycleEdgeIds = new Set<string>();
  for (const cycle of cycleList) {
    const nodes = cycle.nodes;
    for (let index = 0; index < nodes.length; index += 1) {
      const source = nodes[index]!;
      const target = nodes[(index + 1) % nodes.length]!;
      cycleEdgeIds.add(edgeKey(source, target));
    }
  }

  const edges = rawEdges
    .map((edge) => cycleEdgeIds.has(edge.id) ? { ...edge, kind: "cycle" as const } : edge)
    .sort((a, b) => a.id.localeCompare(b.id));

  const layers = graphLayers(inputs, blockedBy, cycleNodeIds);
  const columns = graphColumns(inputs, blockedBy, layers);
  const nodes = inputs
    .map((input) => ({
      id: input.id,
      label: input.id,
      title: input.title,
      status: input.status,
      priority: input.priority,
      layer: layers.get(input.id) ?? 0,
      column: columns.get(input.id) ?? 0,
      inCycle: cycleNodeIds.has(input.id),
      isReadyFrontier: false,
      readyFrontierCount: 0,
    }))
    .sort((a, b) => a.layer - b.layer || a.column - b.column || a.id.localeCompare(b.id));

  for (const values of blocks.values()) values.sort((a, b) => a.localeCompare(b));
  for (const values of blockedBy.values()) values.sort((a, b) => a.localeCompare(b));
  return { nodes, edges, blocks, blockedBy, cycles: cycleList };
}

export function findCycles(graph: DagGraph): readonly DagCycle[] {
  return graph.cycles;
}

export function computeReadyFrontier(graph: DagGraph): readonly string[] {
  const nodeById = new Map(graph.nodes.map((node) => [node.id, node]));
  return graph.nodes
    .filter((node) => {
      if (node.inCycle || isClosedStatus(node.status) || node.status === "deferred") return false;
      return (graph.blockedBy.get(node.id) ?? []).every((blockerId) =>
        isClosedStatus(nodeById.get(blockerId)?.status ?? "unknown"),
      );
    })
    .sort(compareDagNodes)
    .map((node) => node.id);
}

export function computeCriticalPath(graph: DagGraph): readonly string[] {
  const nodeById = new Map(graph.nodes.map((node) => [node.id, node]));
  const openLeaves = graph.nodes.filter(
    (node) =>
      !node.inCycle &&
      !isClosedStatus(node.status) &&
      node.status !== "deferred" &&
      (graph.blocks.get(node.id) ?? []).length === 0,
  );
  const leaf = [...openLeaves].sort((a, b) => {
    if (a.priority !== b.priority) return a.priority - b.priority;
    return graphDepth(b.id, graph.blockedBy, new Set()) - graphDepth(a.id, graph.blockedBy, new Set());
  })[0];
  if (!leaf) return [];

  const path = [leaf.id];
  let currentId = leaf.id;
  const seen = new Set<string>([currentId]);
  while (true) {
    const candidates = (graph.blockedBy.get(currentId) ?? [])
      .filter((id) => {
        const node = nodeById.get(id);
        return (
          node !== undefined &&
          !node.inCycle &&
          !isClosedStatus(node.status) &&
          node.status !== "deferred" &&
          !seen.has(node.id)
        );
      })
      .sort((a, b) => {
        const depthDiff =
          graphDepth(b, graph.blockedBy, new Set()) -
          graphDepth(a, graph.blockedBy, new Set());
        if (depthDiff !== 0) return depthDiff;
        const nodeA = nodeById.get(a);
        const nodeB = nodeById.get(b);
        if (nodeA === undefined || nodeB === undefined) return a.localeCompare(b);
        return compareDagNodes(nodeA, nodeB);
      });
    const selectedId = candidates[0] ?? "";
    if (selectedId.length === 0) break;
    path.push(selectedId);
    seen.add(selectedId);
    currentId = selectedId;
  }
  return path.reverse();
}

export function topoSort(graph: DagGraph): readonly string[] {
  const indegree = new Map<string, number>();
  const outgoing = new Map<string, string[]>();
  const cycleIds = new Set(graph.nodes.filter((node) => node.inCycle).map((node) => node.id));
  for (const node of graph.nodes) {
    indegree.set(node.id, 0);
    outgoing.set(node.id, []);
  }
  for (const edge of graph.edges) {
    if (edge.kind === "soft" || cycleIds.has(edge.source) || cycleIds.has(edge.target)) continue;
    indegree.set(edge.target, (indegree.get(edge.target) ?? 0) + 1);
    outgoing.get(edge.source)?.push(edge.target);
  }

  const queue = graph.nodes
    .filter((node) => !cycleIds.has(node.id) && (indegree.get(node.id) ?? 0) === 0)
    .sort(compareDagNodes);
  const ordered: string[] = [];
  while (queue.length > 0) {
    const node = queue.shift()!;
    ordered.push(node.id);
    for (const target of outgoing.get(node.id) ?? []) {
      const next = (indegree.get(target) ?? 0) - 1;
      indegree.set(target, next);
      if (next === 0) {
        const targetNode = graph.nodes.find((candidate) => candidate.id === target);
        if (targetNode) queue.push(targetNode);
        queue.sort(compareDagNodes);
      }
    }
  }

  return [
    ...ordered,
    ...graph.nodes
      .filter((node) => !ordered.includes(node.id))
      .map((node) => node.id)
      .sort((a, b) => a.localeCompare(b)),
  ];
}

function graphLayers(
  inputs: readonly BeadInput[],
  blockedBy: ReadonlyMap<string, readonly string[]>,
  cycleNodeIds: ReadonlySet<string>,
): ReadonlyMap<string, number> {
  const memo = new Map<string, number>();
  const visiting = new Set<string>();
  const layerOf = (id: string): number => {
    const cached = memo.get(id);
    if (cached !== undefined) return cached;
    if (cycleNodeIds.has(id) || visiting.has(id)) {
      memo.set(id, 0);
      return 0;
    }
    visiting.add(id);
    const parents = blockedBy.get(id) ?? [];
    const layer = parents.length === 0 ? 0 : 1 + Math.max(...parents.map(layerOf));
    visiting.delete(id);
    memo.set(id, layer);
    return layer;
  };
  for (const input of inputs) layerOf(input.id);
  return memo;
}

function graphColumns(
  inputs: readonly BeadInput[],
  blockedBy: ReadonlyMap<string, readonly string[]>,
  layers: ReadonlyMap<string, number>,
): ReadonlyMap<string, number> {
  const byLayer = new Map<number, BeadInput[]>();
  for (const input of inputs) {
    const layer = layers.get(input.id) ?? 0;
    const row = byLayer.get(layer) ?? [];
    row.push(input);
    byLayer.set(layer, row);
  }
  const columns = new Map<string, number>();
  for (const layer of Array.from(byLayer.keys()).sort((a, b) => a - b)) {
    const row = [...(byLayer.get(layer) ?? [])].sort((a, b) => {
      if (layer === 0) return compareInputs(a, b);
      const ax = averageGraphParentColumn(a.id, blockedBy, columns);
      const bx = averageGraphParentColumn(b.id, blockedBy, columns);
      if (ax !== bx) return ax - bx;
      return compareInputs(a, b);
    });
    row.forEach((input, index) => columns.set(input.id, index));
  }
  return columns;
}

function inputCycles(
  inputs: readonly BeadInput[],
  blockedBy: ReadonlyMap<string, readonly string[]>,
): readonly DagCycle[] {
  const inputById = new Map(inputs.map((input) => [input.id, input]));
  const visiting = new Set<string>();
  const visited = new Set<string>();
  const stack: string[] = [];
  const seenCycles = new Set<string>();
  const cycles: DagCycle[] = [];

  const visit = (id: string): void => {
    if (visited.has(id)) return;
    if (visiting.has(id)) {
      const start = stack.indexOf(id);
      if (start >= 0) {
        const raw = stack.slice(start);
        const forward = [...raw].reverse();
        const key = canonicalCycleKey(forward);
        if (!seenCycles.has(key)) {
          seenCycles.add(key);
          cycles.push({ nodes: forward });
        }
      }
      return;
    }
    visiting.add(id);
    stack.push(id);
    for (const depId of blockedBy.get(id) ?? []) {
      if (inputById.has(depId)) visit(depId);
    }
    stack.pop();
    visiting.delete(id);
    visited.add(id);
  };

  for (const input of inputs) visit(input.id);
  return cycles;
}

function graphDepth(
  id: string,
  blockedBy: ReadonlyMap<string, readonly string[]>,
  seen: Set<string>,
): number {
  if (seen.has(id)) return 0;
  seen.add(id);
  const parents = blockedBy.get(id) ?? [];
  if (parents.length === 0) return 0;
  return 1 + Math.max(...parents.map((parent) => graphDepth(parent, blockedBy, new Set(seen))));
}

function averageGraphParentColumn(
  id: string,
  blockedBy: ReadonlyMap<string, readonly string[]>,
  columns: ReadonlyMap<string, number>,
): number {
  const parentColumns = (blockedBy.get(id) ?? [])
    .map((parent) => columns.get(parent))
    .filter((column): column is number => typeof column === "number");
  if (parentColumns.length === 0) return 0;
  return parentColumns.reduce((sum, column) => sum + column, 0) / parentColumns.length;
}

function compareInputs(a: BeadInput, b: BeadInput): number {
  if (a.priority !== b.priority) return a.priority - b.priority;
  return a.id.localeCompare(b.id);
}

function compareDagNodes(a: DagNode, b: DagNode): number {
  if (a.priority !== b.priority) return a.priority - b.priority;
  return a.id.localeCompare(b.id);
}
