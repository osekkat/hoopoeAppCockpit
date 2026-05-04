// hp-e7k — Phase 2 SSH tunnel manager shared types.
//
// Maps to plan.md §2.5 'transport ladder' + §5.3 'one narrow exception':
//   The desktop opens an SSH tunnel from a local port → daemon-127.0.0.1:<port>
//   on the VPS. All HTTPS/WS traffic to the daemon goes through that tunnel.
//   Hoopoe never invokes project-level git/br/bv/ntm/shell commands — the
//   only direct exec from the desktop is `ssh` for the tunnel itself plus
//   read-only git plumbing on the desktop's local clone (§7.7).
//
// The FSM in `fsm.ts` is the single source of truth for connection state.
// Real ssh2 + PowerMonitor + heartbeat wiring live in a follow-up
// orchestrator bead; this module is pure data + state transitions.

/** §2.5 'ConnectionManager FSM' — the canonical state set. */
export const TUNNEL_STATES = [
  /** No VPS profile selected yet. */
  "unconfigured",
  /** Probing SSH reachability (host is up, port 22 responds). */
  "ssh_probing",
  /** Pushing the daemon binary + systemd unit + initial pairing token. */
  "bootstrapping",
  /** Opening the local-port → VPS:127.0.0.1:<daemon> SSH tunnel. */
  "tunnel_connecting",
  /** Tunnel is up; exchanging pairing → bearer or proving bearer. */
  "authenticating",
  /** Healthy: tunnel + bearer + heartbeat all good. */
  "ready",
  /** Tunnel up but a downstream signal degraded (heartbeat slow, version
   *  mismatch warning, etc.) — UI shows the project as degraded but does
   *  NOT block requests. */
  "degraded",
  /** Tunnel died; backoff timer is running before the next attempt. */
  "reconnecting",
  /** Explicit user disconnect / app quit. */
  "disconnected",
] as const;
export type TunnelState = (typeof TUNNEL_STATES)[number];

/** Triggers the FSM responds to. Names are stable + grep-friendly. */
export const TUNNEL_EVENTS = [
  "profile_set",          // user chose / created a VPS profile
  "profile_cleared",      // user removed the profile
  "ssh_probe_succeeded",
  "ssh_probe_failed",
  "bootstrap_succeeded",
  "bootstrap_failed",
  "tunnel_opened",
  "tunnel_closed",        // ssh2 'close' / 'end' / 'error'
  "auth_succeeded",
  "auth_failed",
  "heartbeat_ok",
  "heartbeat_timeout",
  "version_mismatch",
  "bearer_expired",       // HTTP 401 on a privileged call
  "network_changed",      // macOS Wi-Fi / VPN / captive-portal probe
  "system_sleep",         // powerMonitor 'suspend'
  "system_wake",          // powerMonitor 'resume'
  "user_disconnect",
  "user_reconnect",
  "backoff_elapsed",      // reconnect timer fired
] as const;
export type TunnelEvent = (typeof TUNNEL_EVENTS)[number];

/** Stable error code set the FSM stamps on `lastFault`. */
export const FAULT_CODES = [
  "ssh_unreachable",
  "ssh_auth_required",
  "ssh_fingerprint_changed",
  "bootstrap_install_failed",
  "tunnel_dropped",
  "auth_rejected",
  "version_incompatible",
  "heartbeat_timeout",
  "network_unavailable",
  "system_sleep",
  "user_initiated",
  "unknown",
] as const;
export type FaultCode = (typeof FAULT_CODES)[number];

export interface ConnectionFault {
  readonly code: FaultCode;
  readonly message: string;
  /** RFC3339 timestamp of when the fault was recorded. */
  readonly capturedAt: string;
}

/** A VPS connection profile. Non-secret material only — the SSH passphrase
 *  goes through SecretStore (Phase 1 Keychain task). */
export interface VpsProfile {
  /** Stable id (UUID). */
  readonly id: string;
  /** Human-readable label. */
  readonly label: string;
  /** SSH host (DNS or IP). */
  readonly host: string;
  /** SSH port; default 22. */
  readonly port: number;
  /** SSH username. */
  readonly username: string;
  /** Path to private key on the user's Mac (~/.ssh/id_ed25519 etc.). */
  readonly privateKeyPath: string;
  /** Daemon binary URL the bootstrapper installed on the VPS. */
  readonly daemonBinaryUrl: string | null;
  /** Local port preference for the tunnel (the manager picks an
   *  ephemeral fallback if it's taken). */
  readonly preferredLocalPort: number;
  /** RFC3339. */
  readonly createdAt: string;
  readonly updatedAt: string;
}

export const PROFILE_SCHEMA_VERSION = 1 as const;

export interface VpsProfileFile {
  readonly schemaVersion: typeof PROFILE_SCHEMA_VERSION;
  readonly profiles: readonly VpsProfile[];
  /** Active profile id (which one the connection manager is using). */
  readonly activeProfileId: string | null;
}

export function emptyVpsProfileFile(): VpsProfileFile {
  return {
    schemaVersion: PROFILE_SCHEMA_VERSION,
    profiles: [],
    activeProfileId: null,
  };
}

/** Snapshot of the tunnel manager's internal state. The renderer reads
 *  via daemon-RPC subscription; the FSM produces fresh snapshots after
 *  every transition. */
export interface TunnelSnapshot {
  readonly state: TunnelState;
  /** Active profile id, when any. */
  readonly activeProfileId: string | null;
  /** Local port the tunnel is forwarding (when known). */
  readonly localPort: number | null;
  /** Most recent fault, cleared once we re-enter `ready`. */
  readonly lastFault: ConnectionFault | null;
  /** Reconnect attempt counter (resets on `ready`). */
  readonly reconnectAttempts: number;
  /** When the next backoff fires (RFC3339), null when not in
   *  `reconnecting`. */
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
