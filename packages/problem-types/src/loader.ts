// `@hoopoe/problem-types` — YAML loader (hp-g6sp).
//
// Uses `Bun.YAML.parse`; validates per-entry shape + status code +
// enum values; deduplicates ids and typeUris so the registry can never
// have two entries claim the same identity.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  PROBLEM_ACTIONABILITIES,
  PROBLEM_SURFACES,
  PROBLEM_TYPES_SCHEMA_VERSION,
  type ProblemActionability,
  type ProblemRegistry,
  type ProblemSurface,
  type ProblemType,
} from "./types.ts";

export class ProblemTypesError extends Error {
  override readonly name = "ProblemTypesError";
  readonly path: string;
  constructor(path: string, message: string) {
    super(`problem-types (${path}): ${message}`);
    this.path = path;
  }
}

interface RawRegistry {
  schemaVersion?: number;
  problems?: unknown;
}

interface RawEntry {
  id?: unknown;
  type_uri?: unknown;
  title?: unknown;
  status?: unknown;
  surface?: unknown;
  actionability?: unknown;
  user_message?: unknown;
  detail_template?: unknown;
}

const SURFACES: ReadonlySet<ProblemSurface> = new Set(PROBLEM_SURFACES);
const ACTIONABILITIES: ReadonlySet<ProblemActionability> = new Set(PROBLEM_ACTIONABILITIES);

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function parseEntry(path: string, raw: unknown): ProblemType {
  if (!isObject(raw)) {
    throw new ProblemTypesError(path, "entry is not a mapping");
  }
  const e = raw as RawEntry;
  if (typeof e.id !== "string" || e.id.length === 0) {
    throw new ProblemTypesError(path, "entry missing 'id' string");
  }
  if (typeof e.type_uri !== "string" || !/^https?:\/\//.test(e.type_uri)) {
    throw new ProblemTypesError(
      path,
      `problem '${e.id}' type_uri must be an http(s) URL, got ${String(e.type_uri)}`,
    );
  }
  if (typeof e.title !== "string" || e.title.length === 0) {
    throw new ProblemTypesError(path, `problem '${e.id}' missing 'title'`);
  }
  if (typeof e.status !== "number" || !Number.isInteger(e.status) || e.status < 100 || e.status >= 600) {
    throw new ProblemTypesError(
      path,
      `problem '${e.id}' status must be a valid HTTP status (100-599), got ${String(e.status)}`,
    );
  }
  if (typeof e.surface !== "string" || !SURFACES.has(e.surface as ProblemSurface)) {
    throw new ProblemTypesError(
      path,
      `problem '${e.id}' surface must be one of ${PROBLEM_SURFACES.join(", ")}, got '${String(e.surface)}'`,
    );
  }
  if (
    typeof e.actionability !== "string" ||
    !ACTIONABILITIES.has(e.actionability as ProblemActionability)
  ) {
    throw new ProblemTypesError(
      path,
      `problem '${e.id}' actionability must be one of ${PROBLEM_ACTIONABILITIES.join(", ")}, got '${String(e.actionability)}'`,
    );
  }
  if (typeof e.user_message !== "string" || e.user_message.length === 0) {
    throw new ProblemTypesError(path, `problem '${e.id}' missing 'user_message'`);
  }
  if (e.detail_template !== undefined && typeof e.detail_template !== "string") {
    throw new ProblemTypesError(
      path,
      `problem '${e.id}' detail_template must be a string when present`,
    );
  }
  const out: ProblemType = {
    id: e.id,
    typeUri: e.type_uri,
    title: e.title,
    status: e.status,
    surface: e.surface as ProblemSurface,
    actionability: e.actionability as ProblemActionability,
    userMessage: e.user_message,
  };
  if (typeof e.detail_template === "string") {
    out.detailTemplate = e.detail_template;
  }
  return out;
}

export interface LoadProblemTypesOptions {
  /** Override the file path. Default: `<repoRoot>/packages/schemas/problem-types.yaml`. */
  path?: string;
  /** Repo root used when `path` is omitted. Default: `process.cwd()`. */
  repoRoot?: string;
}

export function loadProblemTypes(
  options: LoadProblemTypesOptions = {},
): ProblemRegistry {
  const path =
    options.path ??
    resolve(options.repoRoot ?? process.cwd(), "packages", "schemas", "problem-types.yaml");
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    throw new ProblemTypesError(path, `failed to read: ${(err as Error).message}`);
  }
  let parsed: unknown;
  try {
    parsed = Bun.YAML.parse(text);
  } catch (err) {
    throw new ProblemTypesError(path, `failed to parse YAML: ${(err as Error).message}`);
  }
  if (!isObject(parsed)) {
    throw new ProblemTypesError(path, "root must be a mapping");
  }
  const root = parsed as RawRegistry;
  if (root.schemaVersion !== PROBLEM_TYPES_SCHEMA_VERSION) {
    throw new ProblemTypesError(
      path,
      `schemaVersion must be ${PROBLEM_TYPES_SCHEMA_VERSION}, got ${String(root.schemaVersion ?? "<missing>")}`,
    );
  }
  if (!Array.isArray(root.problems)) {
    throw new ProblemTypesError(path, "'problems' must be a list");
  }
  const seenIds = new Set<string>();
  const seenUris = new Set<string>();
  const problems: ProblemType[] = [];
  for (const raw of root.problems) {
    const entry = parseEntry(path, raw);
    if (seenIds.has(entry.id)) {
      throw new ProblemTypesError(path, `duplicate problem id '${entry.id}'`);
    }
    if (seenUris.has(entry.typeUri)) {
      throw new ProblemTypesError(path, `duplicate problem type_uri '${entry.typeUri}'`);
    }
    seenIds.add(entry.id);
    seenUris.add(entry.typeUri);
    problems.push(entry);
  }
  return {
    schemaVersion: PROBLEM_TYPES_SCHEMA_VERSION,
    problems,
    sourcePath: path,
  };
}
