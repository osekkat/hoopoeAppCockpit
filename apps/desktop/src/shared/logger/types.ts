// Hoopoe-owned. Renderer + main shared structured logger. Same envelope,
// same redaction patterns, and same level set as
// `apps/daemon/internal/logger/`. The Go and TS implementations are
// intentionally byte-identical at the JSON envelope level — audit-log
// replay tools must consume desktop and daemon logs unchanged.
//
// hp-lxs deliverable. See `docs/observability.md` for the authoritative
// reference.

/** Log severity, sorted ascending. Total order via `levelRank`. */
export type Level = "trace" | "debug" | "info" | "warn" | "error" | "fatal";

export const LEVEL_RANK: Readonly<Record<Level, number>> = {
  trace: 0,
  debug: 1,
  info: 2,
  warn: 3,
  error: 4,
  fatal: 5,
};

export function levelRank(level: Level): number {
  return LEVEL_RANK[level];
}

/** Origin of a log entry. */
export type ActorKind = "user" | "agent" | "tending_job" | "pre_script" | "system";

export interface Actor {
  readonly kind: ActorKind;
  readonly id: string;
}

export type LogValuePrimitive = string | number | boolean | null;
export type LogValue =
  | LogValuePrimitive
  | LogValue[]
  | { readonly [key: string]: LogValue };

/** The canonical envelope shared with the daemon (`apps/daemon/internal/
 *  logger/types.go::Entry`). JSON field names are the wire contract. */
export interface LogEntry {
  readonly ts: string; // RFC3339 (UTC)
  readonly level: Level;
  readonly msg: string;
  readonly component: string;
  readonly subsystem?: string;
  readonly correlationId?: string;
  readonly causationId?: string;
  readonly actor?: Actor;
  readonly jobId?: string;
  readonly beadId?: string;
  readonly swarmId?: string;
  readonly planId?: string;
  readonly runId?: string;
  readonly fields?: Readonly<Record<string, LogValue>>;
}

/** Field names that map to dedicated envelope columns rather than
 *  `entry.fields[*]`. Mirrors the Go logger's switch statement in
 *  `applyField`. */
export const ENVELOPE_FIELDS = [
  "component",
  "subsystem",
  "correlationId",
  "causationId",
  "actor",
  "jobId",
  "beadId",
  "swarmId",
  "planId",
  "runId",
] as const;

export type EnvelopeField = (typeof ENVELOPE_FIELDS)[number];

/** Common component values. Other subsystems pass arbitrary strings. */
export const Component = {
  DesktopMain: "desktop.main",
  DesktopRenderer: "desktop.renderer",
  DesktopMainAuth: "desktop.main.auth",
  DesktopMainSettings: "desktop.main.settings",
  DesktopMainIPC: "desktop.main.ipc",
  DesktopRendererStage: "desktop.renderer.stage",
  TestRunner: "test.runner",
} as const;
