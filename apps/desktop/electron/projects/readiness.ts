// hp-ilt — Phase 4 readiness checker.
//
// Evaluates the §4.2 'Project imported' gate invariant:
//
//   - Git repo present
//   - Branch known
//   - AGENTS.md present
//   - .hoopoe/ initialized
//   - Tool detection done (at least one language manifest detected
//     OR explicit "no manifest required" override)
//
// Returns a structured `ReadinessReport` so the renderer can render
// one bullet per missing precondition.

import { detectToolEnvironment, readGitRepoInfo, type CommandRunner } from "./lifecycle.ts";
import type { DetectedToolEnvironment, GitRepoInfo } from "./types.ts";

export type ReadinessGateId = "imported";

export interface ReadinessRequirement {
  /** Stable id used by tests and the renderer for routing. */
  id: string;
  /** Human-readable label. */
  label: string;
  /** Whether the requirement is currently satisfied. */
  satisfied: boolean;
  /** Free-form note (e.g., the missing-precondition explanation). */
  note?: string;
}

export interface ReadinessReport {
  /** Which gate this report covers. */
  gate: ReadinessGateId;
  /** Project root the report is about. */
  rootPath: string;
  /** Whether ALL requirements are satisfied. */
  satisfied: boolean;
  /** Per-requirement breakdown (in evaluation order). */
  requirements: readonly ReadinessRequirement[];
  /** Captured snapshot of the tool environment for diagnostics. */
  tools: DetectedToolEnvironment;
  /** Captured snapshot of the git repo info for diagnostics. */
  git: GitRepoInfo;
}

export interface CheckReadinessOptions {
  /** Custom command runner (tests). */
  runCommand?: CommandRunner;
  /** When true, accept projects without any language manifest (e.g.,
   *  documentation-only repos). Default: false (at least one
   *  language manifest required, OR an explicit AGENTS.md). */
  allowNoLanguageManifest?: boolean;
}

/** Evaluate the §4.2 'Project imported' gate. */
export function checkProjectImportedGate(
  rootPath: string,
  options: CheckReadinessOptions = {},
): ReadinessReport {
  const git = readGitRepoInfo(rootPath, options.runCommand !== undefined ? { runCommand: options.runCommand } : {});
  const tools = detectToolEnvironment(rootPath);
  const requirements: ReadinessRequirement[] = [
    {
      id: "git.present",
      label: "Git repository initialized",
      satisfied: git.isGitRepo,
      note: git.isGitRepo ? undefined : "no .git directory at project root",
    },
    {
      id: "git.origin",
      label: "origin remote configured",
      satisfied: git.originRemote !== null,
      note: git.originRemote === null
        ? "v1 requires an external Git remote (plan.md §1.1)"
        : `origin: ${git.originRemote}`,
    },
    {
      id: "git.branch",
      label: "branch known (not detached)",
      satisfied: git.branch !== null,
      note: git.branch === null ? "detached HEAD or empty repo" : `branch: ${git.branch}`,
    },
    {
      id: "agents.md",
      label: "AGENTS.md present",
      satisfied: tools.agentsMdRelative !== null,
      note: tools.agentsMdRelative === null
        ? "create AGENTS.md so coding agents have project guidelines"
        : `AGENTS.md: ${tools.agentsMdRelative}`,
    },
    {
      id: "hoopoe.dir",
      label: ".hoopoe/ initialized",
      satisfied: tools.hasHoopoeDir,
      note: tools.hasHoopoeDir
        ? undefined
        : "run initializeHoopoeDir(rootPath) to create .hoopoe/project.json + plans/",
    },
    {
      id: "tools.detected",
      label: "language manifest detected",
      satisfied:
        tools.manifests.length > 0 ||
        (options.allowNoLanguageManifest === true && tools.agentsMdRelative !== null),
      note:
        tools.manifests.length === 0
          ? options.allowNoLanguageManifest === true
            ? "no language manifest; allowed because allowNoLanguageManifest=true"
            : "no language manifest detected (package.json / pyproject.toml / Cargo.toml / go.mod / …)"
          : `manifests: ${tools.manifests.map((m) => m.name).join(", ")}`,
    },
  ];
  const satisfied = requirements.every((r) => r.satisfied);
  return {
    gate: "imported",
    rootPath,
    satisfied,
    requirements,
    tools,
    git,
  };
}

/** Convenience: returns true iff every requirement is satisfied.
 *  Equivalent to `checkProjectImportedGate(...).satisfied` but
 *  spares callers the report allocation when they only need the bit. */
export function isProjectImported(
  rootPath: string,
  options: CheckReadinessOptions = {},
): boolean {
  return checkProjectImportedGate(rootPath, options).satisfied;
}
