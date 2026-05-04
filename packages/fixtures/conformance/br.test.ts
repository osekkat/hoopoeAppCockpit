import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { goldenOutputPath, type GoldenOutputFixture } from "../src/index.ts";
import {
  BR_BEAD_SCHEMA_VERSION,
  assertToolConformance,
  mapBrToBeadListResponse,
} from "./harness.ts";

type Phase0Scenario = "fresh" | "active" | "failure";

interface Phase0Manifest {
  readonly mode: string;
  readonly realVpsAcceptance: boolean;
  readonly scenarios: readonly string[];
  readonly adapterTools: readonly string[];
}

interface Phase0AdapterIndex {
  readonly scenario: string;
  readonly mode: string;
  readonly adapters: readonly string[];
}

interface Phase0Capture {
  readonly argv: readonly string[];
  readonly exit: number;
  readonly stdoutText: string;
  readonly stderrText: string;
  readonly truncated: boolean;
  readonly redacted: boolean;
  readonly tags?: readonly string[];
}

interface Phase0BrAdapter {
  readonly tool: "br";
  readonly present: boolean;
  readonly binPath: string;
  readonly version: string;
  readonly capabilities: Record<string, { readonly status: string; readonly notes?: string }>;
  readonly captures: Record<string, Phase0Capture | undefined>;
  readonly capturedAt: string;
}

const here = dirname(fileURLToPath(import.meta.url));
const phase0Root = resolve(here, "..", "phase0-2026-05-02");
const phase0Scenarios = ["fresh", "active", "failure"] as const;

function readJSON<T>(path: string): T {
  return parseJSON<T>(readFileSync(path, "utf8"), path);
}

function parseJSON<T>(text: string, where: string): T {
  try {
    return JSON.parse(text) as T;
  } catch (error) {
    throw new Error(
      `${where} did not parse as JSON: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
}

function readPhase0Br(scenario: Phase0Scenario): Phase0BrAdapter {
  return readJSON<Phase0BrAdapter>(
    resolve(phase0Root, "scenarios", scenario, "adapters", "br.json"),
  );
}

function readPhase0Index(scenario: Phase0Scenario): Phase0AdapterIndex {
  return readJSON<Phase0AdapterIndex>(
    resolve(phase0Root, "scenarios", scenario, "adapter-index.json"),
  );
}

function requireCapture(adapter: Phase0BrAdapter, name: string): Phase0Capture {
  const capture = adapter.captures[name];
  expect(capture, `${adapter.tool}.${name} capture`).toBeDefined();
  return capture as Phase0Capture;
}

function parseStdout<T>(capture: Phase0Capture): T {
  expect(capture.truncated).toBe(false);
  expect(capture.redacted).toBe(false);
  return parseJSON<T>(capture.stdoutText, capture.argv.join(" "));
}

function expectPresentBr(adapter: Phase0BrAdapter): void {
  expect(adapter.tool).toBe("br");
  expect(adapter.present).toBe(true);
  expect(adapter.binPath).toBe("/home/admin/.local/bin/br");
  expect(adapter.version).toBe("br 0.2.3");
  expect(adapter.capturedAt).toMatch(/^2026-05-04T/);
}

function expectSchemaCapture(adapter: Phase0BrAdapter): void {
  const schema = parseStdout<{
    readonly tool: string;
    readonly commands: Record<string, unknown>;
    readonly schemas: Record<string, unknown>;
  }>(requireCapture(adapter, "schema"));
  expect(schema.tool).toBe("br");
  for (const command of ["list", "ready", "show", "stats", "status"]) {
    expect(schema.commands).toHaveProperty(command);
  }
  for (const schemaName of ["Issue", "IssueDetails", "ReadyIssue", "ErrorEnvelope"]) {
    expect(schema.schemas).toHaveProperty(schemaName);
  }
}

describe("br adapter contract conformance", () => {
  test("normal, round-trip, negative, and capability cases match the contract", () => {
    assertToolConformance("br");
  });

  test("timeout fixture degrades br timeout capability", () => {
    const fixture = readJSON<GoldenOutputFixture>(goldenOutputPath("br", "timeout"));

    expect(fixture.meta).toMatchObject({
      adapter: "br",
      state: "timeout",
    });
    expect(fixture.argv).toEqual(["br", "--json", "list"]);
    expect(fixture.exit).toBe(124);
    expect(fixture.stderrText).toContain("timeout");
    expect(fixture.capabilities?.["br._timeout"]).toMatchObject({
      status: "degraded",
      notes: "exceeded ENVELOPE_TIMEOUT_S; adapter must surface; do not retry without backoff",
    });
  });

  test("mapBrToBeadListResponse rewrites raw br --json into BeadListResponse shape", () => {
    // hp-b6r2: the conformance harness now validates the *daemon-facing*
    // BeadListResponse contract; the mapper is what bridges raw br stdout
    // into that shape. This test pins the contract: snake_case fields like
    // issue_type / created_at / has_more must come out camelCase, every
    // bead must carry schemaVersion, and the response must be `{items, page}`
    // — not `{issues, total, has_more}`.
    const raw = {
      issues: [
        {
          id: "hp-r3i",
          title: "Plumb the schema package",
          description: "Generate TS + Go from one OpenAPI source.",
          status: "open",
          priority: 1,
          issue_type: "task",
          created_at: "2026-05-02T22:42:34.352955Z",
          updated_at: "2026-05-04T10:11:12.000000Z",
          created_by: "ubuntu",
          source_repo: ".",
          compaction_level: 0,
          original_size: 0,
          dependency_count: 2,
          dependent_count: 5,
        },
      ],
      total: 1,
      limit: 250,
      offset: 0,
      has_more: false,
    };

    const mapped = mapBrToBeadListResponse(raw) as {
      items: Array<Record<string, unknown>>;
      page: { hasMore: boolean; total?: number };
    };

    // Top-level shape: {items, page} — never raw {issues, total, has_more}.
    expect(Object.keys(mapped).sort()).toEqual(["items", "page"]);
    expect(mapped).not.toHaveProperty("issues");
    expect(mapped).not.toHaveProperty("has_more");
    expect(mapped.page.hasMore).toBe(false);
    expect(mapped.page.total).toBe(1);
    expect(mapped.items).toHaveLength(1);

    const bead = mapped.items[0]!;

    // schemaVersion is non-negotiable per OpenAPI Bead.required.
    expect(bead.schemaVersion).toBe(BR_BEAD_SCHEMA_VERSION);

    // camelCase boundary: every snake_case field rewritten or dropped.
    expect(bead.issueType).toBe("task");
    expect(bead.createdAt).toBe("2026-05-02T22:42:34.352955Z");
    expect(bead.updatedAt).toBe("2026-05-04T10:11:12.000000Z");
    expect(bead.createdBy).toBe("ubuntu");
    expect(bead.sourceRepo).toBe(".");
    expect(bead).not.toHaveProperty("issue_type");
    expect(bead).not.toHaveProperty("created_at");
    expect(bead).not.toHaveProperty("updated_at");
    expect(bead).not.toHaveProperty("created_by");
    expect(bead).not.toHaveProperty("source_repo");

    // br-only fields that don't appear on the OpenAPI Bead must be dropped.
    expect(bead).not.toHaveProperty("compaction_level");
    expect(bead).not.toHaveProperty("original_size");
    expect(bead).not.toHaveProperty("dependency_count");
    expect(bead).not.toHaveProperty("dependent_count");

    // Required core fields preserved verbatim.
    expect(bead.id).toBe("hp-r3i");
    expect(bead.title).toBe("Plumb the schema package");
    expect(bead.status).toBe("open");
    expect(bead.priority).toBe(1);
    expect(bead.description).toBe("Generate TS + Go from one OpenAPI source.");
  });

  test("mapBrToBeadListResponse handles empty br ready/list output", () => {
    // br --json on an empty project emits {issues: [], total: 0, has_more: false};
    // the mapper must produce {items: [], page: {hasMore: false, total: 0}}.
    const mapped = mapBrToBeadListResponse({
      issues: [],
      total: 0,
      limit: 250,
      offset: 0,
      has_more: false,
    }) as { items: unknown[]; page: { hasMore: boolean; total?: number } };
    expect(mapped.items).toEqual([]);
    expect(mapped.page.hasMore).toBe(false);
    expect(mapped.page.total).toBe(0);
  });

  test("mapBrToBeadListResponse drops issues missing required fields", () => {
    // The mapper enforces required Bead fields; malformed entries are
    // dropped rather than emitted as half-Beads. This keeps schema
    // validation honest and surfaces fixture corruption as "items
    // dropped," not "schema passes with garbage."
    const mapped = mapBrToBeadListResponse({
      issues: [
        { id: "hp-ok", title: "valid", status: "open", priority: 0, issue_type: "task" },
        { id: "hp-no-title", status: "open", priority: 0, issue_type: "task" },
        { id: "hp-no-priority", title: "no prio", status: "open", issue_type: "task" },
        { id: "hp-bad-priority", title: "bad", status: "open", priority: "high", issue_type: "task" },
      ],
      total: 4,
      has_more: false,
    }) as { items: Array<{ id: string }> };
    expect(mapped.items.map((bead) => bead.id)).toEqual(["hp-ok"]);
  });

  test("phase0 real-VPS fixture matrix advertises br for every scenario", () => {
    const manifest = readJSON<Phase0Manifest>(resolve(phase0Root, "manifest.json"));
    expect(manifest.mode).toBe("real-vps");
    expect(manifest.realVpsAcceptance).toBe(true);
    expect(manifest.adapterTools).toContain("br");
    expect(manifest.scenarios).toEqual([...phase0Scenarios]);

    for (const scenario of phase0Scenarios) {
      const index = readPhase0Index(scenario);
      expect(index).toEqual({
        scenario,
        mode: "real-vps",
        adapters: expect.arrayContaining(["br"]),
      });
      expectPresentBr(readPhase0Br(scenario));
    }
  });

  test("phase0 fresh br captures cover empty ready/list/stats surfaces", () => {
    const adapter = readPhase0Br("fresh");
    expect(adapter.capabilities["br.list.read"]?.status).toBe("ok");
    expect(adapter.capabilities["br.ready.read"]?.status).toBe("ok");
    expect(adapter.capabilities["br.show.read"]?.status).toBe("ok");

    const version = requireCapture(adapter, "version");
    expect(version.exit).toBe(0);
    expect(version.stdoutText).toBe("br 0.2.3\n");

    const ready = parseStdout<unknown[]>(requireCapture(adapter, "ready_json_empty"));
    expect(ready).toEqual([]);

    const list = parseStdout<{ readonly issues: unknown[]; readonly page: { readonly total: number } }>(
      requireCapture(adapter, "list_json_empty"),
    );
    expect(list.issues).toEqual([]);
    expect(list.page.total).toBe(0);

    const stats = parseStdout<{ readonly summary: { readonly total_issues: number } }>(
      requireCapture(adapter, "stats_json_empty"),
    );
    expect(stats.summary.total_issues).toBe(0);
    expectSchemaCapture(adapter);
  });

  test("phase0 active br captures cover list, ready, show, and stats payloads", () => {
    const adapter = readPhase0Br("active");
    expect(adapter.capabilities["br.list.read"]?.status).toBe("ok");
    expect(adapter.capabilities["br.ready.read"]?.status).toBe("ok");
    expect(adapter.capabilities["br.show.read"]?.status).toBe("ok");

    const ready = parseStdout<ReadonlyArray<{ readonly id: string; readonly status: string }>>(
      requireCapture(adapter, "ready_json"),
    );
    expect(ready.length).toBeGreaterThan(0);
    expect(ready[0]?.id).toBe("nexusaudio-9ga.1");
    expect(ready[0]?.status).toBe("open");

    const list = parseStdout<{
      readonly issues: ReadonlyArray<{ readonly id: string; readonly dependency_count: number }>;
      readonly total: number;
      readonly has_more: boolean;
    }>(requireCapture(adapter, "list_json"));
    expect(list.total).toBe(list.issues.length);
    expect(list.has_more).toBe(false);
    expect(list.issues.map((issue) => issue.id)).toContain("nexusaudio-9ga.1");

    const shown = parseStdout<ReadonlyArray<{ readonly id: string; readonly parent: string }>>(
      requireCapture(adapter, "show_existing_json"),
    );
    expect(shown).toHaveLength(1);
    expect(shown[0]).toEqual(expect.objectContaining({ id: "nexusaudio-9ga.1" }));
    expect(shown[0]?.parent).toBe("nexusaudio-9ga");

    const stats = parseStdout<{ readonly summary: { readonly total_issues: number } }>(
      requireCapture(adapter, "stats_json"),
    );
    expect(stats.summary.total_issues).toBeGreaterThan(0);
    expectSchemaCapture(adapter);
  });

  test("phase0 failure br capture preserves structured issue-not-found errors", () => {
    const adapter = readPhase0Br("failure");
    expect(adapter.capabilities["br.show.read"]?.status).toBe("degraded");
    expect(adapter.capabilities["br.show.read"]?.notes).toContain("ISSUE_NOT_FOUND");

    const missing = requireCapture(adapter, "show_missing_json");
    expect(missing.argv).toEqual(["br", "show", "does-not-exist", "--json"]);
    expect(missing.exit).toBe(3);
    expect(missing.stderrText).toBe("");
    expect(missing.tags).toEqual(["error", "issue-not-found"]);

    const envelope = parseStdout<{
      readonly error: {
        readonly code: string;
        readonly retryable: boolean;
        readonly context: { readonly searched_id: string };
      };
    }>(missing);
    expect(envelope.error.code).toBe("ISSUE_NOT_FOUND");
    expect(envelope.error.retryable).toBe(false);
    expect(envelope.error.context.searched_id).toBe("does-not-exist");
    expectSchemaCapture(adapter);
  });
});
