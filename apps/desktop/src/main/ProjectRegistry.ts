import type { ShellRouteId } from "../renderer/stages.ts";

export const PROJECT_REGISTRY_SCHEMA_VERSION = 1;

export interface ProjectRegistryRecord {
  readonly id: string;
  readonly name: string;
  readonly rootPath: string;
  readonly repoUrl: string;
  readonly branch: string;
  readonly pinned: boolean;
  readonly lastActivatedAt: string;
}

export interface ProjectRegistryViewState {
  readonly lastStageId: ShellRouteId;
  readonly lastOpenBeadDrawerId: string | null;
  readonly lastBeadDrawerScrollY: number;
  readonly stageScrollYByStage: Partial<Record<ShellRouteId, number>>;
  readonly activityPanelOpen: boolean;
  readonly activityPanelFilter: string;
  readonly activityPanelScrollY: number;
  readonly dagViewport: {
    readonly zoom: number;
    readonly pan: {
      readonly x: number;
      readonly y: number;
    };
  };
}

export interface ProjectRegistrySnapshot {
  readonly schemaVersion: typeof PROJECT_REGISTRY_SCHEMA_VERSION;
  readonly lastActiveProjectId: string | null;
  readonly projects: readonly ProjectRegistryRecord[];
  readonly perProjectState: Record<string, ProjectRegistryViewState>;
}

export interface ProjectRegistryStorage {
  readonly read: () => ProjectRegistrySnapshot | null;
  readonly write: (snapshot: ProjectRegistrySnapshot) => void;
}

export interface ProjectRegistryLogger {
  readonly info: (event: string, meta?: Record<string, unknown>) => void;
}

const noopLogger: ProjectRegistryLogger = { info() {} };

export class ProjectRegistry {
  private snapshot: ProjectRegistrySnapshot;
  private readonly storage: ProjectRegistryStorage;
  private readonly logger: ProjectRegistryLogger;

  constructor(input: {
    readonly storage: ProjectRegistryStorage;
    readonly defaults?: ProjectRegistrySnapshot;
    readonly logger?: ProjectRegistryLogger;
  }) {
    this.storage = input.storage;
    this.logger = input.logger ?? noopLogger;
    this.snapshot =
      sanitizeSnapshot(input.storage.read()) ??
      input.defaults ??
      createEmptyProjectRegistrySnapshot();
  }

  list(): readonly ProjectRegistryRecord[] {
    return this.snapshot.projects;
  }

  get lastActiveProjectId(): string | null {
    return this.snapshot.lastActiveProjectId;
  }

  activate(projectId: string, source: "top-bar" | "cmd-p" | "deep-link"): ProjectRegistryRecord {
    const project = this.snapshot.projects.find((candidate) => candidate.id === projectId);
    if (!project) {
      throw new Error(`Unknown project: ${projectId}`);
    }

    const now = new Date().toISOString();
    const fromProjectId = this.snapshot.lastActiveProjectId;
    this.snapshot = {
      ...this.snapshot,
      lastActiveProjectId: projectId,
      projects: this.snapshot.projects.map((candidate) =>
        candidate.id === projectId
          ? { ...candidate, lastActivatedAt: now }
          : candidate,
      ),
      perProjectState: ensureProjectViewState(this.snapshot.perProjectState, projectId),
    };
    this.persist("project_switcher.selected", {
      from_project_id: fromProjectId,
      to_project_id: projectId,
      source,
    });
    return { ...project, lastActivatedAt: now };
  }

  pin(projectId: string, pinned: boolean): void {
    assertKnownProject(this.snapshot, projectId);
    this.snapshot = {
      ...this.snapshot,
      projects: this.snapshot.projects.map((project) =>
        project.id === projectId ? { ...project, pinned } : project,
      ),
    };
    this.persist("project_switcher.pin_updated", { project_id: projectId, pinned });
  }

  updateViewState(projectId: string, viewState: Partial<ProjectRegistryViewState>): void {
    assertKnownProject(this.snapshot, projectId);
    const previous = viewStateFor(this.snapshot.perProjectState, projectId);
    this.snapshot = {
      ...this.snapshot,
      perProjectState: {
        ...this.snapshot.perProjectState,
        [projectId]: { ...previous, ...viewState },
      },
    };
    this.persist("project_state.saved", {
      project_id: projectId,
      fields_saved: Object.keys(viewState).toSorted(),
    });
  }

  restoreLaunchTarget(): {
    readonly project: ProjectRegistryRecord | null;
    readonly viewState: ProjectRegistryViewState | null;
    readonly fallbackUsed: boolean;
  } {
    const lastActive = this.snapshot.projects.find(
      (project) => project.id === this.snapshot.lastActiveProjectId,
    );
    if (lastActive) {
      return {
        project: lastActive,
        viewState: viewStateFor(this.snapshot.perProjectState, lastActive.id),
        fallbackUsed: false,
      };
    }

    const fallback = this.snapshot.projects
      .toSorted((a, b) => Date.parse(b.lastActivatedAt) - Date.parse(a.lastActivatedAt))
      .at(0) ?? null;
    if (this.snapshot.lastActiveProjectId) {
      this.logger.info("project_switcher.last_active_missing", {
        last_active_project_id: this.snapshot.lastActiveProjectId,
        fallback_project_id: fallback?.id ?? null,
      });
    }
    return {
      project: fallback,
      viewState: fallback ? viewStateFor(this.snapshot.perProjectState, fallback.id) : null,
      fallbackUsed: true,
    };
  }

  exportSnapshot(): ProjectRegistrySnapshot {
    return this.snapshot;
  }

  private persist(event: string, meta: Record<string, unknown>): void {
    this.storage.write(this.snapshot);
    this.logger.info(event, meta);
  }
}

export function createEmptyProjectRegistrySnapshot(): ProjectRegistrySnapshot {
  return {
    schemaVersion: PROJECT_REGISTRY_SCHEMA_VERSION,
    lastActiveProjectId: null,
    projects: [],
    perProjectState: {},
  };
}

export function createDefaultProjectRegistryViewState(): ProjectRegistryViewState {
  return {
    lastStageId: "plan",
    lastOpenBeadDrawerId: null,
    lastBeadDrawerScrollY: 0,
    stageScrollYByStage: {},
    activityPanelOpen: false,
    activityPanelFilter: "all",
    activityPanelScrollY: 0,
    dagViewport: {
      zoom: 1,
      pan: { x: 0, y: 0 },
    },
  };
}

function sanitizeSnapshot(
  snapshot: ProjectRegistrySnapshot | null,
): ProjectRegistrySnapshot | null {
  if (!snapshot || snapshot.schemaVersion !== PROJECT_REGISTRY_SCHEMA_VERSION) {
    return null;
  }
  return snapshot;
}

function assertKnownProject(snapshot: ProjectRegistrySnapshot, projectId: string): void {
  if (!snapshot.projects.some((project) => project.id === projectId)) {
    throw new Error(`Unknown project: ${projectId}`);
  }
}

function viewStateFor(
  stateByProject: Record<string, ProjectRegistryViewState>,
  projectId: string,
): ProjectRegistryViewState {
  return stateByProject[projectId] ?? createDefaultProjectRegistryViewState();
}

function ensureProjectViewState(
  stateByProject: Record<string, ProjectRegistryViewState>,
  projectId: string,
): Record<string, ProjectRegistryViewState> {
  if (stateByProject[projectId]) return stateByProject;
  return {
    ...stateByProject,
    [projectId]: createDefaultProjectRegistryViewState(),
  };
}
