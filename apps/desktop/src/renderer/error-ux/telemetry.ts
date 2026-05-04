// Opt-in error telemetry (hp-8dym).
//
// PII redaction is structural: `recordTelemetry` builds the payload
// from a strict allowlist (severity, actionability, problemType,
// source, surface). It NEVER reads `envelope.detail`,
// `envelope.instance`, `payload.context`, or any extension on the
// envelope. Tests assert that constructed events contain only
// allowlist keys — so future field drift in `PublishedError` cannot
// silently leak.

import type { ErrorTelemetryEvent, PublishedError } from "./types.ts";

export const TELEMETRY_ALLOWED_KEYS: ReadonlySet<string> = new Set([
  "type",
  "ts",
  "severity",
  "actionability",
  "problemType",
  "source",
  "surface",
]);

export interface TelemetryConfig {
  readonly enabled: boolean;
  readonly sink: TelemetrySink;
  readonly clock: () => Date;
}

export interface TelemetrySink {
  write(event: ErrorTelemetryEvent): void;
}

/** Default in-memory sink — unit tests assert against this. The real
 *  desktop binding will replace it with an IPC-backed sink that writes
 *  to `~/.hoopoe/telemetry.jsonl` from the main process (after
 *  consulting `desktopSettings.telemetryOptIn`). */
export class InMemoryTelemetrySink implements TelemetrySink {
  private readonly events: ErrorTelemetryEvent[] = [];

  write(event: ErrorTelemetryEvent): void {
    this.events.push(event);
  }

  drain(): readonly ErrorTelemetryEvent[] {
    return this.events.slice();
  }

  clear(): void {
    this.events.length = 0;
  }
}

export function buildTelemetryEvent(
  error: PublishedError,
  clock: () => Date = () => new Date(),
): ErrorTelemetryEvent {
  return {
    type: "error_surfaces.published",
    ts: clock().toISOString(),
    severity: error.severity,
    actionability: error.envelope.actionability,
    problemType: error.envelope.type,
    source: error.source,
    surface: error.surface,
  };
}

export function recordTelemetry(
  error: PublishedError,
  config: TelemetryConfig,
): ErrorTelemetryEvent | null {
  if (!config.enabled) return null;
  const event = buildTelemetryEvent(error, config.clock);
  config.sink.write(event);
  return event;
}
