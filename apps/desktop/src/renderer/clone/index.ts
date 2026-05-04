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
