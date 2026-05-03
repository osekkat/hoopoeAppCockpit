// Public entry point for the shared logger. Renderer + main both import
// from this file; nothing should reach into internal modules directly.

export type {
  Actor,
  ActorKind,
  EnvelopeField,
  Level,
  LogEntry,
  LogValue,
  LogValuePrimitive,
} from "./types.ts";
export { Component, ENVELOPE_FIELDS, LEVEL_RANK, levelRank } from "./types.ts";

export { Redactor } from "./redactor.ts";
export type { RedactionEvent } from "./redactor.ts";

export { createLogger, NOOP_LOGGER } from "./logger.ts";
export type { Logger, LoggerOptions, ScopeFields, Transport } from "./logger.ts";

export {
  CaptureTransport,
  NullTransport,
  RendererConsoleTransport,
  StreamTransport,
} from "./transport.ts";
export type { LineSink } from "./transport.ts";
