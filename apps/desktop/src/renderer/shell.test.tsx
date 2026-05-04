import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  ProjectRegistry,
  type ProjectRegistrySnapshot,
} from "../main/ProjectRegistry.ts";
import { ActivityPanel } from "./shell/activity-panel.tsx";
import { EmptyStage } from "./shell/empty-stage.tsx";
import { StageHeader } from "./shell/stage-header.tsx";
import {
  getStageDefinition,
  stageDefinitions,
  stageForPathname,
} from "./stages.ts";
import {
  createDefaultProjectViewState,
  resolveShellLaunchTarget,
  useShellUiStore,
  type ShellProjectSummary,
} from "./store.ts";
import { shouldRedirectToFirstRun } from "./routes.tsx";
import {
  projectMatchesSearch,
  routeForStage,
  splitProjectSections,
} from "./topbar/project-switcher-model.ts";

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
  expect(markup).toContain("Open Beads");
  expect(markup).toContain("raw panes stay in Diagnostics");
  expect(markup.toLowerCase()).not.toContain("terminal");
});

test("Diagnostics empty state links back to the first-run wizard", () => {
  const markup = renderToStaticMarkup(<EmptyStage stageId="diag" />);
  expect(markup).toContain('data-testid="diagnostics-reconnect-wizard"');
  expect(markup).toContain('data-testid="diagnostics-onboarding-tour"');
  expect(markup).toContain('href="/first-run"');
});

test("shouldRedirectToFirstRun: only fresh installs go to the wizard", () => {
  expect(shouldRedirectToFirstRun({ activeProjectId: null, lastProjectId: null })).toBe(true);
  expect(
    shouldRedirectToFirstRun({
      activeProjectId: null,
      lastProjectId: null,
      firstRunCompletedAt: "2026-05-04T07:00:00Z",
    }),
  ).toBe(false);
  expect(shouldRedirectToFirstRun({ activeProjectId: "local-demo", lastProjectId: null })).toBe(false);
  expect(shouldRedirectToFirstRun({ activeProjectId: null, lastProjectId: "local-demo" })).toBe(false);
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
    activeProjectId: null,
    activityPanelOpen: false,
    firstRunCompletedAt: null,
    lastProjectId: null,
    lastStageId: "plan",
    onboardingTourCompletedAt: null,
    onboardingTourLastOpenedAt: null,
    onboardingTourOpen: false,
    onboardingTourSkippedAt: null,
    onboardingTourStepId: "planning",
    pendingSwitchProjectId: null,
    projectViewStateById: {},
  });

  useShellUiStore.getState().rememberProject("local-demo");
  useShellUiStore.getState().toggleActivityPanel();
  useShellUiStore.getState().rememberStage("bead");

  expect(useShellUiStore.getState().activityPanelOpen).toBe(true);
  expect(useShellUiStore.getState().lastProjectId).toBe("local-demo");
  expect(useShellUiStore.getState().lastStageId).toBe("bead");
  useShellUiStore.getState().markFirstRunCompleted("2026-05-04T07:00:00Z");
  expect(useShellUiStore.getState().firstRunCompletedAt).toBe("2026-05-04T07:00:00Z");
  expect(useShellUiStore.getState().onboardingTourOpen).toBe(true);
  expect(useShellUiStore.getState().onboardingTourStepId).toBe("topbar");
  expect(
    useShellUiStore.getState().projectViewStateById["local-demo"]?.activityPanelOpen,
  ).toBe(true);
  expect(
    useShellUiStore.getState().projectViewStateById["local-demo"]?.lastStageId,
  ).toBe("bead");
});

test("shell UI store persists onboarding tour resume and terminal states", () => {
  useShellUiStore.setState({
    onboardingTourCompletedAt: null,
    onboardingTourLastOpenedAt: null,
    onboardingTourOpen: false,
    onboardingTourSkippedAt: null,
    onboardingTourStepId: "topbar",
  });

  useShellUiStore.getState().openOnboardingTour("planning");
  expect(useShellUiStore.getState().onboardingTourOpen).toBe(true);
  expect(useShellUiStore.getState().onboardingTourStepId).toBe("planning");

  useShellUiStore.getState().advanceOnboardingTour();
  expect(useShellUiStore.getState().onboardingTourStepId).toBe("beads");

  useShellUiStore.getState().retreatOnboardingTour();
  expect(useShellUiStore.getState().onboardingTourStepId).toBe("planning");

  useShellUiStore.getState().skipOnboardingTour("2026-05-04T07:01:00Z");
  expect(useShellUiStore.getState().onboardingTourOpen).toBe(false);
  expect(useShellUiStore.getState().onboardingTourSkippedAt).toBe("2026-05-04T07:01:00Z");

  useShellUiStore.getState().openOnboardingTour();
  useShellUiStore.getState().completeOnboardingTour("2026-05-04T07:02:00Z");
  expect(useShellUiStore.getState().onboardingTourStepId).toBe("hardening");
  expect(useShellUiStore.getState().onboardingTourCompletedAt).toBe("2026-05-04T07:02:00Z");
});

test("shell launch target restores the persisted project and stage from root", () => {
  const projects: readonly ShellProjectSummary[] = [
    projectFixture({
      id: "demo-a",
      pinned: false,
      lastActivatedAt: "2026-05-02T10:00:00.000Z",
    }),
    projectFixture({
      id: "demo-b",
      pinned: false,
      lastActivatedAt: "2026-05-02T12:00:00.000Z",
    }),
  ];

  expect(
    resolveShellLaunchTarget({
      activeProjectId: null,
      lastProjectId: "demo-a",
      lastStageId: "plan",
      projectViewStateById: {
        "demo-a": { ...createDefaultProjectViewState(), lastStageId: "swarm" },
      },
      projects,
    }),
  ).toEqual({ projectId: "demo-a", stageId: "swarm" });

  expect(
    resolveShellLaunchTarget({
      activeProjectId: null,
      lastProjectId: "removed-demo",
      lastStageId: "bead",
      projectViewStateById: {},
      projects,
    }),
  ).toEqual({ projectId: "demo-b", stageId: "bead" });
});

test("project switcher model preserves pinned order and fuzzy-searches separators", () => {
  const projects: readonly ShellProjectSummary[] = [
    projectFixture({ id: "demo", pinned: true, lastActivatedAt: "2026-05-02T10:00:00.000Z" }),
    projectFixture({ id: "frontend-X", pinned: false, lastActivatedAt: "2026-05-02T12:00:00.000Z" }),
    projectFixture({ id: "live-demo", pinned: true, lastActivatedAt: "2026-05-02T11:00:00.000Z" }),
    projectFixture({ id: "older", pinned: false, lastActivatedAt: "2026-05-02T09:00:00.000Z" }),
  ];

  const sections = splitProjectSections(projects, "");

  expect(sections.pinned.map((project) => project.id)).toEqual(["demo", "live-demo"]);
  expect(sections.recent.map((project) => project.id)).toEqual(["frontend-X", "older"]);
  expect(projectMatchesSearch(projects[1]!, "fe/X")).toBe(true);
  expect(projectMatchesSearch(projects[3]!, "missing")).toBe(false);
  expect(routeForStage("diag")).toBe("/$projectId/diag");
});

test("shell UI store confirms project switches without halting a running swarm", () => {
  const originalProjects = useShellUiStore.getState().projects;
  useShellUiStore.setState({
    activeProjectId: null,
    activityPanelOpen: false,
    firstRunCompletedAt: null,
    lastProjectId: null,
    lastStageId: "plan",
    pendingSwitchProjectId: null,
    projectViewStateById: {
      "local-demo": { ...createDefaultProjectViewState(), lastStageId: "bead" },
    },
    projects: originalProjects,
  });

  useShellUiStore.getState().rememberProject("mock-flywheel-project");
  const result = useShellUiStore.getState().requestProjectSwitch("local-demo");

  expect(result).toBe("needs-confirmation");
  expect(useShellUiStore.getState().pendingSwitchProjectId).toBe("local-demo");

  const switchedProjectId =
    useShellUiStore.getState().confirmPendingProjectSwitch("continue");

  expect(switchedProjectId).toBe("local-demo");
  expect(useShellUiStore.getState().activeProjectId).toBe("local-demo");
  expect(useShellUiStore.getState().projectViewStateById["local-demo"]?.lastStageId).toBe(
    "bead",
  );
});

test("ProjectRegistry restores the last active project with persisted view state", () => {
  let persisted: ProjectRegistrySnapshot | null = {
    schemaVersion: 1,
    lastActiveProjectId: "demo-b",
    projects: [
      {
        id: "demo-a",
        name: "Demo A",
        rootPath: "/tmp/demo-a",
        repoUrl: "fixture://demo-a",
        branch: "main",
        pinned: true,
        lastActivatedAt: "2026-05-02T10:00:00.000Z",
      },
      {
        id: "demo-b",
        name: "Demo B",
        rootPath: "/tmp/demo-b",
        repoUrl: "fixture://demo-b",
        branch: "main",
        pinned: false,
        lastActivatedAt: "2026-05-02T11:00:00.000Z",
      },
    ],
    perProjectState: {
      "demo-b": {
        ...createDefaultProjectViewState("swarm"),
        lastOpenBeadDrawerId: "hp-x14e",
        activityPanelOpen: true,
      },
    },
  };
  const events: string[] = [];
  const registry = new ProjectRegistry({
    storage: {
      read: () => persisted,
      write: (snapshot) => {
        persisted = snapshot;
      },
    },
    logger: {
      info: (event) => {
        events.push(event);
      },
    },
  });

  const launchTarget = registry.restoreLaunchTarget();
  expect(launchTarget.project?.id).toBe("demo-b");
  expect(launchTarget.viewState?.lastStageId).toBe("swarm");
  expect(launchTarget.viewState?.lastOpenBeadDrawerId).toBe("hp-x14e");

  registry.updateViewState("demo-b", { activityPanelFilter: "agent=ag-7" });
  expect(persisted?.perProjectState["demo-b"]?.activityPanelFilter).toBe("agent=ag-7");
  expect(events).toContain("project_state.saved");
});

function projectFixture(
  input: Pick<ShellProjectSummary, "id" | "pinned" | "lastActivatedAt">,
): ShellProjectSummary {
  return {
    id: input.id,
    name: input.id,
    slug: input.id,
    repoUrl: `fixture://${input.id}`,
    rootPath: `/tmp/${input.id}`,
    branch: input.id === "frontend-X" ? "fe/X-project-switcher" : "main",
    gitStatus: "clean",
    pinned: input.pinned,
    lastActivatedAt: input.lastActivatedAt,
    swarm: { status: "idle", activeAgents: 0, readyBeads: 0 },
    toolHealth: { vps: "healthy", ntm: "healthy", mail: "healthy" },
  };
}
