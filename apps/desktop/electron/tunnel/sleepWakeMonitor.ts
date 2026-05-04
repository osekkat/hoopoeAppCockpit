// hp-fkov — Sleep/wake → orchestrator wiring.
//
// Subscribes to Electron's `powerMonitor` 'suspend' / 'resume' events
// and drives the corresponding TunnelOrchestrator transitions. The
// kernel may freeze sockets without closing them when the laptop
// suspends; relying on TCP keepalive to notice can take 2h+ on macOS,
// long after the user has reopened the lid and started typing. Hooking
// the OS-level signal closes that gap.
//
// Design: a single factory `installSleepWakeMonitor(opts)` that wires
// the listeners and returns an `uninstall()` handle. Inputs are
// injected so tests can drive the lifecycle with a fake powerMonitor
// without touching Electron — the production caller passes
// `electron.powerMonitor` directly. Audit fires on every sleep/wake
// transition regardless of orchestrator outcome (Guardrail 10) so
// post-mortem can correlate the gap window.
//
// This file is the side-effect layer ONLY. The handler logic itself
// (closing the tunnel on sleep, reconnecting on wake) lives in
// TunnelOrchestrator.handleSystemSleep / handleSystemWake (orchestrator.ts).

export type PowerMonitorEvent = "suspend" | "resume" | "lock-screen" | "unlock-screen";

/** Subset of Electron's `powerMonitor` we depend on. Tests inject a fake
 *  that wraps a Node EventEmitter; production passes
 *  `electron.powerMonitor` from `apps/desktop/src/main/`. */
export interface PowerMonitorLike {
  on(event: PowerMonitorEvent, handler: () => void): void;
  off?(event: PowerMonitorEvent, handler: () => void): void;
  removeListener?(event: PowerMonitorEvent, handler: () => void): void;
}

/** Minimal subset of the orchestrator API we need. The full type is
 *  exported from `./orchestrator.ts` but accepting a structural subset
 *  keeps tests free of TunnelOrchestrator construction. */
export interface SleepWakeOrchestrator {
  handleSystemSleep(): Promise<unknown>;
  handleSystemWake(): Promise<unknown>;
}

export type SleepWakeAuditEventKind =
  | "tunnel.sleep_observed"
  | "tunnel.wake_observed"
  | "tunnel.sleep_handler_failed"
  | "tunnel.wake_handler_failed";

export interface SleepWakeAuditEvent {
  readonly kind: SleepWakeAuditEventKind;
  /** RFC3339 timestamp (UTC). */
  readonly at: string;
  /** Free-form message — captures handler error text on `*_failed` events
   *  so post-mortem doesn't need to chase a separate log. */
  readonly message?: string;
}

export type SleepWakeAuditSink = (event: SleepWakeAuditEvent) => void;

export interface SleepWakeMonitorOptions {
  /** Electron's `powerMonitor` (or a fake in tests). */
  readonly powerMonitor: PowerMonitorLike;
  /** Orchestrator the monitor drives. */
  readonly orchestrator: SleepWakeOrchestrator;
  /** Audit sink. Called on every observed transition + every handler
   *  failure so the audit trail is complete (Guardrail 10). Required
   *  in production; tests inject a recorder. */
  readonly audit: SleepWakeAuditSink;
  /** Wall-clock injection (tests). Defaults to `() => new Date()`. */
  readonly now?: () => Date;
  /** Optional non-fatal logger for handler failures — main process
   *  wires this to the structured logger so a wake-handler crash
   *  surfaces in Diagnostics rather than silently swallowing. The
   *  audit always fires regardless of whether this is set. */
  readonly logFailure?: (kind: "sleep" | "wake", err: unknown) => void;
}

export interface SleepWakeMonitorHandle {
  /** Detach the powerMonitor listeners. Idempotent. */
  uninstall(): void;
}

/** Install the listeners. Returns a handle whose `uninstall()` is
 *  idempotent — calling it twice is safe (the second call is a
 *  no-op). Production wiring stores the handle in BackendLifecycle so
 *  app teardown can detach cleanly. */
export function installSleepWakeMonitor(
  opts: SleepWakeMonitorOptions,
): SleepWakeMonitorHandle {
  const { powerMonitor, orchestrator, audit, logFailure } = opts;
  const now = opts.now ?? (() => new Date());

  const stamp = (): string => now().toISOString();

  const onSuspend = () => {
    audit({ kind: "tunnel.sleep_observed", at: stamp() });
    Promise.resolve()
      .then(() => orchestrator.handleSystemSleep())
      .catch((err: unknown) => {
        audit({
          kind: "tunnel.sleep_handler_failed",
          at: stamp(),
          message: errorMessage(err),
        });
        logFailure?.("sleep", err);
      });
  };

  const onResume = () => {
    audit({ kind: "tunnel.wake_observed", at: stamp() });
    Promise.resolve()
      .then(() => orchestrator.handleSystemWake())
      .catch((err: unknown) => {
        audit({
          kind: "tunnel.wake_handler_failed",
          at: stamp(),
          message: errorMessage(err),
        });
        logFailure?.("wake", err);
      });
  };

  powerMonitor.on("suspend", onSuspend);
  powerMonitor.on("resume", onResume);

  let detached = false;
  return {
    uninstall() {
      if (detached) return;
      detached = true;
      detach(powerMonitor, "suspend", onSuspend);
      detach(powerMonitor, "resume", onResume);
    },
  };
}

function detach(
  pm: PowerMonitorLike,
  event: PowerMonitorEvent,
  handler: () => void,
): void {
  // Electron's powerMonitor exposes both `off` and `removeListener`. Tests
  // may stub only one; production has both. Try `off` first because that's
  // the canonical Node EventEmitter name; fall back to `removeListener`.
  if (typeof pm.off === "function") {
    pm.off(event, handler);
    return;
  }
  if (typeof pm.removeListener === "function") {
    pm.removeListener(event, handler);
  }
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
