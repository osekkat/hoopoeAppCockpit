// hp-fohm: unit tests for the findEsmTopLevel predicate that
// gates the preload bundle. The build script post-step uses this
// predicate against the actual built file; here we exercise the
// rule against minimal fixtures so a regression in the regex
// surfaces independently of running a full desktop build.

import { expect, test } from "bun:test";
import {
  findEsmTopLevel,
  findNodeBuiltinRequire,
} from "./assert-preload-cjs.ts";

test("findEsmTopLevel: structural CJS passes (use strict + Object.defineProperty + require)", () => {
  const cjs = [
    `"use strict";`,
    `Object.defineProperty(exports, Symbol.toStringTag, { value: 'Module' });`,
    `let node_crypto = require("node:crypto");`,
    `let electron = require("electron");`,
    "",
    "// rest of bundle...",
  ].join("\n");
  expect(findEsmTopLevel(cjs)).toBeNull();
});

test("findEsmTopLevel: top-level `import` is rejected (the actual hp-fohm regression)", () => {
  const esm = `import { randomUUID } from "node:crypto";\nimport { contextBridge } from "electron";`;
  const offence = findEsmTopLevel(esm);
  expect(offence).not.toBeNull();
  expect(offence).toContain("import");
});

test("findEsmTopLevel: top-level `export` is rejected", () => {
  const esm = `const x = 1;\nexport { x };`;
  const offence = findEsmTopLevel(esm);
  expect(offence).not.toBeNull();
  expect(offence).toContain("export");
});

test("findEsmTopLevel: top-level `import.meta` is rejected (used by ESM-style URL resolution)", () => {
  const esm = `import.meta.url\n`;
  const offence = findEsmTopLevel(esm);
  expect(offence).not.toBeNull();
  expect(offence).toContain("import.meta");
});

test("findEsmTopLevel: leading whitespace before `import` still trips the rule", () => {
  const esm = `\n  import { x } from "y";`;
  expect(findEsmTopLevel(esm)).not.toBeNull();
});

test("findEsmTopLevel: bundler-emitted CJS preamble passes (defineProperty + require + module.exports)", () => {
  // This fixture mirrors the actual head -5 of the post-fix
  // dist-electron/preload.cjs at hp-fohm landing time, except that
  // hp-r1tk also requires the file no longer require("node:crypto")
  // — see findNodeBuiltinRequire for that gate.
  const cjs = [
    `Object.defineProperty(exports, Symbol.toStringTag, { value: 'Module' });`,
    `let electron = require("electron");`,
    "",
    "//#region src/shared/ipc-contract.ts",
    `const DAEMON_REQUEST_METHODS = ["ping","health"];`,
    `module.exports = { DAEMON_REQUEST_METHODS };`,
  ].join("\n");
  expect(findEsmTopLevel(cjs)).toBeNull();
});

// ── findNodeBuiltinRequire (hp-r1tk) ─────────────────────────────

test("findNodeBuiltinRequire: post-fix preamble (require electron only) passes", () => {
  const ok = [
    `Object.defineProperty(exports, Symbol.toStringTag, { value: 'Module' });`,
    `let electron = require("electron");`,
    `module.exports = { hoopoeBridge: {} };`,
  ].join("\n");
  expect(findNodeBuiltinRequire(ok)).toBeNull();
});

test("findNodeBuiltinRequire: require(\"node:crypto\") rejected (the actual hp-r1tk regression)", () => {
  const bad = `let node_crypto = require("node:crypto");`;
  const offence = findNodeBuiltinRequire(bad);
  expect(offence).not.toBeNull();
  expect(offence).toContain("node:crypto");
});

test("findNodeBuiltinRequire: require('node:fs') (single quotes) also rejected", () => {
  const bad = `let fs = require('node:fs');`;
  const offence = findNodeBuiltinRequire(bad);
  expect(offence).not.toBeNull();
  expect(offence).toContain("node:fs");
});

test("findNodeBuiltinRequire: any node-builtin name surfaces in the diagnostic", () => {
  for (const name of ["path", "child_process", "url", "os"]) {
    const bad = `let x = require("node:${name}");`;
    const offence = findNodeBuiltinRequire(bad);
    expect(offence).not.toBeNull();
    expect(offence).toContain(`node:${name}`);
  }
});

test("findNodeBuiltinRequire: bare-name requires (no node: prefix) are NOT rejected by this gate", () => {
  // The renderer-isolation lint covers `require("electron")`, etc.
  // — this gate is specifically about the `node:` builtin scheme.
  const ok = `let electron = require("electron");`;
  expect(findNodeBuiltinRequire(ok)).toBeNull();
});
