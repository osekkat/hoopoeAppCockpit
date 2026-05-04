// hp-70yz — Per-project clone dirty-state store.
//
// The desktop main-process clone watcher (hp-ndx5) emits CloneDirtyState
// updates over the `events.clone.dirty` IPC topic. The renderer keeps a
// Zustand-backed cache so DirtyBanner can subscribe with constant-time
// re-renders without a TanStack Query round-trip.
//
// This module is the single source of truth for dirty state in the
// renderer. The DirtyBanner reads from here; the subscription effect
// (DirtyBannerSubscription, mounted once in RootLayout) writes to here.
//
// Initial-load story: until the watcher emits an event for a project,
// the store has no entry → banner renders nothing (clean assumption).
// When the daemon-side Get-CloneState RPC lands (follow-up bead), the
// subscription effect can hydrate the store on connect.

import { create } from "zustand";

export interface CloneDirtyState {
  readonly dirty: boolean;
  readonly modifiedCount: number;
  readonly untrackedCount: number;
  readonly aheadCount: number;
  readonly behindCount: number;
}

export const CLEAN_DIRTY_STATE: CloneDirtyState = {
  dirty: false,
  modifiedCount: 0,
  untrackedCount: 0,
  aheadCount: 0,
  behindCount: 0,
};

interface DirtyEntry {
  readonly state: CloneDirtyState;
  readonly updatedAt: string;
}

export interface DirtyStoreState {
  readonly entries: Readonly<Record<string, DirtyEntry>>;
  /** Wire shape from `events.clone.dirty`. */
  readonly recordEvent: (projectId: string, state: CloneDirtyState, now?: () => Date) => void;
  /** Drop the entry for a project (called on project removal). */
  readonly forget: (projectId: string) => void;
  /** Reset everything (test seam). */
  readonly clear: () => void;
}

export const useDirtyStore = create<DirtyStoreState>((set) => ({
  entries: {},
  recordEvent(projectId, state, now = () => new Date()) {
    if (projectId.length === 0) return;
    const updatedAt = now().toISOString();
    set((current) => ({
      entries: {
        ...current.entries,
        [projectId]: { state, updatedAt },
      },
    }));
  },
  forget(projectId) {
    set((current) => {
      if (!(projectId in current.entries)) return current;
      const { [projectId]: _drop, ...rest } = current.entries;
      return { entries: rest };
    });
  },
  clear() {
    set({ entries: {} });
  },
}));

/** Returns the dirty state for `projectId`, or null if no event has been
 *  recorded for it yet. The banner consumer uses null to render nothing
 *  (initial-load case). */
export function selectDirtyState(state: DirtyStoreState, projectId: string | null): CloneDirtyState | null {
  if (!projectId) return null;
  return state.entries[projectId]?.state ?? null;
}

export function selectUpdatedAt(state: DirtyStoreState, projectId: string | null): string | null {
  if (!projectId) return null;
  return state.entries[projectId]?.updatedAt ?? null;
}

// ── Subscription effect ──────────────────────────────────────────────────

interface RendererBridge {
  readonly daemon?: {
    readonly subscribe?: <P>(
      topic: string,
      listener: (payload: P) => void,
    ) => () => void;
  };
}

interface CloneDirtyEventPayload {
  readonly projectId: string;
  readonly state: CloneDirtyState;
}

/** Wire the dirty store to the events.clone.dirty IPC subscription.
 *  Returns an unsubscribe handle. Must be called from a React effect at
 *  the RootLayout level so there's exactly one subscription per app run.
 *
 *  When the bridge isn't available (no daemon paired yet, jsdom test
 *  env), returns a no-op unsubscribe so callers can safely depend on it. */
export function subscribeToCloneDirtyEvents(
  recordEvent: DirtyStoreState["recordEvent"],
): () => void {
  if (typeof window === "undefined") return () => undefined;
  const bridge = (window as Window & { readonly hoopoe?: RendererBridge }).hoopoe;
  const subscribe = bridge?.daemon?.subscribe;
  if (typeof subscribe !== "function") return () => undefined;
  const unsubscribe = subscribe<CloneDirtyEventPayload>("events.clone.dirty", (payload) => {
    if (
      typeof payload === "object" &&
      payload !== null &&
      typeof payload.projectId === "string" &&
      payload.projectId.length > 0 &&
      typeof payload.state === "object" &&
      payload.state !== null
    ) {
      recordEvent(payload.projectId, payload.state);
    }
  });
  return unsubscribe;
}
