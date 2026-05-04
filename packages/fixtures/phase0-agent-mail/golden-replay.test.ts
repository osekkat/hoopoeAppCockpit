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
  provenance: {
    localhostRationale: string;
    canonicalCaptures: string[];
    replayOnlyCaptures: string[];
    notes: string;
  };
  captures: Array<{
    file: string;
    method: string;
    path: string;
    operation: string;
    canonical: boolean;
    provenance: string;
    purpose: string;
  }>;
  drift: {
    userListedTools: string[];
    captured: string[];
    deferredMcpTools: string[];
    notes: string;
  };
}

interface OperationCapture {
  capture: {
    classification: string;
    operation: string;
    serverUrl: string;
    transport: string;
  };
  request: {
    method: string;
    params: {
      name: string;
      arguments: Record<string, unknown>;
    };
  };
  response: {
    status: number;
    body: {
      jsonrpc: string;
      result?: {
        isError?: boolean;
        structuredContent?: unknown;
      };
    };
  };
}

function loadJSON<T>(path: string): T {
  const text = readFileSync(path, "utf8");
  try {
    return JSON.parse(text) as T;
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    throw new Error(`Failed to parse JSON fixture ${path}: ${message}`);
  }
}

const manifest = loadJSON<Manifest>(resolve(here, "manifest.json"));

describe("phase0-agent-mail fixture pack: live mcp-agent-mail HTTP captures replay cleanly", () => {
  test("manifest pedigree pins the agent_mail tool + streamable-http transport", () => {
    expect(manifest.tool).toBe("agent_mail");
    expect(manifest.transport).toBe("streamable-http");
    expect(manifest.serverUrl).toBe("http://127.0.0.1:8765");
    expect(manifest.capturedFrom).toBe("local-acfs-vps-loopback-agent-mail-server");
    expect(manifest.provenance.localhostRationale).toContain("Codex agents");
    expect(manifest.captures.length).toBeGreaterThanOrEqual(13);
  });

  test("every capture file referenced in the manifest exists and is non-empty", () => {
    for (const capture of manifest.captures) {
      const path = resolve(here, capture.file);
      expect(existsSync(path)).toBe(true);
      expect(statSync(path).size).toBeGreaterThan(0);
    }
  });

  test("manifest classifies canonical captures separately from replay-only captures", () => {
    expect(manifest.provenance.replayOnlyCaptures).toEqual([]);
    expect(manifest.provenance.canonicalCaptures.length).toBe(manifest.captures.length);
    for (const capture of manifest.captures) {
      expect(capture.canonical).toBe(true);
      expect(capture.provenance).toMatch(/^canonical-/);
      expect(manifest.provenance.canonicalCaptures).toContain(capture.file);
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

  test("operation captures cover the required Agent Mail JSON-RPC tool surface", () => {
    const required = new Map([
      ["ensure_project", "operation-ensure-project.json"],
      ["register_agent:sender", "operation-register-agent-sender.json"],
      ["register_agent:receiver", "operation-register-agent-receiver.json"],
      ["send_message", "operation-send-message.json"],
      ["fetch_inbox", "operation-fetch-inbox.json"],
      ["file_reservation_paths", "operation-file-reservation-paths.json"],
      ["release_file_reservations", "operation-release-file-reservations.json"],
    ]);

    for (const [operation, file] of required) {
      const payload = loadJSON<OperationCapture>(resolve(here, file));
      expect(payload.capture.classification).toBe("canonical-local-mcp-jsonrpc");
      expect(payload.capture.serverUrl).toBe("http://127.0.0.1:8765");
      expect(payload.request.method).toBe("tools/call");
      expect(payload.response.status).toBe(200);
      expect(payload.response.body.jsonrpc).toBe("2.0");
      expect(payload.response.body.result?.isError ?? false).toBe(false);
      if (operation.includes(":")) {
        expect(payload.capture.operation).toBe(operation.split(":")[0]);
      } else {
        expect(payload.capture.operation).toBe(operation);
      }
    }
  });

  test("operation captures preserve project, message, inbox, and reservation semantics", () => {
    const ensure = loadJSON<OperationCapture>(resolve(here, "operation-ensure-project.json"));
    expect((ensure.response.body.result?.structuredContent as { slug: string }).slug).toBe(
      "tmp-hoopoe-phase0-agent-mail-hp-pr3d",
    );

    const sender = loadJSON<OperationCapture>(
      resolve(here, "operation-register-agent-sender.json"),
    );
    expect((sender.response.body.result?.structuredContent as { name: string }).name).toBe(
      "HoopoePhase0Sender",
    );

    const message = loadJSON<OperationCapture>(resolve(here, "operation-send-message.json"));
    const delivery = (
      message.response.body.result?.structuredContent as {
        deliveries: Array<{ payload: { thread_id: string; to: string[] } }>;
      }
    ).deliveries[0].payload;
    expect(delivery.thread_id).toBe("hp-pr3d-phase0-agent-mail-capture");
    expect(delivery.to).toContain("HoopoePhase0Receiver");

    const inbox = loadJSON<OperationCapture>(resolve(here, "operation-fetch-inbox.json"));
    const inboxItems = (inbox.response.body.result?.structuredContent as { result: unknown[] })
      .result;
    expect(inboxItems.length).toBeGreaterThan(0);

    const reservation = loadJSON<OperationCapture>(
      resolve(here, "operation-file-reservation-paths.json"),
    );
    const granted = (reservation.response.body.result?.structuredContent as { granted: unknown[] })
      .granted;
    expect(granted.length).toBe(1);

    const release = loadJSON<OperationCapture>(
      resolve(here, "operation-release-file-reservations.json"),
    );
    expect((release.response.body.result?.structuredContent as { released: number }).released).toBe(
      1,
    );
  });

  test("manifest documents resolved drift for operation-level MCP captures", () => {
    expect(manifest.drift.userListedTools).toContain("ensure_project");
    expect(manifest.drift.userListedTools).toContain("register_agent");
    expect(manifest.drift.userListedTools).toContain("send_message");
    expect(manifest.drift.userListedTools).toContain("fetch_inbox");
    expect(manifest.drift.userListedTools).toContain("file_reservation_paths");
    expect(manifest.drift.userListedTools).toContain("release_file_reservations");
    expect(manifest.drift.deferredMcpTools).toEqual([]);
    expect(manifest.drift.notes.toLowerCase()).toContain("no replay-only captures");
  });
});
