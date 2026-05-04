// hp-pmw — plan-job usage model tests.

import { expect, test } from "bun:test";
import {
  activeLineText,
  elapsedMs,
  estimateRemainingMs,
  formatElapsed,
  formatQuotaPercent,
  providerLatencyMs,
  quotaSeverity,
  rankUsageRows,
  type PlanJobMeasurement,
  type PlanJobSnapshot,
  type SubscriptionWindowUsage,
} from "./plan-job-usage-model.ts";

const FIXED_NOW = () => new Date("2026-05-04T12:10:00.000Z");

function measurement(input: Partial<PlanJobMeasurement> & Pick<PlanJobMeasurement, "callId">): PlanJobMeasurement {
  return {
    providerId: input.providerId ?? "claude_max",
    harness: input.harness ?? "claude_code",
    caamAccount: input.caamAccount ?? null,
    latencyMs: input.latencyMs ?? 5_000,
    inputHash: input.inputHash ?? "input-hash",
    artifactHash: input.artifactHash ?? "artifact-hash",
    completedAt: input.completedAt ?? "2026-05-04T12:00:30.000Z",
    ...input,
  };
}

function snapshot(input: Partial<PlanJobSnapshot> & Pick<PlanJobSnapshot, "jobId">): PlanJobSnapshot {
  return {
    status: input.status ?? "running",
    startedAt: input.startedAt ?? "2026-05-04T12:00:00.000Z",
    lastActivityAt: input.lastActivityAt ?? "2026-05-04T12:01:00.000Z",
    activeProviderId: input.activeProviderId ?? "claude_max",
    activeCaamAccount: input.activeCaamAccount ?? null,
    measurements: input.measurements ?? [],
    ...input,
  };
}

test("formatElapsed: ms / seconds / minutes / hours scales", () => {
  expect(formatElapsed(0)).toBe("0ms");
  expect(formatElapsed(500)).toBe("500ms");
  expect(formatElapsed(1_000)).toBe("1s");
  expect(formatElapsed(45_000)).toBe("45s");
  expect(formatElapsed(60_000)).toBe("1m");
  expect(formatElapsed(75_000)).toBe("1m 15s");
  expect(formatElapsed(60 * 60 * 1_000)).toBe("1h");
  expect(formatElapsed(75 * 60 * 1_000)).toBe("1h 15m");
});

test("formatElapsed: invalid input renders as em dash", () => {
  expect(formatElapsed(-1)).toBe("—");
  expect(formatElapsed(NaN)).toBe("—");
  expect(formatElapsed(Infinity)).toBe("—");
});

test("elapsedMs: running jobs use now() - startedAt", () => {
  const snap = snapshot({ jobId: "j1", status: "running", startedAt: "2026-05-04T12:00:00.000Z" });
  expect(elapsedMs(snap, FIXED_NOW)).toBe(10 * 60 * 1_000);
});

test("elapsedMs: terminal jobs use lastActivityAt - startedAt (clamped non-negative)", () => {
  const snap = snapshot({
    jobId: "j",
    status: "completed",
    startedAt: "2026-05-04T12:00:00.000Z",
    lastActivityAt: "2026-05-04T12:05:00.000Z",
  });
  expect(elapsedMs(snap, FIXED_NOW)).toBe(5 * 60 * 1_000);
});

test("elapsedMs: queued + missing startedAt → 0", () => {
  const queued = snapshot({ jobId: "q", status: "queued", startedAt: null });
  expect(elapsedMs(queued, FIXED_NOW)).toBe(0);
  const completedNoStart = snapshot({ jobId: "c", status: "completed", startedAt: null });
  expect(elapsedMs(completedNoStart, FIXED_NOW)).toBe(0);
});

test("estimateRemainingMs: returns null for non-running snapshots", () => {
  const completed = snapshot({ jobId: "c", status: "completed" });
  expect(estimateRemainingMs(completed, 4)).toBeNull();
});

test("estimateRemainingMs: zero remaining calls → 0ms / extrapolation source", () => {
  const snap = snapshot({
    jobId: "s",
    status: "running",
    measurements: [measurement({ callId: "1" }), measurement({ callId: "2" })],
  });
  const result = estimateRemainingMs(snap, 2);
  expect(result?.ms).toBe(0);
  expect(result?.source).toBe("extrapolation");
});

test("estimateRemainingMs: zero completed → fallback (60s per call)", () => {
  const snap = snapshot({ jobId: "s", measurements: [] });
  const result = estimateRemainingMs(snap, 4);
  expect(result?.ms).toBe(4 * 60_000);
  expect(result?.source).toBe("fallback");
  expect(result?.confidence).toBe("low");
});

test("estimateRemainingMs: single call → uses that latency, low confidence", () => {
  const snap = snapshot({
    jobId: "s",
    measurements: [measurement({ callId: "1", latencyMs: 30_000 })],
  });
  const result = estimateRemainingMs(snap, 5);
  expect(result?.ms).toBe(4 * 30_000);
  expect(result?.source).toBe("single_call");
  expect(result?.confidence).toBe("low");
});

test("estimateRemainingMs: 3+ calls → average extrapolation, medium confidence", () => {
  const snap = snapshot({
    jobId: "s",
    measurements: [
      measurement({ callId: "1", latencyMs: 10_000 }),
      measurement({ callId: "2", latencyMs: 20_000 }),
      measurement({ callId: "3", latencyMs: 30_000 }),
    ],
  });
  const result = estimateRemainingMs(snap, 5);
  // avg latency = 20s → 2 remaining * 20s = 40s.
  expect(result?.ms).toBe(40_000);
  expect(result?.source).toBe("extrapolation");
  expect(result?.confidence).toBe("medium");
});

test("estimateRemainingMs: caps the moving window at 50 most recent calls", () => {
  // 100 cheap fixture calls (5ms) followed by 50 slow calls (40s); the
  // estimate should reflect the slow calls, not get diluted by the long
  // tail of cheap fixtures.
  const cheap = Array.from({ length: 100 }, (_, i) =>
    measurement({ callId: `cheap-${i}`, latencyMs: 5 }),
  );
  const slow = Array.from({ length: 50 }, (_, i) =>
    measurement({ callId: `slow-${i}`, latencyMs: 40_000 }),
  );
  const snap = snapshot({ jobId: "s", measurements: [...cheap, ...slow] });
  const result = estimateRemainingMs(snap, 200);
  // 50 remaining; avg over the most recent 50 calls = 40000ms.
  expect(result?.ms).toBe(50 * 40_000);
});

test("providerLatencyMs: sums per-provider only", () => {
  const snap = snapshot({
    jobId: "s",
    measurements: [
      measurement({ callId: "a", providerId: "claude_max", latencyMs: 1_000 }),
      measurement({ callId: "b", providerId: "claude_max", latencyMs: 2_000 }),
      measurement({ callId: "c", providerId: "gemini_ultra", latencyMs: 4_000 }),
    ],
  });
  expect(providerLatencyMs(snap, "claude_max")).toBe(3_000);
  expect(providerLatencyMs(snap, "gemini_ultra")).toBe(4_000);
  expect(providerLatencyMs(snap, "gpt_pro")).toBe(0);
});

test("formatQuotaPercent: null + invalid → 'unmeasured' per §7.1", () => {
  expect(formatQuotaPercent(null)).toBe("unmeasured");
  expect(formatQuotaPercent(NaN)).toBe("unmeasured");
  expect(formatQuotaPercent(-5)).toBe("unmeasured");
});

test("formatQuotaPercent: valid values render as N% (clamped at 100)", () => {
  expect(formatQuotaPercent(0)).toBe("0%");
  expect(formatQuotaPercent(42)).toBe("42%");
  expect(formatQuotaPercent(99.4)).toBe("99%");
  expect(formatQuotaPercent(99.5)).toBe("100%");
  expect(formatQuotaPercent(110)).toBe("100%"); // clamped
});

test("quotaSeverity: rate-limited always wins", () => {
  const usage: SubscriptionWindowUsage = {
    providerId: "claude_max",
    label: "Claude",
    usagePercent: 5,
    resetsAt: null,
    rateLimited: true,
  };
  expect(quotaSeverity(usage)).toBe("danger");
});

test("quotaSeverity: thresholds at 70 (warning) and 90 (danger)", () => {
  const make = (pct: number | null): SubscriptionWindowUsage => ({
    providerId: "claude_max",
    label: "Claude",
    usagePercent: pct,
    resetsAt: null,
    rateLimited: false,
  });
  expect(quotaSeverity(make(0))).toBe("ok");
  expect(quotaSeverity(make(69))).toBe("ok");
  expect(quotaSeverity(make(70))).toBe("warning");
  expect(quotaSeverity(make(89))).toBe("warning");
  expect(quotaSeverity(make(90))).toBe("danger");
  expect(quotaSeverity(make(100))).toBe("danger");
  expect(quotaSeverity(make(null))).toBe("unmeasured");
});

test("rankUsageRows: active provider sorts first, others by usage desc", () => {
  const usages: SubscriptionWindowUsage[] = [
    { providerId: "claude_max", label: "Claude", usagePercent: 30, resetsAt: null, rateLimited: false },
    { providerId: "gpt_pro", label: "GPT", usagePercent: 80, resetsAt: null, rateLimited: false },
    { providerId: "gemini_ultra", label: "Gemini", usagePercent: 50, resetsAt: null, rateLimited: false },
  ];
  const ranked = rankUsageRows(usages, "claude_max");
  expect(ranked[0]?.providerId).toBe("claude_max"); // active first
  expect(ranked[1]?.providerId).toBe("gpt_pro");    // higher usage next
  expect(ranked[2]?.providerId).toBe("gemini_ultra");
});

test("rankUsageRows: null active → pure usage desc; unmeasured (null pct) sorts last", () => {
  const usages: SubscriptionWindowUsage[] = [
    { providerId: "claude_max", label: "Claude", usagePercent: 30, resetsAt: null, rateLimited: false },
    { providerId: "gpt_pro", label: "GPT", usagePercent: null, resetsAt: null, rateLimited: false },
    { providerId: "gemini_ultra", label: "Gemini", usagePercent: 50, resetsAt: null, rateLimited: false },
  ];
  const ranked = rankUsageRows(usages, null);
  expect(ranked[0]?.providerId).toBe("gemini_ultra");
  expect(ranked[1]?.providerId).toBe("claude_max");
  expect(ranked[2]?.providerId).toBe("gpt_pro");
});

test("activeLineText: running snapshot composes 'Active: ... — Nm elapsed, ~Nm remaining'", () => {
  const snap = snapshot({
    jobId: "j",
    status: "running",
    startedAt: "2026-05-04T12:00:00.000Z",
    activeProviderId: "claude_max",
    activeCaamAccount: "claude-max.alpha",
    measurements: [
      measurement({ callId: "1", providerId: "claude_max", latencyMs: 30_000, harness: "claude_code" }),
    ],
  });
  const text = activeLineText(snap, 4, FIXED_NOW);
  expect(text).toContain("Active: claude_max");
  expect(text).toContain("via Claude Code");
  expect(text).toContain("(claude-max.alpha)");
  expect(text).toContain("10m");
  // Single-call extrapolation: 3 remaining * 30s = 90s = 1m 30s.
  expect(text).toContain("1m 30s remaining");
});

test("activeLineText: null for non-running snapshots", () => {
  expect(activeLineText(snapshot({ jobId: "j", status: "completed" }), 4)).toBeNull();
  expect(activeLineText(snapshot({ jobId: "j", status: "queued", startedAt: null, activeProviderId: null }), 4)).toBeNull();
});

test("activeLineText: omits remaining estimate when expectedTotalCalls=0", () => {
  const snap = snapshot({ jobId: "j", measurements: [], activeProviderId: "claude_max" });
  const text = activeLineText(snap, 0, FIXED_NOW);
  expect(text).toContain("elapsed");
  expect(text).not.toContain("remaining");
});
