// hp-ngq — Idempotency contract for §2.7 retried writes.
//
// The canonical list of write endpoints that MUST honor an
// `Idempotency-Key` request header per the bead spec. Tests in
// `apps/desktop/tests/integration/idempotency/` iterate over this
// table to verify daemon behavior; consumers can also import it for
// renderer-side request shaping (every write call should set the
// header before retrying through a reconnect).
//
// Cross-references:
//   plan.md §2.7 — Idempotency keys on every write endpoint that can
//   be retried by a reconnecting desktop.
//   plan.md §2.6 — Seed REST contract (the source list of endpoints).

export interface IdempotentWriteEndpoint {
  /** HTTP method. All write endpoints are POST or PATCH today. */
  method: "POST" | "PATCH" | "PUT";
  /** Path template using `{var}` placeholders. */
  path: string;
  /** Free-form notes about the endpoint's idempotency semantics. */
  notes?: string;
  /** Whether the daemon is expected to enforce key presence (return
   *  400 if absent) or merely honor a present key (no-op when absent). */
  enforcement: "required" | "honored";
  /** hp-jsi: marks an endpoint that is declared in this contract but
   *  is not yet wired in the daemon. The enforcement-lane test
   *  treats deferred endpoints as expected-to-return-{404,405,501};
   *  non-deferred endpoints are required to be routed and to honor
   *  the idempotency-key contract. Flipping `deferred` from `true`
   *  to `false` (or removing the field) when an endpoint lands is
   *  the explicit signal that the enforcement lane should now
   *  exercise it. Without this gate the e2e suite passed green
   *  whenever a route returned 501, masking missing-endpoint /
   *  middleware regressions during hardening. */
  deferred?: true;
}

export const IDEMPOTENT_WRITE_ENDPOINTS: ReadonlyArray<IdempotentWriteEndpoint> = [
  {
    // hp-jsi: routed via handlePlannedWrite stub — returns 501 today.
    // Flip `deferred` off when the bearer-bootstrap adapter lands.
    method: "POST",
    path: "/v1/auth/bootstrap/bearer",
    enforcement: "honored",
    deferred: true,
    notes:
      "Once-only by definition; the key handles retry mid-success so the desktop doesn't end up with two bearers.",
  },
  // hp-jsi: handleProjectCreate exists but the mock daemon harness
  // currently does not wire `s.projects`, so the route returns
  // `projects.create.unavailable` 501 from seed_contract.go:303.
  // Flip `deferred` off when the mock daemon plumbs a project
  // registry — at that point the enforcement lane begins exercising
  // the real handleProjectCreate path (idempotency-key required +
  // ImportRequest pipe).
  { method: "POST", path: "/v1/projects", enforcement: "required", deferred: true },
  // hp-jsi: every entry below routes via handlePlannedWrite and
  // returns 501 today. Flip `deferred` off when the matching adapter
  // lands; that's the enforcement-lane gate the bead asks for.
  { method: "POST", path: "/v1/projects/{projectId}/activate", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/git/push", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/plans", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/plans/{planId}/rounds", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/plans/{planId}/lock", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/beads/conversion-runs", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/beads/polish-runs", enforcement: "required", deferred: true },
  { method: "PATCH", path: "/v1/projects/{projectId}/beads/{beadId}", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/swarms", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/swarms/{swarmId}/broadcast", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/swarms/{swarmId}/agents/{agentId}/send", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/swarms/{swarmId}/agents/{agentId}/interrupt", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/swarms/{swarmId}/agents/{agentId}/stop", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/mail/messages", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/reservations/force-release", enforcement: "required", deferred: true },
  { method: "POST", path: "/v1/projects/{projectId}/health/snapshots", enforcement: "required", deferred: true },
];

export const IDEMPOTENCY_HEADER = "Idempotency-Key" as const;

/** Generate a deterministic-but-unique key per test invocation. The
 *  format ties the key to a test id + a salt so a single suite can
 *  produce distinct keys across iterations without collisions across
 *  parallel runs. */
export function makeIdempotencyKey(testId: string, salt: number = 0): string {
  const stamp = Date.now().toString(36);
  return `hp-ngq-${testId}-${stamp}-${salt}`;
}
