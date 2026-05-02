// Hoopoe-owned. Three-tier settings resolver — defaults / user / project —
// built on top of t3code's lifted `writeFileStringAtomically` and
// `stripDefaults` helpers (see `apps/desktop/src/vendored/t3code/settings/`).
//
// Resolution order on read:
//   project overrides user overrides defaults (deep merge per key path).
//
// Persistence on write:
//   - `setUserSettings(partial)` updates `~/.hoopoe/settings.json`.
//   - `setProjectSettings(partial)` updates `<projectRoot>/.hoopoe/settings.json`.
//   - Each file is stripped against DEFAULTS before write — only keys that
//     differ from the in-memory defaults are persisted (forward-compat:
//     a future default bump propagates without an explicit migration).
//
// Per Appendix B "Anti-patterns to refuse":
//   #1 — change-stream queue is BOUNDED; over-capacity drops oldest events
//        and tells the subscriber to refetch a snapshot. We do not lift
//        t3code's `PubSub.unbounded`.
//
// Per plan.md §3 "lifted patterns":
//   - tempfile + fsync + rename for crash-safe atomic writes (vendored).
//   - 100 ms debounce on the file watcher (t3code's chosen value; do not
//     tune without measuring).
//   - schemaVersion field on every persisted file; mismatch falls back to
//     defaults and emits a structured warning rather than crashing.
//   - stripDefaults forward-compat (vendored).
//
// Hot-reload: both user and project files are watched. Changes coalesce
// through a 100 ms debounce per file. Watching the parent directory + a
// basename filter survives atomic-rename writes (Linux inotify path-bound
// watches go stale after rename, directory watches don't).

import * as FS from "node:fs";
import * as Path from "node:path";
import {
  stripDefaults as stripDefaultsImpl,
  writeFileStringAtomically,
} from "../vendored/t3code/settings/index.ts";

export const SETTINGS_SCHEMA_VERSION = 1;

export interface DaemonSettings {
  readonly logLevel: "debug" | "info" | "warn" | "error";
  readonly tendingEnabled: boolean;
  readonly daemonBinaryPath: string | null;
}

export interface DesktopSettings {
  readonly serverExposureMode: "local-only" | "network-accessible";
  readonly updateChannel: "latest" | "nightly";
  readonly updateChannelConfiguredByUser: boolean;
}

export interface ClientSettings {
  readonly activeProjectId: string | null;
  readonly activeStage: "planning" | "beads" | "swarm" | "hardening";
  readonly activityPanelOpen: boolean;
}

export interface HoopoeSettings {
  readonly schemaVersion: number;
  readonly daemon: DaemonSettings;
  readonly desktop: DesktopSettings;
  readonly client: ClientSettings;
}

export const DEFAULT_HOOPOE_SETTINGS: HoopoeSettings = {
  schemaVersion: SETTINGS_SCHEMA_VERSION,
  daemon: {
    logLevel: "info",
    tendingEnabled: true,
    daemonBinaryPath: null,
  },
  desktop: {
    serverExposureMode: "local-only",
    updateChannel: "latest",
    updateChannelConfiguredByUser: false,
  },
  client: {
    activeProjectId: null,
    activeStage: "planning",
    activityPanelOpen: false,
  },
};

/** Keys whose change triggers `relaunchDesktopApp(reason)` because they
 * cannot be hot-applied (boot-only Electron flags, daemon binary path,
 * settings consumed during process startup). Others propagate via the
 * change stream. */
export const RELAUNCH_KEYS: ReadonlySet<string> = new Set<string>([
  "daemon.daemonBinaryPath",
  "desktop.serverExposureMode",
]);

export type SettingsTier = "user" | "project";

export interface SettingsBridgePaths {
  readonly userFile: string;
  readonly projectFile: string | null;
}

export function defaultUserSettingsPath(homeDir: string): string {
  return Path.join(homeDir, ".hoopoe", "settings.json");
}

export function projectSettingsPath(projectRoot: string): string {
  return Path.join(projectRoot, ".hoopoe", "settings.json");
}

export interface SettingsChangeEvent {
  readonly resolved: HoopoeSettings;
  readonly tier: SettingsTier | "init";
  /** Dotted key paths whose resolved value changed. Empty on the initial
   * `init` event. */
  readonly changedKeys: readonly string[];
  /** `true` when this event's `changedKeys` intersected the `RELAUNCH_KEYS`
   * set — the `relaunchDesktopApp` hook has already fired by this point;
   * subscribers get this flag for UI ("Restart now?" prompts). */
  readonly relaunchTriggered: boolean;
  /** Set when a slow listener fell behind and the bus dropped events. */
  readonly dropped?: number;
}

export type SettingsSubscriber = (event: SettingsChangeEvent) => void;

export interface SettingsBridgeLogger {
  readonly warn: (message: string, meta?: Record<string, unknown>) => void;
  readonly info: (message: string, meta?: Record<string, unknown>) => void;
}

const noopLogger: SettingsBridgeLogger = { warn() {}, info() {} };

const MAX_PENDING_PER_SUBSCRIBER = 64;
const HOT_RELOAD_DEBOUNCE_MS = 100;

export class SettingsBridge {
  private readonly paths: SettingsBridgePaths;
  private readonly logger: SettingsBridgeLogger;
  private readonly relaunchImpl: (reason: string) => void;
  private readonly subscribers = new Map<
    number,
    { readonly listener: SettingsSubscriber; queue: SettingsChangeEvent[]; flushing: boolean }
  >();
  private nextSubscriberId = 1;
  private userPartial: DeepPartial<HoopoeSettings>;
  private projectPartial: DeepPartial<HoopoeSettings>;
  private resolvedSnapshot: HoopoeSettings;
  private userWatcher: FS.FSWatcher | null = null;
  private projectWatcher: FS.FSWatcher | null = null;
  private userReloadTimer: NodeJS.Timeout | null = null;
  private projectReloadTimer: NodeJS.Timeout | null = null;
  private hotReloadCount = 0;

  constructor(input: {
    readonly paths: SettingsBridgePaths;
    readonly relaunch?: (reason: string) => void;
    readonly logger?: SettingsBridgeLogger;
  }) {
    this.paths = input.paths;
    this.logger = input.logger ?? noopLogger;
    this.relaunchImpl = input.relaunch ?? defaultRelaunch;
    this.userPartial = readVersionedPartial(this.paths.userFile, this.logger, "user");
    this.projectPartial = this.paths.projectFile
      ? readVersionedPartial(this.paths.projectFile, this.logger, "project")
      : {};
    this.resolvedSnapshot = this.computeResolved();
  }

  /** Resolved settings = deepMerge(DEFAULTS, user, project). */
  resolved(): HoopoeSettings {
    return this.resolvedSnapshot;
  }

  userOverrides(): DeepPartial<HoopoeSettings> {
    return this.userPartial;
  }
  projectOverrides(): DeepPartial<HoopoeSettings> {
    return this.projectPartial;
  }

  setUserSettings(partial: DeepPartial<HoopoeSettings>): void {
    this.userPartial = mergeDeep(this.userPartial, partial);
    const stripped =
      stripDefaultsImpl(this.userPartial, DEFAULT_HOOPOE_SETTINGS) ?? {};
    const persisted = ensureSchemaVersion(stripped);
    writeFileStringAtomically({
      filePath: this.paths.userFile,
      contents: `${JSON.stringify(persisted, null, 2)}\n`,
    });
    this.recompileAndBroadcast("user");
  }

  setProjectSettings(partial: DeepPartial<HoopoeSettings>): void {
    if (!this.paths.projectFile) {
      throw new Error("project settings file path not configured");
    }
    this.projectPartial = mergeDeep(this.projectPartial, partial);
    const stripped =
      stripDefaultsImpl(this.projectPartial, DEFAULT_HOOPOE_SETTINGS) ?? {};
    const persisted = ensureSchemaVersion(stripped);
    writeFileStringAtomically({
      filePath: this.paths.projectFile,
      contents: `${JSON.stringify(persisted, null, 2)}\n`,
    });
    this.recompileAndBroadcast("project");
  }

  /** Trigger a desktop relaunch — invoked automatically when a write
   * touches a `RELAUNCH_KEYS` member, and callable directly when the user
   * confirms a restart prompt. The `reason` shows up in the audit log;
   * never include secrets. */
  relaunchDesktopApp(reason: string): void {
    this.logger.info("settings.relaunch-requested", { reason });
    this.relaunchImpl(reason);
  }

  subscribe(listener: SettingsSubscriber): { readonly unsubscribe: () => void } {
    const id = this.nextSubscriberId++;
    this.subscribers.set(id, { listener, queue: [], flushing: false });
    return {
      unsubscribe: () => {
        this.subscribers.delete(id);
      },
    };
  }

  /** Watch both user and project files (parent-dir + basename filter so
   * atomic-rename writes still fire the event). 100 ms debounce coalesces
   * bursts. */
  startWatching(): void {
    if (!this.userWatcher) {
      this.userWatcher = this.makeWatcher(this.paths.userFile, () => {
        this.scheduleReload("user");
      });
    }
    if (this.paths.projectFile && !this.projectWatcher) {
      this.projectWatcher = this.makeWatcher(this.paths.projectFile, () => {
        this.scheduleReload("project");
      });
    }
  }

  stopWatching(): void {
    if (this.userReloadTimer) clearTimeout(this.userReloadTimer);
    if (this.projectReloadTimer) clearTimeout(this.projectReloadTimer);
    this.userReloadTimer = null;
    this.projectReloadTimer = null;
    if (this.userWatcher) {
      this.userWatcher.close();
      this.userWatcher = null;
    }
    if (this.projectWatcher) {
      this.projectWatcher.close();
      this.projectWatcher = null;
    }
  }

  /** Test seam — synchronous reload of a single tier. */
  reloadNow(tier: SettingsTier): void {
    this.hotReloadCount += 1;
    const filePath = tier === "user" ? this.paths.userFile : this.paths.projectFile;
    if (!filePath) return;
    if (tier === "user") {
      this.userPartial = readVersionedPartial(filePath, this.logger, "user");
    } else {
      this.projectPartial = readVersionedPartial(filePath, this.logger, "project");
    }
    this.recompileAndBroadcast(tier);
  }

  /** Test seam — exposes scheduleReload to deterministically test the
   * 100 ms debounce coalescing without depending on inotify timing. */
  scheduleReloadForTesting(tier: SettingsTier): void {
    this.scheduleReload(tier);
  }

  hotReloadCountForTesting(): number {
    return this.hotReloadCount;
  }

  subscriberQueueDepthForTesting(): number {
    let total = 0;
    for (const subscriber of this.subscribers.values()) {
      total += subscriber.queue.length;
    }
    return total;
  }

  private makeWatcher(filePath: string, onChange: () => void): FS.FSWatcher | null {
    const directory = Path.dirname(filePath);
    const basename = Path.basename(filePath);
    if (!FS.existsSync(directory)) {
      try {
        FS.mkdirSync(directory, { recursive: true });
      } catch (error) {
        this.logger.warn("settings.watch-mkdir-failed", {
          error: error instanceof Error ? error.message : String(error),
        });
        return null;
      }
    }
    try {
      return FS.watch(directory, (_eventType, filename) => {
        if (filename === null || filename === basename) {
          onChange();
        }
      });
    } catch (error) {
      this.logger.warn("settings.watch-failed", {
        error: error instanceof Error ? error.message : String(error),
      });
      return null;
    }
  }

  private scheduleReload(tier: SettingsTier): void {
    if (tier === "user") {
      if (this.userReloadTimer) clearTimeout(this.userReloadTimer);
      this.userReloadTimer = setTimeout(() => {
        this.userReloadTimer = null;
        this.reloadNow("user");
      }, HOT_RELOAD_DEBOUNCE_MS);
    } else {
      if (this.projectReloadTimer) clearTimeout(this.projectReloadTimer);
      this.projectReloadTimer = setTimeout(() => {
        this.projectReloadTimer = null;
        this.reloadNow("project");
      }, HOT_RELOAD_DEBOUNCE_MS);
    }
  }

  private computeResolved(): HoopoeSettings {
    const layered = mergeDeep(
      DEFAULT_HOOPOE_SETTINGS as DeepPartial<HoopoeSettings>,
      this.userPartial,
    );
    const fully = mergeDeep(layered, this.projectPartial);
    return fully as HoopoeSettings;
  }

  private recompileAndBroadcast(tier: SettingsTier): void {
    const previous = this.resolvedSnapshot;
    const next = this.computeResolved();
    this.resolvedSnapshot = next;
    const changedKeys = diffKeyPaths(previous, next);
    const relaunchTriggered = changedKeys.some((key) => RELAUNCH_KEYS.has(key));
    if (relaunchTriggered) {
      this.relaunchDesktopApp(`settings-changed:${changedKeys.join(",")}`);
    }
    this.broadcast({
      resolved: next,
      tier,
      changedKeys,
      relaunchTriggered,
    });
  }

  private broadcast(event: SettingsChangeEvent): void {
    for (const [id, subscriber] of this.subscribers) {
      if (subscriber.queue.length >= MAX_PENDING_PER_SUBSCRIBER) {
        const droppedEvents = subscriber.queue.length;
        subscriber.queue.length = 0;
        const droppedNotice: SettingsChangeEvent = {
          resolved: event.resolved,
          tier: event.tier,
          changedKeys: [],
          relaunchTriggered: false,
          dropped: droppedEvents,
        };
        subscriber.queue.push(droppedNotice);
        subscriber.queue.push(event);
      } else {
        subscriber.queue.push(event);
      }
      void this.flushSubscriber(id);
    }
  }

  private async flushSubscriber(id: number): Promise<void> {
    const subscriber = this.subscribers.get(id);
    if (!subscriber || subscriber.flushing) return;
    subscriber.flushing = true;
    try {
      while (subscriber.queue.length > 0) {
        const next = subscriber.queue.shift();
        if (!next) break;
        try {
          subscriber.listener(next);
        } catch {
          // Listener errors must not poison the bus.
        }
      }
    } finally {
      subscriber.flushing = false;
    }
  }
}

// ── Helpers ────────────────────────────────────────────────────────────────

export type DeepPartial<T> = T extends object
  ? { readonly [K in keyof T]?: DeepPartial<T[K]> }
  : T;

function readVersionedPartial(
  filePath: string,
  logger: SettingsBridgeLogger,
  tier: SettingsTier,
): DeepPartial<HoopoeSettings> {
  try {
    if (!FS.existsSync(filePath)) return {};
    const raw = FS.readFileSync(filePath, "utf8");
    const parsed = JSON.parse(raw);
    if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
      logger.warn("settings.malformed", { tier, detail: "expected object" });
      return {};
    }
    const obj = parsed as Record<string, unknown>;
    const onDiskVersion = obj.schemaVersion;
    if (
      typeof onDiskVersion === "number" &&
      onDiskVersion !== SETTINGS_SCHEMA_VERSION
    ) {
      logger.warn("settings.schema-version-mismatch", {
        tier,
        onDisk: onDiskVersion,
        expected: SETTINGS_SCHEMA_VERSION,
      });
      return {};
    }
    const { schemaVersion: _ignored, ...rest } = obj;
    return rest as DeepPartial<HoopoeSettings>;
  } catch (error) {
    logger.warn("settings.read-failed", {
      tier,
      error: error instanceof Error ? error.message : String(error),
    });
    return {};
  }
}

function ensureSchemaVersion(
  value: unknown,
): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    return { schemaVersion: SETTINGS_SCHEMA_VERSION };
  }
  return { schemaVersion: SETTINGS_SCHEMA_VERSION, ...(value as Record<string, unknown>) };
}

export function mergeDeep<T>(
  base: DeepPartial<T>,
  overlay: DeepPartial<T>,
): DeepPartial<T> {
  if (
    overlay === null ||
    overlay === undefined ||
    typeof overlay !== "object" ||
    Array.isArray(overlay)
  ) {
    return overlay !== undefined ? overlay : base;
  }
  if (
    base === null ||
    base === undefined ||
    typeof base !== "object" ||
    Array.isArray(base)
  ) {
    return overlay;
  }
  const out: Record<string, unknown> = { ...(base as Record<string, unknown>) };
  for (const key of Object.keys(overlay as Record<string, unknown>)) {
    const baseValue = (base as Record<string, unknown>)[key];
    const overlayValue = (overlay as Record<string, unknown>)[key];
    if (overlayValue === undefined) continue;
    out[key] = mergeDeep(
      baseValue as DeepPartial<unknown>,
      overlayValue as DeepPartial<unknown>,
    ) as unknown;
  }
  return out as DeepPartial<T>;
}

/** Walks `previous` and `next` returning the dotted key paths whose
 * resolved value differs. Used by the change-stream to tell subscribers
 * exactly what changed (cheap surface for fine-grained UI invalidation). */
export function diffKeyPaths(previous: unknown, next: unknown, prefix = ""): readonly string[] {
  if (Object.is(previous, next)) return [];
  if (
    previous === null ||
    next === null ||
    typeof previous !== "object" ||
    typeof next !== "object" ||
    Array.isArray(previous) !== Array.isArray(next)
  ) {
    return prefix ? [prefix] : [];
  }
  if (Array.isArray(previous) && Array.isArray(next)) {
    return arraysEqual(previous, next) ? [] : prefix ? [prefix] : [];
  }
  const out: string[] = [];
  const prev = previous as Record<string, unknown>;
  const nxt = next as Record<string, unknown>;
  const keys = new Set([...Object.keys(prev), ...Object.keys(nxt)]);
  for (const key of keys) {
    const childPrefix = prefix ? `${prefix}.${key}` : key;
    out.push(...diffKeyPaths(prev[key], nxt[key], childPrefix));
  }
  return out;
}

function arraysEqual(a: readonly unknown[], b: readonly unknown[]): boolean {
  if (a.length !== b.length) return false;
  for (let index = 0; index < a.length; index += 1) {
    if (!Object.is(a[index], b[index])) return false;
  }
  return true;
}

function defaultRelaunch(reason: string): void {
  // The actual Electron `app.relaunch()` call wires up via main.ts.
  void reason;
}
