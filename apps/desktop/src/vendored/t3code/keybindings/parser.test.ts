import { expect, test } from "bun:test";
import {
  compileResolvedKeybindingRule,
  compileResolvedKeybindingsConfig,
  parseKeybindingShortcut,
  parseKeybindingWhenExpression,
} from "./parser.ts";

test("parseKeybindingShortcut: cmd+shift+p decomposes correctly", () => {
  expect(parseKeybindingShortcut("cmd+shift+p")).toEqual({
    key: "p",
    metaKey: true,
    ctrlKey: false,
    shiftKey: true,
    altKey: false,
    modKey: false,
  });
});

test("parseKeybindingShortcut: mod, control, option aliases", () => {
  expect(parseKeybindingShortcut("mod+k")?.modKey).toBe(true);
  expect(parseKeybindingShortcut("control+/")?.ctrlKey).toBe(true);
  expect(parseKeybindingShortcut("option+enter")?.altKey).toBe(true);
});

test("parseKeybindingShortcut: trailing literal `+` and `space`/`esc` aliases", () => {
  expect(parseKeybindingShortcut("cmd++")?.key).toBe("+");
  expect(parseKeybindingShortcut("space")?.key).toBe(" ");
  expect(parseKeybindingShortcut("esc")?.key).toBe("escape");
});

test("parseKeybindingShortcut: rejects two-letter combos / no key (modifiers only)", () => {
  expect(parseKeybindingShortcut("a+b")).toBeNull();
  expect(parseKeybindingShortcut("cmd+shift")).toBeNull();
  // Trailing-`+` quirk preserved verbatim from t3code: empty input becomes
  // the literal `+` key (the trailing-empties pop reattaches `+`).
  expect(parseKeybindingShortcut("")?.key).toBe("+");
});

test("parseKeybindingWhenExpression: identifiers, ops, parens", () => {
  expect(parseKeybindingWhenExpression("a")).toEqual({ type: "identifier", name: "a" });
  expect(parseKeybindingWhenExpression("!a")).toEqual({
    type: "not",
    node: { type: "identifier", name: "a" },
  });
  expect(parseKeybindingWhenExpression("a && b")).toEqual({
    type: "and",
    left: { type: "identifier", name: "a" },
    right: { type: "identifier", name: "b" },
  });
  expect(parseKeybindingWhenExpression("a || b && c")).toEqual({
    type: "or",
    left: { type: "identifier", name: "a" },
    right: {
      type: "and",
      left: { type: "identifier", name: "b" },
      right: { type: "identifier", name: "c" },
    },
  });
  expect(parseKeybindingWhenExpression("(a || b) && !c")).toEqual({
    type: "and",
    left: {
      type: "or",
      left: { type: "identifier", name: "a" },
      right: { type: "identifier", name: "b" },
    },
    right: { type: "not", node: { type: "identifier", name: "c" } },
  });
});

test("parseKeybindingWhenExpression: rejects unbalanced parens / dangling op / unknown chars", () => {
  expect(parseKeybindingWhenExpression("(a")).toBeNull();
  expect(parseKeybindingWhenExpression("a &&")).toBeNull();
  expect(parseKeybindingWhenExpression("a $$ b")).toBeNull();
  expect(parseKeybindingWhenExpression("")).toBeNull();
});

test("compileResolvedKeybindingRule: rejects unparseable shortcut", () => {
  expect(
    compileResolvedKeybindingRule({ key: "cmd+a+b", command: "x" }),
  ).toBeNull();
});

test("compileResolvedKeybindingRule: rejects unparseable when", () => {
  expect(
    compileResolvedKeybindingRule({ key: "cmd+a", command: "x", when: "(((" }),
  ).toBeNull();
});

test("compileResolvedKeybindingsConfig: drops unparseable entries, keeps order", () => {
  // `cmd+a+b` is unparseable (two non-modifier tokens). Bare strings like
  // `INVALID` parse as the literal key `invalid`, which is intentional —
  // t3code's parser supports literal-key shortcuts without modifiers.
  const compiled = compileResolvedKeybindingsConfig([
    { key: "cmd+1", command: "stage.planning" },
    { key: "cmd+a+b", command: "x" },
    { key: "cmd+2", command: "stage.beads" },
  ]);
  expect(compiled).toHaveLength(2);
  expect(compiled[0]?.command).toBe("stage.planning");
  expect(compiled[1]?.command).toBe("stage.beads");
});
