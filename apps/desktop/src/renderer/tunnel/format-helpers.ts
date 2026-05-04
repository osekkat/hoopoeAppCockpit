// hp-m79e — Pure formatters for the ConnectionStatus pill.
//
// Kept separate from the React component so the rendering tests can
// pin expected strings without dragging the component + Lucide icons
// into the assertion surface.

import type { TunnelState } from "./tunnel-store.ts";

export const TUNNEL_STATE_LABELS: Record<TunnelState, string> = {
  unconfigured: "No VPS",
  ssh_probing: "Probing SSH",
  bootstrapping: "Bootstrapping",
  tunnel_connecting: "Opening tunnel",
  authenticating: "Authenticating",
  ready: "Connected",
  degraded: "Degraded",
  reconnecting: "Reconnecting",
  disconnected: "Disconnected",
};

/** Coarse severity bucket. ToolHealthPill's VPS dot reads from the
 *  same enum so the cockpit chrome stays consistent. */
export type TunnelSeverity = "ok" | "warning" | "danger" | "idle" | "in-flight";

export function tunnelSeverity(state: TunnelState): TunnelSeverity {
  switch (state) {
    case "ready":
      return "ok";
    case "degraded":
      return "warning";
    case "reconnecting":
    case "disconnected":
      return "danger";
    case "ssh_probing":
    case "bootstrapping":
    case "tunnel_connecting":
    case "authenticating":
      return "in-flight";
    case "unconfigured":
    default:
      return "idle";
  }
}

/** Map severity to the same HealthDot enum the ToolHealthPill consumes
 *  so the VPS dot can swap from the seed "unknown" to the live FSM
 *  signal in a single line of glue. */
export function tunnelHealthDot(state: TunnelState): "healthy" | "degraded" | "offline" | "unknown" {
  switch (tunnelSeverity(state)) {
    case "ok": return "healthy";
    case "warning": return "degraded";
    case "danger": return "offline";
    case "in-flight": return "degraded";
    case "idle": return "unknown";
    default: return "unknown";
  }
}

/** Format the reconnect countdown ("retry in 12s") as a human-readable
 *  remaining-time string. Returns null when:
 *   - nextRetryAt is null
 *   - nextRetryAt is invalid
 *   - the deadline has already passed (no countdown left). */
export function formatReconnectCountdown(
  nextRetryAt: string | null,
  now: () => Date = () => new Date(),
): string | null {
  if (!nextRetryAt) return null;
  const ts = Date.parse(nextRetryAt);
  if (Number.isNaN(ts)) return null;
  const deltaMs = ts - now().getTime();
  if (deltaMs <= 0) return null;
  if (deltaMs < 1_000) return "retry in <1s";
  const seconds = Math.round(deltaMs / 1_000);
  if (seconds < 60) return `retry in ${seconds}s`;
  const minutes = Math.round(seconds / 60);
  return `retry in ${minutes}m`;
}

/** Aria text for screen readers: combines state label, fault, and
 *  countdown into a single sentence. */
export function tunnelAriaLabel(input: {
  readonly state: TunnelState;
  readonly fault: { readonly code: string; readonly message: string } | null;
  readonly countdown: string | null;
  readonly reconnectAttempts: number;
}): string {
  const parts: string[] = [`Tunnel ${TUNNEL_STATE_LABELS[input.state]}`];
  if (input.fault) {
    parts.push(`fault ${input.fault.code}: ${input.fault.message}`);
  }
  if (input.reconnectAttempts > 0 && (input.state === "reconnecting" || input.state === "disconnected")) {
    parts.push(`attempt ${input.reconnectAttempts}`);
  }
  if (input.countdown) parts.push(input.countdown);
  return parts.join(", ");
}
