// hp-ilt — Phase 4 project lifecycle shared types.
//
// These types are consumed by the lifecycle helpers, the readiness
// checker, and renderer-side UI. The shape mirrors `<project>/.hoopoe/
// project.json` per the bead spec; daemon-side persistence (SQLite)
// will encode the same fields when storing projects in the registry.

export const PROJECT_LIFECYCLE_STATES = [
  "imported",
  "planning",
  "plan_finalized",
  "beads_created",
  "beads_finalized",
  "swarm_running",
  "hardening_rounds",
  "quality_gates",
  "completed",
] as const;
export type ProjectLifecycleState = (typeof PROJECT_LIFECYCLE_STATES)[number];

export const SUPPORTED_LANGUAGE_MANIFESTS = [
  "package.json",
  "pyproject.toml",
  "requirements.txt",
  "Cargo.toml",
  "go.mod",
  "Gemfile",
  "pom.xml",
  "build.gradle",
  "build.gradle.kts",
  "Makefile",
] as const;
export type LanguageManifestName = (typeof SUPPORTED_LANGUAGE_MANIFESTS)[number];

export interface DetectedManifest {
  /** Manifest filename (e.g. "package.json"). */
  name: LanguageManifestName;
  /** Path RELATIVE to the project root. */
  relativePath: string;
}

export interface DetectedToolEnvironment {
  /** AGENTS.md path relative to project root, or null when missing. */
  agentsMdRelative: string | null;
  /** README.md path relative to project root, or null when missing. */
  readmeRelative: string | null;
  /** Detected language manifests. */
  manifests: readonly DetectedManifest[];
  /** Whether `.beads/` exists at project root. */
  hasBeadsDir: boolean;
  /** Whether `.hoopoe/` exists at project root. */
  hasHoopoeDir: boolean;
}

export interface GitRepoInfo {
  /** Whether the directory is a git work tree. */
  isGitRepo: boolean;
  /** `git remote get-url origin` output, or null when missing. */
  originRemote: string | null;
  /** Current branch (`git branch --show-current`), or null. */
  branch: string | null;
}

export interface ProjectMetadata {
  /** Stable internal id (UUID). */
  id: string;
  /** Human-readable name; defaults to the project root basename. */
  name: string;
  /** Slug derived from the name (lowercase + safe chars). */
  slug: string;
  /** Absolute path on the VPS where the project lives. */
  rootPath: string;
  /** Origin remote URL — REQUIRED per §1.1; v1 does not support
   *  projects without an external remote. */
  originRemote: string;
  /** Current Git branch. */
  branch: string;
  /** Lifecycle state per §4.1. */
  state: ProjectLifecycleState;
  /** RFC3339 timestamps. */
  createdAt: string;
  updatedAt: string;
  /** Detected tool environment as of the last lifecycle scan. */
  tools: DetectedToolEnvironment;
}

export const PROJECT_JSON_SCHEMA_VERSION = 1 as const;

export interface ProjectJson {
  /** Bumped whenever the on-disk shape changes (per plan.md §10.3). */
  schemaVersion: typeof PROJECT_JSON_SCHEMA_VERSION;
  project: ProjectMetadata;
}
