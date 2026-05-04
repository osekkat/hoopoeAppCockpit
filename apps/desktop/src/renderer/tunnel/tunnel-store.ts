// hp-m79e — Renderer-side store for the tunnel ConnectionManager FSM
// snapshot.
//
// The desktop main-process tunnel orchestrator (hp-fkov, on top of the
// hp-e7k FSM) emits TunnelSnapshot updates over the `events.tunnel`
// IPC topic. This module owns the renderer's cache of that snapshot
// + a one-shot subscribe wiring + an initial-state hydrator that
// calls the `tunnel.snapshot` daemon-request method on mount.
//
// Pattern matches dirty-store.ts (hp-70yz) — Zustand singleton, malformed-
// payload defense, no-op when the bridge isn't installed yet.

import { create } from "zustand";

// ── Wire shapes (mirror the FSM types from electron/tunnel/types.ts) ──

export const TUNNEL_STATES = [
  "unconfigured",
  "ssh_probing",
  "bootstrapping",
  "tunnel_connecting",
  "authenticating",
  "ready",
  "degraded",
  "reconnecting",
  "disconnected",
] as const;
export type TunnelState = (typeof TUNNEL_STATES)[number];

export interface ConnectionFault {
  readonly code: string;
  readonly message: string;
  readonly capturedAt: string;
}

export interface TunnelSnapshot {
  readonly state: TunnelState;
  readonly activeProfileId: string | null;
  readonly localPort: number | null;
  readonly lastFault: ConnectionFault | null;
  readonly reconnectAttempts: number;
  readonly nextRetryAt: string | null;
}

export const INITIAL_TUNNEL_SNAPSHOT: TunnelSnapshot = {
  state: "unconfigured",
  activeProfileId: null,
  localPort: null,
  lastFault: null,
  reconnectAttempts: 0,
  nextRetryAt: null,
};

// ── Store ─────────────────────────────────────────────────────────────────

export interface TunnelStoreState {
  readonly snapshot: TunnelSnapshot;
  /** Last RFC3339 timestamp the store received an update — null until
   *  the first hydrate or events.tunnel event lands. */
  readonly receivedAt: string | null;
  /** Update the snapshot from a new event. */
  readonly recordEvent: (snapshot: TunnelSnapshot, now?: () => Date) => void;
  /** Reset to the initial uncfigured state (test seam + on profile_cleared). */
  readonly clear: () => void;
}

export const useTunnelStore = create<TunnelStoreState>((set) => ({
  snapshot: INITIAL_TUNNEL_SNAPSHOT,
  receivedAt: null,
  recordEvent(snapshot, now = () => new Date()) {
    if (!isValidSnapshot(snapshot)) return;
    set({ snapshot, receivedAt: now().toISOString() });
  },
  clear() {
    set({ snapshot: INITIAL_TUNNEL_SNAPSHOT, receivedAt: null });
  },
}));

export function selectTunnelSnapshot(state: TunnelStoreState): TunnelSnapshot {
  return state.snapshot;
}

// ── Bridge resolution ────────────────────────────────────────────────────

interface RendererBridge {
  readonly daemon?: {
    readonly request?: (method: string, body: unknown) => Promise<unknown>;
    readonly subscribe?: <P>(
      topic: string,
      listener: (payload: P) => void,
    ) => () => void;
  };
}

function resolveBridge(): RendererBridge["daemon"] | null {
  if (typeof window === "undefined") return null;
  const hoopoe = (window as Window & { readonly hoopoe?: RendererBridge }).hoopoe;
  return hoopoe?.daemon ?? null;
}

/** Hydrate the store with the current snapshot from the daemon RPC.
 *  Returns void; resolves silently when the bridge isn't installed. */
export async function hydrateFromBridge(
  recordEvent: TunnelStoreState["recordEvent"],
): Promise<void> {
  const daemon = resolveBridge();
  if (!daemon?.request) return;
  try {
    const snapshot = (await daemon.request("tunnel.snapshot", null)) as TunnelSnapshot;
    if (isValidSnapshot(snapshot)) recordEvent(snapshot);
  } catch {
    // Daemon RPC unreachable / not yet implemented — silent fallback,
    // events.tunnel will hydrate the store later when the orchestrator
    // pushes the first transition.
  }
}

/** Subscribe to `events.tunnel`; returns an unsubscribe handle. */
export function subscribeToTunnelEvents(
  recordEvent: TunnelStoreState["recordEvent"],
): () => void {
  const daemon = resolveBridge();
  if (!daemon?.subscribe) return () => undefined;
  return daemon.subscribe<TunnelSnapshot>("events.tunnel", (payload) => {
    if (isValidSnapshot(payload)) recordEvent(payload);
  });
}

// ── Validation (defense against malformed payloads) ──────────────────────

function isValidSnapshot(value: unknown): value is TunnelSnapshot {
  if (typeof value !== "object" || value === null) return false;
  const obj = value as Record<string, unknown>;
  if (typeof obj["state"] !== "string") return false;
  if (!TUNNEL_STATES.includes(obj["state"] as TunnelState)) return false;
  if (obj["activeProfileId"] !== null && typeof obj["activeProfileId"] !== "string") return false;
  if (obj["localPort"] !== null && typeof obj["localPort"] !== "number") return false;
  if (typeof obj["reconnectAttempts"] !== "number") return false;
  if (obj["nextRetryAt"] !== null && typeof obj["nextRetryAt"] !== "string") return false;
  // lastFault: null OR { code, message, capturedAt } strings.
  const fault = obj["lastFault"];
  if (fault !== null) {
    if (typeof fault !== "object") return false;
    const f = fault as Record<string, unknown>;
    if (typeof f["code"] !== "string") return false;
    if (typeof f["message"] !== "string") return false;
    if (typeof f["capturedAt"] !== "string") return false;
  }
  return true;
}
