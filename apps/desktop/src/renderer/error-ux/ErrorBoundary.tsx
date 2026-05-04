// hp-sgy — React ErrorBoundary at the renderer root.
//
// Catches render-time throws so a single bad component cannot unmount
// the entire renderer tree. The existing ErrorUxRoot / BlockingModal /
// errorBus pipeline only handles errors that are *published* to the
// bus; a thrown render error short-circuits React before any bus
// subscriber runs, which is why a class boundary is required (it's
// the only React API that can intercept descendant render errors).
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
//   - apps/desktop/src/renderer/error-ux/ErrorUxRoot.tsx (publishes-to-bus surface)
//   - apps/desktop/src/renderer/error-ux/errorBus.ts

import { Component, type ErrorInfo, type ReactNode } from "react";
import type { ErrorBus, ProblemEnvelope } from "./types.ts";
import { errorBus as defaultBus } from "./errorBus.ts";

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

const PROBLEM_TYPE_URI = "https://hoopoe.io/problems/renderer.crash";

function buildEnvelope(error: Error): ProblemEnvelope {
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
        envelope: buildEnvelope(error),
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

    return (
      <div
        className="hh-error-boundary-root"
        data-testid="error-boundary-root"
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
              data-testid="error-boundary-detail"
            >
              <code>{error.name}: {error.message}</code>
              {error.stack ? `\n\n${error.stack}` : ""}
              {componentStack ? `\n\nComponent stack:${componentStack}` : ""}
            </pre>
          ) : null}
          <div className="hh-error-boundary-actions">
            {dev ? (
              <button
                type="button"
                className="hh-error-boundary-secondary"
                data-testid="error-boundary-reset"
                onClick={this.handleReset}
              >
                Try again
              </button>
            ) : null}
            <button
              type="button"
              className="hh-error-boundary-primary"
              data-testid="error-boundary-reload"
              onClick={this.handleReload}
            >
              Reload window
            </button>
          </div>
        </div>
      </div>
    );
  }
}
