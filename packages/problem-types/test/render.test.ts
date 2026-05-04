import { afterEach, beforeAll, describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import {
  loadProblemTypes,
  renderProblemEnvelope,
  renderTemplate,
  useProblems,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

beforeAll(() => {
  useProblems(loadProblemTypes({ repoRoot: REPO_ROOT }));
});

afterEach(() => {
  useProblems(loadProblemTypes({ repoRoot: REPO_ROOT }));
});

describe("hp-g6sp :: renderTemplate", () => {
  test("substitutes {{var}} placeholders", () => {
    expect(renderTemplate("hello {{name}}", { name: "world" })).toBe("hello world");
  });

  test("preserves whitespace inside braces", () => {
    expect(renderTemplate("hello {{ name }}", { name: "world" })).toBe("hello world");
  });

  test("keeps {{var}} literal on missing values", () => {
    expect(renderTemplate("hello {{name}}", {})).toBe("hello {{name}}");
  });

  test("converts non-string values via String()", () => {
    expect(renderTemplate("count={{n}}", { n: 42 })).toBe("count=42");
    expect(renderTemplate("on={{flag}}", { flag: true })).toBe("on=true");
    expect(renderTemplate("path={{p}}", { p: ["a", "b"] })).toBe("path=a,b");
  });
});

describe("hp-g6sp :: renderProblemEnvelope", () => {
  test("builds canonical envelope from id + extensions", () => {
    const envelope = renderProblemEnvelope("bead.cycle-detected", {
      extensions: { cyclePath: "hp-a -> hp-b -> hp-a" },
      correlationId: "audit-1",
      instance: "/v1/projects/proj/beads/x/deps",
    });
    expect(envelope.type).toBe("https://hoopoe.io/problems/bead/cycle-detected");
    expect(envelope.title).toBe("Dependency cycle detected");
    expect(envelope.status).toBe(422);
    expect(envelope.surface).toBe("banner");
    expect(envelope.actionability).toBe("edit-deps");
    expect(envelope.user_message).toBe("Adding this dependency would create a cycle: hp-a -> hp-b -> hp-a");
    expect(envelope.correlation_id).toBe("audit-1");
    expect(envelope.instance).toBe("/v1/projects/proj/beads/x/deps");
    expect(envelope.cyclePath).toBe("hp-a -> hp-b -> hp-a");
  });

  test("emits detail when detail_template is present", () => {
    const envelope = renderProblemEnvelope("auth.pairing.token-expired", {
      extensions: { issuedAt: "2026-05-01T00:00:00Z", expiredAt: "2026-05-03T00:00:00Z" },
    });
    expect(envelope.detail).toBe("Pairing token issued at 2026-05-01T00:00:00Z expired at 2026-05-03T00:00:00Z.");
  });

  test("omits detail when detail_template is absent", () => {
    const envelope = renderProblemEnvelope("project.not-found", {
      extensions: { projectId: "proj-1" },
    });
    expect(envelope.detail).toBeUndefined();
  });

  test("ignores extension keys that would override reserved fields", () => {
    const envelope = renderProblemEnvelope("not-found", {
      extensions: {
        type: "https://attacker.example/spoof",
        title: "Spoofed",
        status: 200,
        my_field: "ok",
      },
    });
    expect(envelope.type).toBe("https://hoopoe.io/problems/not-found");
    expect(envelope.title).toBe("Not Found");
    expect(envelope.status).toBe(404);
    expect(envelope.my_field).toBe("ok");
  });
});
