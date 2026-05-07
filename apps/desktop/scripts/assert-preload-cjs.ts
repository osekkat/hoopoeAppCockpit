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

function main(): void {
  if (!existsSync(PRELOAD_PATH)) {
    process.stderr.write(
      `[hp-fohm] assert-preload-cjs: ${PRELOAD_PATH} not found.\n` +
        "Run `bun run --cwd apps/desktop build` first.\n",
    );
    exit(1);
  }
  const source = readFileSync(PRELOAD_PATH, "utf8");
  const offence = findEsmTopLevel(source);
  if (offence !== null) {
    process.stderr.write(
      `[hp-fohm] assert-preload-cjs: ${PRELOAD_PATH} is not CommonJS.\n` +
        `  Offence: ${offence}\n` +
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
  // Quiet on success so the build log stays clean.
}

main();
