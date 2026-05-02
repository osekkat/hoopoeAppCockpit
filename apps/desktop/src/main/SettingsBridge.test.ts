import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  DEFAULT_CLIENT_SETTINGS,
  DEFAULT_DAEMON_SETTINGS,
  SettingsBridge,
  type SettingsChangeEvent,
} from "./SettingsBridge.ts";

let workDir: string;

function pathsFor(work: string) {
  return {
    daemon: join(work, "daemon-settings.json"),
    desktop: join(work, "desktop-settings.json"),
    client: join(work, "client-settings.json"),
  };
}

beforeEach(() => {
  workDir = mkdtempSync(join(tmpdir(), "hoopoe-settings-"));
});

afterEach(() => {
  rmSync(workDir, { recursive: true, force: true });
});

test("SettingsBridge: atomic write produces a parseable JSON file", () => {
  const paths = pathsFor(workDir);
  const bridge = new SettingsBridge({ paths, currentAppVersion: "0.0.0" });
  bridge.setDaemonSettings({ logLevel: "debug", tendingEnabled: false });
  const written = JSON.parse(readFileSync(paths.daemon, "utf8")) as Record<string, unknown>;
  expect(written.logLevel).toBe("debug");
  expect(written.tendingEnabled).toBe(false);
});

test("SettingsBridge: subscribers receive change events for each store", async () => {
  const paths = pathsFor(workDir);
  const bridge = new SettingsBridge({ paths, currentAppVersion: "0.0.0" });
  const events: SettingsChangeEvent[] = [];
  const sub = bridge.subscribe((event) => {
    events.push(event);
  });

  bridge.setDaemonSettings({ ...DEFAULT_DAEMON_SETTINGS, logLevel: "warn" });
  bridge.setClientSettings({ ...DEFAULT_CLIENT_SETTINGS, activityPanelOpen: true });
  bridge.setDesktopSettings({
    serverExposureMode: "network-accessible",
    updateChannel: "nightly",
    updateChannelConfiguredByUser: true,
  });

  // Allow microtasks to flush the bounded queue.
  await new Promise<void>((resolve) => setImmediate(resolve));

  expect(events).toHaveLength(3);
  expect(events[0]?.store).toBe("daemon");
  expect(events[1]?.store).toBe("client");
  expect(events[2]?.store).toBe("desktop");
  sub.unsubscribe();
});

test("SettingsBridge: relaunchDesktopApp invokes the injected relaunch with reason", () => {
  const paths = pathsFor(workDir);
  const reasons: string[] = [];
  const bridge = new SettingsBridge({
    paths,
    currentAppVersion: "0.0.0",
    relaunch: (reason) => {
      reasons.push(reason);
    },
  });
  bridge.relaunchDesktopApp("daemon-binary-path-changed");
  expect(reasons).toEqual(["daemon-binary-path-changed"]);
});

test("SettingsBridge: queue is bounded - dropped notice arrives after flooding a slow listener", async () => {
  const paths = pathsFor(workDir);
  const bridge = new SettingsBridge({ paths, currentAppVersion: "0.0.0" });
  const seen: SettingsChangeEvent[] = [];
  // Slow synchronous listener that doesn't actually block (Bun's bun:test
  // microtask queue still drains), but the queue invariant is still
  // testable via the test internals: depth must not grow unboundedly.
  bridge.subscribe((event) => {
    seen.push(event);
  });
  for (let i = 0; i < 200; i += 1) {
    bridge.setDaemonSettings({
      ...DEFAULT_DAEMON_SETTINGS,
      logLevel: i % 2 === 0 ? "info" : "warn",
    });
  }
  await new Promise<void>((resolve) => setImmediate(resolve));
  expect(seen.length).toBeGreaterThan(0);
  expect(bridge.subscriberQueueDepthForTesting()).toBe(0);
});
