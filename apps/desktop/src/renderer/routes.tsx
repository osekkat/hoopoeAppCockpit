import {
  Outlet,
  createRootRoute,
  createRoute,
  createRouter,
  redirect,
  useNavigate,
} from "@tanstack/react-router";
import { RootLayout } from "./shell/RootLayout.tsx";
import { ProjectPickerRoute, StageRoute } from "./shell/routes.tsx";
import { useShellUiStore, resolveShellLaunchTarget } from "./store.ts";
import { routeForStage } from "./topbar/project-switcher-model.ts";
import { WizardShell } from "./wizard/index.ts";

const rootRoute = createRootRoute({
  component: RootLayout,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => {
    const launchTarget = resolveShellLaunchTarget(useShellUiStore.getState());
    if (!launchTarget) return;
    throw redirect({
      to: routeForStage(launchTarget.stageId),
      params: { projectId: launchTarget.projectId },
    });
  },
  component: ProjectPickerRoute,
});

const firstRunRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "first-run",
  component: FirstRunRoute,
});

function FirstRunRoute() {
  const navigate = useNavigate();
  return (
    <WizardShell
      onComplete={() => {
        void navigate({ to: "/" });
      }}
    />
  );
}

const projectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "$projectId",
  component: Outlet,
});

const projectIndexRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/",
  beforeLoad: ({ params }) => {
    throw redirect({
      to: "/$projectId/plan",
      params: { projectId: params.projectId },
    });
  },
});

const planningRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "plan",
  component: () => <StageRoute stageId="plan" />,
});

const beadsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "bead",
  component: () => <StageRoute stageId="bead" />,
});

const swarmRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "swarm",
  component: () => <StageRoute stageId="swarm" />,
});

const hardeningRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "harden",
  component: () => <StageRoute stageId="harden" />,
});

const diagnosticsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "diag",
  component: () => <StageRoute stageId="diag" />,
});

const routeTree = rootRoute.addChildren([
  indexRoute,
  firstRunRoute,
  projectRoute.addChildren([
    projectIndexRoute,
    planningRoute,
    beadsRoute,
    swarmRoute,
    hardeningRoute,
    diagnosticsRoute,
  ]),
]);

export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
