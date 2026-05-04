import {
  Background,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  type Edge,
  type EdgeMouseHandler,
  type Node,
  type NodeProps,
  type NodeTypes,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { AlertTriangle, GitBranch, Layers3, LocateFixed, Route, ShieldCheck } from "lucide-react";
import type { MouseEvent } from "react";
import { useMemo, useState } from "react";
import type { BeadStageItem } from "../../data/stage-data.ts";
import {
  buildBeadDagLayout,
  type BeadDagEdge,
  type BeadDagNode,
} from "./dag-layout.ts";

interface BeadsDagViewProps {
  readonly beads: readonly BeadStageItem[];
}

interface DagNodeData extends Record<string, unknown> {
  readonly node: BeadDagNode;
  readonly showCriticalPath: boolean;
  readonly showReadyFrontier: boolean;
  readonly showCycles: boolean;
}

interface DagEdgeData extends Record<string, unknown> {
  readonly edge: BeadDagEdge;
}

type DagFlowNode = Node<DagNodeData, "beadDag">;
type DagFlowEdge = Edge<DagEdgeData, "smoothstep">;

const nodeTypes = {
  beadDag: BeadDagNodeCard,
} satisfies NodeTypes;

export function BeadsDagView({ beads }: BeadsDagViewProps) {
  const [showCriticalPath, setShowCriticalPath] = useState(true);
  const [showReadyFrontier, setShowReadyFrontier] = useState(true);
  const [showCycles, setShowCycles] = useState(true);
  const [expandedClusterIds, setExpandedClusterIds] = useState<ReadonlySet<string>>(() => new Set());
  const [selectedEdge, setSelectedEdge] = useState<BeadDagEdge | null>(null);

  const layout = useMemo(
    () => buildBeadDagLayout(beads, { expandedClusterIds }),
    [beads, expandedClusterIds],
  );

  const flowNodes = useMemo<readonly DagFlowNode[]>(
    () =>
      layout.nodes.map((node) => ({
        id: node.id,
        type: "beadDag",
        position: { x: node.x, y: node.y },
        data: { node, showCriticalPath, showReadyFrontier, showCycles },
        draggable: false,
        selectable: true,
        focusable: true,
        className: [
          "hh-dag-flow-node",
          node.isCluster ? "hh-dag-flow-node-cluster" : "",
          showCriticalPath && node.isCriticalPath ? "hh-dag-flow-node-critical" : "",
          showReadyFrontier && node.isReadyFrontier ? "hh-dag-flow-node-ready" : "",
        ]
          .filter(Boolean)
          .join(" "),
      })),
    [layout.nodes, showCriticalPath, showCycles, showReadyFrontier],
  );

  const flowEdges = useMemo<readonly DagFlowEdge[]>(
    () =>
      layout.edges.map((edge) => {
        const critical = showCriticalPath && edge.isCriticalPath;
        const cycle = showCycles && edge.isCycle;
        const stroke = cycle
          ? "var(--danger, #ff8e8e)"
          : critical
            ? "var(--hoopoe-russet)"
            : edge.type === "soft"
              ? "var(--text-muted)"
              : "var(--surface-border-strong)";

        return {
          id: edge.id,
          source: edge.source,
          target: edge.target,
          type: "smoothstep",
          data: { edge },
          label: edge.count > 1 ? `${edge.count} deps` : edge.label,
          animated: critical,
          markerEnd: { type: MarkerType.ArrowClosed, color: stroke },
          className: [
            "hh-dag-flow-edge",
            critical ? "hh-dag-flow-edge-critical" : "",
            cycle ? "hh-dag-flow-edge-cycle" : "",
          ]
            .filter(Boolean)
            .join(" "),
          style: {
            stroke,
            strokeWidth: critical || cycle ? 2.2 : 1.35,
            strokeDasharray: edge.type === "soft" ? "5 5" : undefined,
          },
        };
      }),
    [layout.edges, showCriticalPath, showCycles],
  );

  const onNodeClick = (_event: MouseEvent, node: DagFlowNode) => {
    const cluster = node.data.node;
    if (!cluster.isCluster) return;
    setExpandedClusterIds((current) => {
      const next = new Set(current);
      if (next.has(cluster.id)) {
        next.delete(cluster.id);
      } else {
        next.add(cluster.id);
      }
      return next;
    });
  };

  const onEdgeClick: EdgeMouseHandler<DagFlowEdge> = (event, edge) => {
    event.preventDefault();
    setSelectedEdge(edge.data?.edge ?? null);
  };

  return (
    <section className="hh-beads-dag" aria-label="Bead dependency DAG">
      <div className="hh-beads-dag-toolbar">
        <div className="hh-beads-dag-metrics" aria-label="DAG metrics">
          <span>
            <GitBranch size={14} strokeWidth={2.1} />
            {layout.totalBeadCount} nodes
          </span>
          <span>
            <Route size={14} strokeWidth={2.1} />
            {layout.criticalPathIds.length} critical
          </span>
          <span>
            <LocateFixed size={14} strokeWidth={2.1} />
            {layout.readyFrontierIds.length} ready
          </span>
          {layout.clustered ? (
            <span>
              <Layers3 size={14} strokeWidth={2.1} />
              clustered
            </span>
          ) : null}
          {layout.cycles.length > 0 ? (
            <span className="hh-beads-dag-warning">
              <AlertTriangle size={14} strokeWidth={2.1} />
              {layout.cycles.length} cycles
            </span>
          ) : null}
        </div>

        <div className="hh-beads-dag-switches" aria-label="DAG overlays">
          <label>
            <input
              checked={showCriticalPath}
              onChange={(event) => setShowCriticalPath(event.target.checked)}
              type="checkbox"
            />
            Critical path
          </label>
          <label>
            <input
              checked={showReadyFrontier}
              onChange={(event) => setShowReadyFrontier(event.target.checked)}
              type="checkbox"
            />
            Ready frontier
          </label>
          <label>
            <input
              checked={showCycles}
              onChange={(event) => setShowCycles(event.target.checked)}
              type="checkbox"
            />
            Cycles
          </label>
        </div>
      </div>

      <div className="hh-beads-dag-canvas" style={{ minHeight: Math.min(Math.max(layout.height + 90, 420), 760) }}>
        <ReactFlow
          colorMode="dark"
          edges={flowEdges as DagFlowEdge[]}
          edgesFocusable={true}
          edgesReconnectable={false}
          fitView={true}
          maxZoom={1.5}
          minZoom={0.18}
          nodes={flowNodes as DagFlowNode[]}
          nodesConnectable={false}
          nodesDraggable={false}
          nodeTypes={nodeTypes}
          onEdgeClick={onEdgeClick}
          onEdgeContextMenu={onEdgeClick}
          onNodeClick={onNodeClick}
          panOnScroll={true}
          proOptions={{ hideAttribution: true }}
          zoomOnDoubleClick={false}
        >
          <Background gap={22} color="rgba(236, 225, 210, 0.08)" />
          <MiniMap
            nodeColor={(node) => miniMapColor((node as DagFlowNode).data.node)}
            pannable={true}
            zoomable={true}
          />
          <Controls showInteractive={false} />
        </ReactFlow>
      </div>

      <div className="hh-beads-dag-inspector" aria-live="polite">
        {selectedEdge ? (
          <>
            <div className="hh-beads-dag-inspector-title">
              <ShieldCheck size={15} strokeWidth={2.1} />
              <strong>Dependency edge</strong>
              <span>{selectedEdge.label}</span>
            </div>
            <p>
              {selectedEdge.source} blocks {selectedEdge.target}
              {selectedEdge.count > 1 ? ` across ${selectedEdge.count} dependencies` : ""}.
            </p>
            <div className="hh-beads-dag-edge-meta">
              <span>{selectedEdge.addedBy || "unknown author"}</span>
              <span>{selectedEdge.addedAt || "unknown time"}</span>
              <span>{selectedEdge.reason || "no reason recorded"}</span>
            </div>
            <button type="button" disabled>
              Read-only in Stage 02
            </button>
          </>
        ) : (
          <>
            <div className="hh-beads-dag-inspector-title">
              <ShieldCheck size={15} strokeWidth={2.1} />
              <strong>Read-only dependency map</strong>
            </div>
            <p>Click an edge to inspect its metadata. Cluster nodes expand in place.</p>
          </>
        )}
      </div>
    </section>
  );
}

function BeadDagNodeCard({ data }: NodeProps<DagFlowNode>) {
  const node = data.node;
  const critical = data.showCriticalPath && node.isCriticalPath;
  const ready = data.showReadyFrontier && node.isReadyFrontier;
  return (
    <div className="hh-dag-node-card">
      <Handle isConnectable={false} position={Position.Top} type="target" />
      <div className="hh-dag-node-topline">
        <code>{node.label}</code>
        <span>P{node.priority}</span>
      </div>
      <strong>{node.title}</strong>
      <div className="hh-dag-node-flags">
        <span>{node.status}</span>
        <span>{node.issueType}</span>
        {critical ? <span className="hh-dag-node-critical-pill">critical</span> : null}
        {ready ? <span className="hh-dag-node-ready-pill">{node.readyFrontierCount || 1} ready</span> : null}
      </div>
      {node.isCluster ? (
        <div className="hh-dag-node-cluster-counts">
          {node.statusCounts.slice(0, 4).map((item) => (
            <span key={item.status}>
              {item.status}: {item.count}
            </span>
          ))}
        </div>
      ) : null}
      <Handle isConnectable={false} position={Position.Bottom} type="source" />
    </div>
  );
}

function miniMapColor(node: BeadDagNode): string {
  if (node.isCriticalPath) return "#e58253";
  if (node.isReadyFrontier) return "#4bbf7a";
  if (node.isCluster) return "#6f7b91";
  return "#465061";
}
