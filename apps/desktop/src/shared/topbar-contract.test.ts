import { expect, test } from "bun:test";
import {
  TOPBAR_ELEMENTS,
  TOPBAR_FORBIDDEN_POLL_PATTERNS,
  TOPBAR_POLL_ALLOWLIST,
  TOPBAR_RECONNECT_RESYNC_P95_MS,
  TOPBAR_REQUIRED_WS_CHANNELS,
  TOPBAR_SLO_PREFIX,
  elementsForTrigger,
  lookupTopbarElement,
  urlMatchesForbiddenPattern,
  type TopbarElementID,
  type TopbarTriggerEvent,
} from "./topbar-contract.ts";

test("§7.6 catalog covers every top-bar element from plan.md", () => {
  const want: TopbarElementID[] = [
    "project_branch_clean_state",
    "tool_health_dots",
    "swarm_count",
    "beads_pulse",
    "code_health_pill",
    "subscription_pill",
    "activity_unread_badge",
  ];
  expect(TOPBAR_ELEMENTS.map((e) => e.id)).toEqual(want);
});

test("every element has non-empty displayName + description + ≥1 trigger", () => {
  for (const element of TOPBAR_ELEMENTS) {
    expect(element.displayName.length).toBeGreaterThan(0);
    expect(element.description.length).toBeGreaterThan(0);
    expect(element.triggerEvents.length).toBeGreaterThanOrEqual(1);
  }
});

test("every element has a positive p95 latency target", () => {
  for (const element of TOPBAR_ELEMENTS) {
    expect(element.p95LatencyMs).toBeGreaterThan(0);
  }
});

test("every element's p95 target is ≤ 2000ms (the §7.6 hard ceiling)", () => {
  for (const element of TOPBAR_ELEMENTS) {
    expect(element.p95LatencyMs).toBeLessThanOrEqual(2000);
  }
});

test("cheap-render elements (tool_health_dots, activity_unread_badge) target ≤ 1000ms", () => {
  const tool = lookupTopbarElement("tool_health_dots");
  const unread = lookupTopbarElement("activity_unread_badge");
  expect(tool?.p95LatencyMs).toBeLessThanOrEqual(1000);
  expect(unread?.p95LatencyMs).toBeLessThanOrEqual(1000);
});

test("code_health_pill triggered by health.snapshot.landed (§7.4.1)", () => {
  const pill = lookupTopbarElement("code_health_pill");
  expect(pill).toBeDefined();
  expect(pill?.triggerEvents).toContain("health.snapshot.landed");
});

test("subscription_pill triggered by caut.usage.snapshot", () => {
  const pill = lookupTopbarElement("subscription_pill");
  expect(pill).toBeDefined();
  expect(pill?.triggerEvents).toContain("caut.usage.snapshot");
});

test("project_branch_clean_state triggered by both vps_commit_created and git.status.changed", () => {
  const element = lookupTopbarElement("project_branch_clean_state");
  expect(element).toBeDefined();
  expect(element?.triggerEvents).toContain("vps_commit_created");
  expect(element?.triggerEvents).toContain("git.status.changed");
});

test("swarm_count triggered by both agent.registered and agent.departed", () => {
  const element = lookupTopbarElement("swarm_count");
  expect(element).toBeDefined();
  expect(element?.triggerEvents).toContain("agent.registered");
  expect(element?.triggerEvents).toContain("agent.departed");
});

test("element IDs are unique", () => {
  const seen = new Set<TopbarElementID>();
  for (const element of TOPBAR_ELEMENTS) {
    expect(seen.has(element.id)).toBe(false);
    seen.add(element.id);
  }
});

test("forbidden poll patterns include the canonical top-bar HTTP endpoints", () => {
  expect(TOPBAR_FORBIDDEN_POLL_PATTERNS).toContain("/v1/projects/");
  expect(TOPBAR_FORBIDDEN_POLL_PATTERNS).toContain("/v1/health");
  expect(TOPBAR_FORBIDDEN_POLL_PATTERNS).toContain("/v1/caut");
  expect(TOPBAR_FORBIDDEN_POLL_PATTERNS).toContain("/v1/capabilities");
});

test("poll allowlist is intentionally short and well-justified", () => {
  expect(TOPBAR_POLL_ALLOWLIST.length).toBeGreaterThan(0);
  expect(TOPBAR_POLL_ALLOWLIST.length).toBeLessThan(5);
});

test("required WS channels include project, swarm, activity, system:heartbeat", () => {
  expect(TOPBAR_REQUIRED_WS_CHANNELS).toContain("project:{id}");
  expect(TOPBAR_REQUIRED_WS_CHANNELS).toContain("swarm:{id}");
  expect(TOPBAR_REQUIRED_WS_CHANNELS).toContain("activity:{id}");
  expect(TOPBAR_REQUIRED_WS_CHANNELS).toContain("system:heartbeat");
});

test("SLO prefix matches §10.5 keying convention (`desktop.topbar`)", () => {
  expect(TOPBAR_SLO_PREFIX).toBe("desktop.topbar");
});

test("reconnect-resync SLO is 2 seconds (Invariant 3)", () => {
  expect(TOPBAR_RECONNECT_RESYNC_P95_MS).toBe(2000);
});

test("lookupTopbarElement returns undefined for an unknown ID", () => {
  // Cast through unknown to test runtime miss behavior with a
  // value the type system would otherwise reject.
  const got = lookupTopbarElement("does_not_exist" as unknown as TopbarElementID);
  expect(got).toBeUndefined();
});

test("elementsForTrigger returns the right subset for each event", () => {
  expect(elementsForTrigger("health.snapshot.landed").map((e) => e.id))
    .toEqual(["code_health_pill"]);
  expect(elementsForTrigger("capability.flipped").map((e) => e.id))
    .toEqual(["tool_health_dots"]);
  // vps_commit_created drives project/branch/clean state.
  expect(elementsForTrigger("vps_commit_created").map((e) => e.id))
    .toContain("project_branch_clean_state");
});

test("elementsForTrigger returns empty list for an unmapped event", () => {
  // A new event added without registering it should not crash;
  // it just returns no subscribers. Cast to the union for the
  // runtime call.
  const got = elementsForTrigger(
    "not.a.real.event" as unknown as TopbarTriggerEvent,
  );
  expect(got).toEqual([]);
});

test("every triggerEvent referenced is in the canonical TopbarTriggerEvent union", () => {
  // Compile-time check: at runtime we can only confirm the strings
  // are non-empty. The union check is the type system's job.
  for (const element of TOPBAR_ELEMENTS) {
    for (const event of element.triggerEvents) {
      expect(typeof event).toBe("string");
      expect(event.length).toBeGreaterThan(0);
    }
  }
});

test("forbidden patterns are unique strings", () => {
  const seen = new Set<string>();
  for (const pattern of TOPBAR_FORBIDDEN_POLL_PATTERNS) {
    expect(seen.has(pattern)).toBe(false);
    seen.add(pattern);
  }
});

test("urlMatchesForbiddenPattern: literal prefix uses string-startswith", () => {
  expect(urlMatchesForbiddenPattern("/v1/projects/abc", "/v1/projects/")).toBe(true);
  expect(urlMatchesForbiddenPattern("/v1/health", "/v1/health")).toBe(true);
  expect(urlMatchesForbiddenPattern("/v1/health/foo", "/v1/health")).toBe(true);
  expect(urlMatchesForbiddenPattern("/v1/healthy", "/v1/health")).toBe(true);
  expect(urlMatchesForbiddenPattern("/something-else", "/v1/health")).toBe(false);
});

test("urlMatchesForbiddenPattern: {id} placeholder matches one path segment", () => {
  expect(
    urlMatchesForbiddenPattern(
      "/v1/projects/demo/health/summary",
      "/v1/projects/{id}/health/summary",
    ),
  ).toBe(true);
  expect(
    urlMatchesForbiddenPattern(
      "/v1/projects/abc-def-123/git/status",
      "/v1/projects/{id}/git/status",
    ),
  ).toBe(true);
});

test("urlMatchesForbiddenPattern: {id} placeholder does NOT match across slashes", () => {
  // Placeholders match exactly one URL segment; multi-segment IDs
  // must not be silently accepted.
  expect(
    urlMatchesForbiddenPattern(
      "/v1/projects/foo/bar/health/summary",
      "/v1/projects/{id}/health/summary",
    ),
  ).toBe(false);
});

test("urlMatchesForbiddenPattern: template patterns reject unrelated URLs", () => {
  expect(
    urlMatchesForbiddenPattern(
      "/v1/health",
      "/v1/projects/{id}/health/summary",
    ),
  ).toBe(false);
  expect(
    urlMatchesForbiddenPattern(
      "/v1/projects/demo",
      "/v1/projects/{id}/health/summary",
    ),
  ).toBe(false);
});

test("urlMatchesForbiddenPattern: anchored at start (literal `/v1/projects/` does not match `/api/v1/projects/`)", () => {
  expect(
    urlMatchesForbiddenPattern("/api/v1/projects/abc", "/v1/projects/"),
  ).toBe(false);
});

test("every template entry has at least one matching real URL (no dead-code patterns)", () => {
  // Regression-test the hp-39ua finding: every template entry in
  // TOPBAR_FORBIDDEN_POLL_PATTERNS must match a realistic URL
  // shape. If a pattern is silently dead (unmatchable because of
  // a `{id}` typo or path drift), this test surfaces it.
  const fixtureURLForTemplate: Record<string, string> = {
    "/v1/projects/{id}/health/summary": "/v1/projects/demo/health/summary",
    "/v1/projects/{id}/git/status": "/v1/projects/demo/git/status",
    "/v1/projects/{id}/beads": "/v1/projects/demo/beads",
    "/v1/projects/{id}/agents": "/v1/projects/demo/agents",
    "/v1/projects/{id}/swarm": "/v1/projects/demo/swarm",
    "/v1/projects/{id}/activity": "/v1/projects/demo/activity",
  };
  for (const pattern of TOPBAR_FORBIDDEN_POLL_PATTERNS) {
    if (!pattern.includes("{")) continue;
    const fixture = fixtureURLForTemplate[pattern];
    expect(fixture, `template ${pattern} needs a fixture URL in this test`).toBeDefined();
    expect(
      urlMatchesForbiddenPattern(fixture!, pattern),
      `template ${pattern} must match its fixture ${fixture}`,
    ).toBe(true);
  }
});

test("the literal `/v1/projects/` covers every project-scoped template (subsumption documentation)", () => {
  // Reviewers should see explicitly that the literal prefix and
  // the templates BOTH document the same forbidden surface; the
  // templates remain useful as endpoint-by-endpoint inventory.
  for (const pattern of TOPBAR_FORBIDDEN_POLL_PATTERNS) {
    if (!pattern.startsWith("/v1/projects/{")) continue;
    const fixture = pattern.replace(/\{[^}]+\}/g, "demo");
    expect(urlMatchesForbiddenPattern(fixture, "/v1/projects/")).toBe(true);
  }
});
