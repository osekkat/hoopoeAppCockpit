// hp-sgy — React ErrorBoundary at the renderer root.
// hp-vau — Shares its dev/prod fallback UI with `RouterErrorComponent`
//          via `renderErrorFallback`. The class boundary catches throws
//          ABOVE the TanStack `RouterProvider` (e.g., inside
//          QueryClientProvider, or in the Provider's own setup); throws
//          INSIDE the route tree are caught by TanStack's internal
//          `CatchBoundary` first and surfaced through
//          `defaultErrorComponent` — that path goes through
//          `RouterErrorComponent` and re-uses the same fallback +
//          `errorBus.publish`.
//
// On dev (`import.meta.env.DEV`) the fallback shows error name + message
// + stack with a "Try again" reset button so the developer can reload
// after fixing the throw. On prod it shows a recovery message and a
// "Reload window" button (`location.reload()`).
//
// We also publish a synthetic ProblemEnvelope to the errorBus so any
// telemetry sink wired through ErrorUxRoot.recordTelemetry observes the
// crash. The bus publish is best-effort and wrapped in try/catch so a
// secondary failure cannot mask the primary one.
//
// Cross-references:
//   - apps/desktop/src/renderer/main.tsx (mount point)
//   - apps/desktop/src/renderer/error-ux/RouterErrorComponent.tsx
//   - apps/desktop/src/renderer/error-ux/renderErrorFallback.tsx
//   - apps/desktop/src/renderer/error-ux/ErrorUxRoot.tsx (publishes-to-bus surface)
//   - apps/desktop/src/renderer/error-ux/errorBus.ts

import { Component, type ErrorInfo, type ReactNode } from "react";
import type { ErrorBus } from "./types.ts";
import { errorBus as defaultBus } from "./errorBus.ts";
import { buildRendererCrashEnvelope, renderErrorFallback } from "./renderErrorFallback.tsx";

interface ErrorBoundaryProps {
  readonly children: ReactNode;
  /** Optional override for tests. Defaults to the singleton bus. */
  readonly bus?: ErrorBus;
  /** Force the dev branch in tests (otherwise `import.meta.env.DEV`). */
  readonly forceDev?: boolean;
  /** Test seam — production code uses `location.reload()`. */
  readonly onReload?: () => void;
}

interface ErrorBoundaryState {
  readonly error: Error | null;
  readonly componentStack: string | null;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  override state: ErrorBoundaryState = { error: null, componentStack: null };
  private mounted = false;

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    return { error };
  }

  override componentDidMount(): void {
    this.mounted = true;
  }

  override componentWillUnmount(): void {
    this.mounted = false;
  }

  override componentDidCatch(error: Error, info: ErrorInfo): void {
    // `getDerivedStateFromError` already populated `state.error`; we add
    // the component stack as a follow-up so the dev fallback can render
    // it. Guarded by `mounted` so unit tests that drive this lifecycle
    // method directly don't trigger React's "setState on unmounted
    // component" warning.
    if (this.mounted) {
      this.setState({ componentStack: info.componentStack ?? null });
    }
    const bus = this.props.bus ?? defaultBus;
    try {
      bus.publish({
        source: "renderer.boundary",
        severity: "blocking",
        envelope: buildRendererCrashEnvelope(error),
      });
    } catch {
      // The boundary fallback already covers the user; a bus failure
      // here must not mask the primary crash.
    }
  }

  private readonly handleReset = (): void => {
    this.setState({ error: null, componentStack: null });
  };

  private readonly handleReload = (): void => {
    if (this.props.onReload) {
      this.props.onReload();
      return;
    }
    if (typeof window !== "undefined" && typeof window.location?.reload === "function") {
      window.location.reload();
    }
  };

  override render(): ReactNode {
    const { error, componentStack } = this.state;
    if (!error) return this.props.children;
    const dev = this.props.forceDev ?? Boolean(import.meta.env.DEV);
    return renderErrorFallback({
      error,
      componentStack,
      dev,
      onReload: this.handleReload,
      onReset: this.handleReset,
    });
  }
}
