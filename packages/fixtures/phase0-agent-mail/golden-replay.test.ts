import { describe, expect, test } from "bun:test";
import { existsSync, readFileSync, statSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));

interface Manifest {
  fixturesVersion: string;
  tool: "agent_mail";
  serverUrl: string;
  transport: string;
  capturedFrom: string;
  scenarioState: string;
  captures: Array<{
    file: string;
    method: string;
    path: string;
    purpose: string;
  }>;
  drift: {
    userListedTools: string[];
    captured: string[];
    deferredMcpTools: string[];
    notes: string;
  };
}

function loadJSON<T>(path: string): T {
  return JSON.parse(readFileSync(path, "utf8")) as T;
}

const manifest = loadJSON<Manifest>(resolve(here, "manifest.json"));

describe("phase0-agent-mail fixture pack: live mcp-agent-mail HTTP captures replay cleanly", () => {
  test("manifest pedigree pins the agent_mail tool + streamable-http transport", () => {
    expect(manifest.tool).toBe("agent_mail");
    expect(manifest.transport).toBe("streamable-http");
    expect(manifest.serverUrl).toBe("http://127.0.0.1:8765");
    expect(manifest.captures.length).toBeGreaterThanOrEqual(6);
  });

  test("every capture file referenced in the manifest exists and is non-empty", () => {
    for (const capture of manifest.captures) {
      const path = resolve(here, capture.file);
      expect(existsSync(path)).toBe(true);
      expect(statSync(path).size).toBeGreaterThan(0);
    }
  });

  test("liveness + readiness probes both succeed", () => {
    const liveness = loadJSON<{ status: string }>(resolve(here, "health-liveness.json"));
    expect(liveness.status).toBe("alive");
    const readiness = loadJSON<{ status: string }>(resolve(here, "health-readiness.json"));
    expect(readiness.status).toBe("ready");
  });

  test("oauth-server-metadata exposes the mcp_oauth flag", () => {
    const meta = loadJSON<{ mcp_oauth: boolean }>(resolve(here, "oauth-server-metadata.json"));
    expect(typeof meta.mcp_oauth).toBe("boolean");
  });

  test("mail-api-locks returns the canonical reservation envelope shape", () => {
    const payload = loadJSON<{
      locks: unknown[];
      summary: { total: number; active: number; stale: number; metadata_missing: number };
    }>(resolve(here, "mail-api-locks.json"));
    expect(Array.isArray(payload.locks)).toBe(true);
    expect(typeof payload.summary.total).toBe("number");
    expect(typeof payload.summary.active).toBe("number");
    expect(typeof payload.summary.stale).toBe("number");
    expect(typeof payload.summary.metadata_missing).toBe("number");
  });

  test("mail-api-unified-inbox exposes the messages + projects keys with per-message contract", () => {
    const payload = loadJSON<{
      messages: Array<{
        id: number;
        subject: string;
        body_md: string;
        excerpt: string;
        created_ts: string;
        importance: string;
        thread_id: string;
        sender: string;
        project_slug: string;
        project_name: string;
        recipients: string;
        read: boolean;
      }>;
      projects: unknown;
    }>(resolve(here, "mail-api-unified-inbox.json"));
    expect(Array.isArray(payload.messages)).toBe(true);
    expect(payload.messages.length).toBeGreaterThan(0);
    for (const message of payload.messages.slice(0, 5)) {
      expect(typeof message.id).toBe("number");
      expect(typeof message.subject).toBe("string");
      expect(typeof message.thread_id).toBe("string");
      expect(typeof message.sender).toBe("string");
      expect(typeof message.read).toBe("boolean");
      expect(typeof message.importance).toBe("string");
    }
  });

  test("api-projects-agents exposes a registered-agent roster including swarm peers", () => {
    const payload = loadJSON<{ agents: string[] }>(resolve(here, "api-projects-agents.json"));
    expect(Array.isArray(payload.agents)).toBe(true);
    expect(payload.agents.length).toBeGreaterThan(10);
    expect(payload.agents).toContain("orchestrator");
    expect(payload.agents.some((agent) => agent.startsWith("HoopoeCC"))).toBe(true);
  });

  test("manifest documents drift between the user-listed MCP tools and the HTTP-captured READ surface", () => {
    expect(manifest.drift.userListedTools).toContain("ensure_project");
    expect(manifest.drift.userListedTools).toContain("register_agent");
    expect(manifest.drift.userListedTools).toContain("fetch_inbox");
    expect(manifest.drift.deferredMcpTools.length).toBeGreaterThan(0);
    expect(manifest.drift.notes.toLowerCase()).toContain("read-only");
  });
});
