// Hoopoe-owned. Tests for the renderer settings model + search (hp-wg5p).
// React component tests live separately; this file is logic-only and
// runs in plain bun:test without DOM.

import { describe, expect, test } from "bun:test";
import {
  SETTING_DESCRIPTORS,
  SECTION_ORDER,
  groupBySections,
  resolveSettingSource,
  resolveWriteTier,
} from "./SettingsModel.ts";
import { dimmedDescriptors, searchSettings } from "./SettingsSearch.ts";

describe("SettingsModel catalog (hp-wg5p)", () => {
  test("every section has at least one descriptor", () => {
    const grouped = groupBySections();
    for (const section of SECTION_ORDER) {
      expect(grouped[section].length).toBeGreaterThan(0);
    }
  });

  test("audited descriptors match the SECURITY_RELEVANT_SETTING_KEYS list", async () => {
    const auditedKeys = SETTING_DESCRIPTORS.filter((d) => d.audited).map((d) => d.key).sort();
    const { SECURITY_RELEVANT_SETTING_KEYS } = await import(
      "../../main/SettingsAuditTrail.ts"
    );
    const trailKeys = Array.from(SECURITY_RELEVANT_SETTING_KEYS).sort();
    // Every renderer "audited" descriptor MUST be in the audit trail's
    // security-relevant list — otherwise the renderer claims auditing
    // happens but the main process won't actually emit.
    for (const k of auditedKeys) {
      expect(trailKeys.includes(k)).toBe(true);
    }
  });

  test("validators reject out-of-range numbers and pass valid ones", () => {
    const logRetention = SETTING_DESCRIPTORS.find((d) => d.key === "desktop.logRetentionDays");
    expect(logRetention?.validate?.(0)).not.toBeNull();
    expect(logRetention?.validate?.(366)).not.toBeNull();
    expect(logRetention?.validate?.(14)).toBeNull();
  });

  test("enum descriptors have non-empty options", () => {
    for (const d of SETTING_DESCRIPTORS) {
      if (d.widget === "enum") {
        expect(d.options ?? []).not.toEqual([]);
      }
    }
  });
});

describe("resolveSettingSource (hp-wg5p)", () => {
  const defaults = { desktop: { updateChannel: "latest", telemetryOptIn: false } };
  const global = { desktop: { updateChannel: "nightly" } };
  const project = { desktop: { telemetryOptIn: true } };
  const env = { desktop: { updateChannel: "latest" } }; // env overrides global

  test("env tier wins when env value is set", () => {
    const r = resolveSettingSource(defaults, global, project, env, "desktop.updateChannel");
    expect(r.tier).toBe("env");
    expect(r.value).toBe("latest");
  });

  test("project tier wins over global when env unset", () => {
    const r = resolveSettingSource(defaults, global, project, {}, "desktop.telemetryOptIn");
    expect(r.tier).toBe("project");
    expect(r.value).toBe(true);
  });

  test("global tier wins over default when project + env unset", () => {
    const r = resolveSettingSource(defaults, global, {}, {}, "desktop.updateChannel");
    expect(r.tier).toBe("global");
    expect(r.value).toBe("nightly");
  });

  test("default tier wins when nothing else set", () => {
    const r = resolveSettingSource(defaults, {}, {}, {}, "desktop.telemetryOptIn");
    expect(r.tier).toBe("default");
    expect(r.value).toBe(false);
  });
});

describe("resolveWriteTier (hp-sfs0)", () => {
  const projectDescriptor = SETTING_DESCRIPTORS.find((d) => d.key === "project.pushPolicy");
  const globalDescriptor = SETTING_DESCRIPTORS.find((d) => d.key === "desktop.updateChannel");

  test("project-section descriptor with active project writes to project tier even without existing overrides", () => {
    expect(projectDescriptor).toBeDefined();
    expect(resolveWriteTier(projectDescriptor!, "proj-123")).toBe("project");
  });

  test("project-section descriptor without active project falls back to global tier", () => {
    expect(resolveWriteTier(projectDescriptor!, null)).toBe("global");
    expect(resolveWriteTier(projectDescriptor!, undefined)).toBe("global");
    expect(resolveWriteTier(projectDescriptor!, "")).toBe("global");
  });

  test("global-section descriptor always writes to global tier regardless of project context", () => {
    expect(globalDescriptor).toBeDefined();
    expect(resolveWriteTier(globalDescriptor!, "proj-123")).toBe("global");
    expect(resolveWriteTier(globalDescriptor!, null)).toBe("global");
  });

  test("non-project sections (accounts/skills/diagnostics) write to global tier", () => {
    for (const key of ["accounts.caamSummary", "skills.installerPreference", "diagnostics.recoveryShellAccess"]) {
      const d = SETTING_DESCRIPTORS.find((x) => x.key === key);
      expect(d).toBeDefined();
      expect(resolveWriteTier(d!, "proj-123")).toBe("global");
    }
  });
});

describe("searchSettings (hp-wg5p)", () => {
  test("empty query returns full catalog with score 0", () => {
    const hits = searchSettings("");
    expect(hits.length).toBe(SETTING_DESCRIPTORS.length);
    expect(hits.every((h) => h.score === 0)).toBe(true);
  });

  test("'telemetry' matches Global telemetry opt-in row", () => {
    const hits = searchSettings("telemetry");
    expect(hits.length).toBeGreaterThanOrEqual(1);
    expect(hits[0]?.descriptor.key).toBe("desktop.telemetryOptIn");
  });

  test("'clone' matches Project local-clone caps", () => {
    const hits = searchSettings("clone");
    const keys = hits.map((h) => h.descriptor.key);
    expect(keys).toContain("project.localCloneSoftCapMb");
    expect(keys).toContain("project.localCloneHardCapMb");
  });

  test("multi-token query intersects matches", () => {
    const hits = searchSettings("update channel");
    expect(hits.length).toBeGreaterThanOrEqual(1);
    expect(hits[0]?.descriptor.key).toBe("desktop.updateChannel");
  });

  test("dimmedDescriptors returns the inverse set for non-empty query", () => {
    const dim = dimmedDescriptors("telemetry");
    const dimKeys = new Set(dim.map((d) => d.key));
    expect(dimKeys.has("desktop.telemetryOptIn")).toBe(false);
    expect(dimKeys.has("desktop.editorCommand")).toBe(true);
  });

  test("dimmedDescriptors returns [] for empty query (no dimming)", () => {
    expect(dimmedDescriptors("")).toEqual([]);
    expect(dimmedDescriptors("   ")).toEqual([]);
  });
});
