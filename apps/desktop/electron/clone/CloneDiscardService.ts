// hp-58wp — Main-process service that backs the
// `hoopoe.clone.discard-local-changes` preload channel.
//
// hp-hde4 retires the destructive behavior behind this legacy channel:
// the desktop local clone is a read-only sync mirror, and Guardrail 3 has
// no reset/clean exception. The service still validates projectId +
// clone-state and emits audit, then refuses with an explicit read-only
// error before any engine-side mutation can run.
//
// Architecture (per memory note "Engine-first slices for large beads"):
//   1. The engine layer (`apps/desktop/electron/clone/discard.ts`) keeps
//      the same refusal for direct imports.
//   2. THIS file is the Electron-host shim: project-id resolution and
//      audit-event emission. The IpcRegistry registration in
//      BackendLifecycle calls into this service.
//
// Lives in `electron/clone/` (alongside the engine) rather than
// `src/main/` because the desktop tsconfig only includes `src/`; reaching
// from `src/main/` into `electron/` pulls additional electron-only files
// into tsc's project graph and surfaces latent type errors that aren't
// in scope here. BackendLifecycle bootstrap can import from this path
// the same way it imports from `electron/clone/index.ts`.

import {
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
  // Valid desktop mirror, but the requested operation would mutate it.
  | "discard.read-only-mirror";

/** Audit event payload. The audit sink is responsible for shape-stable
 *  serialization. Per Guardrail 10, audit fires even though the legacy
 *  discard action is now always refused. */
export interface CloneDiscardAuditEvent {
  readonly kind: "clone.discard-local-changes";
  readonly projectId: string;
  readonly cloneRepoPath: string;
  readonly outcome: "refused";
  /** Reason code: matches CloneDiscardServiceErrorCode for refused paths. */
  readonly reasonCode: string;
  /** Free-form message — never carries paths the renderer didn't already
   *  know about; safe to surface in Diagnostics. */
  readonly message?: string;
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
  /** Wall-clock injection (tests). Defaults to `() => new Date()`. */
  readonly now?: () => Date;
}

const PROJECT_ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

export class CloneDiscardService {
  readonly #resolveCloneRepoPath: CloneRepoResolver;
  readonly #readCloneState: CloneStateReader;
  readonly #audit: CloneDiscardAuditSink;
  readonly #now: () => Date;

  constructor(options: CloneDiscardServiceOptions) {
    this.#resolveCloneRepoPath = options.resolveCloneRepoPath;
    this.#readCloneState = options.readCloneState;
    this.#audit = options.audit;
    this.#now = options.now ?? (() => new Date());
  }

  /** Refuse discard against `projectId`. Throws CloneDiscardServiceError;
   *  the audit event always fires. */
  discardLocalChanges(input: { readonly projectId: string }): never {
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

    this.#emit({
      projectId,
      cloneRepoPath,
      outcome: "refused",
      reasonCode: "discard.read-only-mirror",
      message: "desktop local clone is read-only; git writes must run on the VPS clone",
    });
    throw new CloneDiscardServiceError(
      "discard.read-only-mirror",
      "desktop local clone is read-only; Hoopoe refuses to reset or clean the mirror",
      { projectId, cloneRepoPath },
    );
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
  // as far enough along for the read-only guard to return the explicit
  // hp-hde4 error instead of a "not cloned yet" setup error.
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
