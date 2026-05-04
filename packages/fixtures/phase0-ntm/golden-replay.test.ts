import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync, statSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));

interface Manifest {
  fixturesVersion: string;
  tool: "ntm";
  ntmVersion: string;
  ntmCommit: string;
  vpsId: string;
  capturedFrom: string;
  scenarioState: string;
  captures: Array<{
    file: string;
    argv: string[];
    purpose: string;
  }>;
  drift: {
    userListedFlags: string[];
    absentFlags: string[];
    substitutions: Record<string, string>;
    notes: string;
  };
}

function loadJSON<T>(path: string): T {
  return JSON.parse(readFileSync(path, "utf8")) as T;
}

const manifest = loadJSON<Manifest>(resolve(here, "manifest.json"));

describe("phase0-ntm fixture pack: real-VPS captures replay cleanly", () => {
  test("manifest pedigree pins ntm 1.8.0 + commit", () => {
    expect(manifest.tool).toBe("ntm");
    expect(manifest.ntmVersion).toBe("1.8.0");
    expect(manifest.ntmCommit).toMatch(/^[0-9a-f]{40}$/);
    expect(manifest.capturedFrom).toBe("real-vps");
    expect(manifest.captures.length).toBeGreaterThanOrEqual(8);
  });

  test("every capture file referenced in the manifest exists and is non-empty", () => {
    for (const capture of manifest.captures) {
      const path = resolve(here, capture.file);
      expect(existsSync(path)).toBe(true);
      expect(statSync(path).size).toBeGreaterThan(0);
    }
  });

  test("robot-version JSON is well-formed and matches the manifest pedigree", () => {
    const path = resolve(here, "robot-version.json");
    const payload = loadJSON<{
      success: boolean;
      version: string;
      system: { version: string; commit: string; build_date: string; go_version: string };
    }>(path);
    expect(payload.success).toBe(true);
    expect(payload.system.version).toBe(manifest.ntmVersion);
    expect(payload.system.commit).toBe(manifest.ntmCommit);
    expect(payload.system.go_version).toMatch(/^go\d+\.\d+/);
  });

  test("robot-snapshot JSON exposes the canonical envelope shape adapters depend on", () => {
    const payload = loadJSON<{
      success: boolean;
      output_format: string;
      sessions: unknown;
      beads_summary: { available: boolean; reason?: string };
      agent_mail: { available: boolean; project?: string };
      tools: Array<{
        name: string;
        installed: boolean;
        capabilities: string[];
        health: { healthy: boolean; message: string };
      }>;
      alerts: unknown[];
    }>(resolve(here, "robot-snapshot.json"));
    expect(payload.success).toBe(true);
    expect(payload.output_format).toBe("json");
    expect(Array.isArray(payload.sessions)).toBe(true);
    expect(typeof payload.beads_summary.available).toBe("boolean");
    expect(typeof payload.agent_mail.available).toBe("boolean");
    expect(Array.isArray(payload.tools)).toBe(true);
    expect(payload.tools.length).toBeGreaterThan(10);
    for (const tool of payload.tools) {
      expect(typeof tool.name).toBe("string");
      expect(typeof tool.installed).toBe("boolean");
      expect(Array.isArray(tool.capabilities)).toBe(true);
      expect(typeof tool.health.healthy).toBe("boolean");
      expect(typeof tool.health.message).toBe("string");
    }
    expect(Array.isArray(payload.alerts)).toBe(true);
  });

  test("robot-snapshot TOON has the same scenario shape as the JSON capture", () => {
    const toonText = readFileSync(resolve(here, "robot-snapshot.toon"), "utf8");
    expect(toonText.length).toBeGreaterThan(0);
    expect(toonText).toContain("success:");
    expect(toonText).toContain("sessions");
    expect(toonText).toContain("tools");
  });

  test("robot-capabilities exposes the discoverable robot API schema", () => {
    const payload = loadJSON<{
      success: boolean;
      version: string;
      commands: Array<{ name: string; flag: string; category: string; parameters: unknown[] }>;
    }>(resolve(here, "robot-capabilities.json"));
    expect(payload.success).toBe(true);
    expect(payload.version).toBe(manifest.ntmVersion);
    expect(Array.isArray(payload.commands)).toBe(true);
    expect(payload.commands.length).toBeGreaterThan(50);
    const flags = new Set(payload.commands.map((c) => c.flag));
    expect(flags.has("--robot-snapshot")).toBe(true);
    expect(flags.has("--robot-capabilities")).toBe(true);
    expect(flags.has("--robot-version")).toBe(true);
  });

  test("robot-status returns a structured envelope with the daemon-relevant fields", () => {
    const payload = loadJSON<Record<string, unknown>>(resolve(here, "robot-status.json"));
    expect(payload.success).toBe(true);
    expect(typeof payload.timestamp).toBe("string");
  });

  test("robot-tools enumerates per-tool health probes the adapter uses for capability composition", () => {
    const payload = loadJSON<{ success: boolean; tools?: unknown[] }>(resolve(here, "robot-tools.json"));
    expect(payload.success).toBe(true);
    if (Array.isArray(payload.tools)) {
      expect(payload.tools.length).toBeGreaterThan(0);
    }
  });

  test("robot-mail succeeds when --mail-project is supplied; --robot-mail-check returns success=false in this scenario", () => {
    const mail = loadJSON<{ success: boolean; available: boolean; project_key: string; messages: Record<string, number> }>(resolve(here, "robot-mail.json"));
    expect(mail.success).toBe(true);
    expect(mail.available).toBe(true);
    expect(mail.project_key).toBe("/home/admin");
    expect(typeof mail.messages.total).toBe("number");

    const check = loadJSON<{ success: boolean }>(resolve(here, "robot-mail-check.json"));
    expect(check.success).toBe(false);
  });

  test("robot-alerts returns a JSON envelope (substituted for the user-listed --robot-attention which does not exist in 1.8.0)", () => {
    const payload = loadJSON<{ success: boolean }>(resolve(here, "robot-alerts.json"));
    expect(payload.success).toBe(true);
    expect(manifest.drift.absentFlags).toContain("--robot-attention");
    expect(manifest.drift.substitutions["--robot-attention"]).toContain("--robot-alerts");
  });

  test("manifest documents drift between the user-requested flag list and the live ntm 1.8.0 surface", () => {
    expect(manifest.drift.absentFlags).toContain("--robot-events");
    expect(manifest.drift.absentFlags).toContain("--robot-attention");
    expect(Object.keys(manifest.drift.substitutions).sort()).toEqual([
      "--robot-attention",
      "--robot-events",
    ]);
  });
});
