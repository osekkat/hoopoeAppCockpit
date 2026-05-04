// hp-m79e — format-helpers tests.

import { expect, test } from "bun:test";
import {
  TUNNEL_STATES,
  TUNNEL_STATE_LABELS,
  formatReconnectCountdown,
  tunnelAriaLabel,
  tunnelHealthDot,
  tunnelSeverity,
} from "./index.ts";

const FIXED_NOW = () => new Date("2026-05-04T03:00:00.000Z");

test("TUNNEL_STATE_LABELS: covers every TunnelState value", () => {
  for (const state of TUNNEL_STATES) {
    expect(TUNNEL_STATE_LABELS[state]).toBeDefined();
    expect(TUNNEL_STATE_LABELS[state].length).toBeGreaterThan(0);
  }
});

test("tunnelSeverity: every state maps to the expected bucket", () => {
  expect(tunnelSeverity("ready")).toBe("ok");
  expect(tunnelSeverity("degraded")).toBe("warning");
  expect(tunnelSeverity("reconnecting")).toBe("danger");
  expect(tunnelSeverity("disconnected")).toBe("danger");
  expect(tunnelSeverity("ssh_probing")).toBe("in-flight");
  expect(tunnelSeverity("bootstrapping")).toBe("in-flight");
  expect(tunnelSeverity("tunnel_connecting")).toBe("in-flight");
  expect(tunnelSeverity("authenticating")).toBe("in-flight");
  expect(tunnelSeverity("unconfigured")).toBe("idle");
});

test("tunnelHealthDot: severity → HealthDot mapping for the ToolHealthPill", () => {
  expect(tunnelHealthDot("ready")).toBe("healthy");
  expect(tunnelHealthDot("degraded")).toBe("degraded");
  expect(tunnelHealthDot("reconnecting")).toBe("offline");
  expect(tunnelHealthDot("disconnected")).toBe("offline");
  expect(tunnelHealthDot("ssh_probing")).toBe("degraded");
  expect(tunnelHealthDot("unconfigured")).toBe("unknown");
});

test("formatReconnectCountdown: null + invalid timestamp → null", () => {
  expect(formatReconnectCountdown(null)).toBeNull();
  expect(formatReconnectCountdown("not-a-date")).toBeNull();
});

test("formatReconnectCountdown: deadline passed → null (no negative countdown)", () => {
  expect(formatReconnectCountdown("2026-05-04T02:59:00.000Z", FIXED_NOW)).toBeNull();
  expect(formatReconnectCountdown("2026-05-04T03:00:00.000Z", FIXED_NOW)).toBeNull();
});

test("formatReconnectCountdown: scales seconds + minutes", () => {
  expect(formatReconnectCountdown("2026-05-04T03:00:00.500Z", FIXED_NOW)).toBe("retry in <1s");
  expect(formatReconnectCountdown("2026-05-04T03:00:05.000Z", FIXED_NOW)).toBe("retry in 5s");
  expect(formatReconnectCountdown("2026-05-04T03:00:30.000Z", FIXED_NOW)).toBe("retry in 30s");
  expect(formatReconnectCountdown("2026-05-04T03:01:30.000Z", FIXED_NOW)).toBe("retry in 2m");
  expect(formatReconnectCountdown("2026-05-04T03:05:00.000Z", FIXED_NOW)).toBe("retry in 5m");
});

test("tunnelAriaLabel: composes state, fault, attempt, countdown", () => {
  expect(
    tunnelAriaLabel({
      state: "ready",
      fault: null,
      countdown: null,
      reconnectAttempts: 0,
    }),
  ).toBe("Tunnel Connected");
  expect(
    tunnelAriaLabel({
      state: "reconnecting",
      fault: { code: "tunnel_dropped", message: "Tunnel closed unexpectedly" },
      countdown: "retry in 30s",
      reconnectAttempts: 3,
    }),
  ).toBe(
    "Tunnel Reconnecting, fault tunnel_dropped: Tunnel closed unexpectedly, attempt 3, retry in 30s",
  );
});

test("tunnelAriaLabel: skips attempt segment outside reconnecting/disconnected states", () => {
  const label = tunnelAriaLabel({
    state: "ready",
    fault: null,
    countdown: null,
    reconnectAttempts: 5, // shouldn't appear
  });
  expect(label).toBe("Tunnel Connected");
});
