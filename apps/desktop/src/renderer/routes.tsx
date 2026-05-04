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
import { PersistentWizardShell } from "./wizard/index.ts";
import { RouterErrorComponent } from "./error-ux/RouterErrorComponent.tsx";

const rootRoute = createRootRoute({
  component: RootLayout,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  beforeLoad: () => {
    const state = useShellUiStore.getState();
    if (shouldRedirectToFirstRun(state)) {
      throw redirect({ to: "/first-run" });
    }
    const launchTarget = resolveShellLaunchTarget(state);
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
    <PersistentWizardShell
      onComplete={() => {
        useShellUiStore.getState().markFirstRunCompleted();
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

export function shouldRedirectToFirstRun(state: {
  readonly activeProjectId: string | null;
  readonly lastProjectId: string | null;
  readonly firstRunCompletedAt?: string | null;
}): boolean {
  const firstRunIncomplete =
    state.firstRunCompletedAt === null || state.firstRunCompletedAt === undefined;
  return state.activeProjectId === null && state.lastProjectId === null && firstRunIncomplete;
}

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

// hp-vau: TanStack Router wraps the rendered route subtree in its own
// `CatchBoundary` (Matches.js:36 in @tanstack/react-router@1.169.1), so
// route-component throws never bubble up to the React `ErrorBoundary` at
// `main.tsx`. `defaultErrorComponent` ensures the same Hoopoe fallback +
// errorBus publish pipeline runs regardless of where the throw
// originates. The above-router `ErrorBoundary` still catches throws in
// `<RouterProvider>`'s own setup or anything outside the matched
// subtree.
export const router = createRouter({
  routeTree,
  defaultPreload: "intent",
  defaultErrorComponent: RouterErrorComponent,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
