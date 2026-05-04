// hp-70yz — public exports for `apps/desktop/src/renderer/clone/`.
//
// Renderer-side surfaces over the desktop clone engine (hp-2n1) +
// watcher (hp-ndx5). The DirtyBanner reads from the per-project
// dirty-state store; the DirtyBannerSubscription wires the store to the
// events.clone.dirty IPC topic.

export {
  CLEAN_DIRTY_STATE,
  selectDirtyState,
  selectUpdatedAt,
  subscribeToCloneDirtyEvents,
  useDirtyStore,
  type CloneDirtyState,
  type DirtyStoreState,
} from "./dirty-store.ts";

export {
  DirtyBanner,
  DirtyBannerView,
  describeDirty,
  type DirtyBannerProps,
  type DirtyBannerViewProps,
} from "./DirtyBanner.tsx";
export { DirtyBannerSubscription } from "./DirtyBannerSubscription.tsx";

export { CloneSettingsCard, type CloneSettingsCardProps } from "./CloneSettingsCard.tsx";

export {
  CAP_HARD_MAX_BYTES,
  CloneActionsBridgeUnavailableError,
  DEFAULT_CACHE_SORT,
  STUB_CLONE_ACTIONS_BRIDGE,
  formatBytes,
  formatRelativeTime,
  resolveCloneActionsBridge,
  sortCacheRows,
  totalCacheBytes,
  validateCapOverride,
  type CapOverrideError,
  type CapOverrideForm,
  type CloneActionsBridge,
  type CloneCacheRow,
  type CloneCacheSort,
  type CloneCacheSortDir,
  type CloneCacheSortKey,
} from "./cache-view-model.ts";
