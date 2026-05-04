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
}

export const IDEMPOTENT_WRITE_ENDPOINTS: ReadonlyArray<IdempotentWriteEndpoint> = [
  {
    method: "POST",
    path: "/v1/auth/bootstrap/bearer",
    enforcement: "honored",
    notes:
      "Once-only by definition; the key handles retry mid-success so the desktop doesn't end up with two bearers.",
  },
  { method: "POST", path: "/v1/projects", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/activate", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/git/push", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/plans", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/plans/{planId}/rounds", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/plans/{planId}/lock", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/beads/conversion-runs", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/beads/polish-runs", enforcement: "required" },
  { method: "PATCH", path: "/v1/projects/{projectId}/beads/{beadId}", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/swarms", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/swarms/{swarmId}/broadcast", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/swarms/{swarmId}/agents/{agentId}/send", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/swarms/{swarmId}/agents/{agentId}/interrupt", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/swarms/{swarmId}/agents/{agentId}/stop", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/mail/messages", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/reservations/force-release", enforcement: "required" },
  { method: "POST", path: "/v1/projects/{projectId}/health/snapshots", enforcement: "required" },
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
