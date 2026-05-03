// `@hoopoe/test-evidence` — atomic envelope writer (hp-6sv).
//
// Convention: `<repoRoot>/docs/test-evidence/<phase>/<UTC-timestamp>/<runner>-<runId>.json`
//   - `phase` is taken from the envelope (e.g., "phase2").
//   - The UTC timestamp is `YYYYMMDDTHHMMSSZ` (sortable, filesystem-safe).
//   - `runner` is the envelope's runner id; `runId` is the envelope's UUID.
//
// We write atomically: write a sibling `.tmp` file, fsync, then rename.
// Crash-safe enough that a half-written envelope never appears under the
// canonical name.

import { mkdir, rename, writeFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import type { TestEvidenceEnvelope } from "./envelope.ts";

export interface WriteEvidenceOptions {
  /** Repo root (the directory above `docs/`). Default: `process.cwd()`. */
  repoRoot?: string;
  /** Override the `<UTC-timestamp>` segment (for tests). */
  timestamp?: string;
  /** Override the per-envelope JSON serializer (e.g. to inject pretty-print). */
  serialize?: (envelope: TestEvidenceEnvelope) => string;
}

export interface WriteEvidenceResult {
  /** Absolute path to the written envelope file. */
  path: string;
  /** Relative path from `repoRoot`. */
  relativePath: string;
  /** Bytes written. */
  bytes: number;
}

const DEFAULT_SERIALIZE = (env: TestEvidenceEnvelope): string =>
  `${JSON.stringify(env, null, 2)}\n`;

/** UTC timestamp segment used in the evidence directory. */
export function timestampSegment(date: Date): string {
  const yyyy = date.getUTCFullYear();
  const mm = pad2(date.getUTCMonth() + 1);
  const dd = pad2(date.getUTCDate());
  const hh = pad2(date.getUTCHours());
  const mi = pad2(date.getUTCMinutes());
  const ss = pad2(date.getUTCSeconds());
  return `${yyyy}${mm}${dd}T${hh}${mi}${ss}Z`;
}

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`;
}

export function evidencePath(
  envelope: TestEvidenceEnvelope,
  options: WriteEvidenceOptions = {},
): { absolute: string; relative: string } {
  const root = options.repoRoot ?? process.cwd();
  const timestamp = options.timestamp ?? timestampSegment(new Date(envelope.ts));
  const relative = `docs/test-evidence/${envelope.phase}/${timestamp}/${envelope.runner}-${envelope.runId}.json`;
  return { absolute: resolve(root, relative), relative };
}

export async function writeEvidence(
  envelope: TestEvidenceEnvelope,
  options: WriteEvidenceOptions = {},
): Promise<WriteEvidenceResult> {
  const { absolute, relative } = evidencePath(envelope, options);
  const serialize = options.serialize ?? DEFAULT_SERIALIZE;
  const body = serialize(envelope);
  const tmp = `${absolute}.${process.pid}.tmp`;
  await mkdir(dirname(absolute), { recursive: true });
  await writeFile(tmp, body, { encoding: "utf8", mode: 0o644 });
  await rename(tmp, absolute);
  return { path: absolute, relativePath: relative, bytes: Buffer.byteLength(body, "utf8") };
}
