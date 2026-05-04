// hp-m79e — ConnectionStatusPill render tests.
//
// Uses the presentational ConnectionStatusPillView to bypass Zustand's
// SSR snapshot semantics. Same pattern as DirtyBanner / DirtyBannerView.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  ConnectionStatusPillView,
  INITIAL_TUNNEL_SNAPSHOT,
  type TunnelSnapshot,
} from "./index.ts";

const FIXED_NOW = () => new Date("2026-05-04T03:00:00.000Z");

const READY: TunnelSnapshot = {
  state: "ready",
  activeProfileId: "profile-1",
  localPort: 17655,
  lastFault: null,
  reconnectAttempts: 0,
  nextRetryAt: null,
};

const RECONNECTING: TunnelSnapshot = {
  state: "reconnecting",
  activeProfileId: "profile-1",
  localPort: null,
  lastFault: { code: "tunnel_dropped", message: "Tunnel closed unexpectedly", capturedAt: "2026-05-04T03:00:00.000Z" },
  reconnectAttempts: 3,
  nextRetryAt: "2026-05-04T03:00:30.000Z",
};

const DEGRADED: TunnelSnapshot = {
  state: "degraded",
  activeProfileId: "profile-1",
  localPort: 17655,
  lastFault: { code: "version_incompatible", message: "API v2 vs daemon v1", capturedAt: "2026-05-04T03:00:00.000Z" },
  reconnectAttempts: 0,
  nextRetryAt: null,
};

test("ConnectionStatusPill: unconfigured snapshot shows 'No VPS' with idle severity", () => {
  const html = renderToStaticMarkup(
    <ConnectionStatusPillView now={FIXED_NOW} snapshot={INITIAL_TUNNEL_SNAPSHOT} />,
  );
  expect(html).toContain("data-testid=\"topbar-connection-status\"");
  expect(html).toContain("data-state=\"unconfigured\"");
  expect(html).toContain("data-severity=\"idle\"");
  expect(html).toContain("No VPS");
  expect(html).not.toContain("data-testid=\"topbar-connection-countdown\"");
  expect(html).not.toContain("data-testid=\"topbar-connection-fault-code\"");
});

test("ConnectionStatusPill: ready snapshot shows 'Connected' + ok severity, no fault badge", () => {
  const html = renderToStaticMarkup(
    <ConnectionStatusPillView now={FIXED_NOW} snapshot={READY} />,
  );
  expect(html).toContain("data-state=\"ready\"");
  expect(html).toContain("data-severity=\"ok\"");
  expect(html).toContain("Connected");
  expect(html).not.toContain("data-testid=\"topbar-connection-fault-code\"");
});

test("ConnectionStatusPill: reconnecting snapshot shows attempt count + countdown + fault code", () => {
  const html = renderToStaticMarkup(
    <ConnectionStatusPillView now={FIXED_NOW} snapshot={RECONNECTING} />,
  );
  expect(html).toContain("data-state=\"reconnecting\"");
  expect(html).toContain("data-severity=\"danger\"");
  expect(html).toContain("Reconnecting");
  expect(html).toContain("data-testid=\"topbar-connection-countdown\"");
  expect(html).toContain("retry in 30s");
  // Attempt counter ("· #3") rendered.
  expect(html).toContain("#3");
  // Fault code badge appears.
  expect(html).toContain("data-testid=\"topbar-connection-fault-code\"");
  expect(html).toContain("tunnel_dropped");
});

test("ConnectionStatusPill: degraded snapshot shows warning severity + fault badge but no countdown", () => {
  const html = renderToStaticMarkup(
    <ConnectionStatusPillView now={FIXED_NOW} snapshot={DEGRADED} />,
  );
  expect(html).toContain("data-state=\"degraded\"");
  expect(html).toContain("data-severity=\"warning\"");
  expect(html).toContain("Degraded");
  expect(html).toContain("data-testid=\"topbar-connection-fault-code\"");
  expect(html).toContain("version_incompatible");
  expect(html).not.toContain("data-testid=\"topbar-connection-countdown\"");
});

test("ConnectionStatusPill: in-flight states render with the spinning icon class", () => {
  const html = renderToStaticMarkup(
    <ConnectionStatusPillView now={FIXED_NOW} snapshot={{ ...INITIAL_TUNNEL_SNAPSHOT, state: "ssh_probing" }} />,
  );
  expect(html).toContain("data-severity=\"in-flight\"");
  expect(html).toContain("hh-spin");
});

test("ConnectionStatusPill: aria label composes state, fault, attempt, countdown", () => {
  const html = renderToStaticMarkup(
    <ConnectionStatusPillView now={FIXED_NOW} snapshot={RECONNECTING} />,
  );
  expect(html).toContain("aria-label=\"Tunnel Reconnecting, fault tunnel_dropped: Tunnel closed unexpectedly, attempt 3, retry in 30s\"");
});

test("ConnectionStatusPill: every TunnelState renders without throwing", () => {
  const states = [
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
  for (const state of states) {
    const snapshot: TunnelSnapshot = {
      ...INITIAL_TUNNEL_SNAPSHOT,
      state,
      reconnectAttempts: state === "reconnecting" ? 1 : 0,
    };
    const html = renderToStaticMarkup(
      <ConnectionStatusPillView now={FIXED_NOW} snapshot={snapshot} />,
    );
    expect(html).toContain(`data-state="${state}"`);
  }
});
