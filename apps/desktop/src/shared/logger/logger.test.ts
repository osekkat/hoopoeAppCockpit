import { describe, expect, test } from "bun:test";
import {
  CaptureTransport,
  Component,
  createLogger,
  RendererConsoleTransport,
  StreamTransport,
} from "./index.ts";
import type { LogEntry } from "./index.ts";

const FIXED_NOW = () => new Date("2026-05-04T00:00:00Z");

describe("createLogger", () => {
  test("emits a canonical envelope", () => {
    const cap = new CaptureTransport();
    const log = createLogger({
      component: Component.DesktopMain,
      minLevel: "debug",
      now: FIXED_NOW,
      transports: [cap],
    });
    log.info("ping", { path: "/v1/health", status: 200 });
    const entries = cap.entries();
    expect(entries.length).toBe(1);
    const e = entries[0];
    expect(e.level).toBe("info");
    expect(e.msg).toBe("ping");
    expect(e.component).toBe(Component.DesktopMain);
    expect(e.fields?.path).toBe("/v1/health");
    expect(e.fields?.status).toBe(200);
    expect(e.ts).toBe("2026-05-04T00:00:00.000Z");
  });

  test("filters by minLevel", () => {
    const cap = new CaptureTransport();
    const log = createLogger({
      component: Component.DesktopMain,
      minLevel: "warn",
      transports: [cap],
    });
    log.trace("t");
    log.debug("d");
    log.info("i");
    log.warn("w");
    log.error("e");
    expect(cap.entries().length).toBe(2);
    expect(cap.entries()[0].level).toBe("warn");
    expect(cap.entries()[1].level).toBe("error");
  });

  test("with() scopes envelope columns", () => {
    const cap = new CaptureTransport();
    const root = createLogger({
      component: Component.DesktopMain,
      minLevel: "debug",
      transports: [cap],
    });
    const scoped = root.with({
      correlationId: "corr-1",
      jobId: "job-42",
      actor: { kind: "system", id: "scheduler" },
      foo: "bar", // non-envelope → fields.foo
    });
    scoped.info("hello");
    const e = cap.entries()[0];
    expect(e.correlationId).toBe("corr-1");
    expect(e.jobId).toBe("job-42");
    expect(e.actor?.kind).toBe("system");
    expect(e.fields?.foo).toBe("bar");
  });

  test("with() does not mutate parent", () => {
    const cap = new CaptureTransport();
    const root = createLogger({
      component: Component.DesktopMain,
      minLevel: "debug",
      transports: [cap],
    });
    const child = root.with({ correlationId: "child-only" });
    root.info("from-root");
    child.info("from-child");
    const entries = cap.entries();
    expect(entries[0].correlationId).toBeUndefined();
    expect(entries[1].correlationId).toBe("child-only");
  });

  test("redacts before buffering", () => {
    const cap = new CaptureTransport();
    const log = createLogger({
      component: Component.DesktopMain,
      minLevel: "debug",
      transports: [cap],
    });
    log.info("token=sk-abcdef0123456789ABCDEF0123456789", {
      argv: ["--key", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"],
    });
    const entry = cap.entries()[0];
    expect(entry.msg).not.toContain("sk-abcdef");
    const argv = entry.fields?.argv as string[];
    expect(argv[1]).not.toContain("ghp_");
  });

  test("call-site fields skip envelope keys (must use .with())", () => {
    const cap = new CaptureTransport();
    const log = createLogger({
      component: Component.DesktopMain,
      minLevel: "debug",
      transports: [cap],
    });
    log.info("sneaky", { jobId: "should-be-ignored", legit: "ok" });
    const e = cap.entries()[0];
    expect(e.jobId).toBeUndefined();
    expect(e.fields?.legit).toBe("ok");
    expect(e.fields?.jobId).toBeUndefined();
  });

  test("at least one transport is required", () => {
    expect(() =>
      createLogger({ component: Component.DesktopMain, transports: [] }),
    ).toThrow();
  });
});

describe("CaptureTransport", () => {
  test("ring-buffers when capacity exceeded", () => {
    const cap = new CaptureTransport(3);
    for (let i = 0; i < 5; i++) {
      cap.emit({
        ts: "2026-05-04T00:00:00Z",
        level: "info",
        msg: `m${i}`,
        component: "test",
      });
    }
    const got = cap.entries();
    expect(got.length).toBe(3);
    expect(got.map((e) => e.msg)).toEqual(["m2", "m3", "m4"]);
  });

  test("len + reset", () => {
    const cap = new CaptureTransport(2);
    cap.emit({ ts: "x", level: "info", msg: "a", component: "t" });
    expect(cap.len()).toBe(1);
    cap.reset();
    expect(cap.len()).toBe(0);
  });

  test("jsonLines emits parseable NDJSON", () => {
    const cap = new CaptureTransport(5);
    cap.emit({ ts: "2026-05-04T00:00:00Z", level: "info", msg: "hello", component: "test" });
    const out = cap.jsonLines();
    const decoded = JSON.parse(out) as LogEntry;
    expect(decoded.msg).toBe("hello");
  });
});

describe("StreamTransport", () => {
  test("writes one JSON line per entry", () => {
    const sink = { lines: [] as string[], write(line: string) { this.lines.push(line); } };
    const transport = new StreamTransport(sink);
    transport.emit({
      ts: "2026-05-04T00:00:00Z",
      level: "info",
      msg: "hi",
      component: "test",
    });
    expect(sink.lines.length).toBe(1);
    expect(sink.lines[0].endsWith("\n")).toBe(true);
    const decoded = JSON.parse(sink.lines[0]);
    expect(decoded.msg).toBe("hi");
  });
});

describe("RendererConsoleTransport", () => {
  test("dispatches to console by level", () => {
    const calls: Array<[string, string]> = [];
    const orig = {
      debug: console.debug,
      info: console.info,
      warn: console.warn,
      error: console.error,
    };
    console.debug = (line: string) => calls.push(["debug", line]);
    console.info = (line: string) => calls.push(["info", line]);
    console.warn = (line: string) => calls.push(["warn", line]);
    console.error = (line: string) => calls.push(["error", line]);
    try {
      const t = new RendererConsoleTransport();
      t.emit({ ts: "x", level: "trace", msg: "t", component: "c" });
      t.emit({ ts: "x", level: "info", msg: "i", component: "c" });
      t.emit({ ts: "x", level: "warn", msg: "w", component: "c" });
      t.emit({ ts: "x", level: "fatal", msg: "f", component: "c" });
    } finally {
      console.debug = orig.debug;
      console.info = orig.info;
      console.warn = orig.warn;
      console.error = orig.error;
    }
    expect(calls.map(([k]) => k)).toEqual(["debug", "info", "warn", "error"]);
  });
});

describe("envelope shape parity (with daemon Go logger)", () => {
  test("required fields match the Go contract", () => {
    const cap = new CaptureTransport();
    const log = createLogger({
      component: "test.parity",
      minLevel: "debug",
      now: FIXED_NOW,
      transports: [cap],
    });
    log.info("entry");
    const e = cap.entries()[0];
    // Required fields in JSON: ts, level, msg, component.
    const json = JSON.parse(JSON.stringify(e)) as Record<string, unknown>;
    expect(typeof json.ts).toBe("string");
    expect(typeof json.level).toBe("string");
    expect(typeof json.msg).toBe("string");
    expect(typeof json.component).toBe("string");
  });

  test("optional fields are omitted when unset (matches Go omitempty)", () => {
    const cap = new CaptureTransport();
    const log = createLogger({
      component: "test.parity",
      minLevel: "debug",
      transports: [cap],
    });
    log.info("bare");
    const json = JSON.parse(JSON.stringify(cap.entries()[0]));
    for (const optional of [
      "subsystem",
      "correlationId",
      "causationId",
      "actor",
      "jobId",
      "beadId",
      "swarmId",
      "planId",
      "runId",
      "fields",
    ]) {
      expect(optional in json).toBe(false);
    }
  });
});
