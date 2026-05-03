// `@hoopoe/tending-actions` — YAML loader (hp-dmz).
//
// Uses `Bun.YAML.parse` to read the canonical
// `packages/schemas/tending-actions.yaml`. Validates the shape of the
// top-level bundle + every per-action entry; throws
// `TendingActionsError` with a precise pointer on shape mismatch.

import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import {
  TENDING_ACTIONS_SCHEMA_VERSION,
  type JsonSchemaFragment,
  type RiskClass,
  type TendingAction,
  type TendingActionsBundle,
} from "./types.ts";

export class TendingActionsError extends Error {
  override readonly name = "TendingActionsError";
  readonly path: string;
  constructor(path: string, message: string) {
    super(`tending-actions (${path}): ${message}`);
    this.path = path;
  }
}

interface RawBundle {
  schemaVersion?: number;
  mirrorsOpenapiSchema?: unknown;
  mirrorsOpenapiYaml?: unknown;
  actions?: unknown;
}

const KNOWN_RISK_CLASSES: ReadonlySet<RiskClass> = new Set([
  "low",
  "medium",
  "high",
  "destructive",
]);

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function asStringArray(path: string, kind: string, field: string, raw: unknown): string[] {
  if (raw === undefined) return [];
  if (!Array.isArray(raw)) {
    throw new TendingActionsError(
      path,
      `action '${kind}' field '${field}' must be a list of strings`,
    );
  }
  for (const item of raw) {
    if (typeof item !== "string") {
      throw new TendingActionsError(
        path,
        `action '${kind}' field '${field}' contains a non-string entry`,
      );
    }
  }
  return raw.slice() as string[];
}

function asJsonSchemaFragment(
  path: string,
  kind: string,
  field: string,
  raw: unknown,
): JsonSchemaFragment {
  if (!isObject(raw)) {
    throw new TendingActionsError(
      path,
      `action '${kind}' field '${field}' must be a mapping`,
    );
  }
  return raw;
}

function parseAction(path: string, kind: string, raw: unknown): TendingAction {
  if (!isObject(raw)) {
    throw new TendingActionsError(path, `action '${kind}' must be a mapping`);
  }
  const r = raw;
  if (typeof r.description !== "string" || r.description.length === 0) {
    throw new TendingActionsError(path, `action '${kind}' missing 'description'`);
  }
  if (typeof r.riskClass !== "string" || !KNOWN_RISK_CLASSES.has(r.riskClass as RiskClass)) {
    throw new TendingActionsError(
      path,
      `action '${kind}' riskClass must be one of ${Array.from(KNOWN_RISK_CLASSES).join(", ")}, got '${String(r.riskClass)}'`,
    );
  }
  if (typeof r.requiresApprovalDefault !== "boolean") {
    throw new TendingActionsError(
      path,
      `action '${kind}' requiresApprovalDefault must be a boolean`,
    );
  }
  return {
    kind,
    description: r.description,
    riskClass: r.riskClass as RiskClass,
    requiresApprovalDefault: r.requiresApprovalDefault,
    target: asJsonSchemaFragment(path, kind, "target", r.target),
    args: asJsonSchemaFragment(path, kind, "args", r.args),
    preconditions: asStringArray(path, kind, "preconditions", r.preconditions),
    postconditions: asStringArray(path, kind, "postconditions", r.postconditions),
  };
}

export interface LoadTendingActionsOptions {
  /** Override the file path. Default:
   *  `<repoRoot>/packages/schemas/tending-actions.yaml`. */
  path?: string;
  /** Repo root used when `path` is omitted. Default: `process.cwd()`. */
  repoRoot?: string;
}

export function loadTendingActions(
  options: LoadTendingActionsOptions = {},
): TendingActionsBundle {
  const path =
    options.path ??
    resolve(options.repoRoot ?? process.cwd(), "packages", "schemas", "tending-actions.yaml");
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    throw new TendingActionsError(path, `failed to read: ${(err as Error).message}`);
  }
  let parsed: unknown;
  try {
    parsed = Bun.YAML.parse(text);
  } catch (err) {
    throw new TendingActionsError(path, `failed to parse YAML: ${(err as Error).message}`);
  }
  if (!isObject(parsed)) {
    throw new TendingActionsError(path, "root must be a mapping");
  }
  const root = parsed as RawBundle;
  if (root.schemaVersion !== TENDING_ACTIONS_SCHEMA_VERSION) {
    throw new TendingActionsError(
      path,
      `schemaVersion must be ${TENDING_ACTIONS_SCHEMA_VERSION}, got ${String(root.schemaVersion ?? "<missing>")}`,
    );
  }
  if (typeof root.mirrorsOpenapiSchema !== "string" || root.mirrorsOpenapiSchema.length === 0) {
    throw new TendingActionsError(path, "mirrorsOpenapiSchema must be a non-empty string");
  }
  if (typeof root.mirrorsOpenapiYaml !== "string" || root.mirrorsOpenapiYaml.length === 0) {
    throw new TendingActionsError(path, "mirrorsOpenapiYaml must be a non-empty string");
  }
  if (!isObject(root.actions)) {
    throw new TendingActionsError(path, "'actions' must be a mapping of kind → entry");
  }
  const actions: TendingAction[] = [];
  const seenKinds = new Set<string>();
  for (const [kind, raw] of Object.entries(root.actions)) {
    if (seenKinds.has(kind)) {
      throw new TendingActionsError(path, `duplicate action kind '${kind}'`);
    }
    seenKinds.add(kind);
    actions.push(parseAction(path, kind, raw));
  }
  return {
    schemaVersion: TENDING_ACTIONS_SCHEMA_VERSION,
    mirrorsOpenapiSchema: root.mirrorsOpenapiSchema,
    mirrorsOpenapiYaml: root.mirrorsOpenapiYaml,
    actions,
    sourcePath: path,
  };
}
