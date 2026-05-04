import { Activity, RotateCcw, Wrench } from "lucide-react";
import { RouterProvider } from "@tanstack/react-router";
import { router } from "../routes.tsx";
import { getStageDefinition } from "../stages.ts";
import { StateSurface } from "../state-view/index.ts";
import { ActivityPanel } from "./activity-panel.tsx";
import { EmptyStage } from "./empty-stage.tsx";
import { StageHeader } from "./stage-header.tsx";

const meta = {
  title: "Desktop/Shell",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const RootShell = {
  render: () => <RouterProvider router={router} />,
};

export const PlanningHeader = {
  render: () => (
    <div className="hh-story-surface">
      <StageHeader
        stage={getStageDefinition("plan")}
        projectName="Local demo"
        breadcrumb={["Planning"]}
      />
    </div>
  ),
};

export const SwarmEmptyState = {
  render: () => (
    <div className="hh-story-surface">
      <StageHeader
        stage={getStageDefinition("swarm")}
        projectName="Local demo"
        breadcrumb={["Swarm"]}
      />
      <EmptyStage stageId="swarm" />
    </div>
  ),
};

export const LoadingAndRecoveryStates = {
  render: () => (
    <div className="hh-story-surface">
      <StateSurface
        variant="loading"
        eyebrow="Beads"
        title="Loading canonical state"
        description="Fetching br issues, bv graph hints, and cached read models."
        details={["Skeleton rows reserve layout space.", "No spinner-only stage blockers."]}
      />
      <StateSurface
        variant="error"
        eyebrow="Daemon"
        title="Stage data unavailable"
        description="Reconnect the daemon or open Diagnostics before retrying."
        actions={[
          {
            label: "Open Diagnostics",
            href: "/local-demo/diag",
            icon: <Wrench size={13} strokeWidth={2.1} />,
            variant: "primary",
          },
          {
            label: "Reconnect VPS",
            href: "/first-run",
            icon: <RotateCcw size={13} strokeWidth={2.1} />,
          },
        ]}
      />
    </div>
  ),
};

export const OpenActivityPanel = {
  render: () => (
    <div className="hh-story-surface">
      <ActivityPanel open={true} onClose={() => undefined} icon={<Activity size={16} />} />
    </div>
  ),
};
