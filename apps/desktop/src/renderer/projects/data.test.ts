// hp-ilt — Phase 4 project lifecycle data-layer tests.
//
// Covers the pure helpers (validation, bridge resolution, error class).
// Hook-based tests live in `ProjectEntry.test.tsx` where the QueryClient
// is wired up.

import { afterEach, beforeEach, expect, test } from "bun:test";
import {
  ProjectsBridgeUnavailableError,
  resolveDaemonRequest,
  validateCloneInput,
  validateCreateInput,
  validateImportInput,
} from "./data.ts";

// `bun test` runs without a DOM by default. Tests that need `window` to
// exist install a minimal stub on globalThis and remove it after the test.
function installWindowStub(): { hoopoe?: { daemon?: { request?: unknown } } } {
  const stub: { hoopoe?: { daemon?: { request?: unknown } } } = {};
  (globalThis as { window?: unknown }).window = stub;
  return stub;
}

beforeEach(() => {
  delete (globalThis as { window?: unknown }).window;
});

afterEach(() => {
  delete (globalThis as { window?: unknown }).window;
});

test("validateImportInput: rootPath required + must be absolute", () => {
  expect(validateImportInput({ rootPath: "" })).toEqual({
    rootPath: "absolute VPS path is required",
  });
  expect(validateImportInput({ rootPath: "   " })).toEqual({
    rootPath: "absolute VPS path is required",
  });
  expect(validateImportInput({ rootPath: "data/projects/foo" })).toEqual({
    rootPath: "path must be absolute (start with `/`)",
  });
  expect(validateImportInput({ rootPath: "/data/projects/foo" })).toEqual({});
});

test("validateCreateInput: name + originRemote required, remote must look like a URL", () => {
  expect(validateCreateInput({ name: "", originRemote: "" })).toEqual({
    name: "name is required",
    originRemote: "origin remote is required (plan.md §1.1 — v1 has no remoteless mode)",
  });
  expect(validateCreateInput({ name: "ok", originRemote: "not-a-url" })).toEqual({
    originRemote: "expected a git remote URL or scp-style host:path",
  });
  expect(validateCreateInput({ name: "ok", originRemote: "git@github.com:org/repo.git" })).toEqual({});
  expect(validateCreateInput({ name: "ok", originRemote: "https://github.com/org/repo.git" })).toEqual({});
  expect(validateCreateInput({ name: "ok", originRemote: "ssh://git@example.com/org/repo.git" })).toEqual({});
});

test("validateCloneInput: remote URL required and validated", () => {
  expect(validateCloneInput({ remoteUrl: "" })).toEqual({ remoteUrl: "remote URL is required" });
  expect(validateCloneInput({ remoteUrl: "garbage with spaces" })).toEqual({
    remoteUrl: "expected a git remote URL or scp-style host:path",
  });
  expect(validateCloneInput({ remoteUrl: "https://github.com/org/repo.git" })).toEqual({});
});

test("resolveDaemonRequest: returns null when no window or bridge", () => {
  // No window.hoopoe — happens in pre-launch states + jsdom defaults.
  expect(resolveDaemonRequest()).toBeNull();
});

test("resolveDaemonRequest: returns null when window exists but bridge is missing", () => {
  installWindowStub();
  expect(resolveDaemonRequest()).toBeNull();
});

test("resolveDaemonRequest: returns the injected request fn", async () => {
  const calls: Array<[string, unknown]> = [];
  const stub = installWindowStub();
  stub.hoopoe = {
    daemon: {
      request: async (method: string, body: unknown) => {
        calls.push([method, body]);
        return { ok: true };
      },
    },
  };
  const request = resolveDaemonRequest();
  expect(typeof request).toBe("function");
  const result = await request!("projects.import", { rootPath: "/data/projects/x" });
  expect(result).toEqual({ ok: true });
  expect(calls).toEqual([["projects.import", { rootPath: "/data/projects/x" }]]);
});

test("ProjectsBridgeUnavailableError: stable name + helpful message", () => {
  const err = new ProjectsBridgeUnavailableError();
  expect(err.name).toBe("ProjectsBridgeUnavailableError");
  expect(err.message.toLowerCase()).toContain("daemon");
  expect(err.message.toLowerCase()).toContain("pair");
  expect(err instanceof Error).toBe(true);
});
