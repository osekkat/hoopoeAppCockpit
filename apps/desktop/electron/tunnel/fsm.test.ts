// hp-e7k — ConnectionManager FSM tests.

import { expect, test } from "bun:test";
import {
  TUNNEL_STATES,
  dispatch,
  dispatchAll,
  enumerateTransitions,
  freshSnapshot,
  type DispatchContext,
  type TunnelEvent,
  type TunnelSnapshot,
} from "./index.ts";

const FIXED_NOW = () => new Date("2026-05-04T03:00:00Z");
const NO_JITTER: DispatchContext["backoff"] = {
  baseMs: 500,
  maxMs: 30_000,
  jitter: 0,
  random: () => 0.5,
};

test("freshSnapshot: starts in unconfigured with no profile", () => {
  const snapshot = freshSnapshot();
  expect(snapshot.state).toBe("unconfigured");
  expect(snapshot.activeProfileId).toBeNull();
  expect(snapshot.lastFault).toBeNull();
  expect(snapshot.reconnectAttempts).toBe(0);
});

test("dispatch: profile_set in unconfigured advances to ssh_probing", () => {
  const result = dispatch(freshSnapshot(), "profile_set", { profileId: "vps-1" });
  expect(result.transitioned).toBe(true);
  expect(result.snapshot.state).toBe("ssh_probing");
  expect(result.snapshot.activeProfileId).toBe("vps-1");
});

test("dispatch: unknown event for a state is a no-op (no crash)", () => {
  const result = dispatch(freshSnapshot(), "heartbeat_ok");
  expect(result.transitioned).toBe(false);
  expect(result.snapshot.state).toBe("unconfigured");
});

test("dispatch: invalid event string is rejected (no transition)", () => {
  const result = dispatch(freshSnapshot(), "garbage" as TunnelEvent);
  expect(result.transitioned).toBe(false);
  expect(result.snapshot).toBe(freshSnapshot());
});

test("happy-path sequence reaches ready", () => {
  const final = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "vps-1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
  ]);
  expect(final.state).toBe("ready");
  expect(final.localPort).toBe(17655);
  expect(final.lastFault).toBeNull();
  expect(final.reconnectAttempts).toBe(0);
});

test("ssh_probe_failed → reconnecting with attempt counter + backoff timestamp", () => {
  const ssh = dispatch(freshSnapshot(), "profile_set", { profileId: "v1" }).snapshot;
  const failed = dispatch(ssh, "ssh_probe_failed", {
    backoff: NO_JITTER,
    now: FIXED_NOW,
    faultMessage: "host unreachable",
  });
  expect(failed.snapshot.state).toBe("reconnecting");
  expect(failed.snapshot.reconnectAttempts).toBe(1);
  expect(failed.snapshot.lastFault?.code).toBe("ssh_unreachable");
  expect(failed.snapshot.lastFault?.message).toBe("host unreachable");
  // First attempt with jitter=0, base=500 → nextRetryAt is now + 500ms.
  expect(failed.snapshot.nextRetryAt).toBe("2026-05-04T03:00:00.500Z");
});

test("repeated reconnect attempts increment + back off doubles", () => {
  let snap: TunnelSnapshot = dispatch(freshSnapshot(), "profile_set", { profileId: "v1" }).snapshot;
  const ctx: DispatchContext = { backoff: NO_JITTER, now: FIXED_NOW };
  for (let i = 0; i < 4; i += 1) {
    snap = dispatch(snap, "ssh_probe_failed", ctx).snapshot;
    snap = dispatch(snap, "backoff_elapsed").snapshot;
  }
  // After 4 retries we should be back in ssh_probing with attempts=4.
  expect(snap.state).toBe("ssh_probing");
  expect(snap.reconnectAttempts).toBe(4);
});

test("entering ready clears fault + reconnect attempts + nextRetryAt", () => {
  let snap: TunnelSnapshot = dispatch(freshSnapshot(), "profile_set", { profileId: "v1" }).snapshot;
  snap = dispatch(snap, "ssh_probe_failed", { backoff: NO_JITTER, now: FIXED_NOW }).snapshot;
  expect(snap.lastFault).not.toBeNull();
  // Recover.
  snap = dispatch(snap, "backoff_elapsed").snapshot;
  snap = dispatch(snap, "ssh_probe_succeeded").snapshot;
  snap = dispatch(snap, "bootstrap_succeeded").snapshot;
  snap = dispatch(snap, "tunnel_opened", { localPort: 17655 }).snapshot;
  snap = dispatch(snap, "auth_succeeded", { localPort: 17655 }).snapshot;
  expect(snap.state).toBe("ready");
  expect(snap.lastFault).toBeNull();
  expect(snap.reconnectAttempts).toBe(0);
  expect(snap.nextRetryAt).toBeNull();
});

test("ready + heartbeat_timeout → reconnecting (full reconnect, not just degraded)", () => {
  let snap: TunnelSnapshot = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "v1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
  ]);
  snap = dispatch(snap, "heartbeat_timeout", { backoff: NO_JITTER, now: FIXED_NOW }).snapshot;
  expect(snap.state).toBe("reconnecting");
  expect(snap.lastFault?.code).toBe("heartbeat_timeout");
});

test("ready + version_mismatch → degraded (NOT reconnecting; non-blocking)", () => {
  let snap: TunnelSnapshot = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "v1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
  ]);
  snap = dispatch(snap, "version_mismatch", { now: FIXED_NOW, faultMessage: "API v2 client vs v1 daemon" }).snapshot;
  expect(snap.state).toBe("degraded");
  expect(snap.lastFault?.code).toBe("version_incompatible");
  // Heartbeat OK should bring us back to ready (clears the fault).
  snap = dispatch(snap, "heartbeat_ok").snapshot;
  expect(snap.state).toBe("ready");
  expect(snap.lastFault).toBeNull();
});

test("ready + bearer_expired → authenticating (refresh, not reconnect)", () => {
  let snap: TunnelSnapshot = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "v1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
  ]);
  snap = dispatch(snap, "bearer_expired").snapshot;
  expect(snap.state).toBe("authenticating");
});

test("system_sleep from ready → disconnected (no fault recorded)", () => {
  let snap: TunnelSnapshot = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "v1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
  ]);
  snap = dispatch(snap, "system_sleep").snapshot;
  expect(snap.state).toBe("disconnected");
  // system_wake → ssh_probing.
  snap = dispatch(snap, "system_wake").snapshot;
  expect(snap.state).toBe("ssh_probing");
});

test("network_changed in ready → reconnecting with network_unavailable fault", () => {
  let snap: TunnelSnapshot = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "v1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
  ]);
  snap = dispatch(snap, "network_changed", { backoff: NO_JITTER, now: FIXED_NOW }).snapshot;
  expect(snap.state).toBe("reconnecting");
  expect(snap.lastFault?.code).toBe("network_unavailable");
});

test("network_offline in ready → awaiting_network with reconnect paused", () => {
  let snap: TunnelSnapshot = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "v1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
  ]);
  snap = dispatch(snap, "network_offline", { now: FIXED_NOW }).snapshot;
  expect(snap.state).toBe("awaiting_network");
  expect(snap.localPort).toBeNull();
  expect(snap.nextRetryAt).toBeNull();
  expect(snap.lastFault).toMatchObject({
    code: "network_unavailable",
    message: "Network unavailable",
  });
});

test("awaiting_network + network_online → reconnecting immediately", () => {
  let snap: TunnelSnapshot = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "v1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
    { event: "network_offline", ctx: { now: FIXED_NOW } },
  ]);
  snap = dispatch(snap, "network_online", { now: FIXED_NOW }).snapshot;
  expect(snap.state).toBe("reconnecting");
  expect(snap.reconnectAttempts).toBe(1);
  expect(snap.nextRetryAt).toBe("2026-05-04T03:00:00.000Z");
  expect(snap.lastFault?.message).toBe("Network back online");
});

test("route and VPN changes force immediate reconnect without backoff delay", () => {
  let snap: TunnelSnapshot = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "v1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
  ]);
  snap = dispatch(snap, "network_route_changed", { now: FIXED_NOW }).snapshot;
  expect(snap.state).toBe("reconnecting");
  expect(snap.nextRetryAt).toBe("2026-05-04T03:00:00.000Z");
  expect(snap.lastFault?.message).toBe("Network route changed");

  snap = dispatch(snap, "network_vpn_state_changed", { now: FIXED_NOW }).snapshot;
  expect(snap.reconnectAttempts).toBe(2);
  expect(snap.nextRetryAt).toBe("2026-05-04T03:00:00.000Z");
  expect(snap.lastFault?.message).toBe("VPN state changed");
});

test("positive captive portal probe blocks reconnect until cleared", () => {
  let snap: TunnelSnapshot = dispatchAll(freshSnapshot(), [
    { event: "profile_set", ctx: { profileId: "v1" } },
    { event: "ssh_probe_succeeded" },
    { event: "bootstrap_succeeded" },
    { event: "tunnel_opened", ctx: { localPort: 17655 } },
    { event: "auth_succeeded", ctx: { localPort: 17655 } },
    { event: "network_captive_portal_detected", ctx: { now: FIXED_NOW } },
  ]);
  expect(snap.state).toBe("captive_portal_blocked");
  expect(snap.localPort).toBeNull();
  expect(snap.nextRetryAt).toBeNull();
  expect(snap.lastFault?.code).toBe("network_captive_portal");

  snap = dispatch(snap, "network_captive_portal_cleared", { now: FIXED_NOW }).snapshot;
  expect(snap.state).toBe("reconnecting");
  expect(snap.nextRetryAt).toBe("2026-05-04T03:00:00.000Z");
  expect(snap.lastFault?.message).toBe("Captive portal cleared");
});

test("user_disconnect from any non-unconfigured state → disconnected with user_initiated fault", () => {
  for (const start of ["ssh_probing", "tunnel_connecting", "authenticating", "ready", "awaiting_network", "captive_portal_blocked", "degraded"] as const) {
    let snap: TunnelSnapshot = dispatch(freshSnapshot(), "profile_set", { profileId: "v" }).snapshot;
    if (start !== "ssh_probing") snap = { ...snap, state: start };
    const after = dispatch(snap, "user_disconnect", { now: FIXED_NOW }).snapshot;
    expect(after.state).toBe("disconnected");
    expect(after.lastFault?.code).toBe("user_initiated");
  }
});

test("profile_cleared returns to unconfigured from anywhere", () => {
  for (const state of TUNNEL_STATES) {
    if (state === "unconfigured") continue;
    let snap: TunnelSnapshot = dispatch(freshSnapshot(), "profile_set", { profileId: "v" }).snapshot;
    snap = { ...snap, state };
    const after = dispatch(snap, "profile_cleared").snapshot;
    expect(after.state).toBe("unconfigured");
    expect(after.activeProfileId).toBeNull();
  }
});

test("disconnected + user_reconnect → ssh_probing", () => {
  let snap: TunnelSnapshot = dispatch(freshSnapshot(), "profile_set", { profileId: "v" }).snapshot;
  snap = dispatch(snap, "user_disconnect", { now: FIXED_NOW }).snapshot;
  expect(snap.state).toBe("disconnected");
  snap = dispatch(snap, "user_reconnect").snapshot;
  expect(snap.state).toBe("ssh_probing");
});

test("enumerateTransitions: every TUNNEL_STATES entry has at least one outgoing transition", () => {
  const transitions = enumerateTransitions();
  const seenFrom = new Set(transitions.map((t) => t.from));
  // unconfigured → profile_set + user_reconnect (which falls through to setProfile).
  expect(seenFrom.has("unconfigured")).toBe(true);
  for (const state of TUNNEL_STATES) {
    expect(seenFrom.has(state)).toBe(true);
  }
});
