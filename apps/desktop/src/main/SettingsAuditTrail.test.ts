// Hoopoe-owned. Tests for SettingsAuditTrail (hp-wg5p).

import { describe, expect, test } from "bun:test";
import {
  SECURITY_RELEVANT_SETTING_KEYS,
  auditResolvedTreeDelta,
  auditSettingsChange,
  createInMemoryAuditSink,
  isSecurityRelevantChange,
  type SettingsActor,
} from "./SettingsAuditTrail.ts";

const userActor: SettingsActor = { kind: "user", id: "osekkat" };
const fixedTs = () => "2026-05-03T00:00:00Z";

describe("isSecurityRelevantChange", () => {
  test("returns true for known security-relevant keys", () => {
    expect(isSecurityRelevantChange("desktop.updateChannel")).toBe(true);
    expect(isSecurityRelevantChange("desktop.telemetryOptIn")).toBe(true);
    expect(isSecurityRelevantChange("project.pushPolicy")).toBe(true);
  });

  test("returns false for routine keys", () => {
    expect(isSecurityRelevantChange("desktop.editorCommand")).toBe(false);
    expect(isSecurityRelevantChange("client.activityPanelOpen")).toBe(false);
    expect(isSecurityRelevantChange("nonexistent.key")).toBe(false);
  });
});

describe("auditSettingsChange", () => {
  test("emits audit entry for security-relevant change", async () => {
    const { sink, drain } = createInMemoryAuditSink();
    const entry = await auditSettingsChange(
      sink,
      {
        key: "desktop.updateChannel",
        oldValue: "latest",
        newValue: "nightly",
        actor: userActor,
        tier: "user",
      },
      { now: fixedTs },
    );
    expect(entry).not.toBeNull();
    expect(entry?.entry).toBe("setting_changed");
    expect(entry?.key).toBe("desktop.updateChannel");
    expect(entry?.oldValue).toBe("latest");
    expect(entry?.newValue).toBe("nightly");
    expect(entry?.actor.kind).toBe("user");
    expect(entry?.tier).toBe("user");
    expect(entry?.ts).toBe("2026-05-03T00:00:00Z");
    const written = drain();
    expect(written.length).toBe(1);
    expect(written[0]?.key).toBe("desktop.updateChannel");
  });

  test("does NOT emit audit entry for routine change", async () => {
    const { sink, drain } = createInMemoryAuditSink();
    const entry = await auditSettingsChange(sink, {
      key: "desktop.editorCommand",
      oldValue: "code",
      newValue: "vim",
      actor: userActor,
      tier: "user",
    });
    expect(entry).toBeNull();
    expect(drain().length).toBe(0);
  });

  test("uses provided ts when present", async () => {
    const { sink } = createInMemoryAuditSink();
    const entry = await auditSettingsChange(sink, {
      key: "desktop.updateChannel",
      oldValue: "latest",
      newValue: "nightly",
      actor: userActor,
      tier: "user",
      ts: "2026-01-01T00:00:00Z",
    });
    expect(entry?.ts).toBe("2026-01-01T00:00:00Z");
  });
});

describe("auditResolvedTreeDelta", () => {
  test("emits one entry per changed security-relevant key, skips routine", async () => {
    const before = {
      desktop: { updateChannel: "latest", editorCommand: "code", telemetryOptIn: false },
      project: { pushPolicy: "manual" },
      daemon: { tendingEnabled: true, logLevel: "info" },
      client: { activityPanelOpen: false },
    };
    const after = {
      desktop: { updateChannel: "nightly", editorCommand: "vim", telemetryOptIn: true },
      project: { pushPolicy: "auto-push-every-commit" },
      daemon: { tendingEnabled: false, logLevel: "debug" }, // logLevel routine
      client: { activityPanelOpen: true }, // routine
    };
    const { sink, drain } = createInMemoryAuditSink();
    const emitted = await auditResolvedTreeDelta(sink, before, after, userActor, "user", {
      now: fixedTs,
    });
    const keys = emitted.map((e) => e.key).sort();
    expect(keys).toEqual([
      "daemon.tendingEnabled",
      "desktop.telemetryOptIn",
      "desktop.updateChannel",
      "project.pushPolicy",
    ]);
    // Routine changes (editorCommand, logLevel, activityPanelOpen) NOT in audit.
    const written = drain();
    expect(written.length).toBe(4);
    expect(written.find((e) => e.key === "desktop.editorCommand")).toBeUndefined();
    expect(written.find((e) => e.key === "daemon.logLevel")).toBeUndefined();
  });

  test("returns empty when nothing changed", async () => {
    const same = { desktop: { updateChannel: "latest" } };
    const { sink } = createInMemoryAuditSink();
    const emitted = await auditResolvedTreeDelta(sink, same, same, userActor);
    expect(emitted).toEqual([]);
  });
});

describe("SECURITY_RELEVANT_SETTING_KEYS module guard", () => {
  test("contains the expected v1 set", () => {
    expect(SECURITY_RELEVANT_SETTING_KEYS.size).toBeGreaterThanOrEqual(8);
    expect(SECURITY_RELEVANT_SETTING_KEYS.has("desktop.updateChannel")).toBe(true);
    expect(SECURITY_RELEVANT_SETTING_KEYS.has("project.modelContextPolicy")).toBe(true);
  });

  test("no key in the list is secret-bearing (token/password/etc.)", () => {
    // Module-load-time assert already guarantees this; double-checking
    // the invariant keeps a regression visible.
    const forbiddenSubstrings = ["token", "password", "secret", "passphrase", "bearer", "apiKey"];
    for (const key of SECURITY_RELEVANT_SETTING_KEYS) {
      for (const sub of forbiddenSubstrings) {
        expect(key.toLowerCase().includes(sub.toLowerCase())).toBe(false);
      }
    }
  });
});
