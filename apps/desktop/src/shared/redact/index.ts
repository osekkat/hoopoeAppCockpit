// Public entry point for the desktop redaction layer. Mirrors the daemon
// package at `apps/daemon/internal/redaction/`. See docs/observability.md.

export {
  Redactor,
  SurfaceAudit,
  SurfaceEvents,
  SurfaceLogger,
} from "./redact.ts";
export type {
  PatternStat,
  RedactValue,
  StatsSnapshot,
  Surface,
  TraceEvent,
} from "./redact.ts";
