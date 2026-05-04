// hp-fkov — HeartbeatTimer: periodic /v1/health probe wired to the
// orchestrator's `handleHeartbeatTimeout`.
//
// Engine-first slice 3 of hp-fkov. The orchestrator's connect pipeline
// already calls `heartbeat.check(...)` once during the connect
// transitions; this timer adds the *post-ready* periodic probe that
// the §10.5 SLO target depends on (a frozen socket from a Wi-Fi
// handoff or kernel suspend that the powerMonitor missed needs to
// surface within the timer's tick budget, not 2h+ later when TCP
// keepalive eventually fires).
//
// Design:
// - `start({ profile, localPort })` schedules the first probe + every
//   subsequent one. Calling `start` while already running rebinds the
//   probe to the new (profile, localPort) — used after reconnect when
//   the local port may have changed.
// - `stop()` cancels the next scheduled probe and ignores any in-flight
//   result (so a slow probe completing after stop never reports a stale
//   timeout into the orchestrator).
// - On `HeartbeatStatus === "version_mismatch"` we still call
//   `handleHeartbeatTimeout` with a distinct fault message — the
//   orchestrator's transition table treats it as the same recovery
//   class. A future slice can route version_mismatch through a
//   dedicated handler if the FSM grows one.
// - Audit fires on every tick boundary (`heartbeat.tick_started`,
//   `heartbeat.tick_ok`, `heartbeat.tick_failed`) so post-mortem can
//   reconstruct the probe history without grepping process logs.
//
// Out of scope (this slice):
// - FSM-state subscription that auto-starts/stops the timer on
//   `ready` / leave-`ready`. That binding lives in the orchestrator's
//   caller (BackendLifecycle production wiring), which knows how to
//   pump it from the snapshot stream — adding it here would couple
//   the timer to the FSM type and make it untestable in isolation.

import type { HeartbeatDriver, HeartbeatStatus } from "./orchestrator.ts";
import type { VpsProfile } from "./types.ts";

export interface ScheduledHandle {
  cancel(): void;
}

export interface IntervalScheduler {
  /** Schedule `callback` after `delayMs`. Returns a cancellable handle.
   *  Production wiring uses `setTimeout`; tests inject a fake clock. */
  schedule(delayMs: number, callback: () => void): ScheduledHandle;
}

export type HeartbeatTimerAuditEventKind =
  | "heartbeat.tick_started"
  | "heartbeat.tick_ok"
  | "heartbeat.tick_failed"
  | "heartbeat.timer_started"
  | "heartbeat.timer_stopped";

export interface HeartbeatTimerAuditEvent {
  readonly kind: HeartbeatTimerAuditEventKind;
  /** RFC3339 UTC timestamp. */
  readonly at: string;
  /** Free-form message — populated on tick_failed with the error
   *  message; populated on timer_started with the interval. */
  readonly message?: string;
}

export type HeartbeatTimerAuditSink = (event: HeartbeatTimerAuditEvent) => void;

export interface HeartbeatTimerOrchestrator {
  /** Mirrors the subset of TunnelOrchestrator the timer needs. Tests
   *  inject a recorder. */
  handleHeartbeatTimeout(message?: string): unknown;
}

export interface HeartbeatTimerOptions {
  readonly heartbeat: HeartbeatDriver;
  readonly orchestrator: HeartbeatTimerOrchestrator;
  readonly audit: HeartbeatTimerAuditSink;
  readonly scheduler?: IntervalScheduler;
  readonly intervalMs?: number;
  readonly now?: () => Date;
}

const DEFAULT_INTERVAL_MS = 30_000;

/** Periodic heartbeat timer. NOT auto-started — the caller drives the
 *  start/stop lifecycle from the FSM snapshot stream. */
export class HeartbeatTimer {
  readonly #heartbeat: HeartbeatDriver;
  readonly #orchestrator: HeartbeatTimerOrchestrator;
  readonly #audit: HeartbeatTimerAuditSink;
  readonly #scheduler: IntervalScheduler;
  readonly #intervalMs: number;
  readonly #now: () => Date;

  #binding: { profile: VpsProfile; localPort: number } | null = null;
  #pending: ScheduledHandle | null = null;
  /** Generation counter — incremented on every start/stop. An in-flight
   *  probe stamped with the previous generation discards its result on
   *  return so the orchestrator never sees a stale tick. */
  #generation = 0;

  constructor(options: HeartbeatTimerOptions) {
    this.#heartbeat = options.heartbeat;
    this.#orchestrator = options.orchestrator;
    this.#audit = options.audit;
    this.#scheduler = options.scheduler ?? defaultScheduler;
    this.#intervalMs = options.intervalMs ?? DEFAULT_INTERVAL_MS;
    this.#now = options.now ?? (() => new Date());
  }

  /** Bind to a (profile, localPort) and schedule the first probe.
   *  Calling while running rebinds — used after reconnect when the
   *  local port changes. */
  start(input: { readonly profile: VpsProfile; readonly localPort: number }): void {
    this.#cancelPending();
    this.#binding = { profile: input.profile, localPort: input.localPort };
    this.#generation += 1;
    this.#emit({
      kind: "heartbeat.timer_started",
      message: `every ${this.#intervalMs}ms (port ${input.localPort})`,
    });
    this.#scheduleNext();
  }

  /** Cancel the next scheduled probe + ignore any in-flight result. */
  stop(): void {
    if (this.#binding === null && this.#pending === null) return;
    this.#cancelPending();
    this.#binding = null;
    this.#generation += 1;
    this.#emit({ kind: "heartbeat.timer_stopped" });
  }

  /** Whether a probe is currently scheduled. Diagnostics + tests use
   *  this; production callers usually don't need it. */
  get running(): boolean {
    return this.#pending !== null;
  }

  #scheduleNext(): void {
    if (this.#binding === null) return;
    const generation = this.#generation;
    this.#pending = this.#scheduler.schedule(this.#intervalMs, () => {
      this.#pending = null;
      void this.#runTick(generation);
    });
  }

  async #runTick(generation: number): Promise<void> {
    if (generation !== this.#generation) return;
    const binding = this.#binding;
    if (!binding) return;

    this.#emit({ kind: "heartbeat.tick_started" });

    let status: HeartbeatStatus | null = null;
    let failure: unknown = null;
    try {
      status = await this.#heartbeat.check(binding);
    } catch (err) {
      failure = err;
    }

    // Discard if start/stop fired during the probe — prevents a stale
    // result from corrupting orchestrator state.
    if (generation !== this.#generation) return;

    if (failure !== null) {
      const message = errorMessage(failure);
      this.#emit({ kind: "heartbeat.tick_failed", message });
      try {
        this.#orchestrator.handleHeartbeatTimeout(message);
      } catch {
        // Defensive: orchestrator throws shouldn't break the timer.
      }
      // Don't reschedule on failure — the orchestrator's
      // handleHeartbeatTimeout transitions to reconnecting + resets
      // the connect pipeline; a fresh start() will land via the
      // caller's FSM-snapshot subscription.
      this.#binding = null;
      return;
    }

    if (status === "version_mismatch") {
      const message = "Daemon API version mismatch";
      this.#emit({ kind: "heartbeat.tick_failed", message });
      try {
        this.#orchestrator.handleHeartbeatTimeout(message);
      } catch {
        // Defensive.
      }
      this.#binding = null;
      return;
    }

    this.#emit({ kind: "heartbeat.tick_ok" });
    this.#scheduleNext();
  }

  #cancelPending(): void {
    this.#pending?.cancel();
    this.#pending = null;
  }

  #emit(event: Omit<HeartbeatTimerAuditEvent, "at">): void {
    const stamped: HeartbeatTimerAuditEvent = {
      at: this.#now().toISOString(),
      ...event,
    };
    try {
      this.#audit(stamped);
    } catch {
      // Defensive: audit sink throws cannot break the timer loop.
    }
  }
}

const defaultScheduler: IntervalScheduler = {
  schedule(delayMs, callback) {
    const handle = setTimeout(callback, delayMs);
    return { cancel: () => clearTimeout(handle) };
  },
};

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
