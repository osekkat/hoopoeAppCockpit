// Hoopoe-owned. Transport implementations for the desktop logger.

import type { LogEntry } from "./types.ts";
import type { Transport } from "./logger.ts";

/** Drops every entry. Used by the no-op logger. */
export class NullTransport implements Transport {
  emit(_: LogEntry): void {}
}

/** Buffers a fixed-size ring of entries in memory. Tests use it to grab
 *  the last N entries on assertion failure. Mirrors the Go logger's
 *  CaptureTransport. */
export class CaptureTransport implements Transport {
  private readonly cap: number;
  private readonly buf: LogEntry[];
  private head = 0;
  private full = false;

  constructor(capacity = 200) {
    this.cap = capacity > 0 ? capacity : 200;
    this.buf = new Array<LogEntry>(this.cap);
  }

  emit(entry: LogEntry): void {
    this.buf[this.head] = entry;
    this.head = (this.head + 1) % this.cap;
    if (this.head === 0) this.full = true;
  }

  /** Snapshot in chronological order (oldest first). */
  entries(): readonly LogEntry[] {
    if (!this.full) return this.buf.slice(0, this.head);
    return [...this.buf.slice(this.head), ...this.buf.slice(0, this.head)];
  }

  len(): number {
    return this.full ? this.cap : this.head;
  }

  reset(): void {
    this.head = 0;
    this.full = false;
  }

  jsonLines(): string {
    return this.entries().map((e) => JSON.stringify(e)).join("\n");
  }
}

/** Emits one JSON line per entry to a stream-like sink. The sink is
 *  typically `process.stdout` / `process.stderr` (main process) or a
 *  `console`-backed wrapper in the renderer. */
export interface LineSink {
  write(line: string): void;
}

export class StreamTransport implements Transport {
  readonly sink: LineSink;

  constructor(sink: LineSink) {
    this.sink = sink;
  }

  emit(entry: LogEntry): void {
    this.sink.write(`${JSON.stringify(entry)}\n`);
  }
}

/** Renderer-friendly Transport that prints via `console.<level>` so DevTools
 *  retains the colored levels. The renderer sees redacted entries already;
 *  DevTools console is acceptable for dev. */
export class RendererConsoleTransport implements Transport {
  emit(entry: LogEntry): void {
    const line = JSON.stringify(entry);
    switch (entry.level) {
      case "trace":
      case "debug":
        // eslint-disable-next-line no-console
        console.debug(line);
        break;
      case "info":
        // eslint-disable-next-line no-console
        console.info(line);
        break;
      case "warn":
        // eslint-disable-next-line no-console
        console.warn(line);
        break;
      case "error":
      case "fatal":
        // eslint-disable-next-line no-console
        console.error(line);
        break;
    }
  }
}
