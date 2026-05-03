#!/usr/bin/env bun
//
// gen-go.ts — regenerate `packages/schemas/go/schemas.gen.go` from openapi.yaml.
//
// Pins oapi-codegen v2.7.0 so a stray `go install ...@latest` doesn't quietly
// drift the output between committers. Run:
//   bun run --cwd packages/schemas generate:go
//
// Drift is policed by `validate:go` (next commit).

import { spawnSync } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const OAPI_CODEGEN_VERSION = "v2.7.0";
const OAPI_CODEGEN_PKG =
  `github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@${OAPI_CODEGEN_VERSION}`;

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const cfg = join(pkgRoot, "go", "cfg.yaml");
const spec = join(pkgRoot, "openapi.yaml");

const result = spawnSync("go", ["run", OAPI_CODEGEN_PKG, "-config", cfg, spec], {
  cwd: join(pkgRoot, "go"),
  stdio: "inherit",
});

if (result.status !== 0) {
  process.stderr.write(`[gen-go] oapi-codegen failed (exit ${result.status})\n`);
  process.exit(result.status ?? 1);
}

process.stdout.write("[gen-go] OK — packages/schemas/go/schemas.gen.go regenerated\n");
