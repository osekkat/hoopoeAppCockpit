#!/usr/bin/env bun
// Hoopoe-owned. Desktop helper-process boundary lint (hp-5z7).
//
// The desktop may own a few local helper processes: daemon bootstrap/local
// demo, SSH tunnel and network probes, ssh-keygen, read-only origin-mirror git
// plumbing, project bootstrap checks, and macOS power assertion. It must not
// become the owner of project jobs, swarms, tending runs, review runs, builds,
// tests, or arbitrary shell execution. Those durable operations belong to the
// VPS daemon/NTM boundary.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

export interface Finding {
  readonly file: string;
  readonly line: number;
  readonly column: number;
  readonly command: string;
  readonly rule: string;
  readonly message: string;
}

interface CommandUse {
  readonly command: string;
  readonly subcommand: string | null;
  readonly line: number;
  readonly column: number;
  readonly snippet: string;
}

interface HelperPolicy {
  readonly id: string;
  readonly paths?: readonly string[];
  readonly prefixes?: readonly string[];
  readonly commands: ReadonlyMap<string, ReadonlySet<string> | null>;
  readonly allowDynamicProcessCommand?: boolean;
}

const REPO_ROOT = process.cwd();
const SCAN_ROOTS = [
  join(REPO_ROOT, "apps", "desktop", "src"),
  join(REPO_ROOT, "apps", "desktop", "electron"),
];

const SKIP_DIR_NAMES = new Set([
  "node_modules",
  ".turbo",
  "dist",
  "dist-electron",
  "build",
  "release",
  "vendored",
]);

const SKIP_PATH_PARTS = [
  "/tests/",
  "/test-utils/",
  "/__tests__/",
];

const JOB_OR_MUTATING_COMMANDS = new Set([
  "br",
  "bun",
  "cargo",
  "go",
  "git",
  "make",
  "npm",
  "ntm",
  "pnpm",
  "pytest",
  "rch",
  "tmux",
  "yarn",
]);

const RUNTIME_VALIDATED_SUBCOMMAND = "<runtime-validated>";

const LOCAL_MIRROR_GIT_SUBCOMMANDS = new Set([
  "branch",
  "clone",
  "diff",
  "fetch",
  "log",
  "ls-tree",
  "remote",
  "rev-parse",
  "show",
  "status",
  RUNTIME_VALIDATED_SUBCOMMAND,
]);

const HELPER_POLICIES: readonly HelperPolicy[] = [
  {
    id: "daemon-bootstrap-local-demo",
    paths: ["apps/desktop/src/main/BackendLifecycle.ts"],
    commands: new Map(),
    allowDynamicProcessCommand: true,
  },
  {
    id: "ssh-keygen",
    paths: ["apps/desktop/src/main/SshKeyService.ts"],
    commands: new Map([["ssh-keygen", null]]),
    allowDynamicProcessCommand: true,
  },
  {
    id: "macos-power-assertion",
    paths: ["apps/desktop/src/main/macPowerAssert.ts"],
    commands: new Map([["caffeinate", null]]),
    allowDynamicProcessCommand: true,
  },
  {
    id: "desktop-origin-mirror-git",
    prefixes: ["apps/desktop/electron/clone/"],
    commands: new Map([["git", LOCAL_MIRROR_GIT_SUBCOMMANDS]]),
    allowDynamicProcessCommand: true,
  },
  {
    id: "project-readiness-local-ux",
    paths: ["apps/desktop/electron/projects/lifecycle.ts"],
    commands: new Map([
      ["git", new Set(["branch", "remote"])],
      ["br", new Set(["init"])],
    ]),
    allowDynamicProcessCommand: true,
  },
  {
    id: "tunnel-network-signals",
    paths: ["apps/desktop/electron/tunnel/networkMonitor.ts"],
    commands: new Map([
      ["ifconfig", null],
      ["networksetup", null],
      ["route", null],
      ["scutil", null],
    ]),
    allowDynamicProcessCommand: true,
  },
];

const COMMAND_LITERAL_CALL =
  /\b(?:spawn|spawnSync|execFile|execFileSync|execFileAsync|run|runCommand)\s*\(\s*(["'])(?<command>[^"']+)\1/g;

const DYNAMIC_PROCESS_CALL =
  /\b(?:spawn|spawnSync|execFile|execFileSync|execFileAsync)\s*\(\s*(?!["'])(?<expr>[^,\n)]+)/g;

function* walk(dir: string): IterableIterator<string> {
  let entries: readonly string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    if (SKIP_DIR_NAMES.has(entry)) continue;
    const fullPath = join(dir, entry);
    const stat = statSync(fullPath);
    if (stat.isDirectory()) {
      yield* walk(fullPath);
    } else if (stat.isFile() && (entry.endsWith(".ts") || entry.endsWith(".tsx"))) {
      yield fullPath;
    }
  }
}

function repoRelative(filePath: string): string {
  return relative(REPO_ROOT, filePath);
}

function shouldSkipFile(filePath: string): boolean {
  const rel = repoRelative(filePath);
  if (rel.endsWith(".test.ts") || rel.endsWith(".test.tsx") || rel.endsWith(".spec.ts") || rel.endsWith(".spec.tsx")) {
    return true;
  }
  return SKIP_PATH_PARTS.some((part) => rel.includes(part));
}

function matchingPolicy(relPath: string): HelperPolicy | null {
  for (const policy of HELPER_POLICIES) {
    if (policy.paths?.includes(relPath)) return policy;
    if (policy.prefixes?.some((prefix) => relPath.startsWith(prefix))) return policy;
  }
  return null;
}

function normalizeCommand(command: string): string {
  const trimmed = command.trim();
  const parts = trimmed.split("/");
  return parts[parts.length - 1] ?? trimmed;
}

function lineAndColumn(source: string, index: number): { line: number; column: number } {
  let line = 1;
  let lastLineStart = 0;
  for (let i = 0; i < index; i += 1) {
    if (source.charCodeAt(i) === 10) {
      line += 1;
      lastLineStart = i + 1;
    }
  }
  return { line, column: index - lastLineStart + 1 };
}

function lineSnippet(source: string, index: number): string {
  const start = source.lastIndexOf("\n", index) + 1;
  const end = source.indexOf("\n", index);
  return source.slice(start, end === -1 ? source.length : end).trim();
}

function stripCommentsPreserveStrings(source: string): string {
  let out = "";
  let inBlockComment = false;
  let inString: '"' | "'" | "`" | null = null;
  for (let i = 0; i < source.length; i += 1) {
    const ch = source[i]!;
    const next = source[i + 1] ?? "";

    if (inBlockComment) {
      if (ch === "*" && next === "/") {
        out += "  ";
        i += 1;
        inBlockComment = false;
      } else {
        out += ch === "\n" ? "\n" : " ";
      }
      continue;
    }

    if (inString) {
      out += ch;
      if (ch === "\\") {
        if (i + 1 < source.length) {
          out += source[i + 1]!;
          i += 1;
        }
        continue;
      }
      if (ch === inString) inString = null;
      continue;
    }

    if (ch === "/" && next === "/") {
      out += "  ";
      i += 1;
      while (i + 1 < source.length && source[i + 1] !== "\n") {
        out += " ";
        i += 1;
      }
      continue;
    }
    if (ch === "/" && next === "*") {
      out += "  ";
      i += 1;
      inBlockComment = true;
      continue;
    }
    if (ch === '"' || ch === "'" || ch === "`") {
      inString = ch;
      out += ch;
      continue;
    }
    out += ch;
  }
  return out;
}

function firstArrayStringAfter(source: string, index: number): string | null {
  const tail = source.slice(index, index + 400);
  const match = /\[\s*(["'])(?<subcommand>[^"']+)\1/.exec(tail);
  return match?.groups?.subcommand ?? null;
}

function collectCommandUses(source: string): readonly CommandUse[] {
  const scan = stripCommentsPreserveStrings(source);
  const uses: CommandUse[] = [];
  let match: RegExpExecArray | null;

  COMMAND_LITERAL_CALL.lastIndex = 0;
  while ((match = COMMAND_LITERAL_CALL.exec(scan)) !== null) {
    const command = match.groups?.command;
    if (!command) continue;
    const location = lineAndColumn(scan, match.index);
    uses.push({
      command,
      subcommand: firstArrayStringAfter(scan, COMMAND_LITERAL_CALL.lastIndex),
      line: location.line,
      column: location.column,
      snippet: lineSnippet(source, match.index),
    });
  }

  DYNAMIC_PROCESS_CALL.lastIndex = 0;
  while ((match = DYNAMIC_PROCESS_CALL.exec(scan)) !== null) {
    const expr = match.groups?.expr?.trim();
    if (!expr) continue;
    const location = lineAndColumn(scan, match.index);
    uses.push({
      command: `<dynamic:${expr}>`,
      subcommand: null,
      line: location.line,
      column: location.column,
      snippet: lineSnippet(source, match.index),
    });
  }

  return uses;
}

function commandAllowedByPolicy(policy: HelperPolicy, command: string, subcommand: string | null): boolean {
  if (command.startsWith("<dynamic:")) return policy.allowDynamicProcessCommand === true;
  const normalized = normalizeCommand(command);
  const allowedSubcommands = policy.commands.get(normalized);
  if (allowedSubcommands === undefined) return false;
  if (allowedSubcommands === null) return true;
  if (subcommand === null && allowedSubcommands.has(RUNTIME_VALIDATED_SUBCOMMAND)) return true;
  return subcommand !== null && allowedSubcommands.has(subcommand);
}

function messageFor(policy: HelperPolicy | null, command: string, subcommand: string | null): string {
  if (policy === null) {
    return "desktop process launch is outside the approved helper-process modules; project jobs must be daemon/NTM-owned";
  }
  if (command.startsWith("<dynamic:")) {
    return `dynamic process launch is not allowed for helper category ${policy.id}`;
  }
  const suffix = subcommand ? ` ${subcommand}` : "";
  return `command '${normalizeCommand(command)}${suffix}' is not allowed for helper category ${policy.id}`;
}

function shouldFlag(command: string, policy: HelperPolicy | null): boolean {
  if (policy !== null) return true;
  if (command.startsWith("<dynamic:")) return true;
  return JOB_OR_MUTATING_COMMANDS.has(normalizeCommand(command));
}

export function scanSource(filePath: string, source: string): readonly Finding[] {
  const rel = repoRelative(filePath);
  if (shouldSkipFile(filePath)) return [];

  const policy = matchingPolicy(rel);
  const findings: Finding[] = [];
  for (const use of collectCommandUses(source)) {
    if (!shouldFlag(use.command, policy)) continue;
    if (policy !== null && commandAllowedByPolicy(policy, use.command, use.subcommand)) continue;
    findings.push({
      file: rel,
      line: use.line,
      column: use.column,
      command: use.command,
      rule: policy === null ? "desktop-helper.unapproved-process" : "desktop-helper.unapproved-command",
      message: messageFor(policy, use.command, use.subcommand),
    });
  }
  return findings;
}

function main(): void {
  const findings: Finding[] = [];
  for (const root of SCAN_ROOTS) {
    for (const filePath of walk(root)) {
      let source: string;
      try {
        source = readFileSync(filePath, "utf8");
      } catch {
        continue;
      }
      findings.push(...scanSource(filePath, source));
    }
  }

  if (findings.length === 0) {
    console.log("[desktop-helper-boundary] OK — desktop helper-process usage stays within hp-5z7 policy.");
    return;
  }

  console.error(`[desktop-helper-boundary] FAIL — ${findings.length} helper-boundary violation(s):`);
  for (const finding of findings) {
    console.error(`  ${finding.file}:${finding.line}:${finding.column} [${finding.rule}] ${finding.message}`);
    console.error(`    command: ${finding.command}`);
  }
  process.exit(1);
}

if (import.meta.main) {
  main();
}
