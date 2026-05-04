// `apps/desktop/src/renderer/error-ux` — public API for hp-8dym.

export { errorBus, createErrorBus, COALESCE_WINDOW_MS, MAX_VISIBLE_BANNERS, MAX_VISIBLE_TOASTS } from "./errorBus.ts";
export {
  ariaLiveFor,
  defaultActionLabel,
  defaultAutoDismissMs,
  defaultDismissible,
  deriveSeverity,
  deriveSurface,
  isBannerSurface,
  isInlinePillSurface,
  isModalSurface,
  isToastSurface,
} from "./classification.ts";
export {
  buildTelemetryEvent,
  InMemoryTelemetrySink,
  recordTelemetry,
  TELEMETRY_ALLOWED_KEYS,
  type TelemetryConfig,
  type TelemetrySink,
} from "./telemetry.ts";
export { ErrorUxRoot } from "./ErrorUxRoot.tsx";
export { ToastStack } from "./surfaces/Toast.tsx";
export { BannerStack } from "./surfaces/Banner.tsx";
export { InlinePill } from "./surfaces/InlinePill.tsx";
export { BlockingModal } from "./surfaces/BlockingModal.tsx";
export type {
  ErrorBus,
  ErrorBusListener,
  ErrorBusSnapshot,
  ErrorPayload,
  ErrorPayloadHints,
  ErrorSeverity,
  ErrorTelemetryEvent,
  ProblemActionability,
  ProblemEnvelope,
  ProblemSurface,
  PublishedError,
} from "./types.ts";
