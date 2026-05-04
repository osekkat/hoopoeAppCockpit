import { afterEach, beforeAll, describe, expect, test } from "bun:test";
import { resolve } from "node:path";
import {
  PROBLEM_JSON_CONTENT_TYPE,
  ProblemAssertionError,
  assertProblemMatchesRegistry,
  assertResponseIsProblemJson,
  loadProblemTypes,
  renderProblemEnvelope,
  useProblems,
} from "../src/index.ts";

const REPO_ROOT = resolve(__dirname, "..", "..", "..");

beforeAll(() => {
  useProblems(loadProblemTypes({ repoRoot: REPO_ROOT }));
});

afterEach(() => {
  useProblems(loadProblemTypes({ repoRoot: REPO_ROOT }));
});

function fakeResponse(status: number, contentType: string): { status: number; headers: { get: (n: string) => string | null } } {
  return {
    status,
    headers: { get: (n: string) => (n.toLowerCase() === "content-type" ? contentType : null) },
  };
}

describe("hp-g6sp :: assertions", () => {
  test("assertResponseIsProblemJson accepts the canonical content-type + 4xx/5xx", () => {
    expect(() => assertResponseIsProblemJson(fakeResponse(400, PROBLEM_JSON_CONTENT_TYPE))).not.toThrow();
    expect(() => assertResponseIsProblemJson(fakeResponse(500, `${PROBLEM_JSON_CONTENT_TYPE}; charset=utf-8`))).not.toThrow();
  });

  test("assertResponseIsProblemJson rejects wrong content-type", () => {
    expect(() => assertResponseIsProblemJson(fakeResponse(400, "application/json"))).toThrow(ProblemAssertionError);
  });

  test("assertResponseIsProblemJson rejects non-error status", () => {
    expect(() => assertResponseIsProblemJson(fakeResponse(200, PROBLEM_JSON_CONTENT_TYPE))).toThrow(ProblemAssertionError);
    expect(() => assertResponseIsProblemJson(fakeResponse(302, PROBLEM_JSON_CONTENT_TYPE))).toThrow(ProblemAssertionError);
  });

  test("assertProblemMatchesRegistry passes on a freshly-rendered envelope", () => {
    const envelope = renderProblemEnvelope("bead.cycle-detected", {
      extensions: { cyclePath: "a->b->a" },
      correlationId: "x",
    });
    expect(() => assertProblemMatchesRegistry(envelope, "bead.cycle-detected")).not.toThrow();
  });

  test("assertProblemMatchesRegistry catches type drift", () => {
    const envelope = renderProblemEnvelope("not-found", { extensions: {} });
    envelope.type = "https://hoopoe.io/problems/wrong";
    expect(() => assertProblemMatchesRegistry(envelope, "not-found")).toThrow(ProblemAssertionError);
  });

  test("assertProblemMatchesRegistry catches surface drift", () => {
    const envelope = renderProblemEnvelope("auth.pairing.token-expired", {
      extensions: { issuedAt: "x", expiredAt: "y" },
    });
    envelope.surface = "toast";
    expect(() => assertProblemMatchesRegistry(envelope, "auth.pairing.token-expired")).toThrow(ProblemAssertionError);
  });

  test("assertProblemMatchesRegistry catches status drift", () => {
    const envelope = renderProblemEnvelope("internal-error", { extensions: { correlationId: "x" } });
    envelope.status = 200;
    expect(() => assertProblemMatchesRegistry(envelope, "internal-error")).toThrow(ProblemAssertionError);
  });
});
