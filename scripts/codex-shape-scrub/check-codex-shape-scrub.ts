#!/usr/bin/env bun
// Hoopoe-owned. Codex-shape scrub (hp-4nrd).
//
// §14 ("Lifted code carries Codex-shaped assumptions") names this risk
// directly: t3code's desktop layer was written for a chat/agent product;
// subtle assumptions around Thread / Chat / Provider / MessageList /
// Conversation can leak through scrubbing into Hoopoe's purpose-built
// code. This script is the durable enforcement that prevents that
// leakage. It runs in CI as a hard gate (parallel to the hp-ara
// provider-SDK lint and the hp-rflj renderer-isolation lint).
//
// Scope: every TS/TSX file under `apps/`, `packages/`, and `scripts/`
//   EXCLUDING `apps/desktop/src/vendored/t3code/**` (vendored code keeps
//   its upstream identifiers — that's the point of vendoring; the diff
//   against `/tmp/t3code-pinned/` stays small).
//
// Banned identifiers (whole-word, case-sensitive):
//   - Thread, Chat, Provider, MessageList, Conversation, ConversationItem,
//     ChatTurn, MAX_THREAD_MESSAGES (the t3code-shape names listed in
//     plan.md §14 + Appendix B anti-patterns).
//   - messageList, threadList (camelCase compound forms).
//
// What this script SKIPS (so legitimate uses of these words don't
// flag):
//   - String literals (double / single / template) — the words can
//     appear in user-visible labels, audit reasons, etc.
//   - Comments (`//` to EOL, `/* */` blocks across lines) — doc comments
//     and rationale notes can mention the Codex shapes by name.
//   - Allowlist suppression: a line annotated `// codex-shape-scrub-ok:
//     <reason>` is skipped (use sparingly; reason is mandatory).
//
// Exits 0 on no violations, 1 otherwise. The findings list includes
// each rule, file:line:col, the offending text, and a short message.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

interface Finding {
  readonly file: string;
  readonly line: number;
  readonly column: number;
  readonly text: string;
  readonly identifier: string;
  readonly message: string;
}

const REPO_ROOT = process.cwd();

const SCAN_ROOTS = ["apps", "packages", "scripts"];

const SKIP_DIRS = new Set([
  "node_modules",
  ".bun",
  ".turbo",
  "dist",
  "dist-electron",
  "build",
  "release",
  ".git",
]);

const VENDORED_PREFIX = "apps/desktop/src/vendored/t3code/";

interface BannedIdentifier {
  readonly identifier: string;
  readonly message: string;
}

const BANNED_IDENTIFIERS: ReadonlyArray<BannedIdentifier> = [
  {
    identifier: "Thread",
    message: "Codex-shape: chat-thread metaphor doesn't fit Hoopoe. Use bead/swarm/activity terms.",
  },
  {
    identifier: "Chat",
    message: "Codex-shape: bare 'Chat' is a Codex chat-surface identifier. Hoopoe's user↔orchestrator surface is the Activity panel; orchestrator-chat agent is allowed as a string only.",
  },
  {
    identifier: "Provider",
    message: "Codex-shape: 'Provider' as an identifier suggests Codex's model-provider registry. Hoopoe is subscription-only via CAAM; use harness/account terms.",
  },
  {
    identifier: "MessageList",
    message: "Codex-shape: replace with ActivityTimeline / BeadList / AgentGrid per the design system.",
  },
  {
    identifier: "Conversation",
    message: "Codex-shape: replace with the Hoopoe domain term (plan, bead, swarm, activity).",
  },
  {
    identifier: "ConversationItem",
    message: "Codex-shape: replace with TimelineRow + the appropriate kind variant.",
  },
  {
    identifier: "ChatTurn",
    message: "Codex-shape: Hoopoe doesn't model chat turns; use ActivityEvent or bead transitions.",
  },
  {
    identifier: "MAX_THREAD_MESSAGES",
    message: "Codex-shape silent cap (Appendix B anti-pattern #3). Virtualize or 'showing latest N' instead.",
  },
  {
    identifier: "messageList",
    message: "Codex-shape: rename to activityTimeline / beadList / agentGrid.",
  },
  {
    identifier: "threadList",
    message: "Codex-shape: Hoopoe surfaces beads, not threads. Rename.",
  },
];

/**
 * Banned imports — package names that the Hoopoe surface MUST NOT depend on
 * outside `apps/desktop/src/vendored/t3code/**`. Closes the §14 risk that
 * Effect framework dependencies leak from vendored code into Hoopoe-owned
 * adapters: every Effect-shaped helper must be re-implemented in plain TS
 * or imported through the vendored shim layer (`vendored/t3code/_shims.ts`).
 *
 * Matched against the RAW line (before string-stripping) so the package
 * name inside the import quote is visible. Allowlisted via the same
 * `// codex-shape-scrub-ok: <reason>` suppression as identifier hits.
 */
interface BannedImport {
  readonly pattern: RegExp;
  readonly source: string;
  readonly message: string;
}

const BANNED_IMPORTS: ReadonlyArray<BannedImport> = [
  {
    pattern: /from\s+["']effect["']/,
    source: "effect",
    message: "Effect framework leakage: re-implement the pattern in plain TS or import via vendored/t3code/_shims.ts (plan.md §3 'Effect framework is not adopted').",
  },
  {
    pattern: /from\s+["']effect\/[^"']+["']/,
    source: "effect/*",
    message: "Effect submodule leakage: same as `effect`. Use the vendored shim or re-implement.",
  },
  {
    pattern: /from\s+["']@effect\/[^"']+["']/,
    source: "@effect/*",
    message: "Effect ecosystem package leakage. Re-implement or use the vendored shim.",
  },
  {
    pattern: /from\s+["']@t3tools\/[^"']+["']/,
    source: "@t3tools/*",
    message: "T3 Code workspace package leakage. Hoopoe doesn't ship @t3tools/* runtime; use the vendored copy + Hoopoe-owned adapter.",
  },
];

const SUPPRESSION_MARKER = "codex-shape-scrub-ok:";

function* walk(dir: string): IterableIterator<string> {
  let entries: ReadonlyArray<string>;
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    if (SKIP_DIRS.has(entry)) continue;
    const fullPath = join(dir, entry);
    const stat = statSync(fullPath);
    if (stat.isDirectory()) {
      yield* walk(fullPath);
    } else if (
      stat.isFile() &&
      (entry.endsWith(".ts") || entry.endsWith(".tsx"))
    ) {
      yield fullPath;
    }
  }
}

function isVendoredPath(filePath: string): boolean {
  const rel = relative(REPO_ROOT, filePath);
  return rel.startsWith(VENDORED_PREFIX);
}

function isSelfPath(filePath: string): boolean {
  // The lint script and its test allowlist mention the banned names in
  // plain prose (rule definitions, fixture lines). Skip them.
  const rel = relative(REPO_ROOT, filePath);
  return rel.startsWith("scripts/codex-shape-scrub/");
}

interface CleanedLine {
  /** The original line, used for findings output. */
  readonly raw: string;
  /** The line with strings, comments, and template literals replaced by
   * spaces of equal length. Banned-identifier matching runs against this
   * so legitimate mentions inside literals/comments don't fire. */
  readonly cleaned: string;
}

function cleanLines(source: string): ReadonlyArray<CleanedLine> {
  const out: CleanedLine[] = [];
  const length = source.length;

  let inBlockComment = false;
  let inString: '"' | "'" | "`" | null = null;

  let lineRaw = "";
  let lineCleaned = "";

  for (let index = 0; index < length; index += 1) {
    const ch = source[index]!;
    const next = source[index + 1] ?? "";
    lineRaw += ch;

    if (ch === "\n") {
      out.push({ raw: lineRaw.slice(0, -1), cleaned: lineCleaned });
      lineRaw = "";
      lineCleaned = "";
      continue;
    }

    if (inBlockComment) {
      lineCleaned += " ";
      if (ch === "*" && next === "/") {
        lineCleaned += " ";
        index += 1;
        inBlockComment = false;
      }
      continue;
    }

    if (inString) {
      lineCleaned += " ";
      if (ch === "\\") {
        // Escaped char — consume the next char as part of the string.
        if (index + 1 < length) {
          lineRaw += source[index + 1]!;
          lineCleaned += " ";
          index += 1;
        }
        continue;
      }
      if (ch === inString) {
        inString = null;
      }
      continue;
    }

    if (ch === "/" && next === "/") {
      // Single-line comment: skip rest of line.
      while (index < length && source[index] !== "\n") {
        const c = source[index]!;
        lineRaw += index === 0 ? "" : ""; // already added above
        if (c !== ch) lineRaw += c;
        lineCleaned += " ";
        index += 1;
      }
      // Don't consume the \n; loop continues naturally.
      // Fix: the loop pushes the line at \n. Since we're now at \n (or EOF),
      // back up the index by one so the outer loop sees the \n.
      index -= 1;
      continue;
    }
    if (ch === "/" && next === "*") {
      inBlockComment = true;
      lineCleaned += "  ";
      index += 1;
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") {
      inString = ch as '"' | "'" | "`";
      lineCleaned += " ";
      continue;
    }

    lineCleaned += ch;
  }
  if (lineRaw.length > 0 || lineCleaned.length > 0) {
    out.push({ raw: lineRaw, cleaned: lineCleaned });
  }
  return out;
}

function buildMatcher(): RegExp {
  const sortedByLength = [...BANNED_IDENTIFIERS].toSorted(
    (a, b) => b.identifier.length - a.identifier.length,
  );
  const alternation = sortedByLength.map((b) => b.identifier).join("|");
  return new RegExp(`\\b(${alternation})\\b`, "g");
}

function isLineSuppressed(rawLine: string): boolean {
  const idx = rawLine.indexOf(SUPPRESSION_MARKER);
  if (idx < 0) return false;
  // Require a non-empty reason after the marker.
  const reason = rawLine.slice(idx + SUPPRESSION_MARKER.length).trim();
  return reason.length > 0;
}

export function scanFile(filePath: string, source: string): ReadonlyArray<Finding> {
  const findings: Finding[] = [];
  const cleaned = cleanLines(source);
  const matcher = buildMatcher();
  const messageByIdentifier = new Map(
    BANNED_IDENTIFIERS.map((entry) => [entry.identifier, entry.message] as const),
  );
  for (let lineIndex = 0; lineIndex < cleaned.length; lineIndex += 1) {
    const { raw, cleaned: cleanedLine } = cleaned[lineIndex]!;
    if (isLineSuppressed(raw)) continue;
    matcher.lastIndex = 0;
    let match: RegExpExecArray | null;
    while ((match = matcher.exec(cleanedLine)) !== null) {
      const identifier = match[1] ?? match[0];
      findings.push({
        file: filePath,
        line: lineIndex + 1,
        column: match.index + 1,
        text: raw.trim(),
        identifier,
        message: messageByIdentifier.get(identifier) ?? "Codex-shape identifier not allowed.",
      });
    }
    // Banned-imports check runs against the RAW line (before string
    // stripping) so the package name inside the import quote is visible.
    for (const banned of BANNED_IMPORTS) {
      const importMatch = banned.pattern.exec(raw);
      if (importMatch) {
        findings.push({
          file: filePath,
          line: lineIndex + 1,
          column: importMatch.index + 1,
          text: raw.trim(),
          identifier: banned.source,
          message: banned.message,
        });
      }
    }
  }
  return findings;
}

function main(): void {
  const findings: Finding[] = [];
  for (const root of SCAN_ROOTS) {
    for (const filePath of walk(join(REPO_ROOT, root))) {
      if (isVendoredPath(filePath)) continue;
      if (isSelfPath(filePath)) continue;
      let source: string;
      try {
        source = readFileSync(filePath, "utf8");
      } catch {
        continue;
      }
      findings.push(...scanFile(filePath, source));
    }
  }
  if (findings.length === 0) {
    console.log(
      "[codex-shape-scrub] OK — no §14 Codex-shape leakage outside apps/desktop/src/vendored/t3code/.",
    );
    return;
  }
  console.error(
    `[codex-shape-scrub] FAIL — ${findings.length} Codex-shape violation(s):`,
  );
  for (const finding of findings) {
    console.error(
      `  ${relative(REPO_ROOT, finding.file)}:${finding.line}:${finding.column}  [${finding.identifier}]  ${finding.message}`,
    );
    console.error(`    > ${finding.text}`);
  }
  process.exit(1);
}

if (import.meta.main) {
  main();
}
