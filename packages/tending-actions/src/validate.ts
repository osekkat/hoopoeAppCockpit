// `@hoopoe/tending-actions` — drift checker against OpenAPI's `ActionKind` enum.
//
// `tending-actions.yaml` declares `mirrorsOpenapiSchema: ActionKind`.
// This module enforces that invariant: the YAML's action `kind` set
// must equal the OpenAPI enum, in both directions. Drift is reported
// per direction so the failure message points at the offending side.
//
// We hand-extract the enum from the OpenAPI YAML rather than running
// the full OpenAPI parser; the regex looks for the well-known
// `ActionKind:` block that's followed by a `type: string` and an
// `enum:` list. If the OpenAPI shape ever drifts past what this
// extractor handles, the test fails loudly with a missing-enum error
// (better than silently passing).

import { readFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { TendingActionsError } from "./loader.ts";
import type { TendingActionsBundle } from "./types.ts";

export interface DriftReport {
  /** Action kinds that exist in YAML but not in OpenAPI's enum. */
  extraInYaml: readonly string[];
  /** Action kinds that exist in OpenAPI's enum but not in YAML. */
  missingInYaml: readonly string[];
  /** True iff both lists are empty. */
  inSync: boolean;
}

const ENUM_BLOCK_RE = /\n\s*ActionKind:\s*\n([\s\S]*?)(?=\n\s{0,4}[A-Za-z_][A-Za-z0-9_]*:\s*\n)/;

function extractEnumValues(openapiText: string, openapiPath: string): string[] {
  const block = ENUM_BLOCK_RE.exec(openapiText);
  if (block === null) {
    throw new TendingActionsError(
      openapiPath,
      `failed to locate the 'ActionKind:' block in OpenAPI`,
    );
  }
  const body = block[1] ?? "";
  const enumIdx = body.indexOf("enum:");
  if (enumIdx < 0) {
    throw new TendingActionsError(
      openapiPath,
      `'ActionKind:' block missing an 'enum:' list`,
    );
  }
  const after = body.slice(enumIdx + "enum:".length);
  const values: string[] = [];
  for (const raw of after.split("\n")) {
    const trimmed = raw.trim();
    if (trimmed.length === 0) continue;
    if (!trimmed.startsWith("-")) break;
    const item = trimmed.replace(/^-\s*/, "").replace(/^['"]|['"]$/g, "");
    if (item.length > 0) values.push(item);
  }
  if (values.length === 0) {
    throw new TendingActionsError(openapiPath, `'ActionKind:' enum list is empty`);
  }
  return values;
}

export interface CheckDriftOptions {
  /** Path to the OpenAPI YAML. Defaults to a sibling of the YAML
   *  bundle's `mirrorsOpenapiYaml` reference. */
  openapiPath?: string;
}

export function checkActionKindDrift(
  bundle: TendingActionsBundle,
  options: CheckDriftOptions = {},
): DriftReport {
  const openapiPath =
    options.openapiPath ?? resolve(dirname(bundle.sourcePath), bundle.mirrorsOpenapiYaml);
  let openapiText: string;
  try {
    openapiText = readFileSync(openapiPath, "utf8");
  } catch (err) {
    throw new TendingActionsError(
      openapiPath,
      `failed to read OpenAPI for drift check: ${(err as Error).message}`,
    );
  }
  const enumValues = new Set(extractEnumValues(openapiText, openapiPath));
  const yamlKinds = new Set(bundle.actions.map((a) => a.kind));
  const extraInYaml: string[] = [];
  for (const k of yamlKinds) {
    if (!enumValues.has(k)) extraInYaml.push(k);
  }
  const missingInYaml: string[] = [];
  for (const v of enumValues) {
    if (!yamlKinds.has(v)) missingInYaml.push(v);
  }
  extraInYaml.sort();
  missingInYaml.sort();
  return {
    extraInYaml,
    missingInYaml,
    inSync: extraInYaml.length === 0 && missingInYaml.length === 0,
  };
}

/** Throw if drift is detected. Used by the conformance test. */
export function assertActionKindInSync(
  bundle: TendingActionsBundle,
  options: CheckDriftOptions = {},
): void {
  const report = checkActionKindDrift(bundle, options);
  if (!report.inSync) {
    throw new TendingActionsError(
      bundle.sourcePath,
      `tending-actions.yaml drift vs OpenAPI ActionKind enum: ` +
        `extraInYaml=[${report.extraInYaml.join(", ") || "(none)"}], ` +
        `missingInYaml=[${report.missingInYaml.join(", ") || "(none)"}]`,
    );
  }
}
