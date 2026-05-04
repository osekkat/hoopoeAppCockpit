import { errorBus as defaultBus } from "./errorBus.ts";
import { coerceToError } from "./renderErrorFallback.tsx";
import type { ErrorBus, ProblemEnvelope } from "./types.ts";

export type RendererWindowErrorKind = "unhandled-rejection" | "window-error";

export type RendererWindowErrorTarget = Pick<
  EventTarget,
  "addEventListener" | "removeEventListener"
>;

const WINDOW_ERROR_TYPE_URI = "https://hoopoe.io/problems/renderer.window-error";
const UNHANDLED_REJECTION_TYPE_URI =
  "https://hoopoe.io/problems/renderer.unhandled-rejection";

export interface InstallRendererWindowErrorHandlersOptions {
  readonly bus?: ErrorBus;
  readonly target?: RendererWindowErrorTarget | null;
}

export function buildRendererWindowErrorEnvelope(
  kind: RendererWindowErrorKind,
  error: Error,
): ProblemEnvelope {
  const unhandledRejection = kind === "unhandled-rejection";
  return {
    type: unhandledRejection ? UNHANDLED_REJECTION_TYPE_URI : WINDOW_ERROR_TYPE_URI,
    title: unhandledRejection ? "Background task failed" : "Renderer task failed",
    status: 500,
    surface: "toast",
    actionability: "manual",
    user_message: unhandledRejection
      ? "A background task failed. Check Diagnostics if the problem repeats."
      : "A renderer task failed. Check Diagnostics if the problem repeats.",
    detail: error.message,
  };
}

export function publishRendererWindowError(
  kind: RendererWindowErrorKind,
  reason: unknown,
  bus: ErrorBus = defaultBus,
): void {
  try {
    bus.publish({
      source: sourceFor(kind),
      severity: "error",
      envelope: buildRendererWindowErrorEnvelope(kind, coerceToError(reason)),
    });
  } catch {
    // Global error listeners must never throw while reporting a failure.
  }
}

export function installRendererWindowErrorHandlers(
  options: InstallRendererWindowErrorHandlersOptions = {},
): () => void {
  const bus = options.bus ?? defaultBus;
  const target =
    options.target ?? (typeof window === "undefined" ? null : window);
  if (!target) return () => {};

  const onUnhandledRejection = (event: Event) => {
    publishRendererWindowError(
      "unhandled-rejection",
      (event as PromiseRejectionEvent).reason,
      bus,
    );
  };

  const onWindowError = (event: Event) => {
    const errorEvent = event as ErrorEvent;
    publishRendererWindowError(
      "window-error",
      errorEvent.error ?? errorEvent.message,
      bus,
    );
  };

  target.addEventListener("unhandledrejection", onUnhandledRejection);
  target.addEventListener("error", onWindowError);

  return () => {
    target.removeEventListener("unhandledrejection", onUnhandledRejection);
    target.removeEventListener("error", onWindowError);
  };
}

function sourceFor(kind: RendererWindowErrorKind): string {
  return kind === "unhandled-rejection"
    ? "renderer.unhandled-rejection"
    : "renderer.window-error";
}
