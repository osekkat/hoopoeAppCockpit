// hp-58wp — Main-process service that backs the
// `hoopoe.clone.discard-local-changes` preload channel.
//
// The channel runs `git reset --hard @{u} && git clean -fd` against a
// project's local clone (§7.7 destructive action). The renderer never
// supplies a path or argv — the service resolves everything from a
// projectId via an injected resolver. Audit fires on every invocation
// regardless of outcome (Guardrail 10).
//
// Architecture (per memory note "Engine-first slices for large beads"):
//   1. The git work itself lives in the engine layer
//      (`apps/desktop/electron/clone/discard.ts`) so it stays runnable
//      from unit tests without an Electron host.
//   2. THIS file is the Electron-host shim: project-id resolution,
//      audit-event emission, dirty-state refresh hook. The IpcRegistry
//      registration in BackendLifecycle calls into this service.
//
// Lives in `electron/clone/` (alongside the engine) rather than
// `src/main/` because the desktop tsconfig only includes `src/`; reaching
// from `src/main/` into `electron/` pulls additional electron-only files
// into tsc's project graph and surfaces latent type errors that aren't
// in scope here. BackendLifecycle bootstrap can import from this path
// the same way it imports from `electron/clone/index.ts`.

import {
  type DiscardLocalChangesResult,
  CloneGitError,
  discardLocalChanges as engineDiscard,
  readCloneState,
  type CloneState,
  type CloneStorageLayout,
  cloneRepoPath as resolveCloneRepoPath,
} from "./index.ts";

export class CloneDiscardServiceError extends Error {
  override readonly name = "CloneDiscardServiceError";
  readonly code: CloneDiscardServiceErrorCode;
  readonly details: Readonly<Record<string, string>>;

  constructor(
    code: CloneDiscardServiceErrorCode,
    message: string,
    details: Readonly<Record<string, string>> = {},
  ) {
    super(message);
    this.code = code;
    this.details = details;
  }
}

export type CloneDiscardServiceErrorCode =
  // Renderer-supplied projectId failed shape validation.
  | "discard.projectId-invalid"
  // No CloneState recorded for this projectId (`clone-state.json`
  // missing) — the project hasn't been cloned yet.
  | "discard.clone-not-cloned"
  // CloneState exists but reports an empty / never-cloned repo.
  | "discard.clone-empty"
  // engineDiscard threw a CloneGitError (passed through with the
  // engine-side code preserved in `details.engineCode`).
  | "discard.git-failed";

/** Audit event payload. The audit sink is responsible for shape-stable
 *  serialization — this service emits the same fields on success and
 *  failure so audit traces are uniformly queryable. Per Guardrail 10:
 *  audit fires whether the discard succeeds, refuses to start, or
 *  throws. */
export interface CloneDiscardAuditEvent {
  readonly kind: "clone.discard-local-changes";
  readonly projectId: string;
  readonly cloneRepoPath: string;
  readonly outcome: "ok" | "refused" | "failed";
  /** Reason code: matches CloneDiscardServiceErrorCode for refused/failed,
   *  `"ok"` for success. */
  readonly reasonCode: string;
  /** Free-form message — never carries paths the renderer didn't already
   *  know about; safe to surface in Diagnostics. */
  readonly message?: string;
  /** Populated on success: SHA the working tree now matches. */
  readonly resetToSha?: string;
  /** Populated on success: number of paths `git clean -fd` removed. -1
   *  when clean reported nothing. */
  readonly removedPathCount?: number;
  /** Wall-clock timestamp from the service's clock injection. */
  readonly at: string;
}

export type CloneDiscardAuditSink = (event: CloneDiscardAuditEvent) => void;

/** Resolves a projectId to its local clone repo path. Production wiring
 *  uses the existing CloneStorageLayout convention
 *  (`<projectsRoot>/<projectId>/repo/`); tests inject a stub so they
 *  don't need a real Hoopoe app data dir. */
export interface CloneRepoResolver {
  (projectId: string): string;
}

/** Subset of the engine's clone-state APIs the service needs. Tests inject
 *  a recorder; production passes the layout-bound implementation. */
export interface CloneStateReader {
  (projectId: string): CloneState | null;
}

export interface CloneDiscardServiceOptions {
  /** Resolves a projectId → on-disk clone path. Required. */
  readonly resolveCloneRepoPath: CloneRepoResolver;
  /** Reads CloneState for a projectId. Required. */
  readonly readCloneState: CloneStateReader;
  /** Audit sink. Called on every invocation regardless of outcome
   *  (Guardrail 10). Required — no silent default; tests inject a spy. */
  readonly audit: CloneDiscardAuditSink;
  /** Engine-layer git invoker injection (tests). Defaults to
   *  `engineDiscard`. */
  readonly engine?: typeof engineDiscard;
  /** Wall-clock injection (tests). Defaults to `() => new Date()`. */
  readonly now?: () => Date;
}

const PROJECT_ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

export class CloneDiscardService {
  readonly #resolveCloneRepoPath: CloneRepoResolver;
  readonly #readCloneState: CloneStateReader;
  readonly #audit: CloneDiscardAuditSink;
  readonly #engine: typeof engineDiscard;
  readonly #now: () => Date;

  constructor(options: CloneDiscardServiceOptions) {
    this.#resolveCloneRepoPath = options.resolveCloneRepoPath;
    this.#readCloneState = options.readCloneState;
    this.#audit = options.audit;
    this.#engine = options.engine ?? engineDiscard;
    this.#now = options.now ?? (() => new Date());
  }

  /** Run discard against `projectId`. Throws CloneDiscardServiceError on
   *  any failure (refused or git-failed); the audit event always fires. */
  discardLocalChanges(input: { readonly projectId: string }): DiscardLocalChangesResult {
    const projectId = input.projectId;
    if (typeof projectId !== "string" || !PROJECT_ID_RE.test(projectId)) {
      this.#emit({
        projectId: typeof projectId === "string" ? projectId : "",
        cloneRepoPath: "",
        outcome: "refused",
        reasonCode: "discard.projectId-invalid",
        message: "projectId must match /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/",
      });
      throw new CloneDiscardServiceError(
        "discard.projectId-invalid",
        "projectId is not a valid identifier",
        { projectId: typeof projectId === "string" ? projectId : "" },
      );
    }

    const cloneRepoPath = this.#resolveCloneRepoPath(projectId);

    const state = this.#readCloneState(projectId);
    if (state === null) {
      this.#emit({
        projectId,
        cloneRepoPath,
        outcome: "refused",
        reasonCode: "discard.clone-not-cloned",
        message: "no clone-state.json found for project — clone first",
      });
      throw new CloneDiscardServiceError(
        "discard.clone-not-cloned",
        "no clone-state.json found for project — clone first",
        { projectId, cloneRepoPath },
      );
    }
    if (isCloneEmpty(state)) {
      this.#emit({
        projectId,
        cloneRepoPath,
        outcome: "refused",
        reasonCode: "discard.clone-empty",
        message: "clone-state reports the project has not been cloned yet",
      });
      throw new CloneDiscardServiceError(
        "discard.clone-empty",
        "clone-state reports the project has not been cloned yet",
        { projectId, cloneRepoPath },
      );
    }

    let result: DiscardLocalChangesResult;
    try {
      result = this.#engine({ cloneRepoPath });
    } catch (err) {
      const engineCode = err instanceof CloneGitError ? err.code : "unknown";
      const message = err instanceof Error ? err.message : String(err);
      this.#emit({
        projectId,
        cloneRepoPath,
        outcome: "failed",
        reasonCode: "discard.git-failed",
        message,
      });
      throw new CloneDiscardServiceError("discard.git-failed", message, {
        projectId,
        cloneRepoPath,
        engineCode,
      });
    }

    this.#emit({
      projectId,
      cloneRepoPath,
      outcome: "ok",
      reasonCode: "ok",
      ...(result.resetToSha ? { resetToSha: result.resetToSha } : {}),
      removedPathCount: result.removedPathCount,
    });
    return result;
  }

  #emit(event: Omit<CloneDiscardAuditEvent, "kind" | "at">): void {
    const stamped: CloneDiscardAuditEvent = {
      kind: "clone.discard-local-changes",
      at: this.#now().toISOString(),
      ...event,
    };
    try {
      this.#audit(stamped);
    } catch {
      // Defensive: an audit sink that throws cannot block the throw the
      // caller expects. Swallow — production wiring uses a logger that
      // doesn't throw.
    }
  }
}

function isCloneEmpty(state: CloneState): boolean {
  // CloneState reports "never cloned" via syncStatus === "uncloned" with
  // no lastFetchedSha and zero sizeBytes. We treat anything that claims
  // a real on-disk clone (non-zero size OR a recorded last-fetched SHA)
  // as eligible for discard. The state file's existence is necessary but
  // not sufficient — engineDiscard re-validates the actual filesystem.
  if (state.lastFetchedSha) return false;
  if (state.sizeBytes > 0) return false;
  return true;
}

/** Convenience binding for production wiring: returns
 *  `cloneRepoPath(layout, projectId)` so BackendLifecycle can pass a
 *  `CloneRepoResolver` without re-importing from this module's path. */
export function bindCloneRepoResolver(layout: CloneStorageLayout): CloneRepoResolver {
  return (projectId) => resolveCloneRepoPath(layout, projectId);
}

/** Convenience binding: returns `readCloneState(layout, projectId)`. */
export function bindCloneStateReader(layout: CloneStorageLayout): CloneStateReader {
  return (projectId) => readCloneState(layout, projectId);
}
