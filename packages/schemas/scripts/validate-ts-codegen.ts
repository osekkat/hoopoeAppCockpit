#!/usr/bin/env bun
//
// validate-ts-codegen.ts — codegen drift gate for the TypeScript surface.
//
// Re-runs `openapi-typescript openapi.yaml` to a temp file, then byte-compares
// it against the committed `src/generated/openapi.ts`. Exits non-zero on any
// difference so CI fails when a developer edits openapi.yaml without
// regenerating the TS client (or vice versa).
//
// Run locally: `bun run --cwd packages/schemas validate:ts`
// Run in CI:   wired into `bun run validate` (future: also `:go`).

import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const spec = join(pkgRoot, "openapi.yaml");
const committed = join(pkgRoot, "src", "generated", "openapi.ts");

const tmpDir = mkdtempSync(join(tmpdir(), "hoopoe-schemas-validate-"));
const fresh = join(tmpDir, "openapi.ts");

try {
  const result = spawnSync(
    "bunx",
    ["--bun", "openapi-typescript@7.13.0", spec, "--output", fresh],
    { stdio: ["ignore", "pipe", "pipe"], encoding: "utf8" },
  );
  if (result.status !== 0) {
    process.stderr.write(
      `[validate-ts-codegen] openapi-typescript failed (exit ${result.status})\n` +
        `stdout:\n${result.stdout}\nstderr:\n${result.stderr}\n`,
    );
    process.exit(1);
  }

  const want = readFileSync(committed, "utf8");
  const got = readFileSync(fresh, "utf8");
  if (want === got) {
    process.stdout.write("[validate-ts-codegen] OK — src/generated/openapi.ts matches openapi.yaml\n");
    process.exit(0);
  }

  // On drift, write the fresh output next to the committed one so a developer
  // can `diff` it locally; CI gets the path in the failure log.
  const driftPath = join(pkgRoot, "src", "generated", "openapi.ts.drift");
  writeFileSync(driftPath, got);
  process.stderr.write(
    "[validate-ts-codegen] DRIFT — openapi.yaml and src/generated/openapi.ts disagree.\n" +
      `Fresh codegen written to ${driftPath} for inspection.\n` +
      "Fix: run `bun run --cwd packages/schemas generate:ts` and commit the result.\n",
  );
  process.exit(1);
} finally {
  rmSync(tmpDir, { recursive: true, force: true });
}
