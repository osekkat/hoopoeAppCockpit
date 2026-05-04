// hp-2n1 — public entry point for `apps/desktop/electron/clone/`.
//
// Engine layer for the desktop-side local Git clone (§7.7). UI surfaces
// (dirty banner in stage header, total-cache view in settings, per-project
// Clear/Reveal/Open actions) consume from this module and live in the
// renderer + main process.

export {
  CLEAN_CLONE_STATE,
  CLONE_STATE_SCHEMA_VERSION,
  CLONE_SYNC_STATES,
  DEFAULT_CLONE_CAPS,
  emptyCloneState,
  type CloneCapConfig,
  type CloneDirtyState,
  type CloneError,
  type CloneState,
  type CloneStateFile,
  type CloneSyncStatus,
} from "./types.ts";

export {
  CloneStateError,
  cloneRepoPath,
  cloneStateFilePath,
  ensureCloneState,
  projectDir,
  readCloneState,
  updateCloneState,
  writeCloneState,
  type CloneStorageLayout,
} from "./state.ts";

export {
  directorySizeBytes,
  evaluateCaps,
  validateCaps,
  type CapEvaluation,
  type CapVerdict,
} from "./disk.ts";

export {
  CloneGitError,
  classifyGitFailure,
  defaultRunCommand,
  fetchAll,
  initialClone,
  readCurrentBranch,
  readHeadSha,
  type CommandResult,
  type CommandRunner,
  type FetchAllInput,
  type FetchAllResult,
  type InitialCloneInput,
  type InitialCloneResult,
} from "./git.ts";

export {
  parsePorcelain,
  probeDirtyState,
  type ProbeDirtyStateInput,
} from "./dirty.ts";
