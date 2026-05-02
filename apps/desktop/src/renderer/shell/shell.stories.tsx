import { Activity } from "lucide-react";
import { RouterProvider } from "@tanstack/react-router";
import { router } from "../routes.tsx";
import { getStageDefinition } from "../stages.ts";
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

export const OpenActivityPanel = {
  render: () => (
    <div className="hh-story-surface">
      <ActivityPanel open={true} onClose={() => undefined} icon={<Activity size={16} />} />
    </div>
  ),
};
