// Smoke tests for the codex-shape-scrub matcher (hp-4nrd). The full
// executable runs in CI via `bun run lint:codex-shape-scrub`; here we
// unit-test the per-line matcher logic against representative fixtures.

import { expect, test } from "bun:test";
import { scanFile } from "./check-codex-shape-scrub.ts";

test("scrub: flags PascalCase Codex identifiers in real code", () => {
  const source = [
    "export class Thread {",
    "  // doesn't matter",
    "}",
    "export interface Provider {}",
    "export type Conversation = unknown;",
    "export const MessageList = [];",
    "export const MAX_THREAD_MESSAGES = 2000;",
    "",
  ].join("\n");
  const findings = scanFile("/fake/file.ts", source);
  const ids = findings.map((f) => f.identifier).toSorted();
  expect(ids).toEqual([
    "Conversation",
    "MAX_THREAD_MESSAGES",
    "MessageList",
    "Provider",
    "Thread",
  ]);
});

test("scrub: skips banned identifiers inside string literals", () => {
  const source = [
    'const reason = "settings-changed:daemon.thread";',
    "const note = 'orchestrator-chat tending agent surfaces here';",
    "const audit = `Thread metaphor mentioned in audit prose only`;",
  ].join("\n");
  expect(scanFile("/fake/file.ts", source)).toHaveLength(0);
});

test("scrub: skips banned identifiers inside line comments", () => {
  const source = [
    "// Codex-shape: Thread, Chat, Provider — banned as identifiers.",
    "/* But Conversation in a block comment is also fine. */",
    "const x = 1;",
  ].join("\n");
  expect(scanFile("/fake/file.ts", source)).toHaveLength(0);
});

test("scrub: skips identifiers inside multi-line block comments", () => {
  const source = [
    "/**",
    " * The MessageList class shape (Thread / Chat / Provider) is a",
    " * Codex carryover that doesn't fit Hoopoe.",
    " */",
    "const x = 1;",
  ].join("\n");
  expect(scanFile("/fake/file.ts", source)).toHaveLength(0);
});

test("scrub: whole-word matching does not flag compound identifiers", () => {
  // `OrchestratorChat` doesn't trigger `\bChat\b` because there's no word
  // boundary between `r` and `C` (both word chars). This is intentional —
  // the orchestrator-chat tending agent is legitimately named.
  const source = [
    "export class OrchestratorChat {}",
    "export type ChatBox = string;",
    "function getChats() { return []; }",
    "export const threadId = 'br-123';",
    "export const threadIds = ['br-123'];",
  ].join("\n");
  // Note: `ChatBox` and `getChats` start with `Chat`/`Chats` so `\bChat\b`
  // would fire on the four-letter prefix `Chat` followed by a non-word
  // boundary. Wait — `\bChat\b` requires a non-word char AFTER `Chat`;
  // `ChatBox` has `B` next, which is a word char, so no boundary →
  // no match. Same for `getChats`: `s` is a word char.
  expect(scanFile("/fake/file.ts", source)).toHaveLength(0);
});

test("scrub: flags camelCase compound forms (messageList, threadList)", () => {
  const source = [
    "export const messageList = [];",
    "export const threadList = new Set();",
  ].join("\n");
  const ids = scanFile("/fake/file.ts", source).map((f) => f.identifier).toSorted();
  expect(ids).toEqual(["messageList", "threadList"]);
});

test("scrub: respects // codex-shape-scrub-ok: <reason> suppression", () => {
  const source = [
    "// codex-shape-scrub-ok: lift docstring includes the upstream identifier",
    "export class ProviderOnlyForLiftDocstring {}",
    "// codex-shape-scrub-ok: ",
    "export class Thread {}", // empty reason → suppression NOT honored
  ].join("\n");
  const findings = scanFile("/fake/file.ts", source);
  // Line 2 suppressed; line 4 NOT suppressed (empty reason).
  // ProviderOnlyForLiftDocstring contains `Provider` as a prefix; whole-word
  // matching means the next char `O` makes no boundary → no match.
  // Line 4: `Thread` is whole-word → match.
  expect(findings.map((f) => f.identifier).toSorted()).toEqual(["Thread"]);
});

test("scrub: reports file/line/column accurately", () => {
  const source = [
    "const x = 1;",
    "export class Thread {}",
    "",
  ].join("\n");
  const findings = scanFile("/fake/path.ts", source);
  expect(findings).toHaveLength(1);
  expect(findings[0]?.file).toBe("/fake/path.ts");
  expect(findings[0]?.line).toBe(2);
  expect(findings[0]?.column).toBeGreaterThan(0);
  expect(findings[0]?.identifier).toBe("Thread");
});

test("scrub: flags banned imports (effect / @effect/* / @t3tools/*)", () => {
  const source = [
    'import { Schema } from "effect";',
    'import * as Eff from "effect/Effect";',
    'import { Layer } from "@effect/platform-node";',
    'import { ServerSettings } from "@t3tools/contracts";',
  ].join("\n");
  const findings = scanFile("/fake/file.ts", source);
  const sources = findings.map((f) => f.identifier).toSorted();
  expect(sources).toEqual(["@effect/*", "@t3tools/*", "effect", "effect/*"]);
});

test("scrub: same-line suppression skips banned imports", () => {
  // Same-line suppression matches the existing identifier-suppression
  // behavior: the `// codex-shape-scrub-ok: <reason>` annotation must be
  // on the same line as the offending code. Multi-line suppression is a
  // future enhancement; for now keep the model uniform.
  const source = [
    'import { Schema } from "effect"; // codex-shape-scrub-ok: vendored shim re-export',
  ].join("\n");
  expect(scanFile("/fake/file.ts", source)).toHaveLength(0);
});

test("scrub: legitimate imports (react / node:fs / @hoopoe/*) are not flagged", () => {
  const source = [
    'import { useEffect } from "react";', // useEffect is whole-word OK; "react" not banned
    'import * as FS from "node:fs";',
    'import { type ToneToken } from "@hoopoe/design-system";',
    'import { writeFileStringAtomically } from "../vendored/t3code/settings/atomicWrite.ts";',
  ].join("\n");
  expect(scanFile("/fake/file.ts", source)).toHaveLength(0);
});
