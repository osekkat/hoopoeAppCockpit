// hp-fkov — CompositeHeartbeatDriver: folds /v1/health + /v1/version
// into the single HeartbeatStatus the TunnelOrchestrator consumes.
//
// Engine-first slice 5 of hp-fkov. The orchestrator's connect pipeline
// expects exactly one HeartbeatDriver. Slices 2 + 4 ship two narrow
// drivers — the heartbeat (cheap, frequent, "is the daemon alive?")
// and the version probe (coarser, "is the schema what we expect?").
// This composite is the production wiring that calls them in the
// right order and translates their results into the orchestrator's
// existing "ok" / "version_mismatch" vocabulary.
//
// Order matters: heartbeat first.
//   - If the daemon process can't even serve /v1/health, the version
//     probe will fail too. Returning the heartbeat error is cheaper
//     and more accurate than letting both probes fire and surfacing
//     a confusing "version unreachable" message when the real fault
//     is the daemon being down.
//   - If heartbeat passes, version answers the schema-compatibility
//     question. A version probe failure is treated identically to a
//     heartbeat-network failure (rethrown) — better to reconnect than
//     to silently degrade the FSM on a transient version-probe blip.
//
// Periodicity: the orchestrator currently calls `heartbeat.check(...)`
// once during the connect transition (see orchestrator.ts:155). The
// composite reuses the same call site — no new wiring required. The
// follow-up periodic timer (slice 3) wraps any HeartbeatDriver
// uniformly, so this composite drops in there too.

import type { HeartbeatDriver, HeartbeatStatus } from "./orchestrator.ts";
import type { VpsProfile } from "./types.ts";

/** Subset of HttpHeartbeatDriver we need — declared structurally so
 *  this file doesn't import `httpHeartbeatDriver.ts` directly (avoids
 *  a circular type import when the index re-exports both). */
export interface HealthProbe {
  check(input: { readonly profile: VpsProfile; readonly localPort: number }): Promise<HeartbeatStatus>;
}

/** Subset of HttpVersionDriver we need — same structural decoupling. */
export interface VersionProbe {
  check(input: { readonly profile: VpsProfile; readonly localPort: number }): Promise<{
    readonly compatibility: "compatible" | "version_mismatch";
    readonly reportedSchemaVersion: number;
  }>;
}

export interface CompositeHeartbeatAuditEvent {
  readonly kind:
    | "heartbeat.composite.health_failed"
    | "heartbeat.composite.version_failed"
    | "heartbeat.composite.version_mismatch_detected"
    | "heartbeat.composite.ok";
  readonly at: string;
  readonly message?: string;
  /** Populated on `version_mismatch_detected` so audit captures
   *  exactly which version was reported when the gate fired. */
  readonly reportedSchemaVersion?: number;
}

export type CompositeHeartbeatAuditSink = (event: CompositeHeartbeatAuditEvent) => void;

export interface CompositeHeartbeatDriverOptions {
  readonly health: HealthProbe;
  readonly version: VersionProbe;
  /** Audit sink. Required — calls fire on every probe outcome (ok,
   *  health-failed, version-failed, version-mismatch) so post-mortem
   *  can correlate composite verdicts with single-probe traces. */
  readonly audit: CompositeHeartbeatAuditSink;
  readonly now?: () => Date;
}

export class CompositeHeartbeatDriver implements HeartbeatDriver {
  readonly #health: HealthProbe;
  readonly #version: VersionProbe;
  readonly #audit: CompositeHeartbeatAuditSink;
  readonly #now: () => Date;

  constructor(options: CompositeHeartbeatDriverOptions) {
    this.#health = options.health;
    this.#version = options.version;
    this.#audit = options.audit;
    this.#now = options.now ?? (() => new Date());
  }

  async check(input: { readonly profile: VpsProfile; readonly localPort: number }): Promise<HeartbeatStatus> {
    // Phase 1: health probe — must succeed before we even ask about
    // the schema version.
    let healthStatus: HeartbeatStatus;
    try {
      healthStatus = await this.#health.check(input);
    } catch (err) {
      this.#emit({
        kind: "heartbeat.composite.health_failed",
        message: errorMessage(err),
      });
      throw err;
    }

    // Defensive: if a future health probe ever returns
    // version_mismatch (it shouldn't — that's our composite's job),
    // we surface it without re-asking the version probe. This keeps
    // the composite forward-compatible with a future evolution of
    // HeartbeatStatus.
    if (healthStatus === "version_mismatch") {
      this.#emit({
        kind: "heartbeat.composite.version_mismatch_detected",
        message: "health probe reported version_mismatch directly",
      });
      return "version_mismatch";
    }

    // Phase 2: version probe.
    let versionResult: Awaited<ReturnType<VersionProbe["check"]>>;
    try {
      versionResult = await this.#version.check(input);
    } catch (err) {
      this.#emit({
        kind: "heartbeat.composite.version_failed",
        message: errorMessage(err),
      });
      // Re-throw rather than swallow: a transient version-probe blip
      // is treated as a heartbeat failure so the orchestrator
      // reconnects instead of silently masking the issue.
      throw err;
    }

    if (versionResult.compatibility === "version_mismatch") {
      this.#emit({
        kind: "heartbeat.composite.version_mismatch_detected",
        message: `daemon reports schemaVersion ${versionResult.reportedSchemaVersion}; not in accepted set`,
        reportedSchemaVersion: versionResult.reportedSchemaVersion,
      });
      return "version_mismatch";
    }

    this.#emit({ kind: "heartbeat.composite.ok" });
    return "ok";
  }

  #emit(event: Omit<CompositeHeartbeatAuditEvent, "at">): void {
    const stamped: CompositeHeartbeatAuditEvent = {
      at: this.#now().toISOString(),
      ...event,
    };
    try {
      this.#audit(stamped);
    } catch {
      // Defensive: audit-sink throws cannot break the probe loop.
    }
  }
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
