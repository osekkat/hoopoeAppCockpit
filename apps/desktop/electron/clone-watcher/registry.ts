// hp-ndx5 — CloneWatcherRegistry. One watcher per active project.
//
// Lives in the main process. The renderer never sees this directly; the
// registry forwards CloneWatcherEvents to the IPC bus on `events.clone.dirty`.
// The BackendLifecycle (apps/desktop/src/main/BackendLifecycle.ts) is the
// expected caller — it adds a watcher when a project is registered + the
// initial clone exists, and stops the watcher on project removal / app quit.

import {
  createCloneWatcher,
  type CloneWatcher,
  type CreateWatcherOptions,
} from "./watcher.ts";
import { type CloneWatcherEvent, type CloneWatcherListener } from "./types.ts";

export interface CloneWatcherRegistry {
  /** Add a watcher for `projectId` and immediately start it. Idempotent
   *  — calling twice with the same projectId returns the existing
   *  watcher (and does NOT restart it). */
  readonly add: (input: Omit<CreateWatcherOptions, "onEvent">) => CloneWatcher;
  /** Stop + remove the watcher for `projectId`. No-op when missing. */
  readonly remove: (projectId: string) => void;
  /** All currently-tracked watchers. */
  readonly list: () => readonly CloneWatcher[];
  /** Stop every watcher. Called on app quit. */
  readonly stopAll: () => void;
  /** Subscribe to every watcher's events; returns an unsubscribe handle. */
  readonly subscribe: (listener: CloneWatcherListener) => () => void;
}

export function createCloneWatcherRegistry(): CloneWatcherRegistry {
  const watchers = new Map<string, CloneWatcher>();
  const listeners = new Set<CloneWatcherListener>();

  function broadcast(event: CloneWatcherEvent): void {
    for (const listener of listeners) {
      try { listener(event); } catch { /* swallow listener throws */ }
    }
  }

  return {
    add(input) {
      const existing = watchers.get(input.projectId);
      if (existing) return existing;
      const watcher = createCloneWatcher({ ...input, onEvent: broadcast });
      watchers.set(input.projectId, watcher);
      watcher.start();
      return watcher;
    },
    remove(projectId) {
      const watcher = watchers.get(projectId);
      if (!watcher) return;
      watcher.stop();
      watchers.delete(projectId);
    },
    list() {
      return Array.from(watchers.values());
    },
    stopAll() {
      for (const watcher of watchers.values()) {
        watcher.stop();
      }
      watchers.clear();
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => {
        listeners.delete(listener);
      };
    },
  };
}
