import { spawn } from "node:child_process";
import { randomUUID } from "node:crypto";
import { PRELOAD_IPC_CHANNELS } from "../shared/ipc-contract.ts";
import type { IpcRegistry, IpcValueValidator } from "./IpcRegistry.ts";

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

interface SpawnedProcess extends CaffeinateProcess {
  unref?(): void;
}

type NativeHelperSpawner = (
  command: string,
  args: string[],
  options: { detached: false; stdio: "ignore" },
) => SpawnedProcess;

export interface NSProcessInfoBridgeOptions {
  readonly platform?: NodeJS.Platform;
  readonly helperPath?: string;
  readonly spawn?: NativeHelperSpawner;
  readonly idFactory?: () => string;
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

  release(
    assertionId: string,
    reason: PowerAssertionReleaseReason = "round_complete",
  ): PowerAssertionSnapshot {
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
      out.push(
        this.#emit({
          kind: "pro-round.power_warning",
          roundId: lease.roundId,
          assertionId: lease.assertionId,
          warningKind: "leak_detected",
          message: "power assertion held for a round that is no longer active",
        }),
      );
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
      validateInput: assertAcquireInput,
      validateOutput: assertPowerAssertionSnapshot,
      handler: {
        handle: (input) => manager.acquire(input).snapshot(),
      },
    }),
    registry.register<
      { readonly assertionId: string; readonly reason?: PowerAssertionReleaseReason },
      PowerAssertionSnapshot
    >({
      id: PRELOAD_IPC_CHANNELS.powerRelease,
      validateInput: assertReleaseInput,
      validateOutput: assertPowerAssertionSnapshot,
      handler: {
        handle: (input) => manager.release(input.assertionId, input.reason),
      },
    }),
    registry.register<Record<string, never>, PowerAssertionSnapshot>({
      id: PRELOAD_IPC_CHANNELS.powerSnapshot,
      validateInput: assertEmptyObject,
      validateOutput: assertPowerAssertionSnapshot,
      handler: {
        handle: () => manager.snapshot(),
      },
    }),
  ];
}

export function createNSProcessInfoBridge(
  options: NSProcessInfoBridgeOptions = {},
): NativeActivityBridge | undefined {
  if ((options.platform ?? process.platform) !== "darwin") {
    return undefined;
  }
  const helperPath = options.helperPath ?? "/usr/bin/osascript";
  const idFactory = options.idFactory ?? (() => cryptoRandomId());
  const spawnImpl: NativeHelperSpawner =
    options.spawn ?? ((command, args, spawnOptions) => spawn(command, args, spawnOptions));
  const helpers = new Map<string, SpawnedProcess>();
  return {
    beginActivity(input) {
      const token = idFactory();
      const helper = spawnImpl(
        helperPath,
        ["-l", "JavaScript", "-e", nsProcessInfoActivityScript(input.level, input.reason)],
        { detached: false, stdio: "ignore" },
      );
      helper.unref?.();
      helpers.set(token, helper);
      return token;
    },
    endActivity(token) {
      const key = String(token);
      const helper = helpers.get(key);
      if (!helper) {
        return;
      }
      helpers.delete(key);
      helper.kill("SIGTERM");
    },
  };
}

function normalizeAcquireInput(
  input: PowerAssertionAcquireInput,
): Required<PowerAssertionAcquireInput> {
  return {
    roundId: cleanIdentifier(input.roundId, "roundId"),
    modelId: input.modelId ? cleanIdentifier(input.modelId, "modelId") : "gpt-5.4-pro",
    oracleTopology: input.oracleTopology ?? "mac",
    estimatedDurationMs: input.estimatedDurationMs ?? LONG_ROUND_THRESHOLD_MS,
    reason: input.reason?.trim() || "ChatGPT Pro Oracle round in progress",
  };
}

const POWER_ASSERTION_MECHANISMS: ReadonlySet<PowerAssertionMechanism> = new Set([
  "powersaveblocker",
  "nsprocessinfo",
  "caffeinate",
]);

const POWER_ASSERTION_LEVELS: ReadonlySet<PowerAssertionLevel> = new Set([
  "display",
  "app-suspension",
  "system",
]);

const assertAcquireInput: IpcValueValidator<PowerAssertionAcquireInput> = (
  input,
) => {
  if (!isRecord(input)) {
    throw new PowerAssertionError("invalid_input", "acquire input must be an object");
  }
  if (typeof input.roundId !== "string") {
    throw new PowerAssertionError("invalid_input", "roundId is required");
  }
  const out: {
    roundId: string;
    modelId?: string;
    oracleTopology?: "mac" | "vps";
    estimatedDurationMs?: number;
    reason?: string;
  } = {
    roundId: input.roundId,
  };
  if (input.modelId !== undefined) {
    if (typeof input.modelId !== "string") {
      throw new PowerAssertionError("invalid_input", "modelId must be a string");
    }
    out.modelId = input.modelId;
  }
  if (input.oracleTopology !== undefined) {
    if (input.oracleTopology !== "mac" && input.oracleTopology !== "vps") {
      throw new PowerAssertionError("invalid_input", "invalid oracleTopology");
    }
    out.oracleTopology = input.oracleTopology;
  }
  if (input.estimatedDurationMs !== undefined) {
    if (
      typeof input.estimatedDurationMs !== "number" ||
      !Number.isFinite(input.estimatedDurationMs) ||
      input.estimatedDurationMs < 0
    ) {
      throw new PowerAssertionError("invalid_input", "estimatedDurationMs must be a non-negative finite number");
    }
    out.estimatedDurationMs = input.estimatedDurationMs;
  }
  if (input.reason !== undefined) {
    if (typeof input.reason !== "string") {
      throw new PowerAssertionError("invalid_input", "reason must be a string");
    }
    out.reason = input.reason;
  }
  normalizeAcquireInput(out);
  return out;
};

const assertReleaseInput: IpcValueValidator<{
  readonly assertionId: string;
  readonly reason?: PowerAssertionReleaseReason;
}> = (input) => {
  if (!isRecord(input)) {
    throw new PowerAssertionError("invalid_input", "release input must be an object");
  }
  if (typeof input.assertionId !== "string") {
    throw new PowerAssertionError("invalid_input", "assertionId is required");
  }
  const out: { assertionId: string; reason?: PowerAssertionReleaseReason } = {
    assertionId: input.assertionId,
  };
  if (input.reason !== undefined) {
    if (
      typeof input.reason !== "string" ||
      !POWER_ASSERTION_RELEASE_REASONS.has(input.reason as PowerAssertionReleaseReason)
    ) {
      throw new PowerAssertionError("invalid_input", "invalid release reason");
    }
    out.reason = input.reason as PowerAssertionReleaseReason;
  }
  cleanIdentifier(out.assertionId, "assertionId");
  return out;
};

const assertEmptyObject: IpcValueValidator<Record<string, never>> = (input) => {
  if (!isRecord(input) || Object.keys(input).length > 0) {
    throw new PowerAssertionError("invalid_input", "input must be an empty object");
  }
  return {};
};

const assertPowerAssertionSnapshot: IpcValueValidator<PowerAssertionSnapshot> = (
  input,
) => {
  if (!isRecord(input)) {
    throw new PowerAssertionError("invalid_input", "snapshot output must be an object");
  }
  const active = input.active;
  const assertionId = input.assertionId;
  const mechanism = input.mechanism;
  const level = input.level;
  const ownerRoundIds = input.ownerRoundIds;
  const heldCount = input.heldCount;
  const acquiredAt = input.acquiredAt;

  if (typeof active !== "boolean") {
    throw new PowerAssertionError("invalid_input", "snapshot.active must be boolean");
  }
  if (!(assertionId === null || typeof assertionId === "string")) {
    throw new PowerAssertionError("invalid_input", "snapshot.assertionId must be string|null");
  }
  if (
    !(
      mechanism === null ||
      (typeof mechanism === "string" &&
        POWER_ASSERTION_MECHANISMS.has(mechanism as PowerAssertionMechanism))
    )
  ) {
    throw new PowerAssertionError("invalid_input", "snapshot.mechanism is invalid");
  }
  if (
    !(
      level === null ||
      (typeof level === "string" &&
        POWER_ASSERTION_LEVELS.has(level as PowerAssertionLevel))
    )
  ) {
    throw new PowerAssertionError("invalid_input", "snapshot.level is invalid");
  }
  if (!Array.isArray(ownerRoundIds) || !ownerRoundIds.every((id) => typeof id === "string")) {
    throw new PowerAssertionError("invalid_input", "snapshot.ownerRoundIds must be string[]");
  }
  if (typeof heldCount !== "number" || !Number.isInteger(heldCount) || heldCount < 0) {
    throw new PowerAssertionError("invalid_input", "snapshot.heldCount must be a non-negative integer");
  }
  if (!(acquiredAt === null || typeof acquiredAt === "string")) {
    throw new PowerAssertionError("invalid_input", "snapshot.acquiredAt must be string|null");
  }
  return {
    active,
    assertionId,
    mechanism: mechanism as PowerAssertionMechanism | null,
    level: level as PowerAssertionLevel | null,
    ownerRoundIds,
    heldCount,
    acquiredAt,
  };
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
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
      const args =
        input.level === "display"
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

function nsProcessInfoActivityScript(level: PowerAssertionLevel, reason: string): string {
  const optionsExpr = NS_PROCESS_INFO_OPTIONS[level];
  return [
    "ObjC.import('Foundation');",
    `const reason = ${JSON.stringify(reason)};`,
    `const activity = $.NSProcessInfo.processInfo.beginActivityWithOptionsReason(${optionsExpr}, reason);`,
    "while (true) {",
    "  $.NSRunLoop.currentRunLoop.runUntilDate($.NSDate.dateWithTimeIntervalSinceNow(3600));",
    "}",
    "activity;",
  ].join("\n");
}

const NS_PROCESS_INFO_OPTIONS: Record<PowerAssertionLevel, string> = {
  display: "$.NSActivityIdleDisplaySleepDisabled",
  "app-suspension": "$.NSActivityUserInitiated",
  system: "$.NSActivityIdleSystemSleepDisabled + $.NSActivityUserInitiated",
};

function cryptoRandomId(): string {
  return `pa_${randomUUID()}`;
}

function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
