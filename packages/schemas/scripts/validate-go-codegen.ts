#!/usr/bin/env bun
//
// validate-go-codegen.ts — codegen drift gate for the Go surface.
//
// Re-runs `oapi-codegen -config go/cfg.yaml openapi.yaml` to a temp dir,
// then byte-compares the output against the committed
// `packages/schemas/go/schemas.gen.go`. Exits non-zero on any difference so
// CI fails when a developer edits openapi.yaml without regenerating Go (or
// vice versa).
//
// Run locally: `bun run --cwd packages/schemas validate:go`

import { spawnSync } from "node:child_process";
import {
  copyFileSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const OAPI_CODEGEN_VERSION = "v2.7.0";
const OAPI_CODEGEN_PKG =
  `github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@${OAPI_CODEGEN_VERSION}`;

const here = dirname(fileURLToPath(import.meta.url));
const pkgRoot = resolve(here, "..");
const cfgSource = join(pkgRoot, "go", "cfg.yaml");
const spec = join(pkgRoot, "openapi.yaml");
const committed = join(pkgRoot, "go", "schemas.gen.go");

const tmpDir = mkdtempSync(join(tmpdir(), "hoopoe-schemas-gen-go-"));
// oapi-codegen writes `output:` relative to its CWD, so we copy cfg.yaml into
// tmpDir and run from there to keep the committed file untouched.
const cfgInTmp = join(tmpDir, "cfg.yaml");
copyFileSync(cfgSource, cfgInTmp);

// hp-kcpa: openapi.yaml must stay on a 3.0.x version while the pinned
// oapi-codegen v2.7.0 only has partial OpenAPI 3.1 support (its own
// stderr warning recommends downgrading). A future committer bumping the
// spec back to 3.1.x without first switching the Go generator to one
// with full 3.1 coverage would silently degrade Go ↔ TS contract
// fidelity (3.1-only constructs may parse cleanly under
// openapi-typescript while oapi-codegen omits or models them
// incorrectly). Refuse the drift here at the validate gate — the assert
// fires before the byte-compare so the diagnostic points at the actual
// root cause instead of a confusing schemas.gen.go diff.
const specSource = readFileSync(spec, "utf8");
const versionMatch = specSource.match(/^\s*openapi:\s*(\S+)/m);
if (!versionMatch) {
  process.stderr.write(
    "[validate-go-codegen] openapi.yaml is missing the top-level `openapi:` declaration.\n",
  );
  process.exit(1);
}
const specVersion = versionMatch[1].replace(/^['"]|['"]$/g, "");
if (!specVersion.startsWith("3.0.")) {
  process.stderr.write(
    `[validate-go-codegen] openapi.yaml declares openapi: ${specVersion}, but the pinned ` +
      `oapi-codegen ${OAPI_CODEGEN_VERSION} only fully supports 3.0.x. Switch the Go ` +
      `generator to a 3.1-aware tool first (or downgrade the spec back to 3.0.x).\n` +
      `hp-kcpa parent context: spec drift here silently degrades Go ↔ TS contract fidelity.\n`,
  );
  process.exit(1);
}

try {
  const result = spawnSync(
    "go",
    ["run", OAPI_CODEGEN_PKG, "-config", cfgInTmp, spec],
    { cwd: tmpDir, stdio: ["ignore", "pipe", "pipe"], encoding: "utf8" },
  );
  if (result.status !== 0) {
    process.stderr.write(
      `[validate-go-codegen] oapi-codegen failed (exit ${result.status})\n` +
        `stdout:\n${result.stdout}\nstderr:\n${result.stderr}\n`,
    );
    process.exit(1);
  }
  // hp-kcpa: even on 3.0.x, fail validation if oapi-codegen ever emits the
  // "OpenAPI 3.1.x is not yet supported" warning — the warning is the
  // signal the contract has drifted into 3.1 territory the generator
  // can't model.
  if (/OpenAPI\s+3\.1\.x\s+specification.*is not yet supported/i.test(result.stderr)) {
    process.stderr.write(
      "[validate-go-codegen] oapi-codegen emitted the OpenAPI-3.1-not-supported warning. " +
        "Switch the Go generator before adopting 3.1 features, or keep openapi.yaml on 3.0.x.\n" +
        `oapi-codegen stderr:\n${result.stderr}\n`,
    );
    process.exit(1);
  }

  const fresh = join(tmpDir, "schemas.gen.go");
  const want = readFileSync(committed, "utf8");
  const got = readFileSync(fresh, "utf8");
  if (want === got) {
    process.stdout.write("[validate-go-codegen] OK — go/schemas.gen.go matches openapi.yaml\n");
    process.exit(0);
  }

  const driftPath = join(pkgRoot, "go", "schemas.gen.go.drift");
  writeFileSync(driftPath, got);
  process.stderr.write(
    "[validate-go-codegen] DRIFT — openapi.yaml and go/schemas.gen.go disagree.\n" +
      `Fresh codegen written to ${driftPath} for inspection.\n` +
      "Fix: run `bun run --cwd packages/schemas generate:go` and commit the result.\n",
  );
  process.exit(1);
} finally {
  rmSync(tmpDir, { recursive: true, force: true });
}
