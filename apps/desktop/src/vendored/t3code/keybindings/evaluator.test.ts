import { expect, test } from "bun:test";
import { compileResolvedKeybindingsConfig } from "./parser.ts";
import {
  evaluateWhenNode,
  matchesShortcut,
  matchesWhenClause,
  resolveShortcutCommand,
} from "./evaluator.ts";

test("evaluateWhenNode: identifier / not / and / or", () => {
  const ctx = { a: true, b: false };
  expect(evaluateWhenNode({ type: "identifier", name: "a" }, ctx)).toBe(true);
  expect(evaluateWhenNode({ type: "identifier", name: "b" }, ctx)).toBe(false);
  expect(evaluateWhenNode({ type: "identifier", name: "missing" }, ctx)).toBe(false);
  expect(
    evaluateWhenNode({ type: "not", node: { type: "identifier", name: "a" } }, ctx),
  ).toBe(false);
  expect(
    evaluateWhenNode(
      {
        type: "and",
        left: { type: "identifier", name: "a" },
        right: { type: "identifier", name: "b" },
      },
      ctx,
    ),
  ).toBe(false);
  expect(
    evaluateWhenNode(
      {
        type: "or",
        left: { type: "identifier", name: "a" },
        right: { type: "identifier", name: "b" },
      },
      ctx,
    ),
  ).toBe(true);
});

test("evaluateWhenNode: well-known true/false identifiers short-circuit", () => {
  expect(evaluateWhenNode({ type: "identifier", name: "true" }, {})).toBe(true);
  expect(evaluateWhenNode({ type: "identifier", name: "false" }, {})).toBe(false);
});

test("matchesWhenClause: missing clause means always match", () => {
  expect(matchesWhenClause(undefined, {})).toBe(true);
});

test("matchesShortcut: macOS uses meta for `mod`, others use ctrl", () => {
  const event = {
    key: "k",
    metaKey: true,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
  };
  const shortcut = {
    key: "k",
    metaKey: false,
    ctrlKey: false,
    shiftKey: false,
    altKey: false,
    modKey: true,
  };
  expect(matchesShortcut(event, shortcut, "darwin")).toBe(true);
  expect(matchesShortcut(event, shortcut, "linux")).toBe(false);

  const linuxEvent = { ...event, metaKey: false, ctrlKey: true };
  expect(matchesShortcut(linuxEvent, shortcut, "linux")).toBe(true);
});

test("resolveShortcutCommand: last-rule-wins on conflict (matches both, picks later)", () => {
  const config = compileResolvedKeybindingsConfig([
    { key: "cmd+k", command: "command-palette.open" },
    { key: "cmd+k", command: "stage.planning" },
  ]);
  const command = resolveShortcutCommand(
    { key: "k", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false },
    config,
    {},
    "darwin",
  );
  expect(command).toBe("stage.planning");
});

test("resolveShortcutCommand: skips rules whose `when` is unsatisfied", () => {
  const config = compileResolvedKeybindingsConfig([
    {
      key: "cmd+k",
      command: "command-palette.open",
      when: "!terminalFocus",
    },
    { key: "cmd+k", command: "stage.planning", when: "terminalFocus" },
  ]);
  const command = resolveShortcutCommand(
    { key: "k", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false },
    config,
    { terminalFocus: false },
    "darwin",
  );
  expect(command).toBe("command-palette.open");
});

test("resolveShortcutCommand: returns null when no rule matches", () => {
  const config = compileResolvedKeybindingsConfig([
    { key: "cmd+k", command: "command-palette.open" },
  ]);
  const command = resolveShortcutCommand(
    { key: "x", metaKey: false, ctrlKey: false, shiftKey: false, altKey: false },
    config,
    {},
    "darwin",
  );
  expect(command).toBeNull();
});
