// hp-2n1 — Phase 4 desktop local clone shared types.
//
// The desktop maintains a sync-driven Git clone of every project at
// `~/Library/Application Support/Hoopoe/projects/<project-id>/repo/`,
// fetched FROM ORIGIN (not from the VPS) per plan.md §7.7. The clone is
// a read-only mirror; Hoopoe never writes through it (Guardrail 3).
//
// `clone-state.json` captures everything the renderer needs to render the
// status pill + disk-cap banner without re-walking the filesystem on every
// tick. Atomic writes happen via the helpers in `state.ts`.

export const CLONE_STATE_SCHEMA_VERSION = 1 as const;

export const CLONE_SYNC_STATES = [
  /** No clone yet on disk; first fetch hasn't started. */
  "uncloned",
  /** Initial clone in flight. */
  "cloning",
  /** Background fetch in flight against an existing clone. */
  "fetching",
  /** Last fetch completed cleanly; clone matches origin tip at the
   *  recorded SHA (modulo any new commits pushed since). */
  "synced",
  /** Last sync attempt failed (network, auth, disk full, hard cap). The
   *  renderer surfaces `lastError` next to the project. */
  "error",
] as const;
export type CloneSyncStatus = (typeof CLONE_SYNC_STATES)[number];

export interface CloneError {
  /** Stable code; the renderer maps to a human message. */
  readonly code:
    | "auth_missing"
    | "network"
    | "disk_full"
    | "hard_cap_exceeded"
    | "git_failure"
    | "unknown";
  /** Free-form details from git stderr or the wrapper. */
  readonly message: string;
  /** RFC3339 timestamp of when the error was captured. */
  readonly capturedAt: string;
}

export interface CloneCapConfig {
  /** Bytes; warning surfaces in settings card when exceeded.
   *  Default 2 GB per plan.md §7.7. */
  readonly softCapBytes: number;
  /** Bytes; further fetches refused until the user clears or raises.
   *  Default 5 GB per plan.md §7.7. */
  readonly hardCapBytes: number;
}

export const DEFAULT_CLONE_CAPS: CloneCapConfig = {
  softCapBytes: 2 * 1024 * 1024 * 1024,
  hardCapBytes: 5 * 1024 * 1024 * 1024,
};

export interface CloneDirtyState {
  /** Whether anything (untracked file, modified file, local commit) has
   *  diverged from the upstream-tracking branch. The dirty banner is
   *  shown iff this is true. */
  readonly dirty: boolean;
  /** Number of unstaged-modified files. */
  readonly modifiedCount: number;
  /** Number of untracked files. */
  readonly untrackedCount: number;
  /** Local commits ahead of `@{u}`. */
  readonly aheadCount: number;
  /** Upstream commits behind `@{u}`. Not strictly "dirty" but surfaced
   *  for the same banner. */
  readonly behindCount: number;
}

export const CLEAN_CLONE_STATE: CloneDirtyState = {
  dirty: false,
  modifiedCount: 0,
  untrackedCount: 0,
  aheadCount: 0,
  behindCount: 0,
};

export interface CloneState {
  /** Stable Hoopoe project id (matches `ProjectMetadata.id` from
   *  hp-ilt). The clone lives at
   *  `<HoopoeAppDataRoot>/projects/<projectId>/repo/`. */
  readonly projectId: string;
  /** Origin remote URL the clone was created from. */
  readonly originRemote: string;
  /** Current branch name in the local clone, or null when detached. */
  readonly branch: string | null;
  /** Last-fetched commit SHA (HEAD of `origin/<branch>`), null until
   *  the first fetch completes. */
  readonly lastFetchedSha: string | null;
  /** Sync state machine. */
  readonly syncStatus: CloneSyncStatus;
  /** Recorded clone size on disk in bytes. */
  readonly sizeBytes: number;
  /** RFC3339 timestamp of the last successful fetch (synced state). */
  readonly lastSyncedAt: string | null;
  /** RFC3339 timestamp of the last user-driven open of the clone (e.g.
   *  Reveal in Finder, opening a stage). Used by the cache view to sort
   *  by recency. */
  readonly lastAccessedAt: string;
  /** Last sync error, if any. Cleared on the next successful fetch. */
  readonly lastError: CloneError | null;
  /** Per-project cap override; falls back to the global cap config when
   *  null. */
  readonly capsOverride: CloneCapConfig | null;
  /** Last dirty-state probe. The watcher keeps this fresh; the renderer
   *  reads from here without re-running git status. */
  readonly dirtyState: CloneDirtyState;
}

export interface CloneStateFile {
  readonly schemaVersion: typeof CLONE_STATE_SCHEMA_VERSION;
  readonly state: CloneState;
}

/** Minimal initial state for a brand-new project entry. The clone has not
 *  been created yet; only the projectId + origin remote are known. */
export function emptyCloneState(input: {
  readonly projectId: string;
  readonly originRemote: string;
  readonly now?: () => Date;
}): CloneState {
  const ts = (input.now ?? (() => new Date()))().toISOString();
  return {
    projectId: input.projectId,
    originRemote: input.originRemote,
    branch: null,
    lastFetchedSha: null,
    syncStatus: "uncloned",
    sizeBytes: 0,
    lastSyncedAt: null,
    lastAccessedAt: ts,
    lastError: null,
    capsOverride: null,
    dirtyState: CLEAN_CLONE_STATE,
  };
}
