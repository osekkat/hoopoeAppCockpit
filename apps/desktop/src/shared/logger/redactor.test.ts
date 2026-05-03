// The canonical Redactor tests live alongside the package at
// `apps/desktop/src/shared/redact/redact.test.ts` (per hp-je1p). The tests
// below cover the logger-side integration: redactLogEntry handles the
// LogEntry envelope correctly and back-compat re-exports work.

import { describe, expect, test } from "bun:test";
import { Redactor, redactLogEntry } from "./redactor.ts";
import type { LogEntry } from "./types.ts";

describe("logger.Redactor (re-export from shared/redact)", () => {
  test("re-export still scrubs known patterns via redactText", () => {
    const r = new Redactor();
    const { redacted, events } = r.redactText("logger", "msg", "token=sk-abcdef0123456789ABCDEF0123456789");
    expect(redacted).not.toContain("sk-abcdef");
    expect(events.length).toBeGreaterThan(0);
  });
});

describe("redactLogEntry", () => {
  test("scrubs msg + fields, leaves envelope columns", () => {
    const r = new Redactor();
    const input: LogEntry = {
      ts: "2026-05-04T00:00:00Z",
      level: "info",
      msg: "calling with sk-abcdef0123456789ABCDEF0123456789",
      component: "desktop.main",
      jobId: "job-1",
      fields: {
        argv: ["--token", "ghp_abcdefghijklmnopqrstuvwxyz0123456789"],
        benign: "just a value",
      },
    };
    const { entry, events } = redactLogEntry(r, input);
    expect(entry.msg).not.toContain("sk-abcdef");
    expect(entry.component).toBe("desktop.main");
    expect(entry.jobId).toBe("job-1");
    const argv = entry.fields?.argv as string[];
    expect(argv[1]).not.toContain("ghp_");
    expect(entry.fields?.benign).toBe("just a value");
    expect(events.length).toBeGreaterThanOrEqual(2);
    // All events stamped as logger surface.
    for (const e of events) {
      expect(e.redactor).toBe("logger");
    }
  });

  test("empty entry passes through unchanged", () => {
    const r = new Redactor();
    const input: LogEntry = {
      ts: "x",
      level: "info",
      msg: "",
      component: "test",
    };
    const { entry, events } = redactLogEntry(r, input);
    expect(entry).toEqual(input);
    expect(events).toEqual([]);
  });

  test("does not mutate input entry", () => {
    const r = new Redactor();
    const input: LogEntry = {
      ts: "x",
      level: "info",
      msg: "leak sk-abcdef0123456789ABCDEF0123456789",
      component: "test",
    };
    const before = { ...input };
    redactLogEntry(r, input);
    expect(input.msg).toBe(before.msg);
  });

  test("field context paths propagate", () => {
    const r = new Redactor();
    const input: LogEntry = {
      ts: "x",
      level: "info",
      msg: "ok",
      component: "test",
      fields: {
        token: "sk-abcdef0123456789ABCDEF0123456789",
      },
    };
    const { events } = redactLogEntry(r, input);
    expect(events[0].context).toBe("fields.token");
  });
});
