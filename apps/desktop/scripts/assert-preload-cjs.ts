#!/usr/bin/env bun
// hp-fohm: build-time guard against the preload bundle regressing
// to ESM. Electron's sandboxed preload loader expects CommonJS;
// any top-level `import` / `export` keyword crashes the renderer
// at startup with "SyntaxError: Cannot use import statement
// outside a module" and silently disables the entire window.hoopoe
// IPC bridge.
//
// Runs as the last step of `bun run --cwd apps/desktop build`.
// Exits non-zero (failing the build) when the regex below matches.

import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { exit } from "node:process";

const PRELOAD_PATH = resolve(
  fileURLToPath(import.meta.url),
  "..",
  "..",
  "dist-electron",
  "preload.js",
);

/** Returns the first ESM-shaped offence detected in `source`, or
 * null when the source is structurally CommonJS at the top level.
 *
 * Looks for top-level `import` / `export` / `import.meta` only —
 * occurrences inside string literals or comments are tolerated
 * because the regex is anchored to a line start (or the start of
 * file). False-positive risk is acceptable here; the build
 * pipeline runs this on bundler output, not hand-written code. */
export function findEsmTopLevel(source: string): string | null {
  const TOP_LEVEL_KEYWORD = /(^|\n)\s*(import|export)\s/;
  const match = TOP_LEVEL_KEYWORD.exec(source);
  if (match) return `top-level \`${match[2]}\` keyword detected`;
  if (/(^|\n)\s*import\.meta/.test(source)) {
    return "top-level `import.meta` reference detected";
  }
  return null;
}

/** hp-r1tk: returns the first `require("node:...")` (or
 * `require('node:...')`) offence detected in `source`, or null
 * when no node-builtin require is present. Electron's sandboxed
 * preload context does not expose Node builtins; any `node:`
 * require crashes the preload at load time with
 * `Error: module not found: node:<name>` and silently disables
 * the entire window.hoopoe IPC bridge.
 *
 * The match captures the builtin name so the diagnostic can name
 * the specific offender (`node:crypto`, `node:fs`, `node:path`,
 * etc.). Keeping this as a regex tightens the existing CJS guard
 * without coupling to a list of allowed/denied builtins —
 * preload should require `electron` only. */
export function findNodeBuiltinRequire(source: string): string | null {
  const REQUIRE_NODE_BUILTIN = /require\(["']node:([^"')]+)["']\)/;
  const match = REQUIRE_NODE_BUILTIN.exec(source);
  if (match) return `\`require("node:${match[1]}")\` detected`;
  return null;
}

function main(): void {
  if (!existsSync(PRELOAD_PATH)) {
    process.stderr.write(
      `[hp-fohm] assert-preload-cjs: ${PRELOAD_PATH} not found.\n` +
        "Run `bun run --cwd apps/desktop build` first.\n",
    );
    exit(1);
  }
  const source = readFileSync(PRELOAD_PATH, "utf8");
  const esmOffence = findEsmTopLevel(source);
  if (esmOffence !== null) {
    process.stderr.write(
      `[hp-fohm] assert-preload-cjs: ${PRELOAD_PATH} is not CommonJS.\n` +
        `  Offence: ${esmOffence}\n` +
        "  First 5 lines:\n" +
        source
          .split("\n")
          .slice(0, 5)
          .map((line) => `    ${line}`)
          .join("\n") +
        "\n\n" +
        "Electron's sandboxed preload loader requires CJS. The\n" +
        "tsdown invocation for electron/preload.ts must use\n" +
        "`--format cjs`; see apps/desktop/package.json build script.\n",
    );
    exit(1);
  }
  // hp-r1tk: tighten the guard to also reject node-builtin
  // requires. Sandboxed preloads can't load `node:crypto`, `node:fs`,
  // `node:path`, etc. — any such require crashes the preload at
  // load and silently disables window.hoopoe in the renderer.
  const nodeOffence = findNodeBuiltinRequire(source);
  if (nodeOffence !== null) {
    process.stderr.write(
      `[hp-r1tk] assert-preload-cjs: ${PRELOAD_PATH} requires a node: builtin.\n` +
        `  Offence: ${nodeOffence}\n` +
        "  Sandboxed Electron preloads cannot load Node builtins.\n" +
        "  Use the Web Platform / Web Crypto equivalent, or move the\n" +
        "  functionality into the main process and expose it via IPC.\n" +
        "  Example: replace `node:crypto` randomUUID with\n" +
        "  `globalThis.crypto.randomUUID()` (see\n" +
        "  apps/desktop/electron/preloadRandomUUID.ts).\n",
    );
    exit(1);
  }
  // Quiet on success so the build log stays clean.
}

main();
