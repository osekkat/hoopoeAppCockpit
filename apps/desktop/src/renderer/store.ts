import { create } from "zustand";
import {
  createJSONStorage,
  persist,
  type StateStorage,
} from "zustand/middleware";
import type { ShellRouteId } from "./stages.ts";

export type ProjectGitStatus = "clean" | "dirty" | "unpushed";
export type ProjectToolHealth = "healthy" | "degraded" | "offline";
export type ProjectSwarmStatus = "idle" | "running" | "paused";

export interface ShellProjectSummary {
  readonly id: string;
  readonly name: string;
  readonly slug: string;
  readonly repoUrl: string;
  readonly rootPath: string;
  readonly branch: string;
  readonly gitStatus: ProjectGitStatus;
  readonly pinned: boolean;
  readonly lastActivatedAt: string;
  readonly swarm: {
    readonly status: ProjectSwarmStatus;
    readonly activeAgents: number;
    readonly readyBeads: number;
  };
  readonly toolHealth: {
    readonly vps: ProjectToolHealth;
    readonly ntm: ProjectToolHealth;
    readonly mail: ProjectToolHealth;
  };
}

export interface DagViewport {
  readonly zoom: number;
  readonly pan: {
    readonly x: number;
    readonly y: number;
  };
}

export interface ProjectViewState {
  readonly lastStageId: ShellRouteId;
  readonly lastOpenBeadDrawerId: string | null;
  readonly lastBeadDrawerScrollY: number;
  readonly stageScrollYByStage: Partial<Record<ShellRouteId, number>>;
  readonly activityPanelOpen: boolean;
  readonly activityPanelFilter: string;
  readonly activityPanelScrollY: number;
  readonly dagViewport: DagViewport;
}

export interface ShellUiState {
  readonly projects: readonly ShellProjectSummary[];
  readonly activeProjectId: string | null;
  readonly activityPanelOpen: boolean;
  readonly lastProjectId: string | null;
  readonly lastStageId: ShellRouteId;
  readonly projectSwitcherOpen: boolean;
  readonly projectSearch: string;
  readonly pendingSwitchProjectId: string | null;
  readonly duplicateSwitchSuppressCount: number;
  readonly projectViewStateById: Record<string, ProjectViewState>;
  readonly setActivityPanelOpen: (open: boolean) => void;
  readonly toggleActivityPanel: () => void;
  readonly setProjectSwitcherOpen: (open: boolean) => void;
  readonly setProjectSearch: (search: string) => void;
  readonly requestProjectSwitch: (
    projectId: string,
  ) => "switched" | "needs-confirmation" | "missing" | "duplicate";
  readonly confirmPendingProjectSwitch: (
    choice: "continue" | "pause-first",
  ) => string | null;
  readonly cancelPendingProjectSwitch: () => void;
  readonly viewCurrentProjectSwarm: () => void;
  readonly toggleProjectPin: (projectId: string) => void;
  readonly rememberProject: (projectId: string) => void;
  readonly rememberStage: (stageId: ShellRouteId) => void;
  readonly rememberStageScroll: (
    projectId: string,
    stageId: ShellRouteId,
    scrollY: number,
  ) => void;
  readonly rememberBeadDrawer: (
    projectId: string,
    beadId: string | null,
    scrollY: number,
  ) => void;
  readonly setActivityPanelFilter: (filter: string) => void;
  readonly rememberActivityPanelScroll: (scrollY: number) => void;
  readonly rememberDagViewport: (projectId: string, viewport: DagViewport) => void;
}

const memoryStorage = new Map<string, string>();

const fallbackStorage: StateStorage = {
  getItem: (name) => memoryStorage.get(name) ?? null,
  setItem: (name, value) => {
    memoryStorage.set(name, value);
  },
  removeItem: (name) => {
    memoryStorage.delete(name);
  },
};

const fixtureProjects: readonly ShellProjectSummary[] = [
  {
    id: "local-demo",
    name: "Local demo",
    slug: "local-demo",
    repoUrl: "fixture://mock-flywheel/local-demo",
    rootPath: "~/.hoopoe/demo/local-demo",
    branch: "demo/main",
    gitStatus: "clean",
    pinned: true,
    lastActivatedAt: "2026-05-02T20:30:00.000Z",
    swarm: { status: "idle", activeAgents: 0, readyBeads: 0 },
    toolHealth: { vps: "healthy", ntm: "healthy", mail: "healthy" },
  },
  {
    id: "mock-flywheel-project",
    name: "Mock Flywheel corpus",
    slug: "mock-flywheel",
    repoUrl: "fixture://mock-flywheel/healthy-hour",
    rootPath: "~/.hoopoe/demo/mock-flywheel",
    branch: "fixture/healthy-hour",
    gitStatus: "dirty",
    pinned: true,
    lastActivatedAt: "2026-05-02T19:45:00.000Z",
    swarm: { status: "running", activeAgents: 4, readyBeads: 6 },
    toolHealth: { vps: "healthy", ntm: "healthy", mail: "degraded" },
  },
  {
    id: "hoopoe-cockpit",
    name: "Hoopoe cockpit",
    slug: "hoopoe-cockpit",
    repoUrl: "github.com/osekkat/hoopoeAppCockpit",
    rootPath: "~/Projects/hoopoeAppCockpit",
    branch: "main",
    gitStatus: "unpushed",
    pinned: false,
    lastActivatedAt: "2026-05-02T18:15:00.000Z",
    swarm: { status: "paused", activeAgents: 0, readyBeads: 3 },
    toolHealth: { vps: "degraded", ntm: "healthy", mail: "healthy" },
  },
  {
    id: "frontend-x",
    name: "Frontend X",
    slug: "frontend-X",
    repoUrl: "github.com/example/frontend-X",
    rootPath: "~/Projects/frontend-X",
    branch: "fe/X-project-switcher",
    gitStatus: "clean",
    pinned: false,
    lastActivatedAt: "2026-05-02T16:00:00.000Z",
    swarm: { status: "idle", activeAgents: 0, readyBeads: 1 },
    toolHealth: { vps: "healthy", ntm: "offline", mail: "healthy" },
  },
];

const defaultDagViewport: DagViewport = {
  zoom: 1,
  pan: { x: 0, y: 0 },
};

export function createDefaultProjectViewState(
  stageId: ShellRouteId = "plan",
): ProjectViewState {
  return {
    lastStageId: stageId,
    lastOpenBeadDrawerId: null,
    lastBeadDrawerScrollY: 0,
    stageScrollYByStage: {},
    activityPanelOpen: false,
    activityPanelFilter: "all",
    activityPanelScrollY: 0,
    dagViewport: defaultDagViewport,
  };
}

function projectExists(
  projects: readonly ShellProjectSummary[],
  projectId: string,
): boolean {
  return projects.some((project) => project.id === projectId);
}

function isRunningSwarm(project: ShellProjectSummary | undefined): boolean {
  return (
    project?.swarm.status === "running" &&
    project.swarm.activeAgents > 0
  );
}

function viewStateFor(
  stateById: Record<string, ProjectViewState>,
  projectId: string,
): ProjectViewState {
  return stateById[projectId] ?? createDefaultProjectViewState();
}

function updateProjectViewState(
  stateById: Record<string, ProjectViewState>,
  projectId: string,
  update: (state: ProjectViewState) => ProjectViewState,
): Record<string, ProjectViewState> {
  return {
    ...stateById,
    [projectId]: update(viewStateFor(stateById, projectId)),
  };
}

export const useShellUiStore = create<ShellUiState>()(
  persist(
    (set, get) => ({
      projects: fixtureProjects,
      activeProjectId: null,
      activityPanelOpen: false,
      lastProjectId: null,
      lastStageId: "plan",
      projectSwitcherOpen: false,
      projectSearch: "",
      pendingSwitchProjectId: null,
      duplicateSwitchSuppressCount: 0,
      projectViewStateById: {},
      setActivityPanelOpen: (open) => {
        const activeProjectId = get().activeProjectId;
        set((state) => ({
          activityPanelOpen: open,
          projectViewStateById: activeProjectId
            ? updateProjectViewState(
                state.projectViewStateById,
                activeProjectId,
                (viewState) => ({ ...viewState, activityPanelOpen: open }),
              )
            : state.projectViewStateById,
        }));
      },
      toggleActivityPanel: () => {
        get().setActivityPanelOpen(!get().activityPanelOpen);
      },
      setProjectSwitcherOpen: (open) => {
        set({
          projectSwitcherOpen: open,
          projectSearch: open ? get().projectSearch : "",
        });
      },
      setProjectSearch: (search) => {
        set({ projectSearch: search });
      },
      requestProjectSwitch: (projectId) => {
        const state = get();
        if (!projectExists(state.projects, projectId)) return "missing";
        if (state.activeProjectId === projectId) {
          set((previous) => ({
            duplicateSwitchSuppressCount: previous.duplicateSwitchSuppressCount + 1,
            projectSwitcherOpen: false,
          }));
          return "duplicate";
        }
        const currentProject = state.projects.find(
          (project) => project.id === state.activeProjectId,
        );
        if (isRunningSwarm(currentProject)) {
          set({ pendingSwitchProjectId: projectId });
          return "needs-confirmation";
        }
        get().rememberProject(projectId);
        set({ projectSwitcherOpen: false, pendingSwitchProjectId: null });
        return "switched";
      },
      confirmPendingProjectSwitch: (choice) => {
        const targetProjectId = get().pendingSwitchProjectId;
        const sourceProjectId = get().activeProjectId;
        if (!targetProjectId || !projectExists(get().projects, targetProjectId)) {
          set({ pendingSwitchProjectId: null });
          return null;
        }
        if (choice === "pause-first" && sourceProjectId) {
          set((state) => ({
            projects: state.projects.map((project) =>
              project.id === sourceProjectId
                ? {
                    ...project,
                    swarm: { ...project.swarm, status: "paused", activeAgents: 0 },
                  }
                : project,
            ),
          }));
        }
        get().rememberProject(targetProjectId);
        set({ projectSwitcherOpen: false, pendingSwitchProjectId: null });
        return targetProjectId;
      },
      cancelPendingProjectSwitch: () => {
        set({ pendingSwitchProjectId: null });
      },
      viewCurrentProjectSwarm: () => {
        const activeProjectId = get().activeProjectId;
        set((state) => ({
          pendingSwitchProjectId: null,
          projectSwitcherOpen: false,
          projectViewStateById: activeProjectId
            ? updateProjectViewState(
                state.projectViewStateById,
                activeProjectId,
                (viewState) => ({ ...viewState, lastStageId: "swarm" }),
              )
            : state.projectViewStateById,
          lastStageId: "swarm",
        }));
      },
      toggleProjectPin: (projectId) => {
        set((state) => ({
          projects: state.projects.map((project) =>
            project.id === projectId
              ? { ...project, pinned: !project.pinned }
              : project,
          ),
        }));
      },
      rememberProject: (projectId) => {
        set((state) => {
          const viewState = viewStateFor(state.projectViewStateById, projectId);
          return {
            activeProjectId: projectId,
            lastProjectId: projectId,
            activityPanelOpen: viewState.activityPanelOpen,
            projects: state.projects.map((project) =>
              project.id === projectId
                ? { ...project, lastActivatedAt: new Date().toISOString() }
                : project,
            ),
            projectViewStateById: updateProjectViewState(
              state.projectViewStateById,
              projectId,
              (previous) => previous,
            ),
          };
        });
      },
      rememberStage: (stageId) => {
        const activeProjectId = get().activeProjectId;
        set((state) => ({
          lastStageId: stageId,
          projectViewStateById: activeProjectId
            ? updateProjectViewState(
                state.projectViewStateById,
                activeProjectId,
                (viewState) => ({ ...viewState, lastStageId: stageId }),
              )
            : state.projectViewStateById,
        }));
      },
      rememberStageScroll: (projectId, stageId, scrollY) => {
        set((state) => ({
          projectViewStateById: updateProjectViewState(
            state.projectViewStateById,
            projectId,
            (viewState) => ({
              ...viewState,
              stageScrollYByStage: {
                ...viewState.stageScrollYByStage,
                [stageId]: Math.max(0, Math.round(scrollY)),
              },
            }),
          ),
        }));
      },
      rememberBeadDrawer: (projectId, beadId, scrollY) => {
        set((state) => ({
          projectViewStateById: updateProjectViewState(
            state.projectViewStateById,
            projectId,
            (viewState) => ({
              ...viewState,
              lastOpenBeadDrawerId: beadId,
              lastBeadDrawerScrollY: Math.max(0, Math.round(scrollY)),
            }),
          ),
        }));
      },
      setActivityPanelFilter: (filter) => {
        const activeProjectId = get().activeProjectId;
        set((state) => ({
          projectViewStateById: activeProjectId
            ? updateProjectViewState(
                state.projectViewStateById,
                activeProjectId,
                (viewState) => ({ ...viewState, activityPanelFilter: filter }),
              )
            : state.projectViewStateById,
        }));
      },
      rememberActivityPanelScroll: (scrollY) => {
        const activeProjectId = get().activeProjectId;
        set((state) => ({
          projectViewStateById: activeProjectId
            ? updateProjectViewState(
                state.projectViewStateById,
                activeProjectId,
                (viewState) => ({
                  ...viewState,
                  activityPanelScrollY: Math.max(0, Math.round(scrollY)),
                }),
              )
            : state.projectViewStateById,
        }));
      },
      rememberDagViewport: (projectId, viewport) => {
        set((state) => ({
          projectViewStateById: updateProjectViewState(
            state.projectViewStateById,
            projectId,
            (viewState) => ({ ...viewState, dagViewport: viewport }),
          ),
        }));
      },
    }),
    {
      name: "hoopoe.desktop.shell.v1",
      storage: createJSONStorage(() =>
        typeof window === "undefined" ? fallbackStorage : window.localStorage,
      ),
      partialize: (state) => ({
        activityPanelOpen: state.activityPanelOpen,
        lastProjectId: state.lastProjectId,
        lastStageId: state.lastStageId,
        activeProjectId: state.activeProjectId,
        projects: state.projects,
        projectViewStateById: state.projectViewStateById,
        duplicateSwitchSuppressCount: state.duplicateSwitchSuppressCount,
      }),
    },
  ),
);
