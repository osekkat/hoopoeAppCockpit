#!/usr/bin/env bun
// Hoopoe-owned. Renderer-isolation lint (hp-rflj). Refuses any source file
// under `apps/desktop/src/renderer/**` that imports a Node built-in, the
// `electron` package, or uses `eval` / `new Function` / `window.require` /
// `window.process` / `globalThis.process`. Runs in CI as a hard gate.
//
// Mirrors the policy documented in `apps/desktop/.eslintrc.cjs`. The
// actual enforcement happens here because:
//   1. We lint with `oxlint`, not eslint, so the eslintrc is documentation
//      for editors + future tooling.
//   2. oxlint's `no-restricted-imports` doesn't cover the
//      `window.require` / `window.process` patterns we need to ban.
//   3. A small custom check is the same pattern as the hp-ara provider-SDK
//      lint (`scripts/providerlint/check-provider-sdks.ts`).
//
// Exits 1 if any rendered-isolation violation is found, 0 otherwise.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

interface Finding {
  readonly file: string;
  readonly line: number;
  readonly text: string;
  readonly rule: string;
  readonly message: string;
}

const RENDERER_ROOT = join(
  process.cwd(),
  "apps",
  "desktop",
  "src",
  "renderer",
);

const BANNED_IMPORTS: ReadonlyArray<{
  readonly pattern: RegExp;
  readonly message: string;
}> = [
  {
    pattern: /from\s+["'](?:node:)?(?:fs|fs\/promises)["']/,
    message: "Renderer cannot read the filesystem; use window.hoopoe.files.",
  },
  {
    pattern: /from\s+["'](?:node:)?net["']/,
    message: "Renderer cannot reach the network; use window.hoopoe.daemon.",
  },
  {
    pattern: /from\s+["'](?:node:)?tls["']/,
    message: "Renderer cannot reach TLS; use window.hoopoe.daemon.",
  },
  {
    pattern: /from\s+["'](?:node:)?child_process["']/,
    message: "Renderer cannot spawn processes; route through main + IpcRegistry.",
  },
  {
    pattern: /from\s+["'](?:node:)?https?["']/,
    message: "Renderer must use fetch + window.hoopoe; no raw http(s).",
  },
  {
    pattern: /from\s+["']electron(?:\/.*)?["']/,
    message: "Renderer must not import electron; use window.hoopoe (preload bridge).",
  },
];

const BANNED_PATTERNS: ReadonlyArray<{
  readonly pattern: RegExp;
  readonly rule: string;
  readonly message: string;
}> = [
  {
    pattern: /\beval\s*\(/,
    rule: "no-eval",
    message: "eval() is forbidden in renderer (CSP also blocks at runtime).",
  },
  {
    pattern: /\bnew\s+Function\s*\(/,
    rule: "no-new-func",
    message: "new Function() is forbidden in renderer (CSP also blocks at runtime).",
  },
  {
    pattern: /\bwindow\.require\b/,
    rule: "no-window-require",
    message: "window.require is forbidden; route through window.hoopoe.",
  },
  {
    pattern: /\bwindow\.process\b/,
    rule: "no-window-process",
    message: "window.process is forbidden; route through window.hoopoe.",
  },
  {
    pattern: /\bglobalThis\.process\b/,
    rule: "no-globalthis-process",
    message: "globalThis.process is forbidden; route through window.hoopoe.",
  },
  {
    pattern: /<webview\b/i,
    rule: "no-webview",
    message: "<webview> is forbidden; v1 has no allowlisted embeds.",
  },
];

function* walk(dir: string): IterableIterator<string> {
  let entries: ReadonlyArray<string>;
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    const fullPath = join(dir, entry);
    const stat = statSync(fullPath);
    if (stat.isDirectory()) {
      yield* walk(fullPath);
    } else if (
      stat.isFile() &&
      (entry.endsWith(".ts") || entry.endsWith(".tsx") || entry.endsWith(".js"))
    ) {
      yield fullPath;
    }
  }
}

function scanFile(filePath: string): ReadonlyArray<Finding> {
  const findings: Finding[] = [];
  let contents: string;
  try {
    contents = readFileSync(filePath, "utf8");
  } catch {
    return findings;
  }
  const lines = contents.split("\n");
  for (let lineIndex = 0; lineIndex < lines.length; lineIndex += 1) {
    const text = lines[lineIndex] ?? "";
    if (text.trimStart().startsWith("//")) continue;
    for (const { pattern, message } of BANNED_IMPORTS) {
      if (pattern.test(text)) {
        findings.push({
          file: filePath,
          line: lineIndex + 1,
          text: text.trim(),
          rule: "no-restricted-imports",
          message,
        });
      }
    }
    for (const { pattern, rule, message } of BANNED_PATTERNS) {
      if (pattern.test(text)) {
        findings.push({
          file: filePath,
          line: lineIndex + 1,
          text: text.trim(),
          rule,
          message,
        });
      }
    }
  }
  return findings;
}

function main(): void {
  const findings: Finding[] = [];
  for (const filePath of walk(RENDERER_ROOT)) {
    findings.push(...scanFile(filePath));
  }
  if (findings.length === 0) {
    console.log(
      `[rendererlint] OK — no Guardrail #2 / hp-rflj violations under apps/desktop/src/renderer/.`,
    );
    return;
  }
  console.error(
    `[rendererlint] FAIL — ${findings.length} renderer-isolation violation(s):`,
  );
  for (const finding of findings) {
    console.error(
      `  ${finding.file}:${finding.line}  [${finding.rule}]  ${finding.message}`,
    );
    console.error(`    > ${finding.text}`);
  }
  process.exit(1);
}

main();
