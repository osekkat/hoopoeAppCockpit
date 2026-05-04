// hp-m79e — ConnectionStatus top-bar pill.
//
// Reads from the tunnel store (hp-m79e/tunnel-store.ts) which is
// populated by TunnelSubscription.tsx (subscribes to events.tunnel)
// + the daemon `tunnel.snapshot` RPC for initial hydration.

import { useEffect, useMemo, useState } from "react";
import {
  AlertTriangle,
  CheckCircle2,
  CircleHelp,
  Loader2,
  Plug,
  PlugZap,
} from "lucide-react";
import {
  TUNNEL_STATE_LABELS,
  formatReconnectCountdown,
  tunnelAriaLabel,
  tunnelSnapshotSeverity,
} from "./format-helpers.ts";
import {
  selectTunnelSnapshot,
  useTunnelStore,
  type TunnelState,
} from "./tunnel-store.ts";

export interface ConnectionStatusPillProps {
  /** Override the clock for tests so the countdown renders deterministically. */
  readonly now?: (() => Date) | undefined;
}

export function ConnectionStatusPill({ now }: ConnectionStatusPillProps) {
  const snapshot = useTunnelStore(selectTunnelSnapshot);
  return (
    <ConnectionStatusPillView
      {...(now !== undefined ? { now } : {})}
      snapshot={snapshot}
    />
  );
}

/** Presentational form (test seam — accepts the snapshot directly so
 *  Zustand's SSR-snapshot semantics don't trip up renderToStaticMarkup). */
export function ConnectionStatusPillView({
  now,
  snapshot,
}: ConnectionStatusPillProps & { readonly snapshot: ReturnType<typeof selectTunnelSnapshot> }) {
  // Re-render the countdown on a 1Hz tick when applicable so the user
  // sees the timer drop. Falls back to no-op when no countdown applies.
  const [, setTick] = useState(0);
  useCountdownTick(snapshot.nextRetryAt, () => setTick((t) => t + 1));

  const countdown = useMemo(() => formatReconnectCountdown(snapshot.nextRetryAt, now), [snapshot.nextRetryAt, now]);
  const severity = tunnelSnapshotSeverity(snapshot);
  const aria = tunnelAriaLabel({
    state: snapshot.state,
    fault: snapshot.lastFault,
    countdown,
    reconnectAttempts: snapshot.reconnectAttempts,
  });

  const Icon = iconForState(snapshot.state);

  return (
    <span
      aria-label={aria}
      className="hh-topbar-pill hh-connection-status-pill"
      data-severity={severity}
      data-state={snapshot.state}
      data-testid="topbar-connection-status"
    >
      <Icon
        aria-hidden="true"
        className={severity === "in-flight" ? "hh-spin" : undefined}
        size={14}
        strokeWidth={2.1}
      />
      <span>{TUNNEL_STATE_LABELS[snapshot.state]}</span>
      {snapshot.reconnectAttempts > 0 && (snapshot.state === "reconnecting" || snapshot.state === "disconnected") ? (
        <strong>·{" #"}{snapshot.reconnectAttempts}</strong>
      ) : null}
      {countdown !== null ? (
        <small data-testid="topbar-connection-countdown">{countdown}</small>
      ) : null}
      {snapshot.lastFault && (severity === "danger" || severity === "warning") ? (
        <em
          aria-hidden="true"
          className="hh-connection-fault-code"
          data-testid="topbar-connection-fault-code"
        >
          {snapshot.lastFault.code}
        </em>
      ) : null}
    </span>
  );
}

function iconForState(state: TunnelState): typeof Plug {
  switch (state) {
    case "ready":
    case "degraded":
      return CheckCircle2;
    case "reconnecting":
    case "ssh_probing":
    case "bootstrapping":
    case "tunnel_connecting":
    case "authenticating":
      return Loader2;
    case "awaiting_network":
    case "captive_portal_blocked":
    case "disconnected":
      return AlertTriangle;
    case "unconfigured":
      return CircleHelp;
    default:
      return PlugZap;
  }
}

/** Re-render driver: when nextRetryAt is set + in the future, schedule
 *  a refresh once per second so the countdown ticks down. The effect
 *  bails out (no-op) when the deadline has passed or isn't set. */
function useCountdownTick(nextRetryAt: string | null, refresh: () => void) {
  useEffect(() => {
    if (!nextRetryAt) return;
    const ts = Date.parse(nextRetryAt);
    if (Number.isNaN(ts)) return;
    if (ts - Date.now() <= 0) return;
    const id = setInterval(refresh, 1_000);
    return () => clearInterval(id);
  }, [nextRetryAt, refresh]);
}
