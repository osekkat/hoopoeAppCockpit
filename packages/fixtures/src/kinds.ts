// `@hoopoe/fixtures` — fixture-kind taxonomy (hp-wle).
//
// This file pins the names + structure of every fixture file the corpus
// produces. Mock Flywheel Mode (`plan.md` §13, hp-dr8) and the daemon's
// adapter contract tests (`plan.md` §18.3, hp-pl5o) consume this module.
//
// `index.ts` re-exports a subset for hp-xru's smoke test; the
// authoritative taxonomy lives here.
//
// Cross-references:
// - `plan.md` §2.8 capability-registry shape.
// - `plan.md` §8.8 tending-evaluation scenarios.
// - `docs/integration-contracts/` per-adapter contracts.
// - `packages/fixtures/README.md` per-scenario + per-golden-output file
//   contracts.

/** Stable adapter slugs; mirror snapshot.sh's `ALL_TOOLS`. */
export const ADAPTER_SLUGS = [
  "git",
  "br",
  "bv",
  "ntm",
  "agent_mail",
  "ru",
  "health",
  "caut",
  "caam",
  "dcg",
  "casr",
  "ubs",
  "jsm",
  "jfp",
  "oracle",
  "pt",
  "srp",
  "sbh",
] as const;

export type AdapterSlug = (typeof ADAPTER_SLUGS)[number];

/** §8.8 tending-evaluation scenarios. Stable scenario IDs used as directory names under `scenarios/`. */
export const TENDING_SCENARIOS = [
  "healthy-hour",
  "idle-but-not-stuck",
  "wedged-pane",
  "rate-limited-no-caam",
  "rate-limited-with-caam",
  "stale-reservation",
  "commit-burst",
  "budget-breach",
  "skill-drift",
  "missing-tool",
  "postcondition-failure",
  "action-arbitration",
] as const;

export type TendingScenarioId = (typeof TENDING_SCENARIOS)[number];

/** Real-VPS scenario classes (per `plan.md` §16 Phase 0). */
export const PHASE0_SCENARIOS = ["fresh", "active", "failure"] as const;

export type Phase0ScenarioId = (typeof PHASE0_SCENARIOS)[number];

/** The six per-adapter golden-output states required by `plan.md` §18.3. */
export const GOLDEN_OUTPUT_STATES = [
  "normal",
  "missing-tool",
  "unsupported-version",
  "malformed-json",
  "timeout",
  "high-volume",
] as const;

export type GoldenOutputState = (typeof GOLDEN_OUTPUT_STATES)[number];

/** Fixture-kind enum extended from hp-xru's smoke shape. */
export const FIXTURE_KINDS = [
  // git
  "git_status",
  "git_diff",
  "git_log",
  "git_remote",
  // br
  "br_list",
  "br_ready",
  "br_cycles",
  "br_stats",
  "br_schema",
  "br_info",
  // bv
  "bv_robot_triage",
  "bv_robot_plan",
  "bv_robot_insights",
  "bv_robot_diff",
  "bv_robot_priority",
  "bv_robot_recipes",
  // ntm
  "ntm_robot_snapshot",
  "ntm_robot_status",
  "ntm_robot_tail",
  "ntm_sessions_list",
  // agent_mail
  "agent_mail_dump",
  "agent_mail_threads",
  "file_reservations",
  // ru
  "ru_sync",
  "ru_status",
  "ru_list",
  "ru_prune",
  "ru_schema",
  // safety + accounts
  "caam_accounts_list",
  "caam_account_status",
  "caut_usage",
  "dcg_verdicts",
  "casr_sessions",
  "ubs_findings",
  "pt_list",
  "srp_signals",
  "sbh_status",
  // skills
  "jsm_list",
  "jfp_list",
  // oracle
  "oracle_serve_status",
  "oracle_browser_run",
  // health (cross-language)
  "health_lizard",
  "health_scc",
  "health_tokei",
  // health (per-language)
  "health_ts_coverage",
  "health_ts_complexity",
  "health_python_coverage",
  "health_python_complexity",
  "health_rust_coverage",
  "health_rust_complexity",
  "health_go_coverage",
  "health_go_complexity",
  // tending evaluation
  "events_jsonl",
  "pane_log_bin",
  "build_log_txt",
  "capabilities_snapshot",
  "tools_degraded",
  "expected_outcome",
  // hp-k3u: per-scenario canonical-state snapshots that §8 tending
  // decisions read (stale commits, budget gating, code-health flips,
  // build-queue contention). Without these, fixture replay can pass
  // without exercising the canonical Git / health / build-queue inputs
  // plan.md §8.8 requires.
  "git_state",
  "health_snapshot",
  "build_queue_state",
] as const;

export type FixtureKind = (typeof FIXTURE_KINDS)[number];

/** Per-fixture metadata header (every JSON fixture starts with this). */
export interface FixtureMeta {
  /** `realistic` = captured from a real CLI on a real VPS or dev box.
   *  `synthetic` = hand-written for scenarios real systems cannot exhibit on demand.
   *  `stub` = schema-valid placeholder waiting for VPS-pinned ground truth. */
  kind: "realistic" | "synthetic" | "stub";
  /** Stable scenario or golden-output state ID. */
  scenario?: TendingScenarioId | Phase0ScenarioId | GoldenOutputState;
  /** Tag matching the directory name (e.g. `phase0-2026-05-02`). */
  fixturesVersion: string;
  /** RFC3339 capture timestamp. */
  capturedAt: string;
  /** Stable VPS identifier the capture came from, or `mock-flywheel`. */
  vpsId?: string;
  /** Free-form provenance (e.g. `snapshot.sh --self-test (local)` / `hand-written` / `real-VPS <id>`). */
  source: string;
  /** Free-form notes (deliberate breakage, peculiarities). */
  notes?: string;
}

/** Metadata for one of the six per-adapter golden-output states. */
export interface GoldenOutputMeta extends FixtureMeta {
  adapter: AdapterSlug;
  state: GoldenOutputState;
}

/** A single-invocation envelope (mirror of scripts/research-spike/schema/snapshot.schema.json#$defs/InvocationCapture). */
export interface InvocationEnvelope {
  argv: string[];
  exit: number;
  durationMs: number;
  stdoutBytes: number;
  stderrBytes: number;
  /** Parsed JSON when the invocation produced JSON. */
  stdoutJson?: unknown;
  /** Raw stdout. */
  stdoutText?: string;
  stderrText?: string;
  truncated?: boolean;
  redacted?: boolean;
  tags?: string[];
}

/** Top-level shape of `golden-outputs/<adapter>/<state>.json`. */
export interface GoldenOutputFixture extends InvocationEnvelope {
  meta: GoldenOutputMeta;
  /** Capabilities the adapter is expected to report after consuming this fixture
   *  (per `plan.md` §2.8). Adapter contract tests assert this — not just parser success. */
  capabilities?: Record<string, { status: "ok" | "degraded" | "missing" | "blocked-by-policy" | "untested" }>;
}

/** Top-level shape of `scenarios/<id>/git-state.json` (hp-k3u). Captures
 *  the canonical Git working-state inputs §8 tending jobs read when
 *  deciding stale-commit pushes, push-policy enforcement, and review
 *  flips. Synthetic — real Phase 0 captures replace this once the real
 *  ACFS VPS pack lands. */
export interface GitStateSnapshot {
  meta: FixtureMeta;
  /** Current HEAD commit SHA (40-char synthetic). */
  head: string;
  /** Branch name HEAD is pointing at. */
  branch: string;
  /** Commits HEAD is ahead of `origin/<branch>`. */
  ahead: number;
  /** Commits `origin/<branch>` is ahead of HEAD. */
  behind: number;
  /** True iff working tree has uncommitted modifications. */
  dirty: boolean;
  /** Files modified relative to HEAD when `dirty` is true. */
  uncommittedFiles: readonly string[];
  /** Local commits that have not been pushed (drives the stale-commit-push tend job). */
  stalePushes: readonly { sha: string; subject: string; ageMinutes: number }[];
}

/** Top-level shape of `scenarios/<id>/health-snapshot.json` (hp-k3u).
 *  Captures the latest cross-language code-health snapshot the
 *  snapshot-health tending job reads. */
export interface HealthSnapshotFixture {
  meta: FixtureMeta;
  /** `healthy` / `warning` / `critical` / `unknown` — drives the topbar
   *  pill color and the Hardening review-mode flip threshold. */
  verdict: "healthy" | "warning" | "critical" | "unknown";
  /** Aggregate test coverage 0-100, or null when no run yet. */
  coveragePercent: number | null;
  /** Aggregate cyclomatic complexity, or null when no run yet. */
  avgComplexity: number | null;
  /** Count of hotspot files above the project's threshold. */
  hotspotCount: number;
  /** Minutes since the last snapshot landed (null = never). */
  lastSnapshotAgeMinutes: number | null;
  /** Optional per-language metrics. */
  perLanguage: readonly {
    language: string;
    coveragePercent: number | null;
    avgComplexity: number | null;
  }[];
}

/** Top-level shape of `scenarios/<id>/build-queue-state.json` (hp-k3u).
 *  Captures the daemon build queue snapshot §8 tending jobs read when
 *  deciding rate-limit/budget arbitration and showing build-contention
 *  warnings on swarm launch. */
export interface BuildQueueStateSnapshot {
  meta: FixtureMeta;
  /** Total queued + running. */
  queueDepth: number;
  /** Currently-running build/test jobs. */
  running: readonly {
    id: string;
    kind: "build" | "test" | "lint" | "typecheck";
    startedAt: string;
    elapsedMinutes: number;
  }[];
  /** Queued (not yet running) build/test jobs. */
  queued: readonly {
    id: string;
    kind: "build" | "test" | "lint" | "typecheck";
    enqueuedAt: string;
    waitingMinutes: number;
  }[];
}

/** Top-level shape of `scenarios/<id>/expected-outcome.json` (sketched here; refined in hp-dr8). */
export interface ExpectedOutcome {
  meta: FixtureMeta;
  /** Detections the tending pre-script should emit. */
  detections: Array<{ kind: string; payload?: unknown }>;
  /** Whether the pre-script should wake the LLM tending agent. */
  wakeAgent: boolean;
  /** Typed ActionPlan the tending job should propose or execute (`plan.md` §8.3.1). */
  actionPlan?: {
    schemaVersion: 1;
    jobId: string;
    runId: string;
    summary: string;
    riskClass: "low" | "medium" | "high" | "critical";
    requiresApproval?: boolean;
    evidenceRefs?: string[];
    actions: Array<{
      kind:
        | "agent.ask_status"
        | "agent.send_marching_orders"
        | "agent.pause"
        | "agent.kill_wedged_process"
        | "reservation.force_release"
        | "caam.switch_account"
        | "casr.resume_session"
        | "git.push_branch"
        | "swarm.halt"
        | "review.propose_flip"
        | "bead.create_blocker";
      target: Record<string, unknown>;
      args?: Record<string, unknown>;
      idempotencyKey: string;
      preconditions?: string[];
      postconditions?: string[];
    }>;
  };
  /** Approvals the daemon should raise before executing. */
  approvalsRequested: Array<{ scope: string; reason: string }>;
  /** Postconditions the daemon should verify against canonical state after execution. */
  postconditions: Array<{ check: string; expect: unknown }>;
  /** Activity panel behavior: `silent` keeps it quiet; `audit_only` writes audit but no panel; `surface` emits a panel entry. */
  activityBehavior: "silent" | "audit_only" | "surface";
}
