// hp-4ya — Phase 4 cockpit top bar data layer.
//
// Five pills consume the daemon's reconciled top-bar cache (§7.6 +
// §10.5). For Phase 4 the renderer tries the daemon RPC first; on
// bridge-unavailable (no daemon paired yet, local-demo mode, or pre-
// connect state) it falls back to project-aware seed data so the chrome
// always renders something useful — never a spinner cluster on first
// boot.
//
// Real WebSocket-driven updates (no polling) are a Phase 8/10 follow-up.
// Phase 4 acceptance only requires "render with seed data + click-
// throughs to Diagnostics / Swarm / Beads / Health".
//
// Cross-references:
//   - §7.6 — top-bar element list
//   - §10.5 — display latency p95 < 1s
//   - apps/desktop/src/renderer/data/stage-data.ts — same daemon-first
//     pattern used for stage data.

import { useQuery } from "@tanstack/react-query";
import type { ShellProjectSummary } from "../store.ts";

// ── Wire shapes ───────────────────────────────────────────────────────────

export type HealthDot = "healthy" | "degraded" | "offline" | "unknown";

export interface ToolHealthSnapshot {
  /** VPS daemon health (200/timeout/down). */
  readonly vps: HealthDot;
  /** NTM coordinator health. */
  readonly ntm: HealthDot;
  /** Agent Mail server health. */
  readonly mail: HealthDot;
  /** br (beads_rust) tool availability. */
  readonly br: HealthDot;
  /** bv triage engine availability. */
  readonly bv: HealthDot;
  /** True iff every component is healthy. */
  readonly allHealthy: boolean;
  /** True iff any component is offline. */
  readonly anyOffline: boolean;
}

export interface SwarmStateSummary {
  /** Agents actively making progress. */
  readonly running: number;
  /** Agents waiting / idle / awaiting work. */
  readonly idle: number;
  /** Agents wedged or rate-limited (renderer surfaces an alert). */
  readonly wedged: number;
  /** Total panes across all sessions. */
  readonly total: number;
}

export interface BeadsPulse {
  /** Beads with no blockers and ready to start. */
  readonly ready: number;
  /** Beads currently being worked. */
  readonly inProgress: number;
  /** Beads blocked on at least one open dependency. */
  readonly blocked: number;
}

export interface CodeHealthSummary {
  /** Average test coverage across the project, or null when no run yet. */
  readonly coveragePercent: number | null;
  /** Average cyclomatic complexity across covered files. */
  readonly avgComplexity: number | null;
  /** Count of hotspots above the project's threshold. */
  readonly hotspotCount: number;
  /** Minutes since the last health snapshot landed; null when never run. */
  readonly lastSnapshotAgeMinutes: number | null;
  /** Coarse verdict for the pill color. */
  readonly verdict: "healthy" | "warning" | "critical" | "unknown";
}

export interface SubscriptionProviderUsage {
  /** Stable id (claude_max | gpt_pro | gemini_ultra | chatgpt_pro_browser). */
  readonly id: "claude_max" | "gpt_pro" | "gemini_ultra" | "chatgpt_pro_browser";
  /** Display label shown next to the bar. */
  readonly label: string;
  /** Usage as 0-100 (caut quota %). */
  readonly usagePercent: number;
  /** True when the provider is currently rate-limited. */
  readonly rateLimited: boolean;
}

export interface SubscriptionUsageSummary {
  readonly providers: readonly SubscriptionProviderUsage[];
  /** True iff any provider is rate-limited. */
  readonly anyRateLimited: boolean;
  /** Worst usagePercent across providers. Used for the pill color. */
  readonly maxUsagePercent: number;
}

// ── Bridge resolution ─────────────────────────────────────────────────────

interface RendererDaemonBridge {
  readonly daemon?: {
    readonly request?: (method: string, body: unknown) => Promise<unknown>;
  };
}

function resolveDaemonRequest(): ((method: string, body: unknown) => Promise<unknown>) | null {
  if (typeof window === "undefined") return null;
  const hoopoe = (window as Window & { readonly hoopoe?: RendererDaemonBridge }).hoopoe;
  const request = hoopoe?.daemon?.request;
  return typeof request === "function" ? request : null;
}

/**
 * Bridge-resolution helper for top-bar queries.
 *
 * Returns `null` ONLY when no daemon bridge is wired (no preload, no
 * pairing, jsdom, Mock Flywheel without an installed bridge) — that is
 * the pre-connect / demo state where seeded shell data is the
 * intentional fallback.
 *
 * hp-dk4r: when a bridge IS present and the daemon/capability call
 * fails, the previous `try { ... } catch { return null; }` swallowed
 * the error and tricked queryFn into returning seed data — so a broken
 * canonical integration (offline VPS, missing capability, network
 * outage) was reported to the user as "0% usage / wedged 0 / all 5
 * tools healthy". That hides exactly the failure modes the top bar
 * exists to surface. The fix re-throws bridge errors so useQuery's
 * error/stale state can drive a visible degraded indicator instead of
 * a fabricated healthy snapshot.
 */
async function callDaemonOrNoBridge<O>(method: string, body: unknown): Promise<O | null> {
  const request = resolveDaemonRequest();
  if (!request) return null;
  return (await request(method, body)) as O;
}

/**
 * Test-only re-export of the bridge resolver. The bridge contract is
 * load-bearing for hp-dk4r — a bridge-present-but-failing call must
 * NOT swallow the error into a seed-success — so the test suite
 * exercises it directly. Production code paths only call the
 * package-private `callDaemonOrNoBridge` above; this export exists
 * solely so the bun:test runner (which has no DOM/renderHook) can
 * pin the contract without a full React render.
 */
export const callDaemonOrNoBridgeForTesting = callDaemonOrNoBridge;

// ── Seed data (project-aware where possible) ─────────────────────────────

export function seedToolHealth(project: ShellProjectSummary | null): ToolHealthSnapshot {
  const vps = project?.toolHealth.vps ?? "unknown";
  const ntm = project?.toolHealth.ntm ?? "unknown";
  const mail = project?.toolHealth.mail ?? "unknown";
  // br + bv default to healthy when the VPS is healthy; otherwise unknown
  // until the capability registry reports back (hp-r33).
  const baseline: HealthDot = vps === "healthy" ? "healthy" : "unknown";
  const snapshot: Omit<ToolHealthSnapshot, "allHealthy" | "anyOffline"> = {
    vps,
    ntm,
    mail,
    br: baseline,
    bv: baseline,
  };
  const dots: readonly HealthDot[] = [snapshot.vps, snapshot.ntm, snapshot.mail, snapshot.br, snapshot.bv];
  return {
    ...snapshot,
    allHealthy: dots.every((d) => d === "healthy"),
    anyOffline: dots.some((d) => d === "offline"),
  };
}

export function seedSwarmState(project: ShellProjectSummary | null): SwarmStateSummary {
  // The store currently exposes activeAgents + readyBeads. For Phase 4
  // we pretend any non-running project has 0 wedged. Phase 8 wires real
  // running/idle/wedged counts from NTM.
  const activeAgents = project?.swarm.activeAgents ?? 0;
  const status = project?.swarm.status ?? "idle";
  const running = status === "running" ? activeAgents : 0;
  const idle = status === "idle" ? activeAgents : 0;
  return {
    running,
    idle,
    wedged: 0,
    total: running + idle,
  };
}

export function seedBeadsPulse(project: ShellProjectSummary | null): BeadsPulse {
  // The store has readyBeads; in-progress + blocked are seeded from the
  // running-agent count + zero. Phase 6 (bv robot-triage adapter) wires
  // real values.
  const ready = project?.swarm.readyBeads ?? 0;
  const inProgress = project?.swarm.activeAgents ?? 0;
  return { ready, inProgress, blocked: 0 };
}

export function seedCodeHealth(): CodeHealthSummary {
  // Phase 11 (hp-sgj) wires real coverage / complexity / hotspots from
  // language-native runners. Phase 4 ships a clean "no snapshot yet"
  // shape so the pill renders + the click-through to Health works.
  return {
    coveragePercent: null,
    avgComplexity: null,
    hotspotCount: 0,
    lastSnapshotAgeMinutes: null,
    verdict: "unknown",
  };
}

export function seedSubscriptionUsage(): SubscriptionUsageSummary {
  // Phase 8/10 (hp-2tx, hp-4nz) wire real per-provider usage from caut
  // + rate-limit signals from CAAM/CLI/NTM/rano. Seed is the canonical
  // four providers with 0% usage and no rate-limits.
  const providers: readonly SubscriptionProviderUsage[] = [
    { id: "claude_max", label: "Claude", usagePercent: 0, rateLimited: false },
    { id: "gpt_pro", label: "GPT", usagePercent: 0, rateLimited: false },
    { id: "gemini_ultra", label: "Gemini", usagePercent: 0, rateLimited: false },
    { id: "chatgpt_pro_browser", label: "Pro web", usagePercent: 0, rateLimited: false },
  ];
  return { providers, anyRateLimited: false, maxUsagePercent: 0 };
}

// ── Hooks (daemon-first with seed fallback) ──────────────────────────────

export function useToolHealthQuery(project: ShellProjectSummary | null) {
  const projectId = project?.id ?? "";
  return useQuery<ToolHealthSnapshot>({
    queryKey: ["topbar", "tool-health", projectId],
    queryFn: async () => {
      const remote = await callDaemonOrNoBridge<ToolHealthSnapshot>("capabilities", null);
      return remote ?? seedToolHealth(project);
    },
    placeholderData: () => seedToolHealth(project),
    staleTime: 30_000,
  });
}

export function useSwarmStateQuery(project: ShellProjectSummary | null) {
  const projectId = project?.id ?? "";
  return useQuery<SwarmStateSummary>({
    queryKey: ["topbar", "swarm-state", projectId],
    queryFn: async () => {
      if (!projectId) return seedSwarmState(project);
      const remote = await callDaemonOrNoBridge<SwarmStateSummary>("swarm.snapshot", { projectId });
      return remote ?? seedSwarmState(project);
    },
    placeholderData: () => seedSwarmState(project),
    staleTime: 5_000,
  });
}

export function useBeadsPulseQuery(project: ShellProjectSummary | null) {
  const projectId = project?.id ?? "";
  return useQuery<BeadsPulse>({
    queryKey: ["topbar", "beads-pulse", projectId],
    queryFn: async () => {
      if (!projectId) return seedBeadsPulse(project);
      const remote = await callDaemonOrNoBridge<BeadsPulse>("triage.get", { projectId });
      return remote ?? seedBeadsPulse(project);
    },
    placeholderData: () => seedBeadsPulse(project),
    staleTime: 10_000,
  });
}

export function useCodeHealthQuery(project: ShellProjectSummary | null) {
  const projectId = project?.id ?? "";
  return useQuery<CodeHealthSummary>({
    queryKey: ["topbar", "code-health", projectId],
    queryFn: () => {
      // Phase 11 wires the real /v1/health/snapshot read via a bespoke
      // RPC; for Phase 4 we ship the seed shape regardless.
      return Promise.resolve(seedCodeHealth());
    },
    placeholderData: () => seedCodeHealth(),
    staleTime: 60_000,
  });
}

export function useSubscriptionUsageQuery() {
  return useQuery<SubscriptionUsageSummary>({
    queryKey: ["topbar", "subscription-usage"],
    queryFn: () => Promise.resolve(seedSubscriptionUsage()),
    placeholderData: () => seedSubscriptionUsage(),
    staleTime: 30_000,
  });
}

// ── Pure formatting helpers (testable) ───────────────────────────────────

/** Map a HealthDot to a Tailwind-style status class name. */
export function dotClass(dot: HealthDot): string {
  switch (dot) {
    case "healthy": return "hh-dot-healthy";
    case "degraded": return "hh-dot-degraded";
    case "offline": return "hh-dot-offline";
    case "unknown":
    default: return "hh-dot-unknown";
  }
}

/** Aria label for the tool-health pill. Reads "5 tools healthy" /
 *  "VPS offline, NTM degraded, br healthy, ..." so screen readers
 *  convey state component-by-component. */
export function toolHealthAria(snapshot: ToolHealthSnapshot): string {
  if (snapshot.allHealthy) return "All five tools healthy";
  return [
    ["VPS", snapshot.vps],
    ["NTM", snapshot.ntm],
    ["Mail", snapshot.mail],
    ["br", snapshot.br],
    ["bv", snapshot.bv],
  ]
    .filter(([, d]) => d !== "healthy")
    .map(([name, d]) => `${name} ${d}`)
    .join(", ");
}

export function codeHealthAria(summary: CodeHealthSummary): string {
  if (summary.coveragePercent === null && summary.lastSnapshotAgeMinutes === null) {
    return "No code-health snapshot yet";
  }
  const parts: string[] = [];
  if (summary.coveragePercent !== null) parts.push(`coverage ${summary.coveragePercent}%`);
  if (summary.avgComplexity !== null) parts.push(`complexity ${summary.avgComplexity.toFixed(1)}`);
  if (summary.hotspotCount > 0) parts.push(`${summary.hotspotCount} hotspot${summary.hotspotCount === 1 ? "" : "s"}`);
  if (summary.lastSnapshotAgeMinutes !== null) parts.push(`updated ${summary.lastSnapshotAgeMinutes}m ago`);
  return parts.join(", ");
}

export function subscriptionAria(summary: SubscriptionUsageSummary): string {
  if (summary.anyRateLimited) {
    const rate = summary.providers.filter((p) => p.rateLimited).map((p) => p.label).join(", ");
    return `Rate-limited: ${rate}`;
  }
  if (summary.maxUsagePercent === 0) return "Subscription usage idle";
  return `Subscription usage up to ${summary.maxUsagePercent}%`;
}
