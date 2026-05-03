// hp-6obn — SettingsBridge ↔ SettingsAuditTrail wiring tests.
//
// Covers the four DOD invariants from the bead spec:
//   1. Security-relevant key change → exactly one audit row, with full
//      timestamp/actor/tier/source metadata.
//   2. Non-security-relevant key change (daemon.logLevel) → zero audit rows.
//   3. Audit sink rejection → SettingsAuditWriteError; in-flight setting
//      change rolls back (disk untouched + in-memory partial unchanged +
//      `resolved()` snapshot unchanged).
//   4. Audit values are run through the redaction layer; secret-shaped
//      strings never reach the sink as plaintext.
//
// Plus: hot-reload audit-after-the-fact path surfaces failures via
// `logger.critical` rather than throwing (we cannot roll back disk).

import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  SettingsAuditWriteError,
  SettingsBridge,
  type SettingsBridgeLogger,
} from "./SettingsBridge.ts";
import {
  type SettingsActor,
  type SettingsAuditEntry,
  type SyncSettingsAuditSink,
} from "./SettingsAuditTrail.ts";

let workDir: string;
let userFile: string;
let projectFile: string;

beforeEach(() => {
  workDir = mkdtempSync(join(tmpdir(), "hoopoe-audit-"));
  userFile = join(workDir, "user", "settings.json");
  projectFile = join(workDir, "project", "settings.json");
});

afterEach(() => {
  rmSync(workDir, { recursive: true, force: true });
});

interface CapturingHarness {
  readonly bridge: SettingsBridge;
  readonly entries: SettingsAuditEntry[];
  readonly criticals: { readonly message: string; readonly meta?: Record<string, unknown> }[];
  readonly warnings: { readonly message: string; readonly meta?: Record<string, unknown> }[];
}

function makeBridge(opts: {
  sink?: SyncSettingsAuditSink;
  defaultActor?: SettingsActor;
  now?: () => string;
} = {}): CapturingHarness {
  const entries: SettingsAuditEntry[] = [];
  const criticals: { message: string; meta?: Record<string, unknown> }[] = [];
  const warnings: { message: string; meta?: Record<string, unknown> }[] = [];
  const sink: SyncSettingsAuditSink =
    opts.sink ?? ((entry) => {
      entries.push(entry);
    });
  const logger: SettingsBridgeLogger = {
    warn: (message, meta) => warnings.push({ message, ...(meta ? { meta } : {}) }),
    info: () => {},
    critical: (message, meta) => criticals.push({ message, ...(meta ? { meta } : {}) }),
  };
  const bridge = new SettingsBridge({
    paths: { userFile, projectFile },
    auditSink: sink,
    logger,
    ...(opts.defaultActor ? { defaultActor: opts.defaultActor } : {}),
    ...(opts.now ? { now: opts.now } : {}),
  });
  return { bridge, entries, criticals, warnings };
}

// ── 1. Security-relevant change emits exactly one audit row ───────────────

test("setUserSettings: changing desktop.updateChannel emits one setting_changed audit entry", () => {
  const fixedNow = "2026-05-03T00:00:00.000Z";
  const harness = makeBridge({ now: () => fixedNow });
  harness.bridge.setUserSettings(
    { desktop: { updateChannel: "nightly" } },
    { actor: { kind: "user", source: "ui", id: "renderer-window-1" } },
  );
  expect(harness.entries).toHaveLength(1);
  const entry = harness.entries[0]!;
  expect(entry.entry).toBe("setting_changed");
  expect(entry.key).toBe("desktop.updateChannel");
  expect(entry.oldValue).toBe("latest");
  expect(entry.newValue).toBe("nightly");
  expect(entry.tier).toBe("user");
  expect(entry.actor).toEqual({ kind: "user", source: "ui", id: "renderer-window-1" });
  expect(entry.ts).toBe(fixedNow);
});

test("setProjectSettings: changing project.safetyPreset emits one entry tagged tier=project", () => {
  const harness = makeBridge();
  harness.bridge.setProjectSettings(
    { project: { safetyPreset: "strict" } } as never,
    { actor: { kind: "agent", source: "programmatic", id: "GreenBear" } },
  );
  expect(harness.entries).toHaveLength(1);
  expect(harness.entries[0]?.tier).toBe("project");
});

// ── 2. Non-security-relevant change emits ZERO audit rows ─────────────────

test("setUserSettings: daemon.logLevel is NOT in the security-relevant set — zero audit rows", () => {
  const harness = makeBridge();
  harness.bridge.setUserSettings({ daemon: { logLevel: "warn" } });
  expect(harness.entries).toHaveLength(0);
});

test("setUserSettings: only the changed security-relevant key gets an audit row (mixed delta)", () => {
  const harness = makeBridge();
  harness.bridge.setUserSettings({
    daemon: { logLevel: "warn" },
    desktop: { updateChannel: "nightly" },
  });
  expect(harness.entries).toHaveLength(1);
  expect(harness.entries[0]?.key).toBe("desktop.updateChannel");
});

// ── 3. Sink rejection rolls back the in-flight change ─────────────────────

test("setUserSettings: sink rejection throws SettingsAuditWriteError; disk + in-memory rollback", () => {
  const sink: SyncSettingsAuditSink = () => {
    throw new Error("simulated audit-pipeline outage");
  };
  const harness = makeBridge({ sink });
  const before = harness.bridge.resolved();

  let thrown: unknown = null;
  try {
    harness.bridge.setUserSettings({ desktop: { updateChannel: "nightly" } });
  } catch (error) {
    thrown = error;
  }
  expect(thrown).toBeInstanceOf(SettingsAuditWriteError);
  const auditError = thrown as SettingsAuditWriteError;
  expect(auditError.tier).toBe("user");
  expect(auditError.attemptedEntry.key).toBe("desktop.updateChannel");
  expect(auditError.cause.message).toContain("simulated audit-pipeline outage");

  // Resolved snapshot unchanged.
  expect(harness.bridge.resolved()).toEqual(before);
  expect(harness.bridge.resolved().desktop.updateChannel).toBe("latest");

  // Disk file was never created (no successful write happened).
  let diskBlob: string | null = null;
  try {
    diskBlob = readFileSync(userFile, "utf8");
  } catch {
    diskBlob = null;
  }
  expect(diskBlob).toBeNull();

  // logger.critical fired so the failure is loud.
  expect(harness.criticals.some((c) => c.message === "settings.audit-sink-rejected")).toBe(true);
});

// ── 4. Redaction at the audit boundary ────────────────────────────────────

test("redaction: a secret-shaped value is replaced before reaching the sink", () => {
  // Pre-seed the user-settings file with a JWT-shaped value at a
  // security-relevant key (skills.installerPreference). The bridge will
  // pick it up in the constructor, then we flip the value — the audit row
  // captures (old=JWT-shaped, new=plain) and redaction MUST kick in on
  // oldValue before the sink sees it. Defense-in-depth proof.
  const FS = require("node:fs") as typeof import("node:fs");
  FS.mkdirSync(join(workDir, "user"), { recursive: true });
  FS.writeFileSync(
    userFile,
    JSON.stringify(
      {
        schemaVersion: 1,
        skills: { installerPreference: "eyJhbGciOiJIUzI1NiJ9.payloadSegment.signatureSegment" },
      },
      null,
      2,
    ),
  );
  const harness = makeBridge();
  harness.bridge.setUserSettings({ skills: { installerPreference: "bun" } } as never);
  expect(harness.entries).toHaveLength(1);
  const entry = harness.entries[0]!;
  expect(entry.key).toBe("skills.installerPreference");
  expect(entry.oldValue).toBe("[redacted-jwt]");
  expect(entry.newValue).toBe("bun");
});

// ── 5. Hot-reload audit failure surfaces Critical (no rollback possible) ──

test("reloadNow: audit-sink failure surfaces logger.critical instead of throwing", () => {
  // Seed disk with initial user partial.
  const FS = require("node:fs") as typeof import("node:fs");
  FS.mkdirSync(userFile.replace(/[^/]+$/, ""), { recursive: true });
  FS.writeFileSync(
    userFile,
    JSON.stringify(
      { schemaVersion: 1, desktop: { updateChannel: "latest" } },
      null,
      2,
    ),
  );

  const sink: SyncSettingsAuditSink = () => {
    throw new Error("hot-reload sink outage");
  };
  const harness = makeBridge({ sink });

  // Mutate file out-of-band → resemble a hot-reload pickup.
  FS.writeFileSync(
    userFile,
    JSON.stringify(
      { schemaVersion: 1, desktop: { updateChannel: "nightly" } },
      null,
      2,
    ),
  );

  // reloadNow must NOT throw — disk is already mutated.
  expect(() => harness.bridge.reloadNow("user")).not.toThrow();
  // The change is now reflected in resolved().
  expect(harness.bridge.resolved().desktop.updateChannel).toBe("nightly");
  // Failure was loud.
  expect(harness.criticals.some((c) => c.message === "settings.audit-failed-post-hot-reload")).toBe(true);
});

// ── 6. No sink wired = silent (back-compat) ───────────────────────────────

test("when no auditSink is provided, set* path completes normally (back-compat)", () => {
  const bridge = new SettingsBridge({
    paths: { userFile, projectFile },
  });
  bridge.setUserSettings({ desktop: { updateChannel: "nightly" } });
  expect(bridge.resolved().desktop.updateChannel).toBe("nightly");
});
