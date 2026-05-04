// hp-m79e — public exports for the renderer tunnel surface.

export {
  INITIAL_TUNNEL_SNAPSHOT,
  TUNNEL_STATES,
  hydrateFromBridge,
  selectTunnelSnapshot,
  selectVpsHealthDot,
  subscribeToTunnelEvents,
  useTunnelStore,
  type ConnectionFault,
  type TunnelSnapshot,
  type TunnelState,
  type TunnelStoreState,
} from "./tunnel-store.ts";

export {
  TUNNEL_STATE_LABELS,
  formatReconnectCountdown,
  tunnelAriaLabel,
  tunnelHealthDot,
  tunnelSeverity,
  type TunnelSeverity,
} from "./format-helpers.ts";

export {
  ConnectionStatusPill,
  ConnectionStatusPillView,
  type ConnectionStatusPillProps,
} from "./ConnectionStatusPill.tsx";

export { TunnelSubscription } from "./TunnelSubscription.tsx";
