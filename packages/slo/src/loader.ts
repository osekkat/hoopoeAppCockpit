// `@hoopoe/slo` — YAML loader for the canonical hp-5ja target schema.
//
// Uses Bun's native `Bun.YAML.parse`; no third-party YAML dep. The
// loaded targets are validated and parsed into the typed `SloTargets`
// shape so consumers don't deal with raw YAML AST.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  SLO_SCHEMA_VERSION,
  type BooleanTarget,
  type Direction,
  type PercentileTarget,
  type SloTarget,
  type SloTargets,
  type Target,
} from "./types.ts";

export class SloTargetsError extends Error {
  override readonly name = "SloTargetsError";
  readonly path: string;
  constructor(path: string, message: string) {
    super(`SLO targets (${path}): ${message}`);
    this.path = path;
  }
}

interface ParsedRoot {
  schemaVersion?: number;
  targets?: ReadonlyArray<unknown>;
}

interface ParsedTarget {
  id?: unknown;
  description?: unknown;
  target?: unknown;
  source_section?: unknown;
  enforced_in?: unknown;
}

interface ParsedPercentile {
  percentile?: unknown;
  value?: unknown;
  direction?: unknown;
}

interface ParsedBoolean {
  boolean?: unknown;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseValue(path: string, idForError: string, raw: string): {
  numeric: number;
  unit: PercentileTarget["unit"];
} {
  const trimmed = raw.trim();
  let m = /^(\d+(?:\.\d+)?)(ms|s)$/i.exec(trimmed);
  if (m !== null) {
    const value = Number(m[1]);
    const unit = (m[2] as string).toLowerCase();
    return {
      numeric: unit === "s" ? value * 1000 : value,
      unit: unit === "s" ? "s" : "ms",
    };
  }
  m = /^(\d+(?:\.\d+)?)%$/.exec(trimmed);
  if (m !== null) {
    return { numeric: Number(m[1]), unit: "%" };
  }
  m = /^(\d+(?:\.\d+)?)$/.exec(trimmed);
  if (m !== null) {
    return { numeric: Number(m[1]), unit: "count" };
  }
  throw new SloTargetsError(
    path,
    `target '${idForError}' value '${trimmed}' is not <n>{ms|s|%} or a bare number`,
  );
}

function parsePercentileTarget(
  path: string,
  idForError: string,
  raw: ParsedPercentile,
): PercentileTarget {
  if (typeof raw.percentile !== "number" || raw.percentile <= 0 || raw.percentile > 100) {
    throw new SloTargetsError(
      path,
      `target '${idForError}' percentile must be a number in (0, 100], got ${String(raw.percentile)}`,
    );
  }
  if (typeof raw.value !== "string" || raw.value.length === 0) {
    throw new SloTargetsError(
      path,
      `target '${idForError}' value must be a non-empty string`,
    );
  }
  if (raw.direction !== "max" && raw.direction !== "min") {
    throw new SloTargetsError(
      path,
      `target '${idForError}' direction must be 'max' or 'min', got '${String(raw.direction)}'`,
    );
  }
  const parsed = parseValue(path, idForError, raw.value);
  return {
    kind: "percentile",
    percentile: raw.percentile,
    declared: raw.value,
    numeric: parsed.numeric,
    unit: parsed.unit,
    direction: raw.direction as Direction,
  };
}

function parseBooleanTarget(
  path: string,
  idForError: string,
  raw: ParsedBoolean,
): BooleanTarget {
  if (typeof raw.boolean !== "boolean") {
    throw new SloTargetsError(
      path,
      `target '${idForError}' boolean must be true or false`,
    );
  }
  return { kind: "boolean", expected: raw.boolean };
}

function parseTarget(path: string, idForError: string, raw: unknown): Target {
  if (!isObject(raw)) {
    throw new SloTargetsError(path, `target '${idForError}' missing 'target' object`);
  }
  if ("boolean" in raw) return parseBooleanTarget(path, idForError, raw as ParsedBoolean);
  return parsePercentileTarget(path, idForError, raw as ParsedPercentile);
}

function parseTargetEntry(path: string, raw: unknown): SloTarget {
  if (!isObject(raw)) {
    throw new SloTargetsError(path, `entry is not an object`);
  }
  const entry = raw as ParsedTarget;
  if (typeof entry.id !== "string" || entry.id.length === 0) {
    throw new SloTargetsError(path, `entry missing 'id' string`);
  }
  if (typeof entry.description !== "string" || entry.description.length === 0) {
    throw new SloTargetsError(path, `target '${entry.id}' missing 'description'`);
  }
  if (typeof entry.source_section !== "string" || entry.source_section.length === 0) {
    throw new SloTargetsError(path, `target '${entry.id}' missing 'source_section'`);
  }
  if (!Array.isArray(entry.enforced_in) || entry.enforced_in.some((p) => typeof p !== "string")) {
    throw new SloTargetsError(path, `target '${entry.id}' enforced_in must be string[]`);
  }
  return {
    id: entry.id,
    description: entry.description,
    target: parseTarget(path, entry.id, entry.target),
    sourceSection: entry.source_section,
    enforcedIn: entry.enforced_in.slice() as readonly string[],
  };
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
  let parsed: unknown;
  try {
    parsed = Bun.YAML.parse(text);
  } catch (err) {
    throw new SloTargetsError(path, `failed to parse YAML: ${(err as Error).message}`);
  }
  if (!isObject(parsed)) {
    throw new SloTargetsError(path, "root must be a mapping");
  }
  const root = parsed as ParsedRoot;
  if (root.schemaVersion !== SLO_SCHEMA_VERSION) {
    throw new SloTargetsError(
      path,
      `schemaVersion must be ${SLO_SCHEMA_VERSION}, got ${String(root.schemaVersion ?? "<missing>")}`,
    );
  }
  if (!Array.isArray(root.targets)) {
    throw new SloTargetsError(path, "'targets' must be a list");
  }
  const seenIds = new Set<string>();
  const targets: SloTarget[] = [];
  for (const raw of root.targets) {
    const entry = parseTargetEntry(path, raw);
    if (seenIds.has(entry.id)) {
      throw new SloTargetsError(path, `duplicate target id '${entry.id}'`);
    }
    seenIds.add(entry.id);
    targets.push(entry);
  }
  return { schemaVersion: SLO_SCHEMA_VERSION, targets, sourcePath: path };
}
