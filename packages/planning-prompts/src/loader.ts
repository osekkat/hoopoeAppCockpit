import { readFileSync } from "node:fs";
import { dirname, isAbsolute, relative, resolve } from "node:path";
import { computePromptHash } from "./hash.ts";
import {
  PLANNING_PROMPTS_SCHEMA_VERSION,
  type PlanningPrompt,
  type PromptFrontmatter,
  type PromptManifest,
  type PromptManifestEntry,
} from "./types.ts";

export class PlanningPromptsError extends Error {
  override readonly name = "PlanningPromptsError";
  readonly path: string;

  constructor(path: string, message: string) {
    super(`planning-prompts (${path}): ${message}`);
    this.path = path;
  }
}

interface RawManifest {
  schemaVersion?: number;
  prompts?: unknown;
}

interface RawManifestEntry {
  id?: unknown;
  version?: unknown;
  path?: unknown;
  hash?: unknown;
  owner?: unknown;
  appliesToPipelineVersions?: unknown;
}

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function asStringArray(path: string, field: string, raw: unknown): string[] {
  if (!Array.isArray(raw) || raw.some((item) => typeof item !== "string")) {
    throw new PlanningPromptsError(path, `${field} must be a list of strings`);
  }
  return raw.slice() as string[];
}

function parseFrontmatter(path: string, text: string): PlanningPrompt {
  const normalized = text.replace(/\r\n/g, "\n");
  if (!normalized.startsWith("---\n")) {
    throw new PlanningPromptsError(path, "prompt must start with frontmatter");
  }
  const end = normalized.indexOf("\n---\n", 4);
  if (end === -1) {
    throw new PlanningPromptsError(path, "frontmatter closing marker not found");
  }
  const rawFrontmatter = normalized.slice(4, end).split("\n");
  const body = normalized.slice(end + "\n---\n".length);
  const scalar = new Map<string, string>();
  const list = new Map<string, string[]>();
  for (let i = 0; i < rawFrontmatter.length; i += 1) {
    const line = rawFrontmatter[i] as string;
    if (line.trim() === "") continue;
    const match = /^([A-Za-z][A-Za-z0-9_-]*):(?:\s*(.*))?$/.exec(line);
    if (match === null) {
      throw new PlanningPromptsError(path, `invalid frontmatter line '${line}'`);
    }
    const key = match[1] as string;
    const value = (match[2] ?? "").trim();
    if (value !== "") {
      scalar.set(key, value);
      continue;
    }
    const values: string[] = [];
    while (i + 1 < rawFrontmatter.length) {
      const next = rawFrontmatter[i + 1] as string;
      const trimmed = next.trimStart();
      if (!trimmed.startsWith("- ")) break;
      values.push(trimmed.slice(2));
      i += 1;
    }
    list.set(key, values);
  }
  const version = Number(scalar.get("version"));
  if (!Number.isInteger(version) || version <= 0) {
    throw new PlanningPromptsError(path, "version must be a positive integer");
  }
  const frontmatter: PromptFrontmatter = {
    id: required(path, scalar, "id"),
    version,
    hash: required(path, scalar, "hash"),
    owner: required(path, scalar, "owner"),
    lastEdited: required(path, scalar, "last-edited"),
    appliesToPipelineVersions: list.get("applies-to-pipeline-versions") ?? [],
  };
  if (frontmatter.appliesToPipelineVersions.length === 0) {
    throw new PlanningPromptsError(path, "applies-to-pipeline-versions must not be empty");
  }
  return { frontmatter, body, sourcePath: path };
}

function required(path: string, fields: Map<string, string>, key: string): string {
  const value = fields.get(key);
  if (value === undefined || value.length === 0) {
    throw new PlanningPromptsError(path, `frontmatter missing '${key}'`);
  }
  return value;
}

function parseManifestEntry(path: string, raw: unknown): PromptManifestEntry {
  if (!isObject(raw)) {
    throw new PlanningPromptsError(path, "manifest prompt entry must be an object");
  }
  const entry = raw as RawManifestEntry;
  if (typeof entry.id !== "string" || entry.id.length === 0) {
    throw new PlanningPromptsError(path, "manifest entry missing id");
  }
  if (typeof entry.version !== "number" || !Number.isInteger(entry.version) || entry.version <= 0) {
    throw new PlanningPromptsError(path, `manifest entry '${entry.id}' version must be positive`);
  }
  if (typeof entry.path !== "string" || entry.path.length === 0) {
    throw new PlanningPromptsError(path, `manifest entry '${entry.id}' missing path`);
  }
  if (typeof entry.hash !== "string" || !entry.hash.startsWith("sha256:")) {
    throw new PlanningPromptsError(path, `manifest entry '${entry.id}' hash must be sha256:*`);
  }
  if (typeof entry.owner !== "string" || entry.owner.length === 0) {
    throw new PlanningPromptsError(path, `manifest entry '${entry.id}' missing owner`);
  }
  return {
    id: entry.id,
    version: entry.version,
    path: entry.path,
    hash: entry.hash,
    owner: entry.owner,
    appliesToPipelineVersions: asStringArray(
      path,
      `${entry.id}.appliesToPipelineVersions`,
      entry.appliesToPipelineVersions,
    ),
  };
}

export interface LoadPlanningPromptsOptions {
  manifestPath?: string;
  repoRoot?: string;
}

export function loadPromptFile(path: string): PlanningPrompt {
  let text: string;
  try {
    text = readFileSync(path, "utf8");
  } catch (err) {
    throw new PlanningPromptsError(path, `failed to read: ${(err as Error).message}`);
  }
  return parseFrontmatter(path, text);
}

export function loadManifest(options: LoadPlanningPromptsOptions = {}): PromptManifest {
  const path =
    options.manifestPath ??
    resolveManifestPath(
      options.repoRoot ?? process.cwd(),
      "packages/planning-prompts/manifest.json",
    );
  let parsed: unknown;
  try {
    parsed = JSON.parse(readFileSync(path, "utf8")) as unknown;
  } catch (err) {
    throw new PlanningPromptsError(path, `failed to read manifest: ${(err as Error).message}`);
  }
  if (!isObject(parsed)) {
    throw new PlanningPromptsError(path, "manifest root must be an object");
  }
  const raw = parsed as RawManifest;
  if (raw.schemaVersion !== PLANNING_PROMPTS_SCHEMA_VERSION) {
    throw new PlanningPromptsError(
      path,
      `schemaVersion must be ${PLANNING_PROMPTS_SCHEMA_VERSION}, got ${String(raw.schemaVersion ?? "<missing>")}`,
    );
  }
  if (!Array.isArray(raw.prompts)) {
    throw new PlanningPromptsError(path, "prompts must be a list");
  }
  const seen = new Set<string>();
  const prompts: PromptManifestEntry[] = [];
  for (const rawEntry of raw.prompts) {
    const entry = parseManifestEntry(path, rawEntry);
    if (seen.has(entry.id)) {
      throw new PlanningPromptsError(path, `duplicate prompt id '${entry.id}'`);
    }
    seen.add(entry.id);
    prompts.push(entry);
  }
  return { schemaVersion: PLANNING_PROMPTS_SCHEMA_VERSION, prompts, sourcePath: path };
}

export function loadPlanningPrompts(options: LoadPlanningPromptsOptions = {}): PlanningPrompt[] {
  const manifest = loadManifest(options);
  const root = dirname(manifest.sourcePath);
  return manifest.prompts.map((entry) => loadPromptFile(resolveManifestPath(root, entry.path)));
}

export function validatePromptAgainstManifest(
  manifestPath: string,
  entry: PromptManifestEntry,
): PlanningPrompt {
  const prompt = loadPromptFile(resolveManifestPath(dirname(manifestPath), entry.path));
  if (prompt.frontmatter.id !== entry.id) {
    throw new PlanningPromptsError(
      prompt.sourcePath,
      `frontmatter id '${prompt.frontmatter.id}' does not match manifest id '${entry.id}'`,
    );
  }
  if (prompt.frontmatter.version !== entry.version) {
    throw new PlanningPromptsError(
      prompt.sourcePath,
      `version ${prompt.frontmatter.version} does not match manifest version ${entry.version}`,
    );
  }
  if (prompt.frontmatter.owner !== entry.owner) {
    throw new PlanningPromptsError(
      prompt.sourcePath,
      `owner '${prompt.frontmatter.owner}' does not match manifest owner '${entry.owner}'`,
    );
  }
  const computed = computePromptHash(prompt.body);
  if (prompt.frontmatter.hash !== computed || entry.hash !== computed) {
    throw new PlanningPromptsError(
      prompt.sourcePath,
      `hash drift: computed ${computed}, frontmatter ${prompt.frontmatter.hash}, manifest ${entry.hash}`,
    );
  }
  return prompt;
}

function resolveManifestPath(root: string, entryPath: string): string {
  if (isAbsolute(entryPath)) {
    throw new PlanningPromptsError(root, `manifest path '${entryPath}' must be relative`);
  }
  const resolved = resolve(root, entryPath);
  const rel = relative(root, resolved);
  if (rel === "" || rel.startsWith("..") || isAbsolute(rel)) {
    throw new PlanningPromptsError(root, `manifest path '${entryPath}' escapes prompt package`);
  }
  return resolved;
}
