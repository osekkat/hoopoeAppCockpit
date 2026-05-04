// hp-ndx5 — public exports for `apps/desktop/electron/clone-watcher/`.
//
// Wires the engine modules from `apps/desktop/electron/clone/` into the
// Electron main process via fs.watch + debounce. Registry forwards
// CloneWatcherEvents to the renderer through the events.clone.dirty IPC
// topic (subscribe via `window.hoopoe.daemon.subscribe("events.clone.dirty", ...)`).

export {
  createCloneWatcher,
  CloneWatcherError,
  type CloneWatcher,
  type CloneWatcherDiagnostic,
  type CloneWatcherDiagnosticSink,
  type CreateWatcherOptions,
  type FsWatchHandle,
} from "./watcher.ts";

export {
  createCloneWatcherRegistry,
  type CloneWatcherRegistry,
} from "./registry.ts";

export {
  DEFAULT_DEBOUNCE_MS,
  WATCHER_RETRY_DELAY_MS,
  type CloneWatcherEvent,
  type CloneWatcherListener,
  type WatcherStatus,
} from "./types.ts";

export {
  createMockClock,
  debounce,
  realClock,
  type Clock,
  type DebouncedHandle,
} from "./debounce.ts";
