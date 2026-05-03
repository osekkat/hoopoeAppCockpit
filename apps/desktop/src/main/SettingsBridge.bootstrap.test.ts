// Phase 1.5 cross-review fix: bootstrap-integration test for the
// composition-root SettingsBridge wiring.
//
// review-findings.md "Settings audit is still disabled in the desktop
// composition root" called out that hp-6obn shipped audit plumbing but
// `apps/desktop/src/main.ts` still constructed `new SettingsBridge({
// paths, relaunch })` with no auditSink. Direct unit tests in
// SettingsBridge.audit.test.ts could pass with injected sinks while the
// shipped app remained silent for security-relevant writes.
//
// This test exercises the SAME composition factory that bootstrapDesktop
// uses (`composeProductionSettingsBridge`), changes a security-relevant
// setting, and asserts an audit row landed on disk in the JSONL audit
// file. No daemon or fetch mocks needed — the factory is isolated from
// the rest of bootstrap so the security-relevant wiring can be verified
// without spawning the backend binary.

import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  composeProductionSettingsBridge,
  defaultSettingsAuditPath,
} from "../main.ts";

let homeDir: string;

beforeEach(() => {
  homeDir = mkdtempSync(join(tmpdir(), "hoopoe-bootstrap-"));
});

afterEach(() => {
  rmSync(homeDir, { recursive: true, force: true });
});

test("composeProductionSettingsBridge: changing desktop.updateChannel writes one audit row to the durable JSONL", () => {
  const settings = composeProductionSettingsBridge({ homeDir });

  settings.setUserSettings(
    { desktop: { updateChannel: "nightly" } },
    { actor: { kind: "user", source: "ui", id: "renderer-window-1" } },
  );

  const auditPath = defaultSettingsAuditPath(homeDir);
  const blob = readFileSync(auditPath, "utf8");
  const lines = blob.trim().split("\n").filter((line) => line.length > 0);
  expect(lines).toHaveLength(1);

  const entry = JSON.parse(lines[0]!);
  expect(entry.entry).toBe("setting_changed");
  expect(entry.key).toBe("desktop.updateChannel");
  expect(entry.oldValue).toBe("latest");
  expect(entry.newValue).toBe("nightly");
  expect(entry.tier).toBe("user");
  expect(entry.actor).toEqual({ kind: "user", source: "ui", id: "renderer-window-1" });
});

test("composeProductionSettingsBridge: default actor stamps desktop:main when caller omits actor", () => {
  const settings = composeProductionSettingsBridge({ homeDir });

  settings.setUserSettings({ desktop: { updateChannel: "nightly" } });

  const auditPath = defaultSettingsAuditPath(homeDir);
  const blob = readFileSync(auditPath, "utf8");
  const lines = blob.trim().split("\n").filter((line) => line.length > 0);
  expect(lines).toHaveLength(1);

  const entry = JSON.parse(lines[0]!);
  expect(entry.actor).toEqual({ kind: "system", id: "desktop:main", source: "ipc" });
});

test("composeProductionSettingsBridge: non-security-relevant changes do NOT write audit rows", () => {
  const settings = composeProductionSettingsBridge({ homeDir });

  settings.setUserSettings({ daemon: { logLevel: "warn" } });

  let blob: string | null;
  try {
    blob = readFileSync(defaultSettingsAuditPath(homeDir), "utf8");
  } catch {
    blob = null;
  }
  expect(blob === null || blob.trim() === "").toBe(true);
});

test("composeProductionSettingsBridge: multi-key change appends one batch atomically", () => {
  const settings = composeProductionSettingsBridge({ homeDir });

  settings.setUserSettings({
    desktop: {
      updateChannel: "nightly",
      serverExposureMode: "network-accessible",
    },
  });

  const blob = readFileSync(defaultSettingsAuditPath(homeDir), "utf8");
  const lines = blob.trim().split("\n").filter((line) => line.length > 0);
  expect(lines).toHaveLength(2);

  const keys = lines.map((line) => JSON.parse(line).key).toSorted();
  expect(keys).toEqual(["desktop.serverExposureMode", "desktop.updateChannel"]);
});

test("composeProductionSettingsBridge: multiple successive batches accumulate without overwriting", () => {
  const settings = composeProductionSettingsBridge({ homeDir });

  settings.setUserSettings({ desktop: { updateChannel: "nightly" } });
  settings.setUserSettings({ daemon: { tendingEnabled: false } });

  const blob = readFileSync(defaultSettingsAuditPath(homeDir), "utf8");
  const lines = blob.trim().split("\n").filter((line) => line.length > 0);
  // Both batches survive; the JSONL is append-only and never truncated.
  expect(lines).toHaveLength(2);
  const keys = lines.map((line) => JSON.parse(line).key).toSorted();
  expect(keys).toEqual(["daemon.tendingEnabled", "desktop.updateChannel"]);
});
