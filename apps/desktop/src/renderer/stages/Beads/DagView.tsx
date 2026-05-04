import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  Position,
  ReactFlow,
  type Edge,
  type EdgeMarker,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  AlertTriangle,
  CircleDot,
  GitBranch,
  RotateCcw,
  Sparkles,
  Workflow,
  Wrench,
} from "lucide-react";
import { useMemo, useState } from "react";
import { useDagStageQuery } from "../../data/dag-data.ts";
import { StateSurface } from "../../state-view/index.ts";
import type { BeadDagEdge, BeadDagLayout, BeadDagNode } from "./dag-layout.ts";

const NODE_WIDTH = 220;
const NODE_HEIGHT = 82;

interface DagBeadNodeData extends Record<string, unknown> {
  readonly bead: BeadDagNode;
  readonly highlight: "none" | "critical" | "ready";
}

interface DagBeadEdgeData extends Record<string, unknown> {
  readonly highlight: "none" | "critical";
}

const nodeTypes = {
  bead: DagBeadNode,
} as const;

interface DagViewProps {
  readonly projectId: string;
}

export function DagView({ projectId }: DagViewProps) {
  const query = useDagStageQuery(projectId);
  const [showCriticalPath, setShowCriticalPath] = useState(true);
  const [showReadyFrontier, setShowReadyFrontier] = useState(true);

  if (query.isLoading) {
    return (
      <StateSurface
        variant="loading"
        eyebrow="DAG"
        title="Loading bead graph"
        description="Building the graph layout from canonical bead dependencies."
        details={["bv robot graph", "Critical path", "Ready frontier"]}
        testId="dag-stage-loading"
      />
    );
  }
  if (query.isError || !query.data) {
    return (
      <StateSurface
        variant="error"
        eyebrow="DAG"
        title="Bead graph unavailable"
        description="Refresh canonical br state before opening the dependency graph."
        details={[
          "The renderer waits for canonical graph inputs instead of inferring truth from cached rows.",
        ]}
        actions={[
          {
            label: "Open Diagnostics",
            href: `/${projectId}/diag`,
            icon: <Wrench size={13} strokeWidth={2.1} />,
            variant: "primary",
          },
          {
            label: "Retry",
            icon: <RotateCcw size={13} strokeWidth={2.1} />,
            onClick: () => window.location.reload(),
          },
        ]}
        testId="dag-stage-error"
      />
    );
  }

  if (query.data.layout.nodes.length === 0) {
    return (
      <StateSurface
        variant="empty"
        eyebrow="DAG"
        title="No bead graph yet"
        description="There are no bead dependencies to render for this project."
        details={["Convert a locked plan or import br state before graph review."]}
        actions={[
          {
            label: "Open Beads",
            href: `/${projectId}/bead`,
            icon: <GitBranch size={13} strokeWidth={2.1} />,
            variant: "primary",
          },
        ]}
        testId="dag-stage-empty"
      />
    );
  }

  return (
    <DagCanvas
      layout={query.data.layout}
      showCriticalPath={showCriticalPath}
      showReadyFrontier={showReadyFrontier}
      onToggleCriticalPath={setShowCriticalPath}
      onToggleReadyFrontier={setShowReadyFrontier}
    />
  );
}

interface DagCanvasProps {
  readonly layout: BeadDagLayout;
  readonly showCriticalPath: boolean;
  readonly showReadyFrontier: boolean;
  readonly onToggleCriticalPath: (next: boolean) => void;
  readonly onToggleReadyFrontier: (next: boolean) => void;
}

function DagCanvas({
  layout,
  showCriticalPath,
  showReadyFrontier,
  onToggleCriticalPath,
  onToggleReadyFrontier,
}: DagCanvasProps) {
  const reactFlowNodes: Node<DagBeadNodeData>[] = useMemo(
    () =>
      layout.nodes.map((bead) => ({
        id: bead.id,
        type: "bead",
        position: { x: bead.x, y: bead.y },
        data: {
          bead,
          highlight:
            showCriticalPath && bead.isCriticalPath
              ? "critical"
              : showReadyFrontier && bead.isReadyFrontier
                ? "ready"
                : "none",
        },
        draggable: false,
        connectable: false,
        selectable: true,
      })),
    [layout.nodes, showCriticalPath, showReadyFrontier],
  );

  const reactFlowEdges: Edge<DagBeadEdgeData>[] = useMemo(
    () =>
      layout.edges.map((edge) => buildReactFlowEdge(edge, showCriticalPath)),
    [layout.edges, showCriticalPath],
  );

  const cycleCount = layout.cycles.length;
  const cycleNodeCount = layout.cycles.reduce(
    (sum, cycle) => sum + Math.max(0, cycle.length - 1),
    0,
  );

  return (
    <div className="hh-dag" data-testid="dag-view">
      <header className="hh-dag-toolbar">
        <div className="hh-dag-toolbar-title">
          <Workflow size={14} strokeWidth={2.1} />
          <span>
            DAG · {layout.visibleBeadCount} of {layout.totalBeadCount} beads
            {layout.clustered ? " · clustered" : ""}
          </span>
        </div>
        <div className="hh-dag-toolbar-controls">
          <label className="hh-dag-control">
            <input
              type="checkbox"
              checked={showCriticalPath}
              onChange={(event) => onToggleCriticalPath(event.target.checked)}
              data-testid="dag-toggle-critical"
            />
            <Sparkles size={11} strokeWidth={2.1} />
            <span>Critical path</span>
            {showCriticalPath && layout.criticalPathIds.length > 0 ? (
              <em>{layout.criticalPathIds.length} steps</em>
            ) : null}
          </label>
          <label className="hh-dag-control">
            <input
              type="checkbox"
              checked={showReadyFrontier}
              onChange={(event) => onToggleReadyFrontier(event.target.checked)}
              data-testid="dag-toggle-ready"
            />
            <CircleDot size={11} strokeWidth={2.1} />
            <span>Ready frontier</span>
            {showReadyFrontier && layout.readyFrontierIds.length > 0 ? (
              <em>{layout.readyFrontierIds.length} ready</em>
            ) : null}
          </label>
        </div>
      </header>

      {cycleCount > 0 ? (
        <aside
          className="hh-dag-cycle-warning"
          role="alert"
          data-testid="dag-cycle-warning"
        >
          <AlertTriangle size={14} strokeWidth={2.1} />
          <div className="hh-dag-cycle-warning-body">
            <strong>
              {cycleCount} cycle{cycleCount === 1 ? "" : "s"} detected — {cycleNodeCount}{" "}
              bead{cycleNodeCount === 1 ? "" : "s"} trapped.
            </strong>
            <ul>
              {layout.cycles.slice(0, 3).map((cycle) => (
                <li key={cycle.join("-")}>{cycle.join(" → ")}</li>
              ))}
              {layout.cycles.length > 3 ? <li>… {layout.cycles.length - 3} more</li> : null}
            </ul>
          </div>
        </aside>
      ) : null}

      <div className="hh-dag-canvas">
        <ReactFlow
          nodes={reactFlowNodes}
          edges={reactFlowEdges}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.18, includeHiddenNodes: false }}
          minZoom={0.18}
          maxZoom={2.4}
          panOnScroll
          panOnDrag
          zoomOnScroll
          nodesDraggable={false}
          nodesConnectable={false}
          edgesFocusable={false}
          proOptions={{ hideAttribution: true }}
          attributionPosition="bottom-right"
        >
          <Background color="rgba(255,255,255,0.04)" gap={20} variant={BackgroundVariant.Dots} />
          <Controls position="bottom-left" showInteractive={false} />
        </ReactFlow>
      </div>
    </div>
  );
}

function buildReactFlowEdge(edge: BeadDagEdge, showCritical: boolean): Edge<DagBeadEdgeData> {
  const onCritical = showCritical && edge.isCriticalPath;
  const isCycle = edge.isCycle || edge.type === "cycle";
  const isSoft = edge.type === "soft";
  const stroke = isCycle
    ? "rgb(252, 165, 165)"
    : onCritical
      ? "rgb(253, 224, 71)"
      : isSoft
        ? "rgba(148, 163, 184, 0.5)"
        : "rgba(148, 163, 184, 0.85)";
  const strokeWidth = onCritical ? 2.4 : isCycle ? 2.0 : 1.2;
  const marker: EdgeMarker = {
    type: "arrowclosed" as const,
    color: stroke,
    width: 14,
    height: 14,
  };
  return {
    id: edge.id,
    source: edge.source,
    target: edge.target,
    type: "smoothstep",
    animated: false,
    data: { highlight: onCritical ? "critical" : "none" },
    style: {
      stroke,
      strokeWidth,
      strokeDasharray: isSoft ? "4 4" : isCycle ? "6 3" : undefined,
    },
    className: `hh-dag-edge hh-dag-edge-${edge.type}${onCritical ? " hh-dag-edge-critical" : ""}${isCycle ? " hh-dag-edge-cycle" : ""}`,
    markerEnd: marker,
  };
}

function DagBeadNode({ data }: NodeProps<Node<DagBeadNodeData>>) {
  const { bead, highlight } = data;
  const stateClass = `hh-dag-node-status-${bead.status}`;
  const highlightClass =
    highlight === "critical"
      ? "hh-dag-node-critical"
      : highlight === "ready"
        ? "hh-dag-node-ready"
        : "";
  const cycleClass = bead.beadIds.length === 1 ? "" : "hh-dag-node-cluster";

  return (
    <div
      className={`hh-dag-node ${stateClass} ${highlightClass} ${cycleClass}`.trim()}
      style={{ width: NODE_WIDTH, minHeight: NODE_HEIGHT }}
      data-testid={`dag-node-${bead.id}`}
      data-priority={`P${bead.priority}`}
      data-status={bead.status}
      data-cluster={bead.isCluster ? "true" : "false"}
    >
      <Handle type="target" position={Position.Top} isConnectable={false} />
      <header className="hh-dag-node-head">
        <code className="hh-dag-node-id">{bead.label}</code>
        <span className="hh-dag-node-priority">P{bead.priority}</span>
      </header>
      <p className="hh-dag-node-title">{bead.title}</p>
      <footer className="hh-dag-node-meta">
        <span className="hh-dag-node-status">{bead.status.replace("_", " ")}</span>
        {bead.isReadyFrontier && bead.readyFrontierCount > 0 ? (
          <span className="hh-dag-node-ready-flag">{bead.readyFrontierCount} ready</span>
        ) : null}
      </footer>
      <Handle type="source" position={Position.Bottom} isConnectable={false} />
    </div>
  );
}
