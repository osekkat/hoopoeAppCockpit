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
