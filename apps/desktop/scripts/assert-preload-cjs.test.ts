// hp-fohm: unit tests for the findEsmTopLevel predicate that
// gates the preload bundle. The build script post-step uses this
// predicate against the actual built file; here we exercise the
// rule against minimal fixtures so a regression in the regex
// surfaces independently of running a full desktop build.

import { expect, test } from "bun:test";
import { findEsmTopLevel } from "./assert-preload-cjs.ts";

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
  // dist-electron/preload.cjs at hp-fohm landing time. If a future
  // tsdown upgrade changes the preamble, this test surfaces the
  // change before the build-script assertion catches it on real
  // output.
  const cjs = [
    `Object.defineProperty(exports, Symbol.toStringTag, { value: 'Module' });`,
    `let node_crypto = require("node:crypto");`,
    `let electron = require("electron");`,
    "",
    "//#region src/shared/ipc-contract.ts",
    `const DAEMON_REQUEST_METHODS = ["ping","health"];`,
    `module.exports = { DAEMON_REQUEST_METHODS };`,
  ].join("\n");
  expect(findEsmTopLevel(cjs)).toBeNull();
});
