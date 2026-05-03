// hp-lxs deliverable. Bare console.* / fmt.Print* / log.Print* in production
// code fails this lint. The structured logger
// (apps/desktop/src/shared/logger + apps/daemon/internal/logger) is the
// only sanctioned logging path.
//
// Allowlist (per the bead spec):
//   - vendored/ — upstream code (t3code) we don't rewrite.
//   - *_test.go / *.test.ts — tests can console.log freely.
//   - scripts/ — orchestrator/build scripts.
//   - apps/daemon/cmd/* main.go — fatal-startup paths before logger boots.
//   - apps/desktop/src/shared/logger/transport.ts — RendererConsoleTransport
//     deliberately wraps console for DevTools display.
//
// Cross-references:
//   - hp-lxs DOD: "Lint rule: console.log / fmt.Println fails build outside
//     vendored/ and tests."
//   - plan.md §1.4, §5.4 — logs are inspectable + audited.

import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";

const REPO_ROOT = process.cwd();

const SCAN_ROOTS = ["apps/desktop/src", "apps/daemon"] as const;

const IGNORED_DIRS = new Set([
  ".git",
  ".turbo",
  "bin",
  "coverage",
  "dist",
  "dist-electron",
  "node_modules",
  "vendored",
]);

/** Files that are explicitly allowed to use raw logging. */
const ALLOWLISTED_FILES = new Set([
  // Daemon early-boot fatal paths (before the logger is constructed).
  path.join("apps", "daemon", "cmd", "hoopoe", "main.go"),
  path.join("apps", "daemon", "cmd", "hoopoed", "main.go"),
  path.join("apps", "daemon", "cmd", "hoopoed-mock", "main.go"),
  // Renderer-facing console transport — that's the whole point.
  path.join("apps", "desktop", "src", "shared", "logger", "transport.ts"),
]);

const TS_LIKE = new Set([".cjs", ".cts", ".js", ".jsx", ".mjs", ".mts", ".ts", ".tsx"]);

interface RawLoggingViolation {
  filePath: string;
  line: number;
  language: "ts" | "go";
  call: string;
}

function isAllowlisted(relPath: string): boolean {
  if (relPath.includes(`${path.sep}vendored${path.sep}`)) return true;
  if (relPath.endsWith(".test.ts") || relPath.endsWith(".test.tsx")) return true;
  if (relPath.endsWith("_test.go")) return true;
  if (ALLOWLISTED_FILES.has(relPath)) return true;
  return false;
}

interface CandidateFile {
  filePath: string;
  language: "ts" | "go";
}

function* walk(root: string): Generator<CandidateFile> {
  const queue: string[] = [root];
  while (queue.length > 0) {
    const dir = queue.pop()!;
    let entries: string[];
    try {
      entries = readdirSync(dir);
    } catch {
      continue;
    }
    for (const name of entries) {
      const full = path.join(dir, name);
      let stat;
      try {
        stat = statSync(full);
      } catch {
        continue;
      }
      if (stat.isDirectory()) {
        if (IGNORED_DIRS.has(name)) continue;
        queue.push(full);
        continue;
      }
      const ext = path.extname(name);
      if (ext === ".go") {
        yield { filePath: full, language: "go" };
      } else if (TS_LIKE.has(ext)) {
        yield { filePath: full, language: "ts" };
      }
    }
  }
}

const TS_PATTERNS: Array<{ name: string; regex: RegExp }> = [
  { name: "console.log", regex: /\bconsole\s*\.\s*log\s*\(/g },
  { name: "console.info", regex: /\bconsole\s*\.\s*info\s*\(/g },
  { name: "console.warn", regex: /\bconsole\s*\.\s*warn\s*\(/g },
  { name: "console.error", regex: /\bconsole\s*\.\s*error\s*\(/g },
  { name: "console.debug", regex: /\bconsole\s*\.\s*debug\s*\(/g },
];

const GO_PATTERNS: Array<{ name: string; regex: RegExp }> = [
  { name: "fmt.Println", regex: /\bfmt\.Println\s*\(/g },
  { name: "fmt.Printf", regex: /\bfmt\.Printf\s*\(/g },
  { name: "fmt.Print(", regex: /\bfmt\.Print\s*\(/g },
  { name: "log.Println", regex: /\blog\.Println\s*\(/g },
  { name: "log.Printf", regex: /\blog\.Printf\s*\(/g },
  { name: "log.Print(", regex: /\blog\.Print\s*\(/g },
  { name: "log.Fatalf", regex: /\blog\.Fatalf\s*\(/g },
  { name: "log.Fatal(", regex: /\blog\.Fatal\s*\(/g },
  { name: "log.Panicln", regex: /\blog\.Panicln\s*\(/g },
  { name: "log.Panicf", regex: /\blog\.Panicf\s*\(/g },
];

export function collectViolations(
  filePath: string,
  language: "ts" | "go",
  text: string,
): RawLoggingViolation[] {
  const violations: RawLoggingViolation[] = [];
  const patterns = language === "ts" ? TS_PATTERNS : GO_PATTERNS;
  const lines = text.split(/\r?\n/);
  for (const [index, line] of lines.entries()) {
    // Strip line/block comments to avoid false positives in commentary.
    const stripped = line.replace(/\/\/.*$/, "").replace(/\/\*[\s\S]*?\*\//g, "");
    for (const p of patterns) {
      p.regex.lastIndex = 0;
      if (p.regex.test(stripped)) {
        violations.push({
          filePath,
          line: index + 1,
          language,
          call: p.name,
        });
      }
    }
  }
  return violations;
}

export function scan(): RawLoggingViolation[] {
  const all: RawLoggingViolation[] = [];
  for (const root of SCAN_ROOTS) {
    const abs = path.join(REPO_ROOT, root);
    for (const candidate of walk(abs)) {
      const rel = path.relative(REPO_ROOT, candidate.filePath);
      if (isAllowlisted(rel)) continue;
      let text: string;
      try {
        text = readFileSync(candidate.filePath, "utf8");
      } catch {
        continue;
      }
      const violations = collectViolations(candidate.filePath, candidate.language, text);
      for (const v of violations) {
        all.push({ ...v, filePath: rel });
      }
    }
  }
  return all;
}

function main(): void {
  const violations = scan();
  if (violations.length === 0) {
    console.error(
      "[loggerlint] OK — no raw console.*/fmt.Print*/log.Print* in production code.",
    );
    process.exit(0);
  }
  for (const v of violations) {
    process.stderr.write(
      `${v.filePath}:${v.line}: ${v.call} — use the structured logger (` +
        `apps/desktop/src/shared/logger or apps/daemon/internal/logger). ` +
        `hp-lxs DOD: raw logging fails build outside vendored/ and tests.\n`,
    );
  }
  process.stderr.write(
    `[loggerlint] FAIL — ${violations.length} raw-logging call(s) in production code.\n`,
  );
  process.exit(1);
}

if (import.meta.main) {
  main();
}
