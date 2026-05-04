// hp-2qn — Malformed-adapter chaos primitive.
//
// Wraps `@hoopoe/fixture-replay` to load a Phase 0 scenario where a
// specific tool's golden fixture is the `malformed-json` variant from
// `packages/fixtures/golden-outputs/<tool>/malformed-json.json`.
// Tests use this to verify the daemon's degraded-mode tagging path
// without standing up a real malformed daemon process.
//
// The current Phase 0 corpus uses the snapshot.json shape per
// scenario; per-tool malformed-json variants live under
// `packages/fixtures/golden-outputs/<tool>/`. This primitive returns
// the parsed envelope so chaos tests can feed it into adapter unit
// tests, OR the file path so tests can plumb it into the
// fixture-replay harness's adapter accessor.

import { readFileSync, existsSync } from "node:fs";
import { resolve } from "node:path";

export class ChaosMalformedAdapterError extends Error {
  override readonly name = "ChaosMalformedAdapterError";
  constructor(message: string) {
    super(message);
  }
}

export interface MalformedAdapterOptions {
  /** Adapter slug — must match a directory under
   *  `packages/fixtures/golden-outputs/<tool>/`. */
  tool: string;
  /** Override the corpus root. Default: `<repoRoot>/packages/fixtures/`. */
  corpusRoot?: string;
  /** Repo root used when `corpusRoot` is omitted. Default: `process.cwd()`. */
  repoRoot?: string;
}

export interface MalformedAdapterFixture {
  /** Adapter slug. */
  tool: string;
  /** Absolute path to the malformed-json fixture file. */
  path: string;
  /** Raw bytes from the file (intentionally malformed JSON; downstream
   *  parsers must handle the parse failure). */
  rawText: string;
}

/** Load the canonical "malformed-json" golden fixture for an adapter.
 *  Returns the file path + raw text so chaos tests can either:
 *   - feed the bytes through their own adapter parser to verify it
 *     refuses gracefully, OR
 *   - point the daemon's fixture-mode at this file via env var to
 *     observe the degraded-mode tagging path end-to-end. */
export function loadMalformedAdapterFixture(
  options: MalformedAdapterOptions,
): MalformedAdapterFixture {
  const corpusRoot =
    options.corpusRoot ??
    resolve(options.repoRoot ?? process.cwd(), "packages", "fixtures");
  const path = resolve(corpusRoot, "golden-outputs", options.tool, "malformed-json.json");
  if (!existsSync(path)) {
    throw new ChaosMalformedAdapterError(
      `malformed-json fixture not found at ${path} — is the adapter '${options.tool}' covered by the conformance harness?`,
    );
  }
  let rawText: string;
  try {
    rawText = readFileSync(path, "utf8");
  } catch (err) {
    throw new ChaosMalformedAdapterError(
      `failed to read malformed-json fixture for '${options.tool}': ${(err as Error).message}`,
    );
  }
  return { tool: options.tool, path, rawText };
}

/** Convenience: attempt to parse the fixture body and return the
 *  failure. Tests assert "the parse error happens before the adapter
 *  even sees the bytes" — guarantees the chaos fixture really is
 *  malformed. The try/catch is intentional: a successful parse means
 *  the fixture is bogus and the test must fail. */
export function parseMalformedFixture(
  fixture: MalformedAdapterFixture,
): { ok: false; error: string } {
  try {
    JSON.parse(fixture.rawText);
  } catch (err) {
    return { ok: false, error: (err as Error).message };
  }
  return {
    ok: false,
    error: `chaos fixture at ${fixture.path} parsed cleanly — it should be malformed JSON`,
  };
}
