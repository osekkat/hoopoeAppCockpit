// hp-e7k — public exports for `apps/desktop/electron/tunnel/`.
//
// Engine layer for the desktop SSH tunnel manager (§2.5). Real ssh2 +
// PowerMonitor + heartbeat orchestration ship in follow-up beads; the
// FSM + profile + backoff modules are pure TS so they're testable without
// touching the network.

export {
  FAULT_CODES,
  INITIAL_TUNNEL_SNAPSHOT,
  PROFILE_SCHEMA_VERSION,
  TUNNEL_EVENTS,
  TUNNEL_STATES,
  emptyVpsProfileFile,
  type ConnectionFault,
  type FaultCode,
  type TunnelEvent,
  type TunnelSnapshot,
  type TunnelState,
  type VpsProfile,
  type VpsProfileFile,
} from "./types.ts";

export {
  DEFAULT_BACKOFF,
  backoffSequence,
  computeBackoffMs,
  type BackoffConfig,
} from "./backoff.ts";

export {
  VpsProfileError,
  addProfile,
  findActiveProfile,
  makeProfile,
  readProfileFile,
  removeProfile,
  setActiveProfile,
  writeProfileFile,
  type CreateProfileInput,
  type ProfileStorage,
} from "./profile.ts";

export {
  dispatch,
  dispatchAll,
  enumerateTransitions,
  freshSnapshot,
  type DispatchContext,
  type DispatchResult,
} from "./fsm.ts";

export {
  TunnelOrchestrator,
  type AuthDriver,
  type HeartbeatDriver,
  type HeartbeatStatus,
  type OpenTunnel,
  type ScheduledHandle,
  type Scheduler,
  type TunnelDriver,
  type TunnelOrchestratorOptions,
} from "./orchestrator.ts";

export {
  installSleepWakeMonitor,
  type PowerMonitorEvent,
  type PowerMonitorLike,
  type SleepWakeAuditEvent,
  type SleepWakeAuditEventKind,
  type SleepWakeAuditSink,
  type SleepWakeMonitorHandle,
  type SleepWakeMonitorOptions,
  type SleepWakeOrchestrator,
} from "./sleepWakeMonitor.ts";

export {
  APPLE_CAPTIVE_PORTAL_SUCCESS_HTML,
  CAPTIVE_PORTAL_PROBE_URL,
  classifyCaptivePortalResponse,
  coalesceNetworkSignals,
  detectMacNetworkSignals,
  hasRouteChanged,
  hasSsidChanged,
  hasVpnStateChanged,
  installMacNetworkMonitor,
  networkSignalMessage,
  parseAirportNetwork,
  parseDefaultRoute,
  parseInterfaceList,
  parseWifiDevice,
  probeCaptivePortal,
  sampleMacNetworkState,
  tunnelEventForNetworkSignal,
  vpnInterfacesFromList,
  type CaptivePortalClassification,
  type CaptivePortalFetcher,
  type CaptivePortalProbeResult,
  type CaptivePortalResponse,
  type CommandRunner,
  type DefaultRouteSnapshot,
  type ElectronNetLike,
  type MacNetworkSnapshot,
  type NetworkMonitorHandle,
  type NetworkMonitorOptions,
  type NetworkMonitorScheduler,
  type NetworkSignal,
  type NetworkSignalDetailValue,
  type NetworkSignalKind,
  type NetworkSignalOrchestrator,
  type NetworkSignalSink,
} from "./networkMonitor.ts";
