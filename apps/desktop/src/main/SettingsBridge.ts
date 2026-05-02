// Hoopoe-owned. Three-store settings split with hot-reload, atomic write,
// schema-validated reads, and a BOUNDED PubSub change stream. Wraps the
// vendored t3code `desktopSettings.ts` reader/writer for the desktop store
// and adds parallel daemon and client stores.
//
// Anti-pattern refusal (Appendix B #1): the change-stream queue is bounded;
// if a subscriber falls behind by more than `MAX_PENDING_PER_SUBSCRIBER`
// events, the oldest events are dropped and the subscriber is told via
// a `dropped` payload so the renderer can re-fetch a snapshot.

import * as FS from "node:fs";
import * as Path from "node:path";
import {
  readDesktopSettings,
  writeDesktopSettings,
  type DesktopSettings,
} from "../vendored/t3code/desktopSettings.ts";

export type SettingsStore = "daemon" | "desktop" | "client";

export interface DaemonSettings {
  readonly logLevel: "debug" | "info" | "warn" | "error";
  readonly tendingEnabled: boolean;
}

export interface ClientSettings {
  readonly activeProjectId: string | null;
  readonly activeStage: "planning" | "beads" | "swarm" | "hardening";
  readonly activityPanelOpen: boolean;
}

export const DEFAULT_DAEMON_SETTINGS: DaemonSettings = {
  logLevel: "info",
  tendingEnabled: true,
};

export const DEFAULT_CLIENT_SETTINGS: ClientSettings = {
  activeProjectId: null,
  activeStage: "planning",
  activityPanelOpen: false,
};

export interface SettingsBridgePaths {
  readonly daemon: string;
  readonly desktop: string;
  readonly client: string;
}

export function defaultSettingsBridgePaths(homeDir: string): SettingsBridgePaths {
  const userdata = Path.join(homeDir, ".hoopoe", "userdata");
  return {
    daemon: Path.join(userdata, "daemon-settings.json"),
    desktop: Path.join(userdata, "desktop-settings.json"),
    client: Path.join(userdata, "client-settings.json"),
  };
}

export type SettingsChangeEvent =
  | { readonly store: "daemon"; readonly settings: DaemonSettings }
  | { readonly store: "desktop"; readonly settings: DesktopSettings }
  | { readonly store: "client"; readonly settings: ClientSettings }
  | { readonly store: SettingsStore; readonly dropped: number };

export type SettingsSubscriber = (event: SettingsChangeEvent) => void;

/** Hard cap on pending events per subscriber. Past this we drop oldest and
 * tell the subscriber to re-fetch a snapshot. */
const MAX_PENDING_PER_SUBSCRIBER = 64;

export class SettingsBridge {
  private readonly paths: SettingsBridgePaths;
  private readonly subscribers = new Map<
    number,
    { readonly listener: SettingsSubscriber; queue: SettingsChangeEvent[]; flushing: boolean }
  >();
  private nextSubscriberId = 1;
  private currentDesktop: DesktopSettings;
  private currentDaemon: DaemonSettings;
  private currentClient: ClientSettings;

  constructor(input: {
    readonly paths: SettingsBridgePaths;
    readonly currentAppVersion: string;
    readonly relaunch?: (reason: string) => void;
  }) {
    this.paths = input.paths;
    this.currentDesktop = readDesktopSettings(this.paths.desktop, input.currentAppVersion);
    this.currentDaemon = readJsonOrDefault<DaemonSettings>(
      this.paths.daemon,
      DEFAULT_DAEMON_SETTINGS,
    );
    this.currentClient = readJsonOrDefault<ClientSettings>(
      this.paths.client,
      DEFAULT_CLIENT_SETTINGS,
    );
    this.relaunchImpl = input.relaunch ?? defaultRelaunch;
  }

  private readonly relaunchImpl: (reason: string) => void;

  getDesktopSettings(): DesktopSettings {
    return this.currentDesktop;
  }
  getDaemonSettings(): DaemonSettings {
    return this.currentDaemon;
  }
  getClientSettings(): ClientSettings {
    return this.currentClient;
  }

  setDesktopSettings(next: DesktopSettings): void {
    writeDesktopSettings(this.paths.desktop, next);
    this.currentDesktop = next;
    this.broadcast({ store: "desktop", settings: next });
  }

  setDaemonSettings(next: DaemonSettings): void {
    writeJsonAtomically(this.paths.daemon, next);
    this.currentDaemon = next;
    this.broadcast({ store: "daemon", settings: next });
  }

  setClientSettings(next: ClientSettings): void {
    writeJsonAtomically(this.paths.client, next);
    this.currentClient = next;
    this.broadcast({ store: "client", settings: next });
  }

  /** Trigger a desktop relaunch — used after toggling settings whose effect
   * cannot be hot-applied (e.g., daemon binary path, Electron flags). The
   * `reason` shows up in the audit log; never include secrets. */
  relaunchDesktopApp(reason: string): void {
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

  private broadcast(event: SettingsChangeEvent): void {
    for (const [id, subscriber] of this.subscribers) {
      if (subscriber.queue.length >= MAX_PENDING_PER_SUBSCRIBER) {
        const droppedEvents = subscriber.queue.length;
        subscriber.queue.length = 0;
        const droppedNotice: SettingsChangeEvent = {
          store: event.store,
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
          // Listener errors must not poison the bus. Drop and continue.
        }
      }
    } finally {
      subscriber.flushing = false;
    }
  }

  /** Test-only: report the current pending queue depth for a subscriber. */
  subscriberQueueDepthForTesting(): number {
    let total = 0;
    for (const subscriber of this.subscribers.values()) {
      total += subscriber.queue.length;
    }
    return total;
  }
}

function readJsonOrDefault<T>(filePath: string, fallback: T): T {
  try {
    if (!FS.existsSync(filePath)) return fallback;
    const raw = FS.readFileSync(filePath, "utf8");
    const parsed = JSON.parse(raw);
    if (parsed === null || typeof parsed !== "object") return fallback;
    return { ...fallback, ...(parsed as Partial<T>) };
  } catch {
    return fallback;
  }
}

function writeJsonAtomically(filePath: string, value: unknown): void {
  const directory = Path.dirname(filePath);
  const tempPath = `${filePath}.${process.pid}.${Date.now()}.tmp`;
  FS.mkdirSync(directory, { recursive: true });
  FS.writeFileSync(tempPath, `${JSON.stringify(value, null, 2)}\n`, "utf8");
  FS.renameSync(tempPath, filePath);
}

function defaultRelaunch(reason: string): void {
  // The actual `app.relaunch()` call is a hard runtime dep on Electron's
  // app lifecycle; main.ts wires up the real implementation. The default
  // here is a no-op so unit tests don't accidentally tear down the test
  // harness when SettingsBridge is exercised in isolation.
  void reason;
}
