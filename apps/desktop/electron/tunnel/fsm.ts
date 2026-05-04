// hp-e7k — ConnectionManager FSM (pure).
//
// Plan.md §2.5 'transport ladder' state machine. The machine is a pure
// function over (state, event) → next state. All side effects (real
// ssh2, PowerMonitor, heartbeat timers, IPC) live in a follow-up
// orchestrator that wraps this FSM.
//
// Snapshot semantics:
//   - The FSM owns a `TunnelSnapshot` (state + activeProfileId + last
//     fault + reconnect attempts + nextRetryAt).
//   - `dispatch(event, ctx)` returns a new snapshot. Callers replace
//     their reference; the previous snapshot is unchanged.
//   - On entering `ready`, fault + attempts + nextRetryAt are cleared.
//   - On entering `reconnecting`, `nextRetryAt` is computed from the
//     backoff schedule using the current attempt count.

import { computeBackoffMs, type BackoffConfig } from "./backoff.ts";
import {
  INITIAL_TUNNEL_SNAPSHOT,
  TUNNEL_EVENTS,
  type ConnectionFault,
  type FaultCode,
  type TunnelEvent,
  type TunnelSnapshot,
  type TunnelState,
} from "./types.ts";

export interface DispatchContext {
  /** The profile id being activated by `profile_set` (required for that
   *  event; ignored otherwise). */
  readonly profileId?: string;
  /** Local port the tunnel forwarded to (set on `tunnel_opened`). */
  readonly localPort?: number;
  /** Free-form fault detail (used by *_failed / *_timeout / etc.). */
  readonly faultMessage?: string;
  /** Tests inject a fixed clock. */
  readonly now?: () => Date;
  /** Tests inject deterministic jitter via the backoff config. */
  readonly backoff?: BackoffConfig;
}

export interface DispatchResult {
  readonly snapshot: TunnelSnapshot;
  readonly transitioned: boolean;
}

/** Pure transition. Returns the new snapshot; never mutates the input.
 *  When a transition isn't legal for the current state, returns
 *  `{ snapshot, transitioned: false }` so callers can log + discard
 *  unexpected events without crashing the manager. */
export function dispatch(
  current: TunnelSnapshot,
  event: TunnelEvent,
  ctx: DispatchContext = {},
): DispatchResult {
  if (!TUNNEL_EVENTS.includes(event)) {
    return { snapshot: current, transitioned: false };
  }
  const transition = TRANSITIONS[current.state]?.[event];
  if (!transition) {
    return { snapshot: current, transitioned: false };
  }
  const now = ctx.now ?? (() => new Date());
  const next = transition(current, ctx, now);
  return { snapshot: next, transitioned: next.state !== current.state || next !== current };
}

/** Convenience for tests that drive long event sequences. */
export function dispatchAll(
  start: TunnelSnapshot,
  events: ReadonlyArray<{ readonly event: TunnelEvent; readonly ctx?: DispatchContext }>,
): TunnelSnapshot {
  let snapshot = start;
  for (const { event, ctx } of events) {
    snapshot = dispatch(snapshot, event, ctx).snapshot;
  }
  return snapshot;
}

export function freshSnapshot(): TunnelSnapshot {
  return INITIAL_TUNNEL_SNAPSHOT;
}

// ── Transition table ─────────────────────────────────────────────────────

type Transition = (current: TunnelSnapshot, ctx: DispatchContext, now: () => Date) => TunnelSnapshot;

function gotoState(state: TunnelState): Transition {
  return (current) => ({ ...current, state });
}

function gotoReady(localPortFromCtx = false): Transition {
  return (current, ctx) => ({
    ...current,
    state: "ready",
    lastFault: null,
    reconnectAttempts: 0,
    nextRetryAt: null,
    localPort: localPortFromCtx && ctx.localPort !== undefined ? ctx.localPort : current.localPort,
  });
}

function recordFault(code: FaultCode, fallbackMessage: string): Transition {
  return (current, ctx, now) => {
    const fault: ConnectionFault = {
      code,
      message: ctx.faultMessage ?? fallbackMessage,
      capturedAt: now().toISOString(),
    };
    return { ...current, lastFault: fault };
  };
}

function startReconnect(code: FaultCode, fallbackMessage: string): Transition {
  return (current, ctx, now) => {
    const attempts = current.reconnectAttempts + 1;
    const delayMs = computeBackoffMs(attempts - 1, ctx.backoff);
    const nextRetryAt = new Date(now().getTime() + delayMs).toISOString();
    return {
      ...current,
      state: "reconnecting",
      reconnectAttempts: attempts,
      nextRetryAt,
      lastFault: {
        code,
        message: ctx.faultMessage ?? fallbackMessage,
        capturedAt: now().toISOString(),
      },
      localPort: null,
    };
  };
}

function startImmediateReconnect(code: FaultCode, fallbackMessage: string): Transition {
  return (current, ctx, now) => {
    const attempts = current.reconnectAttempts + 1;
    return {
      ...current,
      state: "reconnecting",
      reconnectAttempts: attempts,
      nextRetryAt: now().toISOString(),
      lastFault: {
        code,
        message: ctx.faultMessage ?? fallbackMessage,
        capturedAt: now().toISOString(),
      },
      localPort: null,
    };
  };
}

function awaitNetwork(): Transition {
  return (current, ctx, now) => ({
    ...current,
    state: "awaiting_network",
    nextRetryAt: null,
    localPort: null,
    lastFault: {
      code: "network_unavailable",
      message: ctx.faultMessage ?? "Network unavailable",
      capturedAt: now().toISOString(),
    },
  });
}

function blockOnCaptivePortal(): Transition {
  return (current, ctx, now) => ({
    ...current,
    state: "captive_portal_blocked",
    nextRetryAt: null,
    localPort: null,
    lastFault: {
      code: "network_captive_portal",
      message: ctx.faultMessage ?? "Captive portal detected",
      capturedAt: now().toISOString(),
    },
  });
}

function setProfile(): Transition {
  return (current, ctx) => ({
    ...current,
    state: "ssh_probing",
    activeProfileId: ctx.profileId ?? current.activeProfileId,
    lastFault: null,
    reconnectAttempts: 0,
    nextRetryAt: null,
    localPort: null,
  });
}

function clearProfile(): Transition {
  return () => INITIAL_TUNNEL_SNAPSHOT;
}

function userDisconnect(): Transition {
  return (current, _ctx, now) => ({
    ...current,
    state: "disconnected",
    nextRetryAt: null,
    localPort: null,
    lastFault: {
      code: "user_initiated",
      message: "User disconnected",
      capturedAt: now().toISOString(),
    },
  });
}

const TRANSITIONS: Partial<Record<TunnelState, Partial<Record<TunnelEvent, Transition>>>> = {
  unconfigured: {
    profile_set: setProfile(),
    user_reconnect: setProfile(), // No profile yet ⇒ effectively a no-op via setProfile fallback
  },
  ssh_probing: {
    profile_cleared: clearProfile(),
    ssh_probe_succeeded: gotoState("bootstrapping"),
    ssh_probe_failed: startReconnect("ssh_unreachable", "SSH probe failed"),
    user_disconnect: userDisconnect(),
    network_changed: startReconnect("network_unavailable", "Network changed"),
    network_offline: awaitNetwork(),
    network_route_changed: startImmediateReconnect("network_unavailable", "Network route changed"),
    network_vpn_state_changed: startImmediateReconnect("network_unavailable", "VPN state changed"),
    network_captive_portal_detected: blockOnCaptivePortal(),
    system_sleep: gotoState("disconnected"),
  },
  bootstrapping: {
    profile_cleared: clearProfile(),
    bootstrap_succeeded: gotoState("tunnel_connecting"),
    bootstrap_failed: startReconnect("bootstrap_install_failed", "Bootstrap failed"),
    user_disconnect: userDisconnect(),
    network_changed: startReconnect("network_unavailable", "Network changed"),
    network_offline: awaitNetwork(),
    network_route_changed: startImmediateReconnect("network_unavailable", "Network route changed"),
    network_vpn_state_changed: startImmediateReconnect("network_unavailable", "VPN state changed"),
    network_captive_portal_detected: blockOnCaptivePortal(),
    system_sleep: gotoState("disconnected"),
  },
  tunnel_connecting: {
    profile_cleared: clearProfile(),
    tunnel_opened: gotoState("authenticating"),
    tunnel_closed: startReconnect("tunnel_dropped", "Tunnel closed before authentication"),
    user_disconnect: userDisconnect(),
    network_changed: startReconnect("network_unavailable", "Network changed"),
    network_offline: awaitNetwork(),
    network_route_changed: startImmediateReconnect("network_unavailable", "Network route changed"),
    network_vpn_state_changed: startImmediateReconnect("network_unavailable", "VPN state changed"),
    network_captive_portal_detected: blockOnCaptivePortal(),
    system_sleep: gotoState("disconnected"),
  },
  authenticating: {
    profile_cleared: clearProfile(),
    auth_succeeded: gotoReady(true),
    auth_failed: recordFault("auth_rejected", "Auth rejected"),
    tunnel_closed: startReconnect("tunnel_dropped", "Tunnel dropped during auth"),
    user_disconnect: userDisconnect(),
    network_changed: startReconnect("network_unavailable", "Network changed"),
    network_offline: awaitNetwork(),
    network_route_changed: startImmediateReconnect("network_unavailable", "Network route changed"),
    network_vpn_state_changed: startImmediateReconnect("network_unavailable", "VPN state changed"),
    network_captive_portal_detected: blockOnCaptivePortal(),
    system_sleep: gotoState("disconnected"),
  },
  ready: {
    profile_cleared: clearProfile(),
    heartbeat_timeout: startReconnect("heartbeat_timeout", "Heartbeat timed out"),
    tunnel_closed: startReconnect("tunnel_dropped", "Tunnel closed unexpectedly"),
    bearer_expired: gotoState("authenticating"),
    version_mismatch: (current, ctx, now) => ({
      ...current,
      state: "degraded",
      lastFault: {
        code: "version_incompatible",
        message: ctx.faultMessage ?? "Daemon API version incompatible",
        capturedAt: now().toISOString(),
      },
    }),
    network_changed: startReconnect("network_unavailable", "Network changed"),
    network_offline: awaitNetwork(),
    network_route_changed: startImmediateReconnect("network_unavailable", "Network route changed"),
    network_vpn_state_changed: startImmediateReconnect("network_unavailable", "VPN state changed"),
    network_captive_portal_detected: blockOnCaptivePortal(),
    system_sleep: gotoState("disconnected"),
    user_disconnect: userDisconnect(),
  },
  awaiting_network: {
    profile_cleared: clearProfile(),
    network_online: startImmediateReconnect("network_unavailable", "Network back online"),
    network_changed: startImmediateReconnect("network_unavailable", "Network changed"),
    network_route_changed: startImmediateReconnect("network_unavailable", "Network route changed"),
    network_vpn_state_changed: startImmediateReconnect("network_unavailable", "VPN state changed"),
    network_captive_portal_detected: blockOnCaptivePortal(),
    user_disconnect: userDisconnect(),
    system_sleep: gotoState("disconnected"),
  },
  captive_portal_blocked: {
    profile_cleared: clearProfile(),
    network_captive_portal_cleared: startImmediateReconnect("network_unavailable", "Captive portal cleared"),
    network_online: startImmediateReconnect("network_unavailable", "Network back online"),
    network_changed: startImmediateReconnect("network_unavailable", "Network changed"),
    network_offline: awaitNetwork(),
    user_disconnect: userDisconnect(),
    system_sleep: gotoState("disconnected"),
  },
  degraded: {
    profile_cleared: clearProfile(),
    heartbeat_ok: gotoReady(),
    heartbeat_timeout: startReconnect("heartbeat_timeout", "Heartbeat timed out"),
    tunnel_closed: startReconnect("tunnel_dropped", "Tunnel closed during degraded state"),
    network_changed: startReconnect("network_unavailable", "Network changed"),
    network_offline: awaitNetwork(),
    network_route_changed: startImmediateReconnect("network_unavailable", "Network route changed"),
    network_vpn_state_changed: startImmediateReconnect("network_unavailable", "VPN state changed"),
    network_captive_portal_detected: blockOnCaptivePortal(),
    user_disconnect: userDisconnect(),
    system_sleep: gotoState("disconnected"),
  },
  reconnecting: {
    profile_cleared: clearProfile(),
    backoff_elapsed: gotoState("ssh_probing"),
    user_reconnect: gotoState("ssh_probing"),
    network_offline: awaitNetwork(),
    network_online: startImmediateReconnect("network_unavailable", "Network back online"),
    network_route_changed: startImmediateReconnect("network_unavailable", "Network route changed"),
    network_vpn_state_changed: startImmediateReconnect("network_unavailable", "VPN state changed"),
    network_captive_portal_detected: blockOnCaptivePortal(),
    user_disconnect: userDisconnect(),
    system_sleep: gotoState("disconnected"),
  },
  disconnected: {
    profile_cleared: clearProfile(),
    user_reconnect: gotoState("ssh_probing"),
    system_wake: gotoState("ssh_probing"),
    profile_set: setProfile(),
  },
};

/** Diagnostic helper: enumerate every legal (state, event, next-state)
 *  triple. Used by tests to assert transition coverage. */
export function enumerateTransitions(): ReadonlyArray<{
  readonly from: TunnelState;
  readonly event: TunnelEvent;
}> {
  const out: Array<{ from: TunnelState; event: TunnelEvent }> = [];
  for (const fromKey of Object.keys(TRANSITIONS)) {
    const from = fromKey as TunnelState;
    const eventMap = TRANSITIONS[from] ?? {};
    for (const eventKey of Object.keys(eventMap)) {
      out.push({ from, event: eventKey as TunnelEvent });
    }
  }
  return out;
}
