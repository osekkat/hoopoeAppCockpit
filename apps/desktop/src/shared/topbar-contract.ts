// hp-wwit engine slice: source-of-truth contract for the §7.6
// top-bar push-not-poll cadence + p95 latency SLO assertions.
//
// This file declares:
//   - The list of top-bar elements (TopbarElement) with their
//     per-element p95 latency targets in milliseconds.
//   - The mapping from element → triggering canonical-state-change
//     event (TopbarTriggerEvent) on the WS event stream.
//   - The forbidden endpoint blocklist the structural-assertion
//     test refuses to see HTTP requests against during steady state.
//   - The local-clone safety-net allowlist exemption.
//
// hp-wwit's harness (apps/desktop/test/topbar/no-poll.test.ts +
// push-latency.test.ts) and ESLint rule (`hoopoe/no-topbar-poll`)
// both consume this contract; per-element SLO targets land in
// packages/slo-targets.yaml (hp-5ja) keyed on the same TopbarElementID.
//
// Per plan.md §7.6 (top-bar push-not-poll + within-seconds claims),
// §7.4.1 ("within seconds of a new snapshot landing"), §10.5 (SLO
// targets), and §7.7 (the local-clone safety-net `git fetch` is NOT
// a top-bar poll — it is a different subsystem's sync loop).

export type TopbarElementID =
  | "project_branch_clean_state"
  | "tool_health_dots"
  | "swarm_count"
  | "beads_pulse"
  | "code_health_pill"
  | "subscription_pill"
  | "activity_unread_badge";

export type TopbarTriggerEvent =
  | "vps_commit_created"
  | "git.status.changed"
  | "capability.flipped"
  | "agent.registered"
  | "agent.departed"
  | "br.state.changed"
  | "health.snapshot.landed"
  | "caut.usage.snapshot"
  | "agent_mail.message.delivered"
  | "agent_mail.event.arrived";

/**
 * One row of the §7.6 top-bar contract.
 */
export interface TopbarElementContract {
  /** Stable identifier; SLO key + ESLint rule name + audit log key. */
  readonly id: TopbarElementID;
  /** User-facing name shown in the top-bar UI. */
  readonly displayName: string;
  /** Description of what the element shows + when it must update. */
  readonly description: string;
  /**
   * The canonical-state-change events that should re-render this
   * element. Order is informational; any of the listed events
   * arriving on the WS stream counts as a valid trigger.
   */
  readonly triggerEvents: readonly TopbarTriggerEvent[];
  /**
   * The p95 latency target in milliseconds, end-to-end:
   * canonical state change → daemon detects via reconciler →
   * emits WS event → renderer applies → DOM updated. Per §7.6
   * top-bar elements must hit p95 < 2000ms; tighter targets
   * apply to elements whose render is cheap (`tool health dots`
   * at 1000ms; `Activity unread badge` at 1000ms).
   */
  readonly p95LatencyMs: number;
}

/**
 * The §7.6 source-of-truth catalog. Order matches plan.md
 * reading top-to-bottom of the top-bar pill list.
 */
export const TOPBAR_ELEMENTS: readonly TopbarElementContract[] = [
  {
    id: "project_branch_clean_state",
    displayName: "Project / branch / clean state",
    description:
      "Project name, branch, and Git clean state. Must reflect a new commit or stage change within the SLO window.",
    triggerEvents: ["vps_commit_created", "git.status.changed"],
    p95LatencyMs: 1500,
  },
  {
    id: "tool_health_dots",
    displayName: "Tool health dots",
    description:
      "Per-tool capability dots (br, bv, ntm, agent-mail, rch, etc.). Must reflect a capability flip immediately; render is cheap.",
    triggerEvents: ["capability.flipped"],
    p95LatencyMs: 1000,
  },
  {
    id: "swarm_count",
    displayName: "Swarm count",
    description:
      "Active-agent count for the current project. Must reflect a new agent registration or departure within the SLO window.",
    triggerEvents: ["agent.registered", "agent.departed"],
    p95LatencyMs: 1500,
  },
  {
    id: "beads_pulse",
    displayName: "Beads pulse",
    description:
      "Bead activity indicator (recent transitions, ready-frontier delta). Must reflect a br state change within the SLO window.",
    triggerEvents: ["br.state.changed"],
    p95LatencyMs: 2000,
  },
  {
    id: "code_health_pill",
    displayName: "Code health pill",
    description:
      "Coverage / complexity / hotspot summary. Must reflect a new health snapshot within the SLO window — this is the explicit §7.6 / §7.4.1 'within seconds of a new snapshot landing' claim.",
    triggerEvents: ["health.snapshot.landed"],
    p95LatencyMs: 2000,
  },
  {
    id: "subscription_pill",
    displayName: "Subscription pill",
    description:
      "Per-provider subscription quota usage from caut. Must reflect a new caut usage snapshot within the SLO window.",
    triggerEvents: ["caut.usage.snapshot"],
    p95LatencyMs: 2000,
  },
  {
    id: "activity_unread_badge",
    displayName: "Activity unread badge",
    description:
      "Unread count for the Activity panel drawer. Must reflect a new mail / event arrival immediately.",
    triggerEvents: ["agent_mail.message.delivered", "agent_mail.event.arrived"],
    p95LatencyMs: 1000,
  },
];

/**
 * Endpoints the structural-assertion test refuses to see HTTP
 * requests against during the steady-state observation window.
 * If any of these are hit during the 120-second window with no
 * user interaction, the cockpit is polling — fail the test.
 *
 * Two pattern shapes coexist:
 * - Plain URL prefixes (e.g. `/v1/projects/`) match via
 *   string-starts-with.
 * - Path templates with `{name}` placeholders (e.g.
 *   `/v1/projects/{id}/health/summary`) match a single URL path
 *   segment per placeholder.
 *
 * Use {@link urlMatchesForbiddenPattern} to test a URL against a
 * pattern; the test harness must call it instead of comparing
 * URLs directly so template entries are not silently dead code.
 *
 * Initial subscribe-phase fetches are allowed and counted
 * separately by the test harness.
 */
export const TOPBAR_FORBIDDEN_POLL_PATTERNS: readonly string[] = [
  "/v1/projects/",
  "/v1/health",
  "/v1/projects/{id}/health/summary",
  "/v1/projects/{id}/git/status",
  "/v1/projects/{id}/beads",
  "/v1/projects/{id}/agents",
  "/v1/projects/{id}/swarm",
  "/v1/projects/{id}/activity",
  "/v1/caut",
  "/v1/capabilities",
];

/**
 * Returns true when the URL is forbidden under the given pattern.
 *
 * Patterns containing `{name}` placeholders are treated as path
 * templates: each placeholder matches exactly one URL path
 * segment (no `/`). Patterns without placeholders are string-
 * startswith.
 *
 * The harness iterates every observed URL against every pattern
 * in {@link TOPBAR_FORBIDDEN_POLL_PATTERNS}; a single match
 * fails the test. Template patterns let the contract document
 * specific endpoints (`/v1/projects/{id}/health/summary`)
 * separately from the coarse-grained literal (`/v1/projects/`)
 * so reviewers see both the surface inventory and the catch-all.
 */
export function urlMatchesForbiddenPattern(url: string, pattern: string): boolean {
  if (!pattern.includes("{")) {
    return url.startsWith(pattern);
  }
  const literalSegments = pattern.split(/\{[^}]+\}/);
  const escaped = literalSegments.map((segment) =>
    segment.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"),
  );
  const regexSrc = "^" + escaped.join("[^/]+");
  return new RegExp(regexSrc).test(url);
}

/**
 * URL patterns the structural-assertion test must EXCLUDE from
 * the no-poll check. The local-clone sync (§7.7) issues a
 * safety-net `git fetch` against origin via the local clone
 * subsystem (hp-ind); that is NOT a top-bar poll.
 *
 * Any future allowlist entry must be approved as a non-top-bar
 * subsystem and documented here with a one-line rationale.
 */
export const TOPBAR_POLL_ALLOWLIST: readonly string[] = [
  // hp-ind: local-clone sync subsystem's safety-net `git fetch`
  // against origin. Runs on a long interval; not driven by top-
  // bar values; not over the daemon HTTP API.
  "git+fetch://",
  // Daemon-internal heartbeat is sent over the WS stream, not
  // HTTP; including the WS heartbeat path here for lint-rule
  // auditors who confuse the two.
  "wss://heartbeat",
];

/**
 * The WS subscription channels the renderer must keep open to
 * receive top-bar updates. The structural-assertion test asserts
 * these are present after the initial subscribe phase.
 */
export const TOPBAR_REQUIRED_WS_CHANNELS: readonly string[] = [
  "project:{id}",
  "swarm:{id}",
  "activity:{id}",
  "system:heartbeat",
];

/**
 * The §10.5-style SLO key prefix for top-bar latency rows in
 * packages/slo-targets.yaml. Per-element targets are written as
 * `${TOPBAR_SLO_PREFIX}.${id}.p95_ms`.
 */
export const TOPBAR_SLO_PREFIX = "desktop.topbar";

/**
 * The reconnect-resync SLO target (Invariant 3 from hp-wwit):
 * after a sleep/wake or tunnel-drop reconnect, the top-bar must
 * re-sync within p95 < 2 seconds of WS reconnect (not 2 seconds
 * of network return).
 */
export const TOPBAR_RECONNECT_RESYNC_P95_MS = 2000;

/**
 * Look up a top-bar element by ID. Returns undefined for
 * unknown IDs.
 */
export function lookupTopbarElement(id: TopbarElementID): TopbarElementContract | undefined {
  return TOPBAR_ELEMENTS.find((element) => element.id === id);
}

/**
 * Returns the elements that subscribe to the given trigger event.
 * Used by the test harness to know which DOM nodes to inspect
 * after firing a synthetic event.
 */
export function elementsForTrigger(event: TopbarTriggerEvent): readonly TopbarElementContract[] {
  return TOPBAR_ELEMENTS.filter((element) => element.triggerEvents.includes(event));
}
