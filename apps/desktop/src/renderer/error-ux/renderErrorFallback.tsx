// hp-vau — Shared dev/prod fallback UI for the renderer's React error
// boundaries.
//
// Used by both the React `ErrorBoundary` (catches throws above the
// TanStack `RouterProvider`) and the `RouterErrorComponent` wired to
// `createRouter({ defaultErrorComponent })` (catches throws inside the
// route tree, which TanStack's internal `CatchBoundary` intercepts before
// they ever reach a parent React boundary).
//
// Cross-references:
//   - apps/desktop/src/renderer/error-ux/ErrorBoundary.tsx
//   - apps/desktop/src/renderer/error-ux/RouterErrorComponent.tsx
//   - hp-vau finding (review-findings beads): TanStack CatchBoundary
//     shadows the root React ErrorBoundary for route-component throws.

import type { ReactNode } from "react";
import type { ProblemEnvelope } from "./types.ts";

const PROBLEM_TYPE_URI = "https://hoopoe.io/problems/renderer.crash";

export interface RenderErrorFallbackInput {
  readonly error: Error;
  readonly componentStack?: string | null;
  readonly dev: boolean;
  readonly onReload: () => void;
  /** Optional reset seam — when present AND `dev`, the fallback shows a
   *  "Try again" button. ErrorBoundary supplies its own setState reset;
   *  RouterErrorComponent forwards TanStack's reset callback. */
  readonly onReset?: (() => void) | undefined;
  /** Stable testid prefix for query-by-data-testid hooks. Defaults to
   *  "error-boundary" so existing snapshots keep matching. */
  readonly testidPrefix?: string;
}

export function renderErrorFallback(input: RenderErrorFallbackInput): ReactNode {
  const { error, componentStack, dev, onReload, onReset, testidPrefix = "error-boundary" } = input;
  return (
    <div
      className="hh-error-boundary-root"
      data-testid={`${testidPrefix}-root`}
      role="alert"
      aria-live="assertive"
    >
      <div className="hh-error-boundary-card">
        <h1 className="hh-error-boundary-title">Hoopoe hit an unexpected error</h1>
        <p className="hh-error-boundary-message">
          {dev
            ? "The renderer caught a thrown render error. Fix the underlying bug, then reset the boundary."
            : "Something went wrong inside the Hoopoe window. Reload to recover; your VPS work is unaffected."}
        </p>
        {dev ? (
          <pre
            className="hh-error-boundary-detail"
            data-testid={`${testidPrefix}-detail`}
          >
            <code>{error.name}: {error.message}</code>
            {error.stack ? `\n\n${error.stack}` : ""}
            {componentStack ? `\n\nComponent stack:${componentStack}` : ""}
          </pre>
        ) : null}
        <div className="hh-error-boundary-actions">
          {dev && onReset ? (
            <button
              type="button"
              className="hh-error-boundary-secondary"
              data-testid={`${testidPrefix}-reset`}
              onClick={onReset}
            >
              Try again
            </button>
          ) : null}
          <button
            type="button"
            className="hh-error-boundary-primary"
            data-testid={`${testidPrefix}-reload`}
            onClick={onReload}
          >
            Reload window
          </button>
        </div>
      </div>
    </div>
  );
}

/** Build the synthetic ProblemEnvelope published to `errorBus` whenever a
 *  React boundary catches a render-time throw. Both `ErrorBoundary` and
 *  `RouterErrorComponent` use this; the boundary identity is carried on
 *  `PublishedError.source` (`renderer.boundary` vs `renderer.router-boundary`). */
export function buildRendererCrashEnvelope(error: Error): ProblemEnvelope {
  return {
    type: PROBLEM_TYPE_URI,
    title: "Renderer crashed",
    status: 500,
    surface: "blocking_modal",
    actionability: "reload",
    user_message:
      "The Hoopoe window hit an unexpected error. Reload to recover.",
    detail: error.message,
  };
}

/** Coerce an unknown thrown value into an Error so the fallback can rely
 *  on `.name` / `.message` / `.stack`. TanStack's `CatchBoundary` types
 *  `error` as `unknown`; user code can also `throw "string"`, throw a
 *  number, etc. */
export function coerceToError(value: unknown): Error {
  if (value instanceof Error) return value;
  if (typeof value === "string") return new Error(value);
  return new Error(String(value));
}
