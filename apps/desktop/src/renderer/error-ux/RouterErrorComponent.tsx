// hp-vau — `defaultErrorComponent` for TanStack Router's internal
// `CatchBoundary` (see `Matches.js:36-44` in
// `@tanstack/react-router@1.169.1`).
//
// Why this exists: TanStack `RouterProvider` mounts a `Matches` subtree
// that wraps the route component tree in its own React error boundary
// (`CatchBoundary`). Errors thrown by route components are caught there
// BEFORE bubbling up to any parent React `ErrorBoundary`. Without
// `defaultErrorComponent`, the fallback is TanStack's vanilla
// "Something went wrong!" inline UI — and the renderer's
// `errorBus` never sees the crash.
//
// `RouterErrorComponent` is wired in `apps/desktop/src/renderer/routes.tsx`
// via `createRouter({ defaultErrorComponent })`. It:
//
//   1. Renders the same dev/prod fallback as `ErrorBoundary` (shared
//      via `renderErrorFallback.tsx`), so the user sees one UI for
//      crashes regardless of where they happened in the React tree.
//   2. Publishes a synthetic `ProblemEnvelope` to the renderer
//      `errorBus` from a `useEffect` keyed on `error`, so telemetry
//      sinks wired through `ErrorUxRoot.recordTelemetry` observe
//      route-component crashes the same way they observe `ErrorBoundary`
//      catches.
//
// Contract from TanStack: `defaultErrorComponent` receives
// `{ error, reset, info? }` per `ErrorComponentProps`. We coerce
// `error` defensively via `coerceToError` so the fallback can rely on
// `.name`/`.message`/`.stack` even if a route component threw a string
// or non-Error value.
//
// Cross-references:
//   - apps/desktop/src/renderer/error-ux/ErrorBoundary.tsx
//   - apps/desktop/src/renderer/error-ux/renderErrorFallback.tsx
//   - apps/desktop/src/renderer/routes.tsx (wiring)
//   - hp-vau review-finding bead

import { useEffect, type ReactNode } from "react";
import type { ErrorComponentProps } from "@tanstack/react-router";
import type { ErrorBus } from "./types.ts";
import { errorBus as defaultBus } from "./errorBus.ts";
import { buildRendererCrashEnvelope, coerceToError, renderErrorFallback } from "./renderErrorFallback.tsx";

export const ROUTER_BOUNDARY_SOURCE = "renderer.router-boundary";

/** Publish a single boundary-caught error to the renderer error bus.
 *  Extracted from the component body so unit tests can exercise the
 *  publish path directly without needing a React reconciler (bun:test
 *  has no DOM and cannot fire `useEffect`). */
export function publishRouterCrash(error: unknown, bus: ErrorBus = defaultBus): void {
  if (error === null || error === undefined) return;
  try {
    bus.publish({
      source: ROUTER_BOUNDARY_SOURCE,
      severity: "blocking",
      envelope: buildRendererCrashEnvelope(coerceToError(error)),
    });
  } catch {
    // Bus failure must not mask the primary crash UI.
  }
}

/** Test/dev seams. Bundled into the same props object as TanStack's
 *  `ErrorComponentProps` so a single render call carries every override. */
export interface RouterErrorComponentSeams {
  /** Override the default singleton bus. */
  readonly bus?: ErrorBus;
  /** Force the dev branch in tests (otherwise `import.meta.env.DEV`). */
  readonly forceDev?: boolean;
  /** Override `window.location.reload()` from tests. */
  readonly onReload?: () => void;
}

export type RouterErrorComponentProps = ErrorComponentProps & RouterErrorComponentSeams;

export function RouterErrorComponent(props: RouterErrorComponentProps): ReactNode {
  const { error, reset, bus, forceDev, onReload } = props;
  const dev = forceDev ?? Boolean(import.meta.env.DEV);

  useEffect(() => {
    publishRouterCrash(error, bus ?? defaultBus);
  }, [error, bus]);

  const handleReload = (): void => {
    if (onReload) {
      onReload();
      return;
    }
    if (typeof window !== "undefined" && typeof window.location?.reload === "function") {
      window.location.reload();
    }
  };

  return renderErrorFallback({
    error: coerceToError(error),
    dev,
    onReload: handleReload,
    onReset: reset,
    testidPrefix: "router-error-boundary",
  });
}
