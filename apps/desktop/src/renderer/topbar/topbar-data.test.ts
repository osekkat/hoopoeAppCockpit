// hp-4ya — top-bar data layer tests (seed helpers + aria formatters).

import { expect, test } from "bun:test";
import {
  codeHealthAria,
  dotClass,
  seedBeadsPulse,
  seedCodeHealth,
  seedSubscriptionUsage,
  seedSwarmState,
  seedToolHealth,
  subscriptionAria,
  toolHealthAria,
  type CodeHealthSummary,
  type HealthDot,
  type SubscriptionUsageSummary,
} from "./index.ts";
import type { ShellProjectSummary } from "../store.ts";

const projectFixture = (overrides: Partial<ShellProjectSummary> = {}): ShellProjectSummary => ({
  id: "p1",
  name: "Test project",
  slug: "test-project",
  repoUrl: "fixture://test",
  rootPath: "/tmp/test",
  branch: "main",
  gitStatus: "clean",
  pinned: false,
  lastActivatedAt: "2026-05-04T00:00:00.000Z",
  swarm: { status: "idle", activeAgents: 0, readyBeads: 0 },
  toolHealth: { vps: "healthy", ntm: "healthy", mail: "healthy" },
  ...overrides,
});

test("seedToolHealth: null project → all unknown", () => {
  const snapshot = seedToolHealth(null);
  expect(snapshot.vps).toBe("unknown");
  expect(snapshot.ntm).toBe("unknown");
  expect(snapshot.mail).toBe("unknown");
  expect(snapshot.br).toBe("unknown");
  expect(snapshot.bv).toBe("unknown");
  expect(snapshot.allHealthy).toBe(false);
  expect(snapshot.anyOffline).toBe(false);
});

test("seedToolHealth: healthy VPS extends to br + bv as healthy baseline", () => {
  const snapshot = seedToolHealth(projectFixture());
  expect(snapshot.vps).toBe("healthy");
  expect(snapshot.br).toBe("healthy");
  expect(snapshot.bv).toBe("healthy");
  expect(snapshot.allHealthy).toBe(true);
});

test("seedToolHealth: degraded mail flips allHealthy false", () => {
  const snapshot = seedToolHealth(
    projectFixture({ toolHealth: { vps: "healthy", ntm: "healthy", mail: "degraded" } }),
  );
  expect(snapshot.mail).toBe("degraded");
  expect(snapshot.allHealthy).toBe(false);
  expect(snapshot.anyOffline).toBe(false);
});

test("seedToolHealth: VPS offline marks anyOffline + br/bv unknown (not propagated as healthy)", () => {
  const snapshot = seedToolHealth(
    projectFixture({ toolHealth: { vps: "offline", ntm: "offline", mail: "offline" } }),
  );
  expect(snapshot.vps).toBe("offline");
  expect(snapshot.anyOffline).toBe(true);
  expect(snapshot.br).toBe("unknown");
  expect(snapshot.bv).toBe("unknown");
});

test("seedSwarmState: running project counts running, idle 0, wedged 0", () => {
  const summary = seedSwarmState(
    projectFixture({ swarm: { status: "running", activeAgents: 4, readyBeads: 6 } }),
  );
  expect(summary).toEqual({ running: 4, idle: 0, wedged: 0, total: 4 });
});

test("seedSwarmState: idle project counts active as idle, not running", () => {
  const summary = seedSwarmState(
    projectFixture({ swarm: { status: "idle", activeAgents: 2, readyBeads: 0 } }),
  );
  expect(summary).toEqual({ running: 0, idle: 2, wedged: 0, total: 2 });
});

test("seedSwarmState: null project → all zero", () => {
  expect(seedSwarmState(null)).toEqual({ running: 0, idle: 0, wedged: 0, total: 0 });
});

test("seedBeadsPulse: pulls ready + activeAgents from project store", () => {
  const pulse = seedBeadsPulse(
    projectFixture({ swarm: { status: "running", activeAgents: 3, readyBeads: 7 } }),
  );
  expect(pulse).toEqual({ ready: 7, inProgress: 3, blocked: 0 });
});

test("seedCodeHealth: returns 'unknown' verdict with null fields when no snapshot", () => {
  const health = seedCodeHealth();
  expect(health.coveragePercent).toBeNull();
  expect(health.avgComplexity).toBeNull();
  expect(health.hotspotCount).toBe(0);
  expect(health.lastSnapshotAgeMinutes).toBeNull();
  expect(health.verdict).toBe("unknown");
});

test("seedSubscriptionUsage: 4 canonical providers, all 0%, no rate-limits", () => {
  const usage = seedSubscriptionUsage();
  expect(usage.providers.map((p) => p.id)).toEqual([
    "claude_max",
    "gpt_pro",
    "gemini_ultra",
    "chatgpt_pro_browser",
  ]);
  expect(usage.providers.every((p) => p.usagePercent === 0)).toBe(true);
  expect(usage.anyRateLimited).toBe(false);
  expect(usage.maxUsagePercent).toBe(0);
});

test("dotClass: maps each HealthDot to its CSS class", () => {
  expect(dotClass("healthy")).toBe("hh-dot-healthy");
  expect(dotClass("degraded")).toBe("hh-dot-degraded");
  expect(dotClass("offline")).toBe("hh-dot-offline");
  expect(dotClass("unknown")).toBe("hh-dot-unknown");
  // Defensive: unknown values fall back to unknown.
  expect(dotClass("garbage" as HealthDot)).toBe("hh-dot-unknown");
});

test("toolHealthAria: all healthy → 'All five tools healthy'", () => {
  const snapshot = seedToolHealth(projectFixture());
  expect(toolHealthAria(snapshot)).toBe("All five tools healthy");
});

test("toolHealthAria: lists each non-healthy component with its state", () => {
  const snapshot = seedToolHealth(
    projectFixture({ toolHealth: { vps: "offline", ntm: "degraded", mail: "healthy" } }),
  );
  const aria = toolHealthAria(snapshot);
  expect(aria).toContain("VPS offline");
  expect(aria).toContain("NTM degraded");
  // br + bv are unknown (because vps is offline → baseline is unknown).
  expect(aria).toContain("br unknown");
  expect(aria).toContain("bv unknown");
});

test("codeHealthAria: 'No snapshot yet' when fields are null", () => {
  const health: CodeHealthSummary = seedCodeHealth();
  expect(codeHealthAria(health)).toBe("No code-health snapshot yet");
});

test("codeHealthAria: composes coverage + complexity + hotspots + age", () => {
  const aria = codeHealthAria({
    coveragePercent: 84,
    avgComplexity: 7.3,
    hotspotCount: 3,
    lastSnapshotAgeMinutes: 5,
    verdict: "warning",
  });
  expect(aria).toContain("coverage 84%");
  expect(aria).toContain("complexity 7.3");
  expect(aria).toContain("3 hotspots");
  expect(aria).toContain("updated 5m ago");
});

test("codeHealthAria: 1 hotspot uses singular", () => {
  const aria = codeHealthAria({
    coveragePercent: null,
    avgComplexity: null,
    hotspotCount: 1,
    lastSnapshotAgeMinutes: 1,
    verdict: "critical",
  });
  expect(aria).toContain("1 hotspot,");
  expect(aria).not.toContain("1 hotspots");
});

test("subscriptionAria: idle when nothing used", () => {
  expect(subscriptionAria(seedSubscriptionUsage())).toBe("Subscription usage idle");
});

test("subscriptionAria: surfaces rate-limited providers by label", () => {
  const usage: SubscriptionUsageSummary = {
    providers: [
      { id: "claude_max", label: "Claude", usagePercent: 90, rateLimited: true },
      { id: "gpt_pro", label: "GPT", usagePercent: 50, rateLimited: false },
      { id: "gemini_ultra", label: "Gemini", usagePercent: 95, rateLimited: true },
      { id: "chatgpt_pro_browser", label: "Pro web", usagePercent: 0, rateLimited: false },
    ],
    anyRateLimited: true,
    maxUsagePercent: 95,
  };
  expect(subscriptionAria(usage)).toBe("Rate-limited: Claude, Gemini");
});

test("subscriptionAria: high usage with no rate-limit prints max%", () => {
  const usage: SubscriptionUsageSummary = {
    providers: [],
    anyRateLimited: false,
    maxUsagePercent: 73,
  };
  expect(subscriptionAria(usage)).toBe("Subscription usage up to 73%");
});
