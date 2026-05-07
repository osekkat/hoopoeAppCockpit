import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { BeadDependency, BeadStageItem } from "../../data/stage-data.ts";
import * as DagLayoutModule from "./dag-layout.ts";
import {
  buildBeadDagLayout,
  clusterIdFor,
  isClosedStatus,
} from "./dag-layout.ts";

function dep(id: string, type: BeadDependency["type"] = "blocks"): BeadDependency {
  return {
    id,
    title: "",
    status: "unknown",
    priority: null,
    type,
    addedBy: "fixture",
    addedAt: "",
    reason: "",
  };
}

function bead(overrides: Partial<BeadStageItem> & Pick<BeadStageItem, "id">): BeadStageItem {
  return {
    id: overrides.id,
    title: overrides.title ?? `Bead ${overrides.id}`,
    status: overrides.status ?? "open",
    priority: overrides.priority ?? 1,
    issueType: overrides.issueType ?? "task",
    updatedAt: overrides.updatedAt ?? "",
    descriptionSnippet: overrides.descriptionSnippet ?? "",
    dependencyCount: (overrides.dependencies ?? []).length,
    dependencies: overrides.dependencies ?? [],
  };
}

describe("hp-s2x :: buildBeadDagLayout — nodes + edges", () => {
  test("normalizes dependencies into edges with stable ids", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a" }),
      bead({ id: "b", dependencies: [dep("a")] }),
      bead({ id: "c", dependencies: [dep("a"), dep("b")] }),
    ]);
    expect(layout.nodes.map((n) => n.id).toSorted()).toEqual(["a", "b", "c"]);
    expect(layout.edges.map((e) => e.id).toSorted()).toEqual([
      "a->b:blocks",
      "a->c:blocks",
      "b->c:blocks",
    ]);
    expect(layout.totalBeadCount).toBe(3);
    expect(layout.visibleBeadCount).toBe(3);
    expect(layout.clustered).toBe(false);
  });

  test("ignores dependency entries pointing at unknown ids", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a", dependencies: [dep("ghost")] }),
    ]);
    expect(layout.edges).toHaveLength(0);
  });

  test("soft dependencies show up with type=soft and label=soft", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a" }),
      bead({ id: "b", dependencies: [dep("a", "soft")] }),
    ]);
    expect(layout.edges).toHaveLength(1);
    expect(layout.edges[0]?.type).toBe("soft");
    expect(layout.edges[0]?.label).toBe("soft");
    expect(layout.edges[0]?.id).toBe("a->b:soft");
  });

  test("redundant dependency entries between the same pair of beads coalesce", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a" }),
      bead({ id: "b", dependencies: [dep("a"), dep("a")] }),
    ]);
    expect(layout.edges).toHaveLength(1);
    expect(layout.edges[0]?.count).toBe(2);
  });
});

describe("hp-s2x :: layer assignment uses LONGEST path from root", () => {
  test("layer 0 holds roots; downstream layer is max(parent layer)+1", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a" }),
      bead({ id: "b" }),
      bead({ id: "c", dependencies: [dep("a")] }),
      bead({ id: "d", dependencies: [dep("b"), dep("c")] }),
    ]);
    const byId = new Map(layout.nodes.map((n) => [n.id, n]));
    expect(byId.get("a")?.layer).toBe(0);
    expect(byId.get("b")?.layer).toBe(0);
    expect(byId.get("c")?.layer).toBe(1);
    expect(byId.get("d")?.layer).toBe(2);
  });

  test("uses LONGEST path when multiple parent paths exist", () => {
    // a → b → c → d (length 3)
    // a → d (length 1)
    const layout = buildBeadDagLayout([
      bead({ id: "a" }),
      bead({ id: "b", dependencies: [dep("a")] }),
      bead({ id: "c", dependencies: [dep("b")] }),
      bead({ id: "d", dependencies: [dep("c"), dep("a")] }),
    ]);
    const d = layout.nodes.find((n) => n.id === "d");
    expect(d?.layer).toBe(3);
  });
});

describe("hp-s2x :: cycle detection + edge flagging", () => {
  test("flags every node + edge participating in a cycle", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a", dependencies: [dep("c")] }),
      bead({ id: "b", dependencies: [dep("a")] }),
      bead({ id: "c", dependencies: [dep("b")] }),
      bead({ id: "d" }),
    ]);
    expect(layout.cycles).toHaveLength(1);
    const cycleNodes = new Set(layout.cycles[0]);
    expect(cycleNodes.has("a")).toBe(true);
    expect(cycleNodes.has("b")).toBe(true);
    expect(cycleNodes.has("c")).toBe(true);
    expect(cycleNodes.has("d")).toBe(false);

    const cycleEdgeCount = layout.edges.filter((e) => e.isCycle).length;
    expect(cycleEdgeCount).toBeGreaterThanOrEqual(2);
  });

  test("acyclic graphs report no cycles", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a" }),
      bead({ id: "b", dependencies: [dep("a")] }),
    ]);
    expect(layout.cycles).toHaveLength(0);
    expect(layout.edges.every((e) => !e.isCycle)).toBe(true);
  });
});

describe("hp-s2x :: ready frontier", () => {
  test("includes only beads with all blockers closed", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "root1", status: "closed" }),
      bead({ id: "root2", status: "open" }),
      bead({ id: "ready", dependencies: [dep("root1")] }),
      bead({ id: "blocked", dependencies: [dep("root2")] }),
    ]);
    expect(layout.readyFrontierIds.toSorted()).toEqual(["ready", "root2"]);
  });

  test("never includes closed beads", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "done1", status: "closed" }),
      bead({ id: "done2", status: "completed" }),
      bead({ id: "open1" }),
    ]);
    expect(layout.readyFrontierIds).toEqual(["open1"]);
  });

  test("soft dependencies do not block the ready frontier", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a", status: "open" }),
      bead({ id: "b", dependencies: [dep("a", "soft")] }),
    ]);
    expect(layout.readyFrontierIds.toSorted()).toEqual(["a", "b"]);
  });
});

describe("hp-s2x :: critical path", () => {
  test("walks back from highest-priority open leaf via deepest predecessor", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "root" }),
      bead({ id: "mid", dependencies: [dep("root")] }),
      bead({ id: "leaf", priority: 0, dependencies: [dep("mid")] }),
      bead({ id: "side", priority: 3, dependencies: [dep("root")] }),
    ]);
    expect(layout.criticalPathIds).toEqual(["root", "mid", "leaf"]);
    const node = layout.nodes.find((n) => n.id === "leaf");
    expect(node?.isCriticalPath).toBe(true);
  });

  test("marks only adjacent critical-path edges as critical", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "root" }),
      bead({ id: "mid", dependencies: [dep("root")] }),
      bead({ id: "leaf", priority: 0, dependencies: [dep("mid"), dep("root")] }),
    ]);
    const criticalEdges = layout.edges.filter((edge) => edge.isCriticalPath).map((edge) => edge.id).toSorted();
    expect(criticalEdges).toEqual(["mid->leaf:blocks", "root->mid:blocks"]);
  });

  test("returns empty when no open beads exist", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a", status: "closed" }),
      bead({ id: "b", status: "closed", dependencies: [dep("a")] }),
    ]);
    expect(layout.criticalPathIds).toEqual([]);
  });
});

describe("hp-s2x :: precomputed positions for React Flow", () => {
  test("nodes carry x/y coordinates with non-trivial layout dimensions", () => {
    const layout = buildBeadDagLayout([
      bead({ id: "a" }),
      bead({ id: "b", dependencies: [dep("a")] }),
    ]);
    expect(layout.width).toBeGreaterThan(0);
    expect(layout.height).toBeGreaterThan(0);
    const nodeA = layout.nodes.find((n) => n.id === "a")!;
    const nodeB = layout.nodes.find((n) => n.id === "b")!;
    expect(nodeB.y).toBeGreaterThan(nodeA.y);
  });
});

describe("hp-s2x :: clustering for >500 nodes", () => {
  test("under threshold: each bead is its own visible item, clustered=false", () => {
    const beads = Array.from({ length: 30 }, (_, i) => bead({ id: `n${i}` }));
    const layout = buildBeadDagLayout(beads, { clusterThreshold: 500 });
    expect(layout.clustered).toBe(false);
    expect(layout.nodes.length).toBe(30);
  });

  test("over threshold: groups by cluster key and sets clustered=true", () => {
    const beads = Array.from({ length: 12 }, (_, i) =>
      bead({
        id: `n${i}`,
        title: `Phase 4 thing ${i}`,
        status: "open",
        priority: 1,
        issueType: "task",
      }),
    );
    const layout = buildBeadDagLayout(beads, { clusterThreshold: 5 });
    expect(layout.clustered).toBe(true);
    expect(layout.nodes.length).toBeLessThan(beads.length);
    expect(layout.nodes.every((node) => node.isCluster || node.beadIds.length === 1)).toBe(true);
  });

  test("expandedClusterIds re-expands a previously clustered group", () => {
    const beads = Array.from({ length: 8 }, (_, i) =>
      bead({
        id: `phase4-${i}`,
        title: `Phase 4 work ${i}`,
        status: "open",
        priority: 1,
        issueType: "task",
      }),
    );
    const clustered = buildBeadDagLayout(beads, { clusterThreshold: 4 });
    const cluster = clustered.nodes.find((n) => n.isCluster);
    expect(cluster).toBeTruthy();
    const expanded = buildBeadDagLayout(beads, {
      clusterThreshold: 4,
      expandedClusterIds: new Set([cluster!.id]),
    });
    expect(expanded.nodes.length).toBeGreaterThan(clustered.nodes.length);
  });
});

describe("hp-s2x :: stress (500-node chain)", () => {
  test("layout completes for a 500-node chain", () => {
    const beads: BeadStageItem[] = [];
    for (let i = 0; i < 500; i += 1) {
      const id = `n${i.toString().padStart(3, "0")}`;
      const previous = i === 0 ? [] : [`n${(i - 1).toString().padStart(3, "0")}`];
      beads.push(bead({ id, dependencies: previous.map((p) => dep(p)) }));
    }
    const start = performance.now();
    const layout = buildBeadDagLayout(beads, { clusterThreshold: 500 });
    const elapsed = performance.now() - start;
    expect(layout.totalBeadCount).toBe(500);
    expect(elapsed).toBeLessThan(2_000);
  });
});

describe("hp-s2x :: helpers", () => {
  test("clusterIdFor produces stable url-safe cluster identifiers", () => {
    expect(clusterIdFor("Phase 4 / task / open")).toBe(
      "cluster:phase-4-%2F-task-%2F-open",
    );
  });

  test("isClosedStatus normalizes common variants", () => {
    expect(isClosedStatus("closed")).toBe(true);
    expect(isClosedStatus("CLOSED")).toBe(true);
    expect(isClosedStatus("done")).toBe(true);
    expect(isClosedStatus("complete")).toBe(true);
    expect(isClosedStatus("completed")).toBe(true);
    expect(isClosedStatus("open")).toBe(false);
    expect(isClosedStatus("in_progress")).toBe(false);
  });
});

// hp-qpu source-of-truth boundary regression. Plan.md §1.1 + AGENTS.md
// name `bv --robot-*` as the canonical owner of bead-graph intelligence
// (PageRank, betweenness, critical path, cycles, ready frontier, k-core,
// articulation points, eigenvector, HITS). Local helpers in
// dag-layout.ts (`detectCycles`, `criticalPath`, `readyFrontier`)
// exist as a Mock-Flywheel offline-rendering fallback gated to
// `MOCK_STAGE_PROJECT_IDS` in dag-data.ts. These tests pin that
// boundary so the renderer cannot drift back into a parallel
// graph-intelligence source for production projects.
describe("hp-qpu :: bv source-of-truth boundary", () => {
  const repoRoot = resolve(__dirname, "..", "..", "..", "..", "..", "..");
  const dagLayoutPath = resolve(
    repoRoot,
    "apps",
    "desktop",
    "src",
    "renderer",
    "stages",
    "Beads",
    "dag-layout.ts",
  );
  const dagDataPath = resolve(
    repoRoot,
    "apps",
    "desktop",
    "src",
    "renderer",
    "data",
    "dag-data.ts",
  );

  test("dag-layout.ts header carries the hp-qpu boundary docblock", () => {
    const source = readFileSync(dagLayoutPath, "utf8");
    const head = source.slice(0, 1500);
    expect(head).toContain("hp-qpu source-of-truth boundary");
    expect(head).toContain("bv --robot-*");
    expect(head).toContain("Mock-Flywheel");
    expect(head).toContain("MOCK_STAGE_PROJECT_IDS");
  });

  test("dag-layout.ts does NOT export PageRank / betweenness / HITS / eigenvector / kcore / articulation symbols (those live in bv)", () => {
    const forbiddenExports = [
      "pageRank",
      "PageRank",
      "betweenness",
      "Betweenness",
      "hits",
      "HITS",
      "eigenvector",
      "Eigenvector",
      "kCore",
      "kcore",
      "KCore",
      "articulation",
      "Articulation",
    ];
    const exported = Object.keys(DagLayoutModule);
    for (const name of forbiddenExports) {
      expect(exported.includes(name)).toBe(false);
    }
  });

  test("dag-layout.ts source does not declare PageRank-style functions or constants", () => {
    const source = readFileSync(dagLayoutPath, "utf8");
    // Strip the boundary docblock so its mention of forbidden metric
    // names doesn't trigger the regex below — the docblock
    // explicitly enumerates forbidden metrics as the rule that the
    // rest of the file must NOT violate.
    const headerEnd = source.indexOf("import type");
    const codeOnly = headerEnd >= 0 ? source.slice(headerEnd) : source;
    const forbiddenDeclarationPatterns = [
      /\b(?:function|const)\s+(?:pageRank|PageRank|computePageRank)\b/,
      /\b(?:function|const)\s+(?:betweenness|computeBetweenness)\b/,
      /\b(?:function|const)\s+(?:hits|HITS|computeHits)\b/,
      /\b(?:function|const)\s+(?:eigenvector|computeEigenvector)\b/,
      /\b(?:function|const)\s+(?:kCore|computeKCore)\b/,
      /\b(?:function|const)\s+(?:articulation|computeArticulation)\b/,
    ];
    for (const pattern of forbiddenDeclarationPatterns) {
      expect(codeOnly.match(pattern)).toBeNull();
    }
  });

  test("dag-data.ts gates the local-fallback DAG path to MOCK_STAGE_PROJECT_IDS only", () => {
    const source = readFileSync(dagDataPath, "utf8");
    expect(source).toContain("MOCK_STAGE_PROJECT_IDS");
    expect(source).toContain('"local-demo"');
    expect(source).toContain('"mock-flywheel-project"');
    // The query must be enabled only for mock IDs; if a future change
    // removes the isDagProjectId guard, this test catches it.
    expect(source).toMatch(/enabled:\s*isDagProjectId\(/);
    // The transport label must stay "fixture-fallback" so a stage
    // route reading this data sees its non-canonical origin.
    expect(source).toContain('transport: "fixture-fallback"');
  });
});
