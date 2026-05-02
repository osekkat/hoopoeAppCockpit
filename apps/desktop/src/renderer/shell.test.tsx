import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { ActivityPanel } from "./shell/activity-panel.tsx";
import { EmptyStage } from "./shell/empty-stage.tsx";
import { StageHeader } from "./shell/stage-header.tsx";
import {
  getStageDefinition,
  stageDefinitions,
  stageForPathname,
} from "./stages.ts";
import { useShellUiStore } from "./store.ts";

test("stage routes are stable and typed for the four primary stages plus diagnostics", () => {
  expect(stageDefinitions.map((stage) => stage.routeSegment)).toEqual([
    "plan",
    "bead",
    "swarm",
    "harden",
    "diag",
  ]);
  expect(stageForPathname("/local-demo/swarm")?.id).toBe("swarm");
  expect(getStageDefinition("harden").routeTo).toBe("/$projectId/harden");
});

test("StageHeader renders STAGE N framing and breadcrumbs", () => {
  const markup = renderToStaticMarkup(
    <StageHeader
      stage={getStageDefinition("plan")}
      projectName="Local demo"
      breadcrumb={["Planning"]}
    />,
  );

  expect(markup).toContain("STAGE 01");
  expect(markup).toContain("PLAN");
  expect(markup).toContain("Local demo");
});

test("Swarm empty state exposes bead board and agent grid without terminal panes", () => {
  const markup = renderToStaticMarkup(<EmptyStage stageId="swarm" />);

  expect(markup).toContain("Bead board");
  expect(markup).toContain("Agent grid");
  expect(markup.toLowerCase()).not.toContain("terminal");
});

test("Activity panel can render open and closed states", () => {
  const openMarkup = renderToStaticMarkup(
    <ActivityPanel open={true} onClose={() => undefined} />,
  );
  const closedMarkup = renderToStaticMarkup(
    <ActivityPanel open={false} onClose={() => undefined} />,
  );

  expect(openMarkup).toContain("data-open=\"true\"");
  expect(closedMarkup).toContain("data-open=\"false\"");
});

test("shell UI store persists activity drawer and route memory state", () => {
  useShellUiStore.setState({
    activityPanelOpen: false,
    lastProjectId: null,
    lastStageId: "plan",
  });

  useShellUiStore.getState().toggleActivityPanel();
  useShellUiStore.getState().rememberProject("local-demo");
  useShellUiStore.getState().rememberStage("bead");

  expect(useShellUiStore.getState().activityPanelOpen).toBe(true);
  expect(useShellUiStore.getState().lastProjectId).toBe("local-demo");
  expect(useShellUiStore.getState().lastStageId).toBe("bead");
});
