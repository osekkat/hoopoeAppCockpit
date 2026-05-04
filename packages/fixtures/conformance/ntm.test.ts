import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync, statSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { assertToolConformance } from "./harness.ts";

const here = dirname(fileURLToPath(import.meta.url));
const phase0NtmRoot = resolve(here, "..", "phase0-ntm");

interface Phase0NtmManifest {
  fixturesVersion: string;
  tool: "ntm";
  ntmVersion: string;
  ntmCommit: string;
  ntmBuildDate: string;
  goVersion: string;
  capturedFrom: string;
  scenarioState: string;
  captures: Array<{
    file: string;
    argv: string[];
    purpose: string;
  }>;
  drift: {
    absentFlags: string[];
    substitutions: Record<string, string>;
  };
}

interface DiscrepancyLedger {
  schemaVersion: number;
  tool: "ntm";
  fixturePack: string;
  scope: string;
  expectedDiscrepancies: Array<{
    id: string;
    status: "accepted-gap" | "accepted-drift";
    reason?: string;
    fields?: string[];
    flag?: string;
    substitution?: string;
  }>;
}

interface RobotTool {
  name: string;
  installed: boolean;
  version: string;
  path?: string | null;
  required?: boolean | null;
  capabilities: string[];
  health: {
    healthy: boolean;
    message: string;
    error?: string | null;
  };
}

interface RobotCommand {
  name: string;
  flag: string;
  category: string;
  parameters: Array<{
    name: string;
    flag: string;
    type: string;
    required: boolean;
    default?: unknown;
    description?: string;
  }>;
}

interface RobotSnapshot {
  success: boolean;
  version: string;
  output_format: string;
  safety_profile: string;
  sessions: unknown[];
  beads_summary: { available: boolean; reason?: string };
  agent_mail: { available: boolean; project?: string };
  tools: RobotTool[];
  alerts: unknown[];
}

interface RobotStatus {
  success: boolean;
  version: string;
  output_format: string;
  system: {
    version: string;
    commit: string;
    build_date: string;
    go_version: string;
    os: string;
    arch: string;
    tmux_available: boolean;
  };
  sessions: unknown[];
  summary: Record<string, number>;
  beads: { available: boolean; reason?: string };
  graph_metrics: Record<string, unknown>;
  agent_mail: { available: boolean; server_url: string };
}

function loadJSON<T>(path: string): T {
  return JSON.parse(readFileSync(path, "utf8")) as T;
}

function normalizeTool(tool: RobotTool) {
  return {
    name: tool.name,
    installed: tool.installed,
    version: tool.version,
    path: tool.path ?? null,
    required: tool.required === true,
    capabilities: [...tool.capabilities].sort(),
    health: {
      healthy: tool.health.healthy,
      message: tool.health.message,
      error: tool.health.error ?? null,
    },
  };
}

function normalizeCommands(commands: RobotCommand[]) {
  return [...commands]
    .map((command) => ({
      name: command.name,
      flag: command.flag,
      category: command.category,
      parameters: [...command.parameters]
        .map((parameter) => ({
          name: parameter.name,
          flag: parameter.flag,
          type: parameter.type,
          required: parameter.required,
          default: parameter.default ?? null,
        }))
        .sort((a, b) => a.flag.localeCompare(b.flag)),
    }))
    .sort((a, b) => a.flag.localeCompare(b.flag));
}

function buildPhase0Matrix() {
  const manifest = loadJSON<Phase0NtmManifest>(resolve(phase0NtmRoot, "manifest.json"));
  const ledger = loadJSON<DiscrepancyLedger>(resolve(here, "ntm-phase0-discrepancies.json"));
  const snapshot = loadJSON<RobotSnapshot>(resolve(phase0NtmRoot, "robot-snapshot.json"));
  const status = loadJSON<RobotStatus>(resolve(phase0NtmRoot, "robot-status.json"));
  const capabilities = loadJSON<{
    success: boolean;
    version: string;
    commands: RobotCommand[];
  }>(resolve(phase0NtmRoot, "robot-capabilities.json"));
  const version = loadJSON<{
    success: boolean;
    system: RobotStatus["system"];
  }>(resolve(phase0NtmRoot, "robot-version.json"));
  const tools = loadJSON<{ success: boolean; tools: RobotTool[] }>(
    resolve(phase0NtmRoot, "robot-tools.json"),
  );
  const mail = loadJSON<{
    success: boolean;
    available: boolean;
    project_key: string;
    messages: Record<string, number>;
  }>(resolve(phase0NtmRoot, "robot-mail.json"));
  const mailCheck = loadJSON<{
    success: boolean;
    error_code: string;
    total_messages: number;
  }>(resolve(phase0NtmRoot, "robot-mail-check.json"));
  const alerts = loadJSON<{ success: boolean; count: number; alerts: unknown[] }>(
    resolve(phase0NtmRoot, "robot-alerts.json"),
  );

  return {
    key: `${manifest.ntmVersion}@${manifest.ntmCommit}`,
    manifest,
    ledger,
    canonical: {
      version: {
        success: version.success,
        system: version.system,
      },
      capabilities: {
        success: capabilities.success,
        version: capabilities.version,
        commands: normalizeCommands(capabilities.commands),
      },
      snapshot: {
        success: snapshot.success,
        version: snapshot.version,
        output_format: snapshot.output_format,
        safety_profile: snapshot.safety_profile,
        sessions: snapshot.sessions,
        beads_summary: snapshot.beads_summary,
        agent_mail: snapshot.agent_mail,
        tools: snapshot.tools.map(normalizeTool).sort((a, b) => a.name.localeCompare(b.name)),
        alerts: snapshot.alerts,
      },
      status: {
        success: status.success,
        version: status.version,
        output_format: status.output_format,
        system: status.system,
        sessions: status.sessions,
        summary: status.summary,
        beads: status.beads,
        graph_metrics: status.graph_metrics,
        agent_mail: status.agent_mail,
      },
      toolInventory: {
        success: tools.success,
        tools: tools.tools.map(normalizeTool).sort((a, b) => a.name.localeCompare(b.name)),
      },
      mail: {
        success: mail.success,
        available: mail.available,
        project_key: mail.project_key,
        messages: mail.messages,
      },
      mailCheck: {
        success: mailCheck.success,
        error_code: mailCheck.error_code,
        total_messages: mailCheck.total_messages,
      },
      alerts: {
        success: alerts.success,
        count: alerts.count,
        alerts: alerts.alerts,
      },
    },
  };
}

describe("ntm adapter contract conformance", () => {
  test("normal, round-trip, negative, and capability cases match the contract", () => {
    assertToolConformance("ntm");
  });

  test("phase0 real-VPS fixture matrix is keyed by ntm version and commit", () => {
    const matrix = buildPhase0Matrix();

    expect(matrix.key).toBe("1.8.0@384f91b06b5f7c5be27b8f63289f9432372b26c7");
    expect(matrix.manifest.tool).toBe("ntm");
    expect(matrix.manifest.capturedFrom).toBe("real-vps");
    expect(matrix.manifest.scenarioState).toBe("fresh-no-sessions-no-beads");
    expect(matrix.ledger.fixturePack).toBe(matrix.manifest.fixturesVersion);
    expect(matrix.ledger.scope).toBe("phase0-real-vps-conformance");

    for (const capture of matrix.manifest.captures) {
      const path = resolve(phase0NtmRoot, capture.file);
      expect(existsSync(path), capture.file).toBe(true);
      expect(statSync(path).size, capture.file).toBeGreaterThan(0);
    }
  });

  test("phase0 captures normalize to stable daemon adapter outputs", () => {
    const matrix = buildPhase0Matrix();
    const canonicalBytes = JSON.stringify(matrix.canonical);
    const roundTripped = JSON.stringify(JSON.parse(canonicalBytes));

    expect(roundTripped).toBe(canonicalBytes);
    expect(matrix.canonical.version.success).toBe(true);
    expect(matrix.canonical.version.system.version).toBe(matrix.manifest.ntmVersion);
    expect(matrix.canonical.version.system.commit).toBe(matrix.manifest.ntmCommit);
    expect(matrix.canonical.version.system.build_date).toBe(matrix.manifest.ntmBuildDate);
    expect(matrix.canonical.version.system.go_version).toBe(matrix.manifest.goVersion);
    expect(matrix.canonical.snapshot.sessions).toEqual([]);
    expect(matrix.canonical.status.sessions).toEqual([]);
    expect(matrix.canonical.status.summary.total_sessions).toBe(0);
    expect(matrix.canonical.status.summary.total_agents).toBe(0);
  });

  test("phase0 capability output covers the robot surfaces the daemon adapter must gate on", () => {
    const matrix = buildPhase0Matrix();
    const flags = new Set(matrix.canonical.capabilities.commands.map((command) => command.flag));

    expect(flags.has("--robot-snapshot")).toBe(true);
    expect(flags.has("--robot-status")).toBe(true);
    expect(flags.has("--robot-capabilities")).toBe(true);
    expect(flags.has("--robot-version")).toBe(true);
    expect(flags.has("--robot-tools")).toBe(true);
    expect(flags.has("--robot-mail")).toBe(true);
    expect(flags.has("--robot-alerts")).toBe(true);
    expect(matrix.canonical.capabilities.commands.length).toBeGreaterThan(50);
  });

  test("phase0 snapshot and tool inventory preserve source-of-truth capability posture", () => {
    const matrix = buildPhase0Matrix();
    const snapshotTools = new Map(matrix.canonical.snapshot.tools.map((tool) => [tool.name, tool]));
    const inventoryTools = new Map(
      matrix.canonical.toolInventory.tools.map((tool) => [tool.name, tool]),
    );

    expect(snapshotTools.size).toBeGreaterThan(10);
    expect(inventoryTools.size).toBe(snapshotTools.size);
    expect(snapshotTools.get("bv")?.required).toBe(true);
    expect(snapshotTools.get("bv")?.capabilities).toContain("robot_mode");
    expect(snapshotTools.get("am")?.capabilities).toContain("server_available");
    expect(snapshotTools.get("bd")?.version).toMatch(/^br /);
    expect(snapshotTools.get("rch")?.health.healthy).toBe(true);
    expect(snapshotTools.get("caam")?.health.healthy).toBe(false);

    for (const [name, tool] of snapshotTools) {
      expect(inventoryTools.get(name)).toEqual(tool);
    }
  });

  test("phase0 mail, alert, and bead surfaces preserve the fresh-VPS state", () => {
    const matrix = buildPhase0Matrix();

    expect(matrix.canonical.snapshot.beads_summary).toEqual({
      available: false,
      reason: "no .beads/ directory in /home/admin",
    });
    expect(matrix.canonical.status.beads).toEqual(matrix.canonical.snapshot.beads_summary);
    expect(matrix.canonical.snapshot.agent_mail).toEqual({
      available: true,
      project: "/home/admin",
    });
    expect(matrix.canonical.mail).toEqual({
      success: true,
      available: true,
      project_key: "/home/admin",
      messages: {
        total: 0,
        unread: 0,
        urgent: 0,
        pending_ack: 0,
      },
    });
    expect(matrix.canonical.mailCheck.success).toBe(false);
    expect(matrix.canonical.mailCheck.error_code).toBe("INTERNAL_ERROR");
    expect(matrix.canonical.alerts).toEqual({
      success: true,
      count: 0,
      alerts: [],
    });
  });

  test("phase0 discrepancy ledger accounts for intentional session and robot-flag gaps", () => {
    const matrix = buildPhase0Matrix();
    const discrepancies = new Map(
      matrix.ledger.expectedDiscrepancies.map((entry) => [entry.id, entry]),
    );

    expect(discrepancies.get("ntm.phase0.no-active-sessions")?.fields).toEqual([
      "sessions",
      "panes",
      "actions",
      "approvals",
    ]);
    expect(matrix.manifest.drift.absentFlags).toEqual(["--robot-events", "--robot-attention"]);
    expect(discrepancies.get("ntm.phase0.robot-events-absent")?.substitution).toBe(
      matrix.manifest.drift.substitutions["--robot-events"],
    );
    expect(discrepancies.get("ntm.phase0.robot-attention-absent")?.substitution).toBe(
      matrix.manifest.drift.substitutions["--robot-attention"],
    );
  });
});
