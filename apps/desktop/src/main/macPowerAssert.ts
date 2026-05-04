import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { PRELOAD_IPC_CHANNELS } from "../shared/ipc-contract.ts";
import type { IpcRegistry } from "./IpcRegistry.ts";

export type PowerAssertionLevel = "display" | "app-suspension" | "system";
export type PowerAssertionMechanism = "powersaveblocker" | "nsprocessinfo" | "caffeinate";
export type PowerAssertionReleaseReason =
  | "round_complete"
  | "round_failed"
  | "round_cancelled"
  | "watchdog_force_release"
  | "user_disabled"
  | "shutdown";

export interface PowerSaveBlockerLike {
  start(type: "prevent-display-sleep" | "prevent-app-suspension"): number;
  stop(id: number): void;
}

export interface NativeActivityBridge {
  beginActivity(input: {
    readonly level: PowerAssertionLevel;
    readonly reason: string;
  }): string | number;
  endActivity(token: string | number): void;
}

export interface CaffeinateProcess {
  readonly pid?: number | undefined;
  kill(signal?: NodeJS.Signals | number): boolean;
}

export interface CaffeinateSpawner {
  spawn(input: {
    readonly pid: number;
    readonly level: PowerAssertionLevel;
    readonly reason: string;
  }): CaffeinateProcess;
}

export interface BatterySnapshot {
  readonly powerSource: "ac" | "battery" | "unknown";
  readonly levelPercent: number | null;
  readonly onBattery: boolean;
}

export interface BatteryStatusProvider {
  current(): BatterySnapshot;
}

export interface PowerAssertionAuditEvent {
  readonly kind: "pro-round.power_acquire" | "pro-round.power_release" | "pro-round.power_warning";
  readonly at: string;
  readonly roundId?: string;
  readonly assertionId?: string;
  readonly level?: PowerAssertionLevel;
  readonly mechanism?: PowerAssertionMechanism;
  readonly reason?: string;
  readonly heldMs?: number;
  readonly heldCount?: number;
  readonly residualHandlesAfterRelease?: number;
  readonly warningKind?:
    | "fallback_used"
    | "double_release"
    | "silence_threshold"
    | "battery_low_downgrade"
    | "leak_detected";
  readonly message?: string;
}

export type PowerAssertionAuditSink = (event: PowerAssertionAuditEvent) => void;

export interface PowerAssertionAcquireInput {
  readonly roundId: string;
  readonly modelId?: string;
  readonly oracleTopology?: "mac" | "vps";
  readonly estimatedDurationMs?: number;
  readonly reason?: string;
}

export interface PowerAssertionSnapshot {
  readonly active: boolean;
  readonly assertionId: string | null;
  readonly mechanism: PowerAssertionMechanism | null;
  readonly level: PowerAssertionLevel | null;
  readonly ownerRoundIds: readonly string[];
  readonly heldCount: number;
  readonly acquiredAt: string | null;
}

export interface PowerAssertionHandle {
  readonly assertionId: string;
  readonly roundId: string;
  snapshot(): PowerAssertionSnapshot;
  release(reason?: PowerAssertionReleaseReason): PowerAssertionSnapshot;
}

export interface PowerAssertionManagerOptions {
  readonly powerSaveBlocker?: PowerSaveBlockerLike;
  readonly nativeActivity?: NativeActivityBridge;
  readonly caffeinate?: CaffeinateSpawner;
  readonly battery?: BatteryStatusProvider;
  readonly audit?: PowerAssertionAuditSink;
  readonly now?: () => Date;
  readonly pid?: number;
  readonly idFactory?: () => string;
  readonly watchdogWarnAfterMs?: number;
  readonly watchdogForceReleaseAfterMs?: number;
  readonly disabled?: boolean;
}

interface Lease {
  readonly assertionId: string;
  readonly roundId: string;
  readonly acquiredAt: Date;
  lastActivityAt: Date;
  releasedAt: Date | null;
  warnedForSilence: boolean;
}

interface ActiveMechanism {
  readonly mechanism: PowerAssertionMechanism;
  readonly level: PowerAssertionLevel;
  readonly acquiredAt: Date;
  readonly powerSaveBlockerId?: number;
  readonly nativeToken?: string | number;
  readonly caffeinateProcess?: CaffeinateProcess;
}

const DEFAULT_WATCHDOG_WARN_AFTER_MS = 180_000;
const DEFAULT_WATCHDOG_FORCE_RELEASE_AFTER_MS = 600_000;
const LONG_ROUND_THRESHOLD_MS = 5 * 60_000;

export class PowerAssertionError extends Error {
  readonly code: "disabled" | "invalid_input" | "no_mechanism" | "unknown_assertion";
  constructor(code: PowerAssertionError["code"], message: string) {
    super(message);
    this.name = "PowerAssertionError";
    this.code = code;
  }
}

export class PowerAssertionManager {
  readonly #powerSaveBlocker: PowerSaveBlockerLike | undefined;
  readonly #nativeActivity: NativeActivityBridge | undefined;
  readonly #caffeinate: CaffeinateSpawner | undefined;
  readonly #battery: BatteryStatusProvider | undefined;
  readonly #audit: PowerAssertionAuditSink;
  readonly #now: () => Date;
  readonly #pid: number;
  readonly #idFactory: () => string;
  readonly #watchdogWarnAfterMs: number;
  readonly #watchdogForceReleaseAfterMs: number;
  #disabled: boolean;

  #active: ActiveMechanism | null = null;
  #leases = new Map<string, Lease>();

  constructor(options: PowerAssertionManagerOptions = {}) {
    this.#powerSaveBlocker = options.powerSaveBlocker;
    this.#nativeActivity = options.nativeActivity;
    this.#caffeinate = options.caffeinate ?? createCaffeinateSpawner();
    this.#battery = options.battery;
    this.#audit = options.audit ?? (() => undefined);
    this.#now = options.now ?? (() => new Date());
    this.#pid = options.pid ?? process.pid;
    this.#idFactory = options.idFactory ?? (() => cryptoRandomId());
    this.#watchdogWarnAfterMs = options.watchdogWarnAfterMs ?? DEFAULT_WATCHDOG_WARN_AFTER_MS;
    this.#watchdogForceReleaseAfterMs =
      options.watchdogForceReleaseAfterMs ?? DEFAULT_WATCHDOG_FORCE_RELEASE_AFTER_MS;
    this.#disabled = options.disabled ?? false;
  }

  acquire(input: PowerAssertionAcquireInput): PowerAssertionHandle {
    const normalized = normalizeAcquireInput(input);
    if (this.#disabled) {
      this.#emit({
        kind: "pro-round.power_warning",
        roundId: normalized.roundId,
        warningKind: "leak_detected",
        message: "power assertions are disabled by settings",
      });
      throw new PowerAssertionError("disabled", "power assertions are disabled");
    }

    const now = this.#now();
    const assertionId = this.#idFactory();
    const requestedLevel = this.#requestedLevel(normalized);
    const active = this.#active;
    if (active === null) {
      this.#active = this.#startMechanism(requestedLevel, normalized);
    } else if (levelRank(requestedLevel) > levelRank(active.level)) {
      this.#stopMechanism(active);
      this.#active = this.#startMechanism(requestedLevel, normalized);
    }

    const lease: Lease = {
      assertionId,
      roundId: normalized.roundId,
      acquiredAt: now,
      lastActivityAt: now,
      releasedAt: null,
      warnedForSilence: false,
    };
    this.#leases.set(assertionId, lease);

    const activeAfterAcquire = this.#active;
    if (activeAfterAcquire === null) {
      throw new PowerAssertionError("no_mechanism", "power assertion was not acquired");
    }
    this.#emit({
      kind: "pro-round.power_acquire",
      roundId: lease.roundId,
      assertionId,
      level: activeAfterAcquire.level,
      mechanism: activeAfterAcquire.mechanism,
      reason: normalized.reason,
      heldCount: this.#heldLeases().length,
    });

    return {
      assertionId,
      roundId: lease.roundId,
      snapshot: () => this.snapshot(),
      release: (reason = "round_complete") => this.release(assertionId, reason),
    };
  }

  release(assertionId: string, reason: PowerAssertionReleaseReason = "round_complete"): PowerAssertionSnapshot {
    const lease = this.#leases.get(assertionId);
    if (!lease) {
      throw new PowerAssertionError("unknown_assertion", `unknown power assertion ${assertionId}`);
    }
    if (lease.releasedAt !== null) {
      this.#emit({
        kind: "pro-round.power_warning",
        roundId: lease.roundId,
        assertionId,
        warningKind: "double_release",
        message: "release called for an already released assertion",
      });
      return this.snapshot();
    }

    const now = this.#now();
    lease.releasedAt = now;
    const remaining = this.#heldLeases().length;
    const releaseEvent: Omit<PowerAssertionAuditEvent, "at"> = {
      kind: "pro-round.power_release",
      roundId: lease.roundId,
      assertionId,
      reason,
      heldMs: Math.max(0, now.getTime() - lease.acquiredAt.getTime()),
      residualHandlesAfterRelease: remaining,
      ...(this.#active ? { mechanism: this.#active.mechanism, level: this.#active.level } : {}),
    };
    this.#emit(releaseEvent);

    if (remaining === 0 && this.#active !== null) {
      this.#stopMechanism(this.#active);
      this.#active = null;
    }
    return this.snapshot();
  }

  setDisabled(disabled: boolean): PowerAssertionSnapshot {
    if (this.#disabled === disabled) {
      return this.snapshot();
    }
    this.#disabled = disabled;
    if (disabled) {
      for (const lease of this.#heldLeases()) {
        this.release(lease.assertionId, "user_disabled");
      }
    }
    return this.snapshot();
  }

  recordRoundActivity(roundId: string): void {
    const now = this.#now();
    for (const lease of this.#heldLeases()) {
      if (lease.roundId === roundId) {
        lease.lastActivityAt = now;
        lease.warnedForSilence = false;
      }
    }
  }

  checkWatchdog(): readonly PowerAssertionAuditEvent[] {
    const emitted: PowerAssertionAuditEvent[] = [];
    const now = this.#now();
    for (const lease of this.#heldLeases()) {
      const silentMs = now.getTime() - lease.lastActivityAt.getTime();
      if (silentMs >= this.#watchdogForceReleaseAfterMs) {
        this.release(lease.assertionId, "watchdog_force_release");
        continue;
      }
      if (silentMs >= this.#watchdogWarnAfterMs && !lease.warnedForSilence) {
        lease.warnedForSilence = true;
        const event = this.#emit({
          kind: "pro-round.power_warning",
          roundId: lease.roundId,
          assertionId: lease.assertionId,
          warningKind: "silence_threshold",
          message: `no Pro round activity for ${silentMs}ms`,
        });
        emitted.push(event);
      }
    }
    return emitted;
  }

  checkIdleLeak(activeRoundIds: readonly string[]): readonly PowerAssertionAuditEvent[] {
    const active = new Set(activeRoundIds);
    const out: PowerAssertionAuditEvent[] = [];
    for (const lease of this.#heldLeases()) {
      if (active.has(lease.roundId)) continue;
      out.push(this.#emit({
        kind: "pro-round.power_warning",
        roundId: lease.roundId,
        assertionId: lease.assertionId,
        warningKind: "leak_detected",
        message: "power assertion held for a round that is no longer active",
      }));
    }
    return out;
  }

  snapshot(): PowerAssertionSnapshot {
    const leases = this.#heldLeases();
    return {
      active: this.#active !== null && leases.length > 0,
      assertionId: leases[0]?.assertionId ?? null,
      mechanism: this.#active?.mechanism ?? null,
      level: this.#active?.level ?? null,
      ownerRoundIds: [...new Set(leases.map((lease) => lease.roundId))].sort(),
      heldCount: leases.length,
      acquiredAt: this.#active?.acquiredAt.toISOString() ?? null,
    };
  }

  shutdown(): void {
    for (const lease of this.#heldLeases()) {
      this.release(lease.assertionId, "shutdown");
    }
  }

  #requestedLevel(input: Required<PowerAssertionAcquireInput>): PowerAssertionLevel {
    const battery = this.#battery?.current();
    if (battery?.onBattery === true && battery.levelPercent !== null && battery.levelPercent < 20) {
      this.#emit({
        kind: "pro-round.power_warning",
        roundId: input.roundId,
        warningKind: "battery_low_downgrade",
        message: "battery below 20%; downgrading to display-only assertion",
      });
      return "display";
    }
    if (input.estimatedDurationMs > 0 && input.estimatedDurationMs < LONG_ROUND_THRESHOLD_MS) {
      return "display";
    }
    return "app-suspension";
  }

  #startMechanism(
    level: PowerAssertionLevel,
    input: Required<PowerAssertionAcquireInput>,
  ): ActiveMechanism {
    const acquiredAt = this.#now();
    const blockerType = level === "display" ? "prevent-display-sleep" : "prevent-app-suspension";
    if (this.#powerSaveBlocker) {
      try {
        const powerSaveBlockerId = this.#powerSaveBlocker.start(blockerType);
        return { mechanism: "powersaveblocker", level, acquiredAt, powerSaveBlockerId };
      } catch (err) {
        this.#emitFallbackWarning(input.roundId, "powersaveblocker", err);
      }
    }
    if (this.#nativeActivity) {
      try {
        const nativeToken = this.#nativeActivity.beginActivity({ level, reason: input.reason });
        return { mechanism: "nsprocessinfo", level, acquiredAt, nativeToken };
      } catch (err) {
        this.#emitFallbackWarning(input.roundId, "nsprocessinfo", err);
      }
    }
    if (this.#caffeinate) {
      try {
        const caffeinateProcess = this.#caffeinate.spawn({
          pid: this.#pid,
          level,
          reason: input.reason,
        });
        return { mechanism: "caffeinate", level, acquiredAt, caffeinateProcess };
      } catch (err) {
        this.#emitFallbackWarning(input.roundId, "caffeinate", err);
      }
    }
    throw new PowerAssertionError("no_mechanism", "no power assertion mechanism is available");
  }

  #stopMechanism(active: ActiveMechanism): void {
    if (active.mechanism === "powersaveblocker") {
      if (active.powerSaveBlockerId !== undefined) {
        this.#powerSaveBlocker?.stop(active.powerSaveBlockerId);
      }
      return;
    }
    if (active.mechanism === "nsprocessinfo") {
      if (active.nativeToken !== undefined) {
        this.#nativeActivity?.endActivity(active.nativeToken);
      }
      return;
    }
    active.caffeinateProcess?.kill("SIGTERM");
  }

  #heldLeases(): Lease[] {
    return [...this.#leases.values()].filter((lease) => lease.releasedAt === null);
  }

  #emitFallbackWarning(roundId: string, mechanism: PowerAssertionMechanism, err: unknown): void {
    this.#emit({
      kind: "pro-round.power_warning",
      roundId,
      warningKind: "fallback_used",
      mechanism,
      message: errorMessage(err),
    });
  }

  #emit(event: Omit<PowerAssertionAuditEvent, "at">): PowerAssertionAuditEvent {
    const stamped: PowerAssertionAuditEvent = {
      at: this.#now().toISOString(),
      ...event,
    };
    try {
      this.#audit(stamped);
    } catch {
      // Audit/log sinks must never break assertion lifecycle cleanup.
    }
    return stamped;
  }
}

export interface PowerAssertionIpcRegistration {
  unregister(): void;
}

export function registerPowerAssertionIpc(
  registry: IpcRegistry,
  manager: PowerAssertionManager,
): readonly PowerAssertionIpcRegistration[] {
  return [
    registry.register<PowerAssertionAcquireInput, PowerAssertionSnapshot>({
      id: PRELOAD_IPC_CHANNELS.powerAcquire,
      handler: {
        handle: (input) => manager.acquire(assertAcquireInput(input)).snapshot(),
      },
    }),
    registry.register<{ readonly assertionId: string; readonly reason?: PowerAssertionReleaseReason }, PowerAssertionSnapshot>({
      id: PRELOAD_IPC_CHANNELS.powerRelease,
      handler: {
        handle: (input) => manager.release(assertReleaseInput(input).assertionId, input.reason),
      },
    }),
    registry.register<Record<string, never>, PowerAssertionSnapshot>({
      id: PRELOAD_IPC_CHANNELS.powerSnapshot,
      handler: {
        handle: () => manager.snapshot(),
      },
    }),
  ];
}

function normalizeAcquireInput(input: PowerAssertionAcquireInput): Required<PowerAssertionAcquireInput> {
  return {
    roundId: cleanIdentifier(input.roundId, "roundId"),
    modelId: input.modelId ? cleanIdentifier(input.modelId, "modelId") : "gpt-5.4-pro",
    oracleTopology: input.oracleTopology ?? "mac",
    estimatedDurationMs: input.estimatedDurationMs ?? LONG_ROUND_THRESHOLD_MS,
    reason: input.reason?.trim() || "ChatGPT Pro Oracle round in progress",
  };
}

function assertAcquireInput(input: PowerAssertionAcquireInput): PowerAssertionAcquireInput {
  normalizeAcquireInput(input);
  return input;
}

function assertReleaseInput(input: {
  readonly assertionId: string;
  readonly reason?: PowerAssertionReleaseReason;
}): { readonly assertionId: string; readonly reason?: PowerAssertionReleaseReason } {
  cleanIdentifier(input.assertionId, "assertionId");
  if (input.reason !== undefined && !POWER_ASSERTION_RELEASE_REASONS.has(input.reason)) {
    throw new PowerAssertionError("invalid_input", "invalid release reason");
  }
  return input;
}

function cleanIdentifier(value: string, field: string): string {
  const trimmed = value.trim();
  if (!/^[a-zA-Z0-9_.:-]{1,160}$/.test(trimmed)) {
    throw new PowerAssertionError("invalid_input", `invalid ${field}`);
  }
  return trimmed;
}

function levelRank(level: PowerAssertionLevel): number {
  return POWER_ASSERTION_LEVEL_RANK[level];
}

const POWER_ASSERTION_LEVEL_RANK: Record<PowerAssertionLevel, number> = {
  display: 1,
  "app-suspension": 2,
  system: 3,
};

const POWER_ASSERTION_RELEASE_REASONS: ReadonlySet<PowerAssertionReleaseReason> = new Set([
  "round_complete",
  "round_failed",
  "round_cancelled",
  "watchdog_force_release",
  "user_disabled",
  "shutdown",
]);

function createCaffeinateSpawner(): CaffeinateSpawner {
  return {
    spawn(input) {
      const args = input.level === "display"
        ? ["-d", "-w", String(input.pid)]
        : ["-dimsu", "-w", String(input.pid)];
      const child = spawn("/usr/bin/caffeinate", args, {
        detached: false,
        stdio: "ignore",
      });
      child.unref();
      return child;
    },
  };
}

function cryptoRandomId(): string {
  return `pa_${randomUUID()}`;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
