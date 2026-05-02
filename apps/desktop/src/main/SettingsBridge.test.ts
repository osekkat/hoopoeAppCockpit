import {
  mkdtempSync,
  readFileSync,
  readdirSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  DEFAULT_HOOPOE_SETTINGS,
  RELAUNCH_KEYS,
  SETTINGS_SCHEMA_VERSION,
  SettingsBridge,
  diffKeyPaths,
  mergeDeep,
  type SettingsChangeEvent,
} from "./SettingsBridge.ts";
import {
  stripDefaults,
  structurallyEqual,
  writeFileStringAtomically,
} from "../vendored/t3code/settings/index.ts";

let workDir: string;
let userFile: string;
let projectFile: string;

beforeEach(() => {
  workDir = mkdtempSync(join(tmpdir(), "hoopoe-settings-"));
  userFile = join(workDir, "user", "settings.json");
  projectFile = join(workDir, "project", "settings.json");
});

afterEach(() => {
  rmSync(workDir, { recursive: true, force: true });
});

function newBridge(opts: {
  relaunch?: (reason: string) => void;
  logger?: {
    warn: (message: string, meta?: Record<string, unknown>) => void;
    info: (message: string, meta?: Record<string, unknown>) => void;
  };
} = {}) {
  return new SettingsBridge({
    paths: { userFile, projectFile },
    ...(opts.relaunch ? { relaunch: opts.relaunch } : {}),
    ...(opts.logger ? { logger: opts.logger } : {}),
  });
}

// ── Three-tier resolver ──────────────────────────────────────────────────

test("SettingsBridge: empty user + project returns defaults", () => {
  const bridge = newBridge();
  expect(bridge.resolved()).toEqual(DEFAULT_HOOPOE_SETTINGS);
});

test("SettingsBridge: user overrides defaults; project overrides user", () => {
  writeFileStringAtomically({
    filePath: userFile,
    contents: JSON.stringify(
      { schemaVersion: SETTINGS_SCHEMA_VERSION, daemon: { logLevel: "warn" } },
      null,
      2,
    ),
  });
  writeFileStringAtomically({
    filePath: projectFile,
    contents: JSON.stringify(
      {
        schemaVersion: SETTINGS_SCHEMA_VERSION,
        daemon: { logLevel: "debug" },
        client: { activeStage: "swarm" },
      },
      null,
      2,
    ),
  });
  const bridge = newBridge();
  expect(bridge.resolved().daemon.logLevel).toBe("debug"); // project wins
  expect(bridge.resolved().daemon.tendingEnabled).toBe(true); // unchanged default
  expect(bridge.resolved().client.activeStage).toBe("swarm");
});

test("SettingsBridge: setUserSettings persists stripped + schema-versioned content", () => {
  const bridge = newBridge();
  bridge.setUserSettings({ daemon: { logLevel: "warn" } });
  const written = JSON.parse(readFileSync(userFile, "utf8")) as Record<string, unknown>;
  expect(written.schemaVersion).toBe(SETTINGS_SCHEMA_VERSION);
  expect(written.daemon).toEqual({ logLevel: "warn" }); // tendingEnabled stripped
  expect(written).not.toHaveProperty("desktop");
  expect(written).not.toHaveProperty("client");
});

test("SettingsBridge: setProjectSettings is independent of user file", () => {
  const bridge = newBridge();
  bridge.setUserSettings({ daemon: { logLevel: "warn" } });
  bridge.setProjectSettings({ daemon: { logLevel: "debug" } });
  expect(bridge.resolved().daemon.logLevel).toBe("debug");
  const userOnDisk = JSON.parse(readFileSync(userFile, "utf8")) as Record<string, unknown>;
  expect((userOnDisk.daemon as Record<string, unknown>).logLevel).toBe("warn");
});

// ── Change stream ─────────────────────────────────────────────────────────

test("SettingsBridge: subscribers receive change events with diffed key paths", async () => {
  const bridge = newBridge();
  const events: SettingsChangeEvent[] = [];
  bridge.subscribe((event) => {
    events.push(event);
  });
  bridge.setUserSettings({ client: { activityPanelOpen: true } });
  bridge.setProjectSettings({ daemon: { logLevel: "debug" } });
  await new Promise<void>((resolve) => setImmediate(resolve));
  expect(events).toHaveLength(2);
  expect(events[0]?.tier).toBe("user");
  expect(events[0]?.changedKeys).toContain("client.activityPanelOpen");
  expect(events[1]?.tier).toBe("project");
  expect(events[1]?.changedKeys).toContain("daemon.logLevel");
});

test("SettingsBridge: queue is bounded — depth doesn't grow unboundedly", async () => {
  const bridge = newBridge();
  const seen: SettingsChangeEvent[] = [];
  bridge.subscribe((event) => {
    seen.push(event);
  });
  for (let i = 0; i < 200; i += 1) {
    bridge.setUserSettings({
      daemon: { logLevel: i % 2 === 0 ? "info" : "warn" },
    });
  }
  await new Promise<void>((resolve) => setImmediate(resolve));
  expect(seen.length).toBeGreaterThan(0);
  expect(bridge.subscriberQueueDepthForTesting()).toBe(0);
});

// ── Relaunch hook ─────────────────────────────────────────────────────────

test("SettingsBridge: writing a RELAUNCH_KEYS member triggers relaunchDesktopApp", () => {
  const reasons: string[] = [];
  const bridge = newBridge({ relaunch: (reason) => reasons.push(reason) });
  bridge.setUserSettings({ daemon: { daemonBinaryPath: "/opt/hoopoe/bin/hoopoe" } });
  expect(reasons).toHaveLength(1);
  expect(reasons[0]).toContain("daemon.daemonBinaryPath");
});

test("SettingsBridge: hot-applicable keys do NOT trigger relaunch", () => {
  const reasons: string[] = [];
  const bridge = newBridge({ relaunch: (reason) => reasons.push(reason) });
  bridge.setUserSettings({ daemon: { logLevel: "warn" } });
  bridge.setUserSettings({ client: { activityPanelOpen: true } });
  expect(reasons).toHaveLength(0);
});

test("RELAUNCH_KEYS: includes daemon.daemonBinaryPath + desktop.serverExposureMode", () => {
  expect(RELAUNCH_KEYS.has("daemon.daemonBinaryPath")).toBe(true);
  expect(RELAUNCH_KEYS.has("desktop.serverExposureMode")).toBe(true);
  expect(RELAUNCH_KEYS.has("daemon.logLevel")).toBe(false);
});

// ── Schema version + malformed handling ───────────────────────────────────

test("SettingsBridge: schemaVersion mismatch falls back to empty overrides + warns", () => {
  writeFileStringAtomically({
    filePath: userFile,
    contents: JSON.stringify({ schemaVersion: 999, daemon: { logLevel: "debug" } }, null, 2),
  });
  const warnings: string[] = [];
  const bridge = newBridge({
    logger: {
      warn: (msg) => warnings.push(msg),
      info() {},
    },
  });
  expect(bridge.resolved()).toEqual(DEFAULT_HOOPOE_SETTINGS);
  expect(warnings).toContain("settings.schema-version-mismatch");
});

test("SettingsBridge: malformed JSON falls back + warns", () => {
  writeFileStringAtomically({ filePath: userFile, contents: "{not valid json" });
  const warnings: string[] = [];
  const bridge = newBridge({
    logger: {
      warn: (msg) => warnings.push(msg),
      info() {},
    },
  });
  expect(bridge.resolved()).toEqual(DEFAULT_HOOPOE_SETTINGS);
  expect(warnings).toContain("settings.read-failed");
});

// ── Atomic write ──────────────────────────────────────────────────────────

test("writeFileStringAtomically: leaves no .tmp residue on success", () => {
  writeFileStringAtomically({
    filePath: userFile,
    contents: JSON.stringify({ schemaVersion: SETTINGS_SCHEMA_VERSION }),
  });
  expect(readFileSync(userFile, "utf8")).toContain('"schemaVersion"');
  const dirEntries = readdirSync(join(workDir, "user"));
  expect(dirEntries.filter((name) => name.endsWith(".tmp"))).toHaveLength(0);
});

// ── 100 ms debounce ───────────────────────────────────────────────────────

test("SettingsBridge: 100 rapid scheduleReload calls coalesce to one reload via 100ms debounce", async () => {
  writeFileStringAtomically({
    filePath: userFile,
    contents: JSON.stringify(
      { schemaVersion: SETTINGS_SCHEMA_VERSION, daemon: { logLevel: "info" } },
      null,
      2,
    ),
  });
  const bridge = newBridge();
  // Direct exercise of the debounce mechanism — independent of fs.watch
  // delivery latency, which varies across OSes and test runners. The DOD
  // invariant is "100 rapid changes → 1 reload"; we prove that here.
  for (let i = 0; i < 100; i += 1) {
    bridge.scheduleReloadForTesting("user");
  }
  expect(bridge.hotReloadCountForTesting()).toBe(0); // none fired yet
  await new Promise<void>((resolve) => setTimeout(resolve, 200));
  expect(bridge.hotReloadCountForTesting()).toBe(1);
});

test("SettingsBridge: startWatching/stopWatching attach + detach without throwing", () => {
  writeFileStringAtomically({
    filePath: userFile,
    contents: JSON.stringify({ schemaVersion: SETTINGS_SCHEMA_VERSION }, null, 2),
  });
  const bridge = newBridge();
  bridge.startWatching();
  bridge.stopWatching();
  // Concrete out-of-band-write integration proof lives in hp-tg0's smoke
  // suite (real-fs harness). The 100 ms debounce coalescing is proven by
  // the deterministic test above.
});

// ── Vendored helpers ──────────────────────────────────────────────────────

test("stripDefaults: deep recursion omits matching keys", () => {
  const stripped = stripDefaults(
    {
      schemaVersion: 1,
      daemon: { logLevel: "warn", tendingEnabled: true, daemonBinaryPath: null },
      desktop: { updateChannel: "latest" },
    },
    DEFAULT_HOOPOE_SETTINGS,
  );
  expect(stripped).toEqual({ daemon: { logLevel: "warn" } });
});

test("structurallyEqual: arrays + objects + primitives + null", () => {
  expect(structurallyEqual(1, 1)).toBe(true);
  expect(structurallyEqual([1, 2, 3], [1, 2, 3])).toBe(true);
  expect(structurallyEqual([1, 2, 3], [1, 2])).toBe(false);
  expect(structurallyEqual({ a: 1, b: 2 }, { b: 2, a: 1 })).toBe(true);
  expect(structurallyEqual(null, null)).toBe(true);
  expect(structurallyEqual(null, 0)).toBe(false);
});

test("mergeDeep: project overrides user overrides defaults", () => {
  const merged = mergeDeep(
    mergeDeep(
      { daemon: { logLevel: "info", tendingEnabled: true } },
      { daemon: { logLevel: "warn" } },
    ),
    { daemon: { tendingEnabled: false } },
  );
  expect(merged).toEqual({ daemon: { logLevel: "warn", tendingEnabled: false } });
});

test("diffKeyPaths: returns dotted key paths of differing leaves", () => {
  const before = {
    daemon: { logLevel: "info", tendingEnabled: true },
    client: { activeStage: "planning" },
  };
  const after = {
    daemon: { logLevel: "warn", tendingEnabled: true },
    client: { activeStage: "swarm" },
  };
  expect([...diffKeyPaths(before, after)].toSorted()).toEqual([
    "client.activeStage",
    "daemon.logLevel",
  ]);
});

// Suppress unused-imports lint when the helper isn't exercised in a given
// path through the test file (writeFileSync is only used by the malformed
// case, but bun:test imports the whole file regardless).
void writeFileSync;
