// hp-pmw — PlanJobUsageSidebar render tests.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  PlanJobUsageSidebar,
  _formatRelativeFutureForTest,
} from "./PlanJobUsageSidebar.tsx";
import type {
  PlanJobMeasurement,
  PlanJobSnapshot,
  SubscriptionWindowUsage,
} from "./plan-job-usage-model.ts";

const FIXED_NOW = () => new Date("2026-05-04T12:10:00.000Z");

function measurement(input: Partial<PlanJobMeasurement> & Pick<PlanJobMeasurement, "callId">): PlanJobMeasurement {
  return {
    providerId: input.providerId ?? "claude_max",
    harness: input.harness ?? "claude_code",
    caamAccount: input.caamAccount ?? null,
    latencyMs: input.latencyMs ?? 5_000,
    inputHash: input.inputHash ?? "input",
    artifactHash: input.artifactHash ?? "artifact",
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

function usage(input: Partial<SubscriptionWindowUsage> & Pick<SubscriptionWindowUsage, "providerId">): SubscriptionWindowUsage {
  return {
    label: input.label ?? input.providerId,
    usagePercent: input.usagePercent ?? 0,
    resetsAt: input.resetsAt ?? null,
    rateLimited: input.rateLimited ?? false,
    ...input,
  };
}

test("PlanJobUsageSidebar: empty state when no snapshot", () => {
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar expectedTotalCalls={0} now={FIXED_NOW} snapshot={null} usages={[]} />,
  );
  expect(html).toContain("data-testid=\"plan-job-usage\"");
  expect(html).toContain("data-testid=\"plan-job-usage-empty\"");
  expect(html).toContain("No active plan job");
});

test("PlanJobUsageSidebar: running snapshot composes the active line + progress + ETA confidence", () => {
  const snap = snapshot({
    jobId: "j",
    status: "running",
    activeProviderId: "claude_max",
    activeCaamAccount: "claude.alpha",
    measurements: [
      measurement({ callId: "1", providerId: "claude_max", latencyMs: 30_000, harness: "claude_code" }),
      measurement({ callId: "2", providerId: "claude_max", latencyMs: 30_000, harness: "claude_code" }),
      measurement({ callId: "3", providerId: "claude_max", latencyMs: 30_000, harness: "claude_code" }),
    ],
  });
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar expectedTotalCalls={6} now={FIXED_NOW} snapshot={snap} usages={[]} />,
  );
  expect(html).toContain("data-testid=\"plan-job-usage-active\"");
  expect(html).toContain("data-testid=\"plan-job-usage-active-line\"");
  expect(html).toContain("Active: claude_max");
  expect(html).toContain("via Claude Code");
  expect(html).toContain("(claude.alpha)");
  expect(html).toContain("3 / 6 calls completed");
  // ETA confidence label.
  expect(html).toContain("data-testid=\"plan-job-usage-eta-confidence\"");
  expect(html).toContain("medium");
});

test("PlanJobUsageSidebar: terminal snapshots show status + total wall-clock", () => {
  const snap = snapshot({
    jobId: "j",
    status: "completed",
    startedAt: "2026-05-04T11:50:00.000Z",
    lastActivityAt: "2026-05-04T11:55:30.000Z",
    measurements: [measurement({ callId: "x" })],
  });
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar expectedTotalCalls={1} now={FIXED_NOW} snapshot={snap} usages={[]} />,
  );
  expect(html).toContain("data-status=\"completed\"");
  expect(html).toContain("Status: completed");
  expect(html).toContain("5m 30s total wall-clock");
  expect(html).toContain("1 call");
});

test("PlanJobUsageSidebar: provider list ranks active first then by usage desc", () => {
  const snap = snapshot({ jobId: "j", activeProviderId: "claude_max", measurements: [] });
  const usages = [
    usage({ providerId: "gpt_pro", label: "GPT", usagePercent: 80 }),
    usage({ providerId: "claude_max", label: "Claude", usagePercent: 30 }),
    usage({ providerId: "gemini_ultra", label: "Gemini", usagePercent: 50 }),
  ];
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar expectedTotalCalls={3} now={FIXED_NOW} snapshot={snap} usages={usages} />,
  );
  // Claude (active) should appear before GPT (next-highest), which appears before Gemini.
  const claudeAt = html.indexOf("data-testid=\"plan-job-usage-provider-claude_max\"");
  const gptAt = html.indexOf("data-testid=\"plan-job-usage-provider-gpt_pro\"");
  const geminiAt = html.indexOf("data-testid=\"plan-job-usage-provider-gemini_ultra\"");
  expect(claudeAt).toBeGreaterThan(0);
  expect(claudeAt).toBeLessThan(gptAt);
  expect(gptAt).toBeLessThan(geminiAt);
  // Active provider gets the active flag.
  expect(html).toMatch(/data-active="true"[^>]*data-testid="plan-job-usage-provider-claude_max"/);
});

test("PlanJobUsageSidebar: rate-limited provider renders danger severity + actionable note", () => {
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar
      expectedTotalCalls={3}
      now={FIXED_NOW}
      snapshot={null}
      usages={[
        usage({ providerId: "claude_max", label: "Claude", usagePercent: 5, rateLimited: true }),
      ]}
    />,
  );
  expect(html).toMatch(/data-severity="danger"[^>]*data-testid="plan-job-usage-provider-claude_max"/);
  expect(html).toContain("data-testid=\"plan-job-usage-provider-claude_max-rate\"");
  expect(html).toContain("Rate-limited");
  expect(html).toContain("switch CAAM account");
});

test("PlanJobUsageSidebar: 'unmeasured' state renders italic explainer (no fabrication)", () => {
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar
      expectedTotalCalls={3}
      now={FIXED_NOW}
      snapshot={null}
      usages={[
        usage({ providerId: "gemini_ultra", label: "Gemini", usagePercent: null, rateLimited: false }),
      ]}
    />,
  );
  expect(html).toContain("data-severity=\"unmeasured\"");
  expect(html).toContain("unmeasured");
  expect(html).toContain("caut isn&#x27;t reporting usage for this provider");
  // No bar meter rendered when percent is null (avoids fake fill).
  expect(html).not.toMatch(/data-testid="plan-job-usage-provider-gemini_ultra"[^>]*hh-plan-job-usage-bar/);
});

test("PlanJobUsageSidebar: 70% usage triggers warning severity", () => {
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar
      expectedTotalCalls={3}
      now={FIXED_NOW}
      snapshot={null}
      usages={[usage({ providerId: "claude_max", label: "Claude", usagePercent: 75 })]}
    />,
  );
  expect(html).toMatch(/data-severity="warning"[^>]*data-testid="plan-job-usage-provider-claude_max"/);
});

test("PlanJobUsageSidebar: window-reset note formats as 'in Nh' / 'in Nm'", () => {
  // Reset is 4 hours from FIXED_NOW.
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar
      expectedTotalCalls={3}
      now={FIXED_NOW}
      snapshot={null}
      usages={[
        usage({
          providerId: "claude_max",
          label: "Claude",
          usagePercent: 30,
          resetsAt: "2026-05-04T16:10:00.000Z",
        }),
      ]}
    />,
  );
  expect(html).toContain("Window resets");
  expect(html).toContain("in 4h");
});

test("formatRelativeFuture (helper): scales", () => {
  const now = () => new Date("2026-05-04T12:00:00.000Z");
  expect(_formatRelativeFutureForTest("2026-05-04T11:00:00.000Z", now)).toBe("now");
  expect(_formatRelativeFutureForTest("2026-05-04T12:00:30.000Z", now)).toBe("in <1m");
  expect(_formatRelativeFutureForTest("2026-05-04T12:30:00.000Z", now)).toBe("in 30m");
  expect(_formatRelativeFutureForTest("2026-05-04T15:00:00.000Z", now)).toBe("in 3h");
  expect(_formatRelativeFutureForTest("2026-05-06T12:00:00.000Z", now)).toBe("in 2d");
});

test("PlanJobUsageSidebar: header surfaces the §13 'no token dollars' framing", () => {
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar expectedTotalCalls={0} now={FIXED_NOW} snapshot={null} usages={[]} />,
  );
  // React escapes the apostrophe, so we match against the encoded form.
  expect(html).toContain("Hoopoe doesn&#x27;t bill token dollars");
});

test("PlanJobUsageSidebar: bar fill width matches the usage percent", () => {
  const html = renderToStaticMarkup(
    <PlanJobUsageSidebar
      expectedTotalCalls={0}
      now={FIXED_NOW}
      snapshot={null}
      usages={[usage({ providerId: "claude_max", label: "Claude", usagePercent: 42 })]}
    />,
  );
  expect(html).toContain("width:42%");
});
