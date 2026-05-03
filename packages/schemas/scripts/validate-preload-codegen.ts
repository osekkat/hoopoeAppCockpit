#!/usr/bin/env bun
//
// validate-preload-codegen.ts — codegen drift gate for the preload IPC
// contract TS surface (hp-rflj).
//
// Re-runs `gen-preload-contract.ts` to a temp file, then byte-compares it
// against the committed `apps/desktop/src/shared/ipc-contract.gen.ts`.
// Exits non-zero on any difference so CI fails when a developer edits
// `preload-api.yaml` without regenerating the TS (or vice versa).
//
// Run locally: `bun run --cwd packages/schemas validate:preload`

import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync, copyFileSync, mkdirSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const repoRoot = resolve(pkgRoot, "..", "..");
const generator = join(pkgRoot, "scripts", "gen-preload-contract.ts");
const committed = resolve(repoRoot, "apps/desktop/src/shared/ipc-contract.gen.ts");

if (!existsSync(committed)) {
  process.stderr.write(
    `[validate-preload-codegen] FAIL — committed file missing at ${committed}.\n` +
      "Fix: run `bun run --cwd packages/schemas generate:preload` and commit the result.\n",
  );
  process.exit(1);
}

const tmpDir = mkdtempSync(join(tmpdir(), "hoopoe-preload-validate-"));
// The generator writes to a fixed absolute path under apps/desktop. To
// validate without touching the committed file, we save its current
// contents, run the generator (which overwrites), capture the fresh
// output, and restore the original from backup.
const backup = join(tmpDir, "ipc-contract.gen.ts.backup");
copyFileSync(committed, backup);

try {
  const result = spawnSync("bun", [generator], {
    stdio: ["ignore", "pipe", "pipe"],
    encoding: "utf8",
  });
  if (result.status !== 0) {
    // Restore backup before exiting on generator error.
    copyFileSync(backup, committed);
    process.stderr.write(
      `[validate-preload-codegen] generator failed (exit ${result.status})\n` +
        `stdout:\n${result.stdout}\nstderr:\n${result.stderr}\n`,
    );
    process.exit(1);
  }

  const want = readFileSync(backup, "utf8");
  const got = readFileSync(committed, "utf8");
  // Restore committed file no matter what.
  copyFileSync(backup, committed);

  if (want === got) {
    process.stdout.write(
      "[validate-preload-codegen] OK — apps/desktop/src/shared/ipc-contract.gen.ts matches preload-api.yaml\n",
    );
    process.exit(0);
  }

  const driftPath = `${committed}.drift`;
  writeFileSync(driftPath, got);
  process.stderr.write(
    "[validate-preload-codegen] DRIFT — preload-api.yaml and ipc-contract.gen.ts disagree.\n" +
      `Fresh codegen written to ${driftPath} for inspection.\n` +
      "Fix: run `bun run --cwd packages/schemas generate:preload` and commit the result.\n",
  );
  process.exit(1);
} finally {
  rmSync(tmpDir, { recursive: true, force: true });
}
