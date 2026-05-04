// hp-ndx5 — Per-clone filesystem watcher.
//
// Wraps `fs.watch(path, { recursive: true })` with:
//   - 500ms debounce (configurable) so editor save bursts collapse into a
//     single probe.
//   - probeDirtyState() invocation that updates CloneState.dirtyState via
//     updateCloneState() — the renderer reads from clone-state.json so
//     the updated dirty count surfaces in the banner without IPC.
//   - one-shot retry on fs.watch error (network drive unmount, sandbox
//     revoke). After the retry, persistent failures emit a "stopped"
//     event with reason="unrecoverable".
//   - explicit stop() that cleans up the fs handle and any pending
//     timers. Idempotent.
//
// All filesystem + git invocations are injectable so tests don't shell
// out or write to real disk.

import { watch, type FSWatcher } from "node:fs";
import {
  cloneRepoPath,
  updateCloneState,
  type CloneStorageLayout,
} from "../clone/state.ts";
import { probeDirtyState, type ProbeDirtyStateInput } from "../clone/dirty.ts";
import { type CloneDirtyState } from "../clone/types.ts";
import {
  debounce,
  realClock,
  type Clock,
  type DebouncedHandle,
} from "./debounce.ts";
import {
  DEFAULT_DEBOUNCE_MS,
  WATCHER_RETRY_DELAY_MS,
  type CloneWatcherEvent,
  type CloneWatcherListener,
  type WatcherStatus,
} from "./types.ts";

export interface FsWatchHandle {
  readonly close: () => void;
  readonly onError: (cb: (err: Error) => void) => void;
}

export interface CloneWatcherDiagnostic {
  readonly projectId: string;
  readonly code: "listener_failed" | "handle_close_failed";
  readonly phase: string;
  readonly message: string;
}

export type CloneWatcherDiagnosticSink = (diagnostic: CloneWatcherDiagnostic) => void;

export interface CreateWatcherOptions {
  readonly projectId: string;
  readonly layout: CloneStorageLayout;
  /** Override debounce window (default 500ms). Tests pass a small value. */
  readonly debounceMs?: number;
  /** Override the clock (tests inject a mock). */
  readonly clock?: Clock;
  /** Override the fs.watch implementation (tests). */
  readonly fsWatchImpl?: typeof watch;
  /** Override probeDirtyState (tests). When omitted, uses the default
   *  with the watcher's `runCommand` injected. */
  readonly probeImpl?: (input: ProbeDirtyStateInput) => CloneDirtyState;
  /** Custom git command runner (tests). Forwarded to probeDirtyState. */
  readonly runCommand?: ProbeDirtyStateInput["runCommand"];
  /** Event sink. Production wires this to the registry → IPC. */
  readonly onEvent?: CloneWatcherListener;
  /** Diagnostics sink for best-effort errors that cannot be emitted as normal watcher events. */
  readonly onDiagnostic?: CloneWatcherDiagnosticSink;
  /** Override the retry delay (tests). */
  readonly retryDelayMs?: number;
}

export interface CloneWatcher {
  readonly projectId: string;
  readonly status: () => WatcherStatus;
  readonly start: () => void;
  readonly stop: () => void;
  /** Force an immediate probe (test seam). */
  readonly probeNow: () => void;
}

export class CloneWatcherError extends Error {
  override readonly name = "CloneWatcherError";
  readonly code: string;
  constructor(code: string, message: string) {
    super(`clone-watcher (${code}): ${message}`);
    this.code = code;
  }
}

export function createCloneWatcher(options: CreateWatcherOptions): CloneWatcher {
  const {
    projectId,
    layout,
    debounceMs = DEFAULT_DEBOUNCE_MS,
    clock = realClock,
    fsWatchImpl = watch,
    onEvent,
    onDiagnostic,
    retryDelayMs = WATCHER_RETRY_DELAY_MS,
  } = options;
  const probeImpl = options.probeImpl ?? probeDirtyState;
  const repoPath = cloneRepoPath(layout, projectId);

  let status: WatcherStatus = "created";
  let handle: FSWatcher | null = null;
  let retryTimer: unknown = null;
  let attempts = 0;

  const probeDebounced: DebouncedHandle = debounce(runProbe, debounceMs, clock);

  function emit(event: CloneWatcherEvent): void {
    emitWatcherEvent(projectId, event, onEvent, onDiagnostic);
  }

  function closeActiveHandle(phase: string): void {
    const active = handle;
    handle = null;
    if (active !== null) {
      closeWatcherHandle(projectId, active, phase, onDiagnostic);
    }
  }

  function runProbe(): void {
    let dirtyState: CloneDirtyState;
    try {
      dirtyState = probeImpl(probeInput(repoPath, options.runCommand));
    } catch (err) {
      emit({
        kind: "error",
        projectId,
        code: "probe_failed",
        message: (err as Error).message,
      });
      return;
    }
    try {
      updateCloneState(layout, projectId, (current) => ({ ...current, dirtyState }));
    } catch (err) {
      emit({
        kind: "error",
        projectId,
        code: "state_update_failed",
        message: (err as Error).message,
      });
      return;
    }
    emit({ kind: "dirty", projectId, state: dirtyState });
  }

  function attachWatcher(): void {
    try {
      handle = fsWatchImpl(repoPath, { recursive: true }, () => {
        probeDebounced.trigger();
      });
    } catch (err) {
      handleFsError(err as Error);
      return;
    }
    handle.on("error", (err) => {
      handleFsError(err);
    });
    status = "running";
    attempts = 0;
    emit({ kind: "started", projectId });
  }

  function handleFsError(err: Error): void {
    closeActiveHandle("fs_error");
    emit({ kind: "error", projectId, code: "fs_watch_failed", message: err.message });
    if (attempts >= 1) {
      // Already retried once; give up.
      status = "stopped";
      emit({ kind: "stopped", projectId, reason: "unrecoverable" });
      return;
    }
    attempts += 1;
    status = "error";
    retryTimer = clock.setTimeout(() => {
      retryTimer = null;
      if (status === "stopped") return; // explicit stop wins
      status = "starting";
      attachWatcher();
    }, retryDelayMs);
  }

  return {
    projectId,
    status: () => status,
    start() {
      if (status === "running" || status === "starting") return;
      status = "starting";
      attachWatcher();
    },
    stop() {
      if (status === "stopped") return;
      status = "stopping";
      probeDebounced.cancel();
      if (retryTimer !== null) {
        clock.clearTimeout(retryTimer);
        retryTimer = null;
      }
      closeActiveHandle("stop");
      status = "stopped";
      emit({ kind: "stopped", projectId, reason: "explicit" });
    },
    probeNow() {
      probeDebounced.cancel();
      runProbe();
    },
  };
}

function probeInput(
  repoPath: string,
  runCommand: ProbeDirtyStateInput["runCommand"] | undefined,
): ProbeDirtyStateInput {
  return {
    cloneRepoPath: repoPath,
    ...(runCommand ? { runCommand } : {}),
  };
}

function emitWatcherEvent(
  projectId: string,
  event: CloneWatcherEvent,
  listener: CloneWatcherListener | undefined,
  diagnostics: CloneWatcherDiagnosticSink | undefined,
): void {
  if (!listener) return;
  try {
    listener(event);
  } catch (err) {
    reportDiagnostic(projectId, diagnostics, {
      code: "listener_failed",
      phase: `emit:${event.kind}`,
      message: (err as Error).message,
    });
  }
}

function closeWatcherHandle(
  projectId: string,
  handle: FSWatcher,
  phase: string,
  diagnostics: CloneWatcherDiagnosticSink | undefined,
): void {
  try {
    handle.close();
  } catch (err) {
    reportDiagnostic(projectId, diagnostics, {
      code: "handle_close_failed",
      phase,
      message: (err as Error).message,
    });
  }
}

function reportDiagnostic(
  projectId: string,
  sink: CloneWatcherDiagnosticSink | undefined,
  diagnostic: Omit<CloneWatcherDiagnostic, "projectId">,
): void {
  if (!sink) return;
  sink({ projectId, ...diagnostic });
}
