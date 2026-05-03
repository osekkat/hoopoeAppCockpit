// `@hoopoe/test-evidence` — SLO targets loader (hp-6sv).
//
// `packages/slo-targets.yaml` is the single source of truth for the §10.5
// numeric targets. Plan, tests, and Diagnostics dashboards read from here
// — never duplicate a number into a test file.
//
// Schema (intentionally narrow):
//
//   schemaVersion: 1
//   targets:
//     <id>:
//       kind: latency_p95 | duration_p95 | boolean | counter | percentage
//       declared: "10000ms" | "true" | "150" | "95%"
//       description: "..."
//       references: ["plan.md §10.5", "hp-XXX"]   # optional
//
// We hand-roll a tiny YAML parser because the schema is flat and we want
// zero npm dependencies in this package. Anything that doesn't fit the
// expected shape throws `SloTargetsError` with the offending path.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";

export type SloTargetKind =
  | "latency_p95"
  | "duration_p95"
  | "boolean"
  | "counter"
  | "percentage";

export interface SloTarget {
  id: string;
  kind: SloTargetKind;
  /** Declared value as written in YAML (e.g. "10000ms", "true", "150"). */
  declared: string;
  /** Parsed numeric form of `declared` (or NaN for boolean kinds). */
  numeric: number;
  /** Inclusive upper bound (latency / duration / counter / percentage) or
   *  the boolean expectation expressed as 1.0 = true / 0.0 = false. */
  threshold: number;
  description: string;
  references: readonly string[];
}

export interface SloTargets {
  schemaVersion: 1;
  targets: Readonly<Record<string, SloTarget>>;
}

export class SloTargetsError extends Error {
  override readonly name = "SloTargetsError";
  readonly path: string;
  constructor(path: string, message: string) {
    super(`SLO targets (${path}): ${message}`);
    this.path = path;
  }
}

const KNOWN_KINDS: ReadonlySet<SloTargetKind> = new Set([
  "latency_p95",
  "duration_p95",
  "boolean",
  "counter",
  "percentage",
]);

function parseDeclared(kind: SloTargetKind, declared: string, idForError: string): {
  numeric: number;
  threshold: number;
} {
  const trimmed = declared.trim();
  if (kind === "boolean") {
    const lower = trimmed.toLowerCase();
    if (lower === "true") return { numeric: 1, threshold: 1 };
    if (lower === "false") return { numeric: 0, threshold: 0 };
    throw new Error(`SLO '${idForError}': boolean target must be 'true' or 'false', got '${trimmed}'`);
  }
  // latency_p95 / duration_p95 → "<n>ms" or "<n>s"
  // percentage → "<n>%"
  // counter → "<n>"
  let value: number;
  if (kind === "latency_p95" || kind === "duration_p95") {
    const m = /^(\d+(?:\.\d+)?)(ms|s)$/i.exec(trimmed);
    if (m === null) {
      throw new Error(
        `SLO '${idForError}': ${kind} target must be '<n>ms' or '<n>s', got '${trimmed}'`,
      );
    }
    const raw = Number(m[1]);
    value = m[2]?.toLowerCase() === "s" ? raw * 1000 : raw;
  } else if (kind === "percentage") {
    const m = /^(\d+(?:\.\d+)?)%$/.exec(trimmed);
    if (m === null) {
      throw new Error(
        `SLO '${idForError}': percentage target must be '<n>%', got '${trimmed}'`,
      );
    }
    value = Number(m[1]);
  } else {
    // counter
    const m = /^(\d+(?:\.\d+)?)$/.exec(trimmed);
    if (m === null) {
      throw new Error(
        `SLO '${idForError}': counter target must be a bare number, got '${trimmed}'`,
      );
    }
    value = Number(m[1]);
  }
  return { numeric: value, threshold: value };
}

interface ParsedTargetEntry {
  kind?: string;
  declared?: string;
  description?: string;
  references?: readonly string[];
}

interface ParsedRoot {
  schemaVersion?: number;
  targets?: Record<string, ParsedTargetEntry>;
}

function tinyYamlParse(text: string, path: string): ParsedRoot {
  // Supports the flat structure we declare: top-level `schemaVersion: <n>`,
  // a `targets:` block whose direct children are id keys, each with the
  // four allowed leaf keys (kind / declared / description / references).
  // No anchors, aliases, multiline scalars, or block-flow mixing. If the
  // file evolves past this shape, swap in a real parser.
  const lines = text.split("\n");
  const root: ParsedRoot = {};
  let mode: "root" | "targets-root" | "target-entry" = "root";
  let currentTargetId: string | null = null;
  let referencesList: string[] | null = null;

  for (let i = 0; i < lines.length; i++) {
    const raw = lines[i] ?? "";
    const stripped = raw.replace(/#.*$/, "");
    const line = stripped.replace(/\s+$/, "");
    if (line.trim().length === 0) continue;
    const indent = line.length - line.trimStart().length;
    const trimmed = line.trim();

    // Top-level
    if (indent === 0) {
      mode = "root";
      currentTargetId = null;
      referencesList = null;
      const m = /^([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.*)$/.exec(trimmed);
      if (m === null) {
        throw new SloTargetsError(path, `line ${i + 1}: unparseable top-level entry '${trimmed}'`);
      }
      const key = m[1] as string;
      const value = (m[2] as string).trim();
      if (key === "schemaVersion") {
        if (!/^\d+$/.test(value)) {
          throw new SloTargetsError(path, `line ${i + 1}: schemaVersion must be an integer`);
        }
        root.schemaVersion = Number(value);
      } else if (key === "targets") {
        if (value !== "" && value !== "{}") {
          throw new SloTargetsError(path, `line ${i + 1}: 'targets:' must be a block`);
        }
        root.targets = {};
        mode = "targets-root";
      } else {
        throw new SloTargetsError(path, `line ${i + 1}: unknown top-level key '${key}'`);
      }
      continue;
    }

    if ((mode === "targets-root" || mode === "target-entry") && indent === 2) {
      const m = /^([A-Za-z_][A-Za-z0-9_.\-]*)\s*:\s*$/.exec(trimmed);
      if (m === null) {
        throw new SloTargetsError(path, `line ${i + 1}: target id must end with ':'`);
      }
      currentTargetId = m[1] as string;
      referencesList = null;
      if (root.targets === undefined) root.targets = {};
      root.targets[currentTargetId] = {};
      mode = "target-entry";
      continue;
    }

    if (mode === "target-entry" && indent === 4) {
      if (currentTargetId === null) {
        throw new SloTargetsError(path, `line ${i + 1}: target leaf without parent id`);
      }
      const entry = root.targets?.[currentTargetId];
      if (entry === undefined) {
        throw new SloTargetsError(path, `line ${i + 1}: missing target '${currentTargetId}'`);
      }
      const m = /^([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.*)$/.exec(trimmed);
      if (m === null) {
        throw new SloTargetsError(path, `line ${i + 1}: unparseable leaf '${trimmed}'`);
      }
      const key = m[1] as string;
      const value = (m[2] as string).trim();
      if (key === "kind") entry.kind = unquote(value);
      else if (key === "declared") entry.declared = unquote(value);
      else if (key === "description") entry.description = unquote(value);
      else if (key === "references") {
        if (value === "" || value === "[]") {
          entry.references = [];
        } else {
          const items = parseInlineList(value, path, i + 1);
          entry.references = items;
        }
        referencesList = entry.references === undefined ? [] : entry.references.slice();
      } else {
        throw new SloTargetsError(path, `line ${i + 1}: unknown target leaf '${key}'`);
      }
      continue;
    }

    if (mode === "target-entry" && indent === 6 && trimmed.startsWith("- ")) {
      // Block-list item under a `references:` leaf.
      if (currentTargetId === null) {
        throw new SloTargetsError(path, `line ${i + 1}: list item without parent`);
      }
      if (referencesList === null) {
        referencesList = [];
        const entry = root.targets?.[currentTargetId];
        if (entry !== undefined) entry.references = referencesList;
      }
      const itemRaw = trimmed.slice(2).trim();
      referencesList.push(unquote(itemRaw));
      const entry = root.targets?.[currentTargetId];
      if (entry !== undefined) entry.references = referencesList.slice();
      continue;
    }

    throw new SloTargetsError(path, `line ${i + 1}: unexpected indentation/content '${raw}'`);
  }

  return root;
}

function unquote(value: string): string {
  const trimmed = value.trim();
  if (trimmed.length >= 2) {
    const first = trimmed[0];
    const last = trimmed[trimmed.length - 1];
    if ((first === '"' && last === '"') || (first === "'" && last === "'")) {
      return trimmed.slice(1, -1);
    }
  }
  return trimmed;
}

function parseInlineList(value: string, path: string, lineNo: number): string[] {
  const trimmed = value.trim();
  if (!(trimmed.startsWith("[") && trimmed.endsWith("]"))) {
    throw new SloTargetsError(path, `line ${lineNo}: inline list must use [ ... ] form`);
  }
  const inner = trimmed.slice(1, -1).trim();
  if (inner.length === 0) return [];
  // Naive split on commas not inside strings (good enough for our use case).
  const items: string[] = [];
  let buf = "";
  let inSingle = false;
  let inDouble = false;
  for (const ch of inner) {
    if (ch === "'" && !inDouble) inSingle = !inSingle;
    else if (ch === '"' && !inSingle) inDouble = !inDouble;
    if (ch === "," && !inSingle && !inDouble) {
      items.push(unquote(buf.trim()));
      buf = "";
      continue;
    }
    buf += ch;
  }
  if (buf.trim().length > 0) items.push(unquote(buf.trim()));
  return items;
}

export interface LoadSloTargetsOptions {
  /** Override the file path. Default: `<repoRoot>/packages/slo-targets.yaml`. */
  path?: string;
  /** Repo root used when `path` is omitted. Default: `process.cwd()`. */
  repoRoot?: string;
}

export function loadSloTargets(options: LoadSloTargetsOptions = {}): SloTargets {
  const path =
    options.path ?? resolve(options.repoRoot ?? process.cwd(), "packages", "slo-targets.yaml");
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    throw new SloTargetsError(path, `failed to read: ${(err as Error).message}`);
  }
  const parsed = tinyYamlParse(text, path);
  if (parsed.schemaVersion !== 1) {
    throw new SloTargetsError(
      path,
      `schemaVersion must be 1, got ${parsed.schemaVersion ?? "<missing>"}`,
    );
  }
  const targetsRaw = parsed.targets ?? {};
  const targets: Record<string, SloTarget> = {};
  for (const [id, entry] of Object.entries(targetsRaw)) {
    if (entry.kind === undefined || !KNOWN_KINDS.has(entry.kind as SloTargetKind)) {
      throw new SloTargetsError(
        path,
        `target '${id}' has unknown kind '${String(entry.kind)}'; allowed: ${Array.from(KNOWN_KINDS).join(", ")}`,
      );
    }
    if (entry.declared === undefined || entry.declared.length === 0) {
      throw new SloTargetsError(path, `target '${id}' missing 'declared'`);
    }
    if (entry.description === undefined) {
      throw new SloTargetsError(path, `target '${id}' missing 'description'`);
    }
    let parsedDeclared;
    try {
      parsedDeclared = parseDeclared(entry.kind as SloTargetKind, entry.declared, id);
    } catch (err) {
      throw new SloTargetsError(path, (err as Error).message);
    }
    targets[id] = {
      id,
      kind: entry.kind as SloTargetKind,
      declared: entry.declared,
      numeric: parsedDeclared.numeric,
      threshold: parsedDeclared.threshold,
      description: entry.description,
      references: entry.references ?? [],
    };
  }
  return { schemaVersion: 1, targets };
}

/** Evaluate an observed value against a target. For latency / duration /
 *  counter / percentage kinds, `passed = observed <= threshold`. For
 *  boolean targets, `passed = (observed >= 1) === (threshold === 1)`. */
export function evaluateAgainst(target: SloTarget, observed: number): boolean {
  if (target.kind === "boolean") {
    const obs = observed >= 1 ? 1 : 0;
    return obs === target.threshold;
  }
  return observed <= target.threshold;
}
