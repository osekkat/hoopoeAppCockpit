// hp-je1p extracted the redactor primitive into `apps/desktop/src/shared/
// redact/`. This file remains as a thin shim so existing call sites
// (notably hp-lxs tests + the in-process logger redaction path) keep
// compiling without source changes.

import type { LogEntry } from "./types.ts";
import {
  Redactor,
  SurfaceLogger,
  type RedactValue,
  type TraceEvent,
} from "../redact/index.ts";

export { Redactor };
/** Re-exported for hp-lxs back-compat. */
export type RedactionEvent = TraceEvent;

/** redactLogEntry: scrubs the given LogEntry's msg + every value in
 *  fields. Returns the redacted entry + the events that fired. Envelope
 *  columns (component / jobId / ...) are NOT redacted. */
export function redactLogEntry(
  redactor: Redactor,
  entry: LogEntry,
): { entry: LogEntry; events: TraceEvent[] } {
  const events: TraceEvent[] = [];
  let msg = entry.msg;
  if (msg) {
    const { redacted, events: msgEvents } = redactor.redactText(SurfaceLogger, "msg", msg);
    msg = redacted;
    if (msgEvents.length) events.push(...msgEvents);
  }
  let fields = entry.fields;
  if (fields) {
    const out: Record<string, RedactValue> = {};
    for (const [k, v] of Object.entries(fields)) {
      const { redacted, events: fieldEvents } = redactor.redactValue(
        SurfaceLogger,
        `fields.${k}`,
        v as RedactValue,
      );
      out[k] = redacted as RedactValue;
      if (fieldEvents.length) events.push(...fieldEvents);
    }
    fields = out as LogEntry["fields"];
  }
  const next: LogEntry = { ...entry, msg };
  if (fields !== undefined) {
    Object.assign(next, { fields });
  }
  return { entry: next, events };
}
