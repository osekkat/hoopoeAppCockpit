// hp-ilt — public entry point for `apps/desktop/electron/projects/`.
//
// Project lifecycle helpers + readiness checker. Pure TS; no Electron
// dependency. The Electron main process and the daemon-side project-
// registry handler can both consume.

export {
  PROJECT_JSON_SCHEMA_VERSION,
  PROJECT_LIFECYCLE_STATES,
  SUPPORTED_LANGUAGE_MANIFESTS,
  type DetectedManifest,
  type DetectedToolEnvironment,
  type GitRepoInfo,
  type LanguageManifestName,
  type ProjectJson,
  type ProjectLifecycleState,
  type ProjectMetadata,
} from "./types.ts";

export {
  ProjectLifecycleError,
  defaultRunCommand,
  detectToolEnvironment,
  initializeBeadsIfMissing,
  initializeHoopoeDir,
  readGitRepoInfo,
  readProjectJson,
  writeProjectJson,
  type BeadsInitOptions,
  type BeadsInitResult,
  type CommandRunner,
  type InitializeHoopoeDirOptions,
  type InitializeHoopoeDirResult,
} from "./lifecycle.ts";

export {
  checkProjectImportedGate,
  isProjectImported,
  type CheckReadinessOptions,
  type ReadinessGateId,
  type ReadinessReport,
  type ReadinessRequirement,
} from "./readiness.ts";
