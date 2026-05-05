// hp-5bhy — Main-process service backing the three remaining clone-action
// preload channels:
//   - hoopoe.clone.reveal-in-finder
//   - hoopoe.clone.open-in-terminal
//   - hoopoe.clone.set-cap-override
//
// Sibling of CloneDiscardService.ts (hp-58wp). Same architectural seam:
// the renderer carries only the projectId; main resolves the clone path
// from the project registry and invokes the underlying side effect with
// safe argv (Guardrail 2). Audit fires on every invocation regardless of
// outcome (Guardrail 10).
//
// Lives in electron/clone/ (next to engine + CloneDiscardService) per
// the boundary noted in CloneDiscardService.ts.

import {
  CloneStateError,
  type CloneCapConfig,
  type CloneState,
  type CloneStorageLayout,
  cloneRepoPath as resolveCloneRepoPathFor,
  emptyCloneState,
  ensureCloneState,
  updateCloneState as engineUpdateCloneState,
} from "./index.ts";

export class CloneActionsServiceError extends Error {
  override readonly name = "CloneActionsServiceError";
  readonly code: CloneActionsServiceErrorCode;
  readonly details: Readonly<Record<string, string>>;

  constructor(
    code: CloneActionsServiceErrorCode,
    message: string,
    details: Readonly<Record<string, string>> = {},
  ) {
    super(message);
    this.code = code;
    this.details = details;
  }
}

export type CloneActionsServiceErrorCode =
  | "actions.projectId-invalid"
  | "actions.caps-invalid"
  | "actions.clone-state-missing"
  | "actions.reveal-failed"
  | "actions.terminal-failed"
  | "actions.cap-write-failed";

export type CloneActionKind = "reveal-in-finder" | "open-in-terminal" | "set-cap-override";

/** hp-z7k: explicit warning attached to every `open-in-terminal` audit
 *  event. The desktop local clone under Library/Application Support is a
 *  read-only sync mirror of origin (Guardrail 3 / plan.md §1.7 + §7.7).
 *  A terminal opened there gives the user shell access — git commits,
 *  pushes, branch mutations from that terminal will NOT propagate to
 *  origin or the VPS clone. Spelling that out in the audit log makes
 *  any later "why did my changes vanish?" investigation deterministic. */
export const TERMINAL_READONLY_MIRROR_NOTICE =
  "Desktop local clone is a read-only sync mirror; git commits / pushes / branch " +
  "mutations from this terminal will NOT propagate to origin or the VPS clone.";

/** Audit event payload. Mirrors CloneDiscardAuditEvent shape so audit
 *  trails across the four clone-action channels are uniformly queryable. */
export interface CloneActionsAuditEvent {
  readonly kind: "clone.action";
  readonly action: CloneActionKind;
  readonly projectId: string;
  readonly cloneRepoPath: string;
  readonly outcome: "ok" | "refused" | "failed";
  readonly reasonCode: string;
  readonly message?: string;
  readonly capsOverride?: CloneCapConfig | null;
  /** hp-z7k: true on every successful `open-in-terminal` audit so log
   *  consumers can grep for terminal opens against the read-only mirror.
   *  Combined with `diagnostics`, lets a reader distinguish "wizard
   *  surface" terminal opens (read-only warning expected) from an
   *  explicit Diagnostics-screen opt-in. */
  readonly mirrorReadOnly?: boolean;
  /** hp-z7k: true when the caller explicitly opted in via the Diagnostics
   *  surface (`{ diagnostics: true }` on the openInTerminal input). The
   *  warning is suppressed in the audit message but `mirrorReadOnly`
   *  stays true — the constraint doesn't go away just because the user
   *  acknowledged it. */
  readonly diagnostics?: boolean;
  readonly at: string;
}

export type CloneActionsAuditSink = (event: CloneActionsAuditEvent) => void;

export interface CloneRepoResolver {
  (projectId: string): string;
}

export interface FinderRevealer {
  (path: string): void | Promise<void>;
}

export interface TerminalOpener {
  (path: string): void | Promise<void>;
}

export interface CloneStateUpdater {
  (
    projectId: string,
    patcher: (current: CloneState) => CloneState,
  ): CloneState;
}

export interface CloneActionsServiceOptions {
  readonly resolveCloneRepoPath: CloneRepoResolver;
  readonly revealInFinder: FinderRevealer;
  readonly openInTerminal: TerminalOpener;
  readonly updateCloneState: CloneStateUpdater;
  readonly audit: CloneActionsAuditSink;
  readonly now?: () => Date;
}

const PROJECT_ID_RE = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

// Sane caps boundaries — ridiculously low or absurdly high values almost
// always indicate a UI bug or hostile renderer payload. The actual product
// limits live alongside CloneSettingsCard's `validateCapOverrideForm`; this
// is the second-line defense against renderer-controlled bytes flowing
// straight into clone-state.json.
const CAP_SOFT_MIN_BYTES = 64 * 1024 * 1024; // 64 MiB
const CAP_HARD_MAX_BYTES = 256 * 1024 * 1024 * 1024; // 256 GiB

export class CloneActionsService {
  readonly #resolveCloneRepoPath: CloneRepoResolver;
  readonly #revealInFinder: FinderRevealer;
  readonly #openInTerminal: TerminalOpener;
  readonly #updateCloneState: CloneStateUpdater;
  readonly #audit: CloneActionsAuditSink;
  readonly #now: () => Date;

  constructor(options: CloneActionsServiceOptions) {
    this.#resolveCloneRepoPath = options.resolveCloneRepoPath;
    this.#revealInFinder = options.revealInFinder;
    this.#openInTerminal = options.openInTerminal;
    this.#updateCloneState = options.updateCloneState;
    this.#audit = options.audit;
    this.#now = options.now ?? (() => new Date());
  }

  async revealInFinder(input: { readonly projectId: string }): Promise<void> {
    const projectId = this.#requireProjectId(input.projectId, "reveal-in-finder");
    const cloneRepoPath = this.#resolveCloneRepoPath(projectId);
    try {
      await this.#revealInFinder(cloneRepoPath);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.#emit({
        action: "reveal-in-finder",
        projectId,
        cloneRepoPath,
        outcome: "failed",
        reasonCode: "actions.reveal-failed",
        message,
      });
      throw new CloneActionsServiceError("actions.reveal-failed", message, {
        projectId,
        cloneRepoPath,
      });
    }
    this.#emit({
      action: "reveal-in-finder",
      projectId,
      cloneRepoPath,
      outcome: "ok",
      reasonCode: "ok",
    });
  }

  async openInTerminal(input: {
    readonly projectId: string;
    /** hp-z7k: set true when the caller has surfaced an explicit
     *  Diagnostics-screen warning to the user (Guardrail 3 — desktop
     *  clone is a read-only mirror). When false/absent, the audit
     *  event carries a read-only-mirror warning string so the log
     *  shows the user reached the terminal from a non-Diagnostics
     *  surface. The action proceeds either way today; a future bead
     *  may flip this to a hard refuse once renderer surfaces adopt
     *  the flag. */
    readonly diagnostics?: boolean;
  }): Promise<void> {
    const projectId = this.#requireProjectId(input.projectId, "open-in-terminal");
    const cloneRepoPath = this.#resolveCloneRepoPath(projectId);
    const diagnostics = input.diagnostics === true;
    try {
      await this.#openInTerminal(cloneRepoPath);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      this.#emit({
        action: "open-in-terminal",
        projectId,
        cloneRepoPath,
        outcome: "failed",
        reasonCode: "actions.terminal-failed",
        message,
        mirrorReadOnly: true,
        diagnostics,
      });
      throw new CloneActionsServiceError("actions.terminal-failed", message, {
        projectId,
        cloneRepoPath,
      });
    }
    this.#emit({
      action: "open-in-terminal",
      projectId,
      cloneRepoPath,
      outcome: "ok",
      reasonCode: "ok",
      // hp-z7k: the warning is on every non-diagnostics open. When
      // diagnostics is true, the user acknowledged the constraint
      // through the Diagnostics surface so the message is suppressed,
      // but `mirrorReadOnly: true` still stamps the audit so log
      // consumers can grep terminal opens against the mirror.
      ...(diagnostics ? {} : { message: TERMINAL_READONLY_MIRROR_NOTICE }),
      mirrorReadOnly: true,
      diagnostics,
    });
  }

  setCapOverride(input: {
    readonly projectId: string;
    readonly capsOverride: CloneCapConfig | null;
  }): CloneState {
    const projectId = this.#requireProjectId(input.projectId, "set-cap-override");
    const cloneRepoPath = this.#resolveCloneRepoPath(projectId);
    const caps = input.capsOverride;
    if (caps !== null) {
      const reason = validateCaps(caps);
      if (reason !== null) {
        this.#emit({
          action: "set-cap-override",
          projectId,
          cloneRepoPath,
          outcome: "refused",
          reasonCode: "actions.caps-invalid",
          message: reason,
          capsOverride: caps,
        });
        throw new CloneActionsServiceError("actions.caps-invalid", reason, {
          projectId,
          cloneRepoPath,
        });
      }
    }
    let updated: CloneState;
    try {
      updated = this.#updateCloneState(projectId, (current) => ({
        ...current,
        capsOverride: caps,
      }));
    } catch (err) {
      // updateCloneState throws CloneStateError("missing_state", ...) when
      // there's no clone-state.json yet — that's a refuse, not a crash.
      if (err instanceof CloneStateError && err.code === "missing_state") {
        this.#emit({
          action: "set-cap-override",
          projectId,
          cloneRepoPath,
          outcome: "refused",
          reasonCode: "actions.clone-state-missing",
          message: err.message,
          capsOverride: caps,
        });
        throw new CloneActionsServiceError(
          "actions.clone-state-missing",
          err.message,
          { projectId, cloneRepoPath },
        );
      }
      const message = err instanceof Error ? err.message : String(err);
      this.#emit({
        action: "set-cap-override",
        projectId,
        cloneRepoPath,
        outcome: "failed",
        reasonCode: "actions.cap-write-failed",
        message,
        capsOverride: caps,
      });
      throw new CloneActionsServiceError("actions.cap-write-failed", message, {
        projectId,
        cloneRepoPath,
      });
    }
    this.#emit({
      action: "set-cap-override",
      projectId,
      cloneRepoPath,
      outcome: "ok",
      reasonCode: "ok",
      capsOverride: caps,
    });
    return updated;
  }

  #requireProjectId(projectId: unknown, action: CloneActionKind): string {
    if (typeof projectId !== "string" || !PROJECT_ID_RE.test(projectId)) {
      const stringId = typeof projectId === "string" ? projectId : "";
      this.#emit({
        action,
        projectId: stringId,
        cloneRepoPath: "",
        outcome: "refused",
        reasonCode: "actions.projectId-invalid",
        message: "projectId must match /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/",
      });
      throw new CloneActionsServiceError(
        "actions.projectId-invalid",
        "projectId is not a valid identifier",
        { projectId: stringId },
      );
    }
    return projectId;
  }

  #emit(event: Omit<CloneActionsAuditEvent, "kind" | "at">): void {
    const stamped: CloneActionsAuditEvent = {
      kind: "clone.action",
      at: this.#now().toISOString(),
      ...event,
    };
    try {
      this.#audit(stamped);
    } catch {
      // Defensive: audit-sink throws cannot block the throw the caller
      // expects.
    }
  }
}

/** Validates a CloneCapConfig before it lands in clone-state.json. The
 *  renderer's `validateCapOverrideForm` runs first; this is defense-in-
 *  depth so a hostile or buggy renderer can't bypass to absurd values. */
export function validateCaps(caps: CloneCapConfig): string | null {
  if (typeof caps.softCapBytes !== "number" || !Number.isFinite(caps.softCapBytes)) {
    return "softCapBytes must be a finite number";
  }
  if (typeof caps.hardCapBytes !== "number" || !Number.isFinite(caps.hardCapBytes)) {
    return "hardCapBytes must be a finite number";
  }
  if (caps.softCapBytes < CAP_SOFT_MIN_BYTES) {
    return `softCapBytes must be at least ${CAP_SOFT_MIN_BYTES} bytes`;
  }
  if (caps.hardCapBytes > CAP_HARD_MAX_BYTES) {
    return `hardCapBytes must be at most ${CAP_HARD_MAX_BYTES} bytes`;
  }
  if (caps.hardCapBytes <= caps.softCapBytes) {
    return "hardCapBytes must be greater than softCapBytes";
  }
  return null;
}

// ── Production-wiring helpers ─────────────────────────────────────────────
//
// BackendLifecycle composes a CloneActionsService by passing these
// helpers — keeps the constructor argument list flat and keeps the call
// sites in the service itself path-agnostic.

export function bindCloneRepoResolver(layout: CloneStorageLayout): CloneRepoResolver {
  return (projectId) => resolveCloneRepoPathFor(layout, projectId);
}

export function bindCloneStateUpdater(layout: CloneStorageLayout): CloneStateUpdater {
  return (projectId, patcher) => engineUpdateCloneState(layout, projectId, patcher);
}

/** Initial state factory bound to the production layout. Used when a
 *  caller wants to ensure `clone-state.json` exists before invoking
 *  setCapOverride against a fresh project. */
export function bindEnsureCloneState(layout: CloneStorageLayout) {
  return (projectId: string, originRemote: string) =>
    ensureCloneState(layout, emptyCloneState({ projectId, originRemote }));
}
