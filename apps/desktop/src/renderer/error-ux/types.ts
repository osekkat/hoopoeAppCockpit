// Renderer error UX types (hp-8dym).
//
// The daemon delivers RFC 7807 problem+json envelopes
// (`@hoopoe/problem-types`); the renderer wraps them in `ErrorPayload`
// to add severity, source, hints, and an opaque id used for dismissal
// and coalescing.

import type {
  ProblemActionability,
  ProblemEnvelope,
  ProblemSurface,
} from "@hoopoe/problem-types";

export type ErrorSeverity = "info" | "warning" | "error" | "critical" | "blocking";

export type { ProblemActionability, ProblemEnvelope, ProblemSurface };

export interface ErrorPayloadHints {
  /** Label for the primary CTA (e.g., "Re-pair", "Open Diagnostics"). */
  readonly primaryActionLabel?: string;
  /** Route to navigate to when the primary CTA is clicked. */
  readonly primaryActionRoute?: string;
  /** Whether the user can dismiss this error from its surface. */
  readonly dismissible?: boolean;
  /** Banner-only: stay visible after route change. Default false. */
  readonly persistAcrossRoutes?: boolean;
  /** Toast-only: dismiss timeout in ms. Default 5000. Capped at 30000. */
  readonly autoDismissMs?: number;
  /** Inline-pill: feature-id this pill anchors to. */
  readonly anchorFeatureId?: string;
  /** Inline-pill: capability that drove the degraded state. */
  readonly capabilityId?: string;
}

export interface ErrorPayload {
  /** Caller-supplied; identifies the feature/screen that produced the error. */
  readonly source: string;
  /** Severity class — derived from the envelope when not provided. */
  readonly severity?: ErrorSeverity;
  /** Optional surface override; default routes from `envelope.surface`. */
  readonly surfaceOverride?: ProblemSurface;
  /** RFC 7807 envelope from the daemon (or constructed renderer-side). */
  readonly envelope: ProblemEnvelope;
  /** Hints for the renderer. Never logged via telemetry. */
  readonly hints?: ErrorPayloadHints;
  /** Opaque caller-supplied context — never logged via telemetry. */
  readonly context?: Readonly<Record<string, unknown>>;
}

export interface PublishedError extends ErrorPayload {
  /** Renderer-assigned id; stable for dismissal + coalescing. */
  readonly id: string;
  /** Resolved severity (after defaulting). */
  readonly severity: ErrorSeverity;
  /** Resolved surface (after defaulting). */
  readonly surface: ProblemSurface;
  /** ms-since-epoch when the bus first received this error. */
  readonly publishedAt: number;
  /** How many duplicates have been coalesced into this entry. */
  readonly coalescedCount: number;
}

export type ErrorBusListener = (
  errors: readonly PublishedError[],
) => void;

export interface ErrorBusSnapshot {
  readonly errors: readonly PublishedError[];
}

export interface ErrorBus {
  /** Publish an error; returns the assigned id. */
  publish(payload: ErrorPayload): string;
  /** Dismiss a single error by id. */
  dismiss(id: string): void;
  /** Dismiss every active error. */
  dismissAll(): void;
  /** Subscribe to all bus changes. Returns an unsubscribe function. */
  subscribe(listener: ErrorBusListener): () => void;
  /** Get the current bus state synchronously. */
  getSnapshot(): ErrorBusSnapshot;
  /** Test-only: reset internal state. */
  reset(): void;
}

/** Telemetry payload — opt-in only, redacted shape. */
export interface ErrorTelemetryEvent {
  readonly type: "error_surfaces.published";
  readonly ts: string;
  readonly severity: ErrorSeverity;
  readonly actionability: ProblemActionability;
  /** RFC 7807 type URI of the envelope. */
  readonly problemType: string;
  /** caller-supplied source string. */
  readonly source: string;
  /** Surface routed to. */
  readonly surface: ProblemSurface;
}
