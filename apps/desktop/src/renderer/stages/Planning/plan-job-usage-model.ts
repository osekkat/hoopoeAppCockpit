// hp-pmw — Plan-job usage model.
//
// Per plan.md §7.1 'Plan round cost and time discipline':
//   - Hoopoe is subscription-only — the metric tracked is per-provider
//     subscription-quota usage, NOT per-token API dollars.
//   - Per-call shapes: wall-clock latency, model identity, harness,
//     CAAM account, input hash, artifact hash.
//   - UI surfaces "X% of daily Claude Max quota" not "$0.42 of credits".
//   - When caut isn't reporting for a provider, label "unmeasured"
//     rather than fabricate.
//
// All helpers in this module are pure. They consume measurements from
// caut (per-call wall-clock + per-provider window usage) and project
// them into the shapes the sidebar renders. The renderer-side data
// pipeline (caut adapter via daemon RPC) wires these later.

import type { PlanProviderId } from "./plan-input-models.ts";

// ── Wire shapes ───────────────────────────────────────────────────────────

export type PlanJobStatus =
  | "queued"
  | "running"
  | "completed"
  | "failed"
  | "cancelled";

export interface PlanJobMeasurement {
  /** Stable id; multiple calls per job carry distinct ids. */
  readonly callId: string;
  /** Provider the call ran against. */
  readonly providerId: PlanProviderId;
  /** Harness used to invoke the model. */
  readonly harness:
    | "claude_code"
    | "codex_cli"
    | "gemini_cli"
    | "oracle_browser"
    | "grok_browser";
  /** CAAM account reference when applicable; null for harness="oracle_browser". */
  readonly caamAccount: string | null;
  /** Wall-clock latency for the call in ms. */
  readonly latencyMs: number;
  /** Hash of the input prompt — for cache lookups + audit. */
  readonly inputHash: string;
  /** Hash of the artifact produced — null when call failed. */
  readonly artifactHash: string | null;
  /** RFC3339 timestamp the call completed. */
  readonly completedAt: string;
}

export interface PlanJobSnapshot {
  readonly jobId: string;
  readonly status: PlanJobStatus;
  /** RFC3339 timestamp of when the job entered "running". null when
   *  still queued. */
  readonly startedAt: string | null;
  /** RFC3339 timestamp of the most recent measurement (null when no
   *  call has completed yet). */
  readonly lastActivityAt: string | null;
  /** Active provider when status==="running" (null for terminal states). */
  readonly activeProviderId: PlanProviderId | null;
  /** Active CAAM account when applicable. */
  readonly activeCaamAccount: string | null;
  /** All completed measurements for this job, in chronological order. */
  readonly measurements: readonly PlanJobMeasurement[];
}

export interface SubscriptionWindowUsage {
  readonly providerId: PlanProviderId;
  /** Display label ("Claude", "GPT", "Gemini", "Pro web"). */
  readonly label: string;
  /** Used quota as 0..100. null = caut isn't reporting. */
  readonly usagePercent: number | null;
  /** RFC3339 timestamp when the window resets (e.g. midnight Pacific). */
  readonly resetsAt: string | null;
  /** Currently rate-limited per CAAM/CLI/NTM/rano signals. */
  readonly rateLimited: boolean;
}

// ── Pure helpers ──────────────────────────────────────────────────────────

/** Format a millisecond duration as "Nm Ns" / "Ns" / "Nm" / "Nh Nm". */
export function formatElapsed(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "—";
  if (ms < 1_000) return `${ms}ms`;
  const seconds = Math.floor(ms / 1_000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remSeconds = seconds % 60;
  if (minutes < 60) {
    return remSeconds === 0 ? `${minutes}m` : `${minutes}m ${remSeconds}s`;
  }
  const hours = Math.floor(minutes / 60);
  const remMinutes = minutes % 60;
  return remMinutes === 0 ? `${hours}h` : `${hours}h ${remMinutes}m`;
}

/** Compute elapsed milliseconds for a snapshot.
 *  - When status === "running" + startedAt != null: now() - startedAt.
 *  - When status === "completed" / "failed" / "cancelled": time between
 *    startedAt and lastActivityAt.
 *  - Otherwise: 0.
 *  Returns 0 for any negative/invalid result. */
export function elapsedMs(snapshot: PlanJobSnapshot, now: () => Date = () => new Date()): number {
  if (!snapshot.startedAt) return 0;
  const startedAt = Date.parse(snapshot.startedAt);
  if (Number.isNaN(startedAt)) return 0;
  if (snapshot.status === "running" || snapshot.status === "queued") {
    return Math.max(0, now().getTime() - startedAt);
  }
  // Terminal: clamp to lastActivityAt.
  if (!snapshot.lastActivityAt) return 0;
  const lastActivityAt = Date.parse(snapshot.lastActivityAt);
  if (Number.isNaN(lastActivityAt)) return 0;
  return Math.max(0, lastActivityAt - startedAt);
}

/** Estimate remaining milliseconds for a running job by extrapolating
 *  from already-completed measurements. Returns null when there's not
 *  enough signal (no measurements yet, or job not running). */
export interface RemainingEstimate {
  readonly ms: number;
  readonly source: "extrapolation" | "single_call" | "fallback";
  readonly confidence: "low" | "medium";
}

export function estimateRemainingMs(
  snapshot: PlanJobSnapshot,
  expectedTotalCalls: number,
  now: () => Date = () => new Date(),
): RemainingEstimate | null {
  if (snapshot.status !== "running") return null;
  if (expectedTotalCalls <= 0) return null;
  if (!snapshot.startedAt) return null;
  const completedCalls = snapshot.measurements.length;
  const remainingCalls = Math.max(0, expectedTotalCalls - completedCalls);
  if (remainingCalls === 0) {
    return { ms: 0, source: "extrapolation", confidence: "medium" };
  }
  if (completedCalls === 0) {
    // Fallback: assume each remaining call takes 60s. Low confidence.
    return {
      ms: remainingCalls * 60_000,
      source: "fallback",
      confidence: "low",
    };
  }
  if (completedCalls === 1) {
    const singleLatency = snapshot.measurements[0]!.latencyMs;
    return {
      ms: remainingCalls * singleLatency,
      source: "single_call",
      confidence: "low",
    };
  }
  // Average across observed calls — clamp to 50 most recent so a long
  // tail of cheap fixtures doesn't underestimate when the active model
  // has been swapped to a slower one.
  const recent = snapshot.measurements.slice(-50);
  const avg = recent.reduce((sum, m) => sum + m.latencyMs, 0) / recent.length;
  // Confidence rises once we have 3+ calls.
  const confidence = recent.length >= 3 ? "medium" : "low";
  return { ms: Math.round(remainingCalls * avg), source: "extrapolation", confidence };
}

/** Total wall-clock latency contributed by a given provider. Useful for
 *  the "this run drained 32% of your Claude Max quota" callout. */
export function providerLatencyMs(
  snapshot: PlanJobSnapshot,
  providerId: PlanProviderId,
): number {
  let total = 0;
  for (const m of snapshot.measurements) {
    if (m.providerId === providerId) total += Math.max(0, m.latencyMs);
  }
  return total;
}

/** Format a usage-percent value with the "unmeasured" fallback per
 *  §7.1. Tests pin the exact strings. */
export function formatQuotaPercent(value: number | null): string {
  if (value === null) return "unmeasured";
  if (!Number.isFinite(value) || value < 0) return "unmeasured";
  return `${Math.min(100, Math.round(value))}%`;
}

/** Bucket a usage percent into a status the renderer uses for color +
 *  warning copy. */
export type QuotaSeverity = "ok" | "warning" | "danger" | "unmeasured";

export function quotaSeverity(usage: SubscriptionWindowUsage): QuotaSeverity {
  if (usage.rateLimited) return "danger";
  if (usage.usagePercent === null) return "unmeasured";
  if (usage.usagePercent >= 90) return "danger";
  if (usage.usagePercent >= 70) return "warning";
  return "ok";
}

/** Sort the active-provider list so the in-flight provider renders
 *  first (when the snapshot is running), followed by usage-desc. Pure;
 *  tests assert order without relying on render order. */
export function rankUsageRows(
  usages: readonly SubscriptionWindowUsage[],
  activeProviderId: PlanProviderId | null,
): readonly SubscriptionWindowUsage[] {
  return [...usages].sort((a, b) => {
    if (a.providerId === activeProviderId && b.providerId !== activeProviderId) return -1;
    if (b.providerId === activeProviderId && a.providerId !== activeProviderId) return 1;
    const aPct = a.usagePercent ?? -1;
    const bPct = b.usagePercent ?? -1;
    return bPct - aPct;
  });
}

/** Compose the canonical "Active: <provider> via <harness> (<account>) —
 *  Nm Ns elapsed, ~Nm remaining" string. Returns null when the job
 *  isn't running. */
export function activeLineText(
  snapshot: PlanJobSnapshot,
  expectedTotalCalls: number,
  now: () => Date = () => new Date(),
): string | null {
  if (snapshot.status !== "running") return null;
  if (!snapshot.activeProviderId) return null;
  const elapsed = elapsedMs(snapshot, now);
  const remaining = estimateRemainingMs(snapshot, expectedTotalCalls, now);
  const accountSegment = snapshot.activeCaamAccount ? ` (${snapshot.activeCaamAccount})` : "";
  const elapsedText = formatElapsed(elapsed);
  const remainingText = remaining ? `, ~${formatElapsed(remaining.ms)} remaining` : "";
  // Active provider's harness is unknown without the most-recent
  // measurement; fall back to the bare provider id when the job has
  // started but no calls completed yet.
  const lastMeasurement = snapshot.measurements.at(-1);
  const harness = lastMeasurement && lastMeasurement.providerId === snapshot.activeProviderId
    ? lastMeasurement.harness
    : null;
  const harnessSegment = harness ? ` via ${harnessLabel(harness)}` : "";
  return `Active: ${snapshot.activeProviderId}${harnessSegment}${accountSegment} — ${elapsedText} elapsed${remainingText}`;
}

function harnessLabel(harness: PlanJobMeasurement["harness"]): string {
  switch (harness) {
    case "claude_code": return "Claude Code";
    case "codex_cli": return "Codex CLI";
    case "gemini_cli": return "Gemini CLI";
    case "oracle_browser": return "Oracle";
    case "grok_browser": return "Grok browser";
    default: return harness;
  }
}
