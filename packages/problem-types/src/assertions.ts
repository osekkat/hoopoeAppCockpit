// `@hoopoe/problem-types` — assertion helpers for contract tests (hp-g6sp).
//
// Daemon-side handlers must emit problem+json that matches the
// registry. These helpers are what the per-endpoint contract tests
// use to verify that contract:
//
//   const response = await fetch(`${baseUrl}/v1/projects/x`);
//   await assertResponseIsProblemJson(response);
//   const envelope = await response.json();
//   assertProblemMatchesRegistry(envelope, "project.not-found");

import { getProblem } from "./registry.ts";
import type { ProblemEnvelope } from "./types.ts";
import type { LoadProblemTypesOptions } from "./loader.ts";

export class ProblemAssertionError extends Error {
  override readonly name = "ProblemAssertionError";
  constructor(message: string) {
    super(message);
  }
}

export const PROBLEM_JSON_CONTENT_TYPE = "application/problem+json" as const;

interface ResponseLike {
  status: number;
  headers: { get(name: string): string | null };
}

/** Assert that the HTTP response is a problem+json (correct
 *  Content-Type + status in the 4xx/5xx range). Does not consume the
 *  body; tests can `await response.json()` afterward. */
export function assertResponseIsProblemJson(response: ResponseLike): void {
  const contentType = response.headers.get("content-type") ?? "";
  // Allow charset suffix etc.
  if (!contentType.toLowerCase().startsWith(PROBLEM_JSON_CONTENT_TYPE)) {
    throw new ProblemAssertionError(
      `expected Content-Type to start with '${PROBLEM_JSON_CONTENT_TYPE}', got '${contentType}'`,
    );
  }
  if (response.status < 400 || response.status >= 600) {
    throw new ProblemAssertionError(
      `expected HTTP error status (4xx/5xx), got ${response.status}`,
    );
  }
}

/** Assert that an envelope matches a registry entry. Verifies the
 *  type URI, title, status, surface, and actionability — the
 *  load-bearing fields renderers (hp-8dym) and contract tests rely
 *  on. The user-facing `user_message` is template-derived so we don't
 *  do a literal compare. */
export function assertProblemMatchesRegistry(
  envelope: ProblemEnvelope,
  expectedId: string,
  options: LoadProblemTypesOptions = {},
): void {
  const expected = getProblem(expectedId, options);
  if (envelope.type !== expected.typeUri) {
    throw new ProblemAssertionError(
      `type mismatch for '${expectedId}': expected '${expected.typeUri}', got '${envelope.type}'`,
    );
  }
  if (envelope.title !== expected.title) {
    throw new ProblemAssertionError(
      `title mismatch for '${expectedId}': expected '${expected.title}', got '${envelope.title}'`,
    );
  }
  if (envelope.status !== expected.status) {
    throw new ProblemAssertionError(
      `status mismatch for '${expectedId}': expected ${expected.status}, got ${envelope.status}`,
    );
  }
  if (envelope.surface !== expected.surface) {
    throw new ProblemAssertionError(
      `surface mismatch for '${expectedId}': expected '${expected.surface}', got '${envelope.surface}'`,
    );
  }
  if (envelope.actionability !== expected.actionability) {
    throw new ProblemAssertionError(
      `actionability mismatch for '${expectedId}': expected '${expected.actionability}', got '${envelope.actionability}'`,
    );
  }
}

/** Convenience: parse the response body and run all assertions. */
export async function assertResponseMatchesRegistry(
  response: Response,
  expectedId: string,
  options: LoadProblemTypesOptions = {},
): Promise<ProblemEnvelope> {
  assertResponseIsProblemJson(response);
  const envelope = (await response.json()) as ProblemEnvelope;
  assertProblemMatchesRegistry(envelope, expectedId, options);
  return envelope;
}
