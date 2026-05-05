// `@hoopoe/fixtures` — §8.8 scenario semantic-invariants test (hp-req).
//
// fixture-quality.test.ts asserts every scenario directory has the right
// files, JSON parses, and basic shape rules hold. golden-replay.test.ts
// asserts the replay output is byte-identical to a committed golden. Both
// pass when the goldens are stable, even if the underlying scenario
// fixture is semantically wrong against plan.md §8.8.
//
// This file closes that gap: each scenario is paired with an explicit
// invariant set drawn from §8.8's expected behaviors (detections,
// wake/no-wake, ActionPlan validity, approvals, postcondition checks,
// safety-arbitration, follow-up-detection sourceActionId chaining,
// activityBehavior). When a future fixture edit breaks one of these
// invariants the test fails, even if the golden has been re-blessed.
//
// Cross-references:
// - bead hp-req
// - plan.md §8.8 (Mock Flywheel tending scenario catalog)
// - plan.md §8.3.1 (ActionPlan + postcondition contract)
// - Guardrail 10 (audit always fires regardless of [SILENT])

import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { fixturesRoot } from "../src/loader.ts";
import { TENDING_SCENARIOS, type TendingScenarioId } from "../src/kinds.ts";

interface ExpectedAction {
  kind: string;
  idempotencyKey?: string;
  target?: Record<string, unknown>;
  args?: Record<string, unknown>;
  postconditions?: unknown[];
  sourceActionId?: string | null;
  requiresApproval?: boolean;
}

interface ExpectedActionPlan {
  schemaVersion?: number;
  jobId?: string;
  runId?: string;
  summary?: string;
  riskClass?: string;
  requiresApproval?: boolean;
  actions?: ExpectedAction[];
}

interface ExpectedDetection {
  kind: string;
  payload?: Record<string, unknown>;
}

interface ExpectedPostcondition {
  check: string;
  expect: boolean;
  scope?: string;
  reason?: string;
}

interface ExpectedApproval {
  scope?: string;
  reason?: string;
}

type ActivityBehavior = "silent" | "surface" | "audit_only" | "diagnostics_only";

interface ExpectedOutcome {
  meta?: Record<string, unknown>;
  detections?: ExpectedDetection[];
  wakeAgent: boolean;
  actionPlan?: ExpectedActionPlan;
  approvalsRequested?: ExpectedApproval[];
  postconditions?: ExpectedPostcondition[];
  activityBehavior?: ActivityBehavior;
}

const FIXTURES_ROOT = fixturesRoot();

function readExpectedOutcome(scenarioId: TendingScenarioId): ExpectedOutcome {
  const path = join(FIXTURES_ROOT, "scenarios", scenarioId, "expected-outcome.json");
  return JSON.parse(readFileSync(path, "utf8")) as ExpectedOutcome;
}

function actionKinds(outcome: ExpectedOutcome): string[] {
  return (outcome.actionPlan?.actions ?? []).map((a) => a.kind);
}

function postconditionChecks(outcome: ExpectedOutcome): string[] {
  return (outcome.postconditions ?? []).map((p) => p.check);
}

function detectionKinds(outcome: ExpectedOutcome): string[] {
  return (outcome.detections ?? []).map((d) => d.kind);
}

// ─── Cross-cutting invariants: must hold for every populated scenario ───

describe("§8.8 cross-cutting invariants (hp-req)", () => {
  for (const scenarioId of TENDING_SCENARIOS) {
    test(`${scenarioId}: structural shape`, () => {
      const outcome = readExpectedOutcome(scenarioId);
      expect(typeof outcome.wakeAgent).toBe("boolean");
      // Every postcondition is {check: string, expect: <boolean|number>}.
      // commit-burst uses numeric counters (jobs_enqueued, llm_wake_count);
      // most scenarios use boolean asserts. Both are §8.8-legal.
      for (const pc of outcome.postconditions ?? []) {
        expect(typeof pc.check).toBe("string");
        expect(pc.check.length).toBeGreaterThan(0);
        expect(["boolean", "number", "string"]).toContain(typeof pc.expect);
      }
      // Every action has a non-empty kind.
      for (const action of outcome.actionPlan?.actions ?? []) {
        expect(typeof action.kind).toBe("string");
        expect(action.kind.length).toBeGreaterThan(0);
      }
    });

    test(`${scenarioId}: wakeAgent=true requires at least one action`, () => {
      const outcome = readExpectedOutcome(scenarioId);
      if (outcome.wakeAgent) {
        const actions = outcome.actionPlan?.actions ?? [];
        expect(actions.length).toBeGreaterThan(0);
      }
    });

    test(`${scenarioId}: every approvalsRequested entry has a non-empty scope`, () => {
      // Plan.md §8.3.1 — approvals must be attributable to a scope so
      // the executor / Activity panel can route them. The plan-level
      // requiresApproval flag is independent: a plan can run a
      // non-blocking action (e.g. agent.pause) while still requesting an
      // approval for an out-of-band repair scope (e.g. skill-drift's
      // `jsm.repin`).
      const outcome = readExpectedOutcome(scenarioId);
      const approvals = outcome.approvalsRequested ?? [];
      for (const approval of approvals) {
        expect(typeof approval.scope).toBe("string");
        expect(approval.scope?.length ?? 0).toBeGreaterThan(0);
      }
    });

    test(`${scenarioId}: swarm.halt is unconditional (requiresApproval=false)`, () => {
      // Plan.md §8.3.1 + §8.8 — swarm.halt is the executor's safety
      // override. It must not be gated on a human approval; if it were,
      // a budget breach or safety-arbitration scenario could stall while
      // waiting for approval, defeating the purpose of the halt.
      const outcome = readExpectedOutcome(scenarioId);
      const haltActions = (outcome.actionPlan?.actions ?? []).filter(
        (a) => a.kind === "swarm.halt",
      );
      if (haltActions.length > 0) {
        expect(outcome.actionPlan?.requiresApproval ?? false).toBe(false);
      }
    });
  }
});

// ─── Per-scenario semantic invariants ───
//
// Each clause is a §8.8 commitment that must remain true for the named
// scenario. Edit one of these only when plan.md §8.8 itself changes.

describe("§8.8 per-scenario invariants (hp-req)", () => {
  test("budget-breach: deterministic swarm.halt without approval", () => {
    const outcome = readExpectedOutcome("budget-breach");
    expect(outcome.wakeAgent).toBe(false);
    expect(actionKinds(outcome)).toContain("swarm.halt");
    expect(outcome.actionPlan?.requiresApproval ?? false).toBe(false);
    // §8.8 budget-breach: post-execution check that NTM swarm halted.
    expect(
      postconditionChecks(outcome).some((c) => c.includes("HALTED") || c.includes("halted")),
    ).toBe(true);
    // Detection chain must include the budget signal (caut.budget_breach).
    expect(detectionKinds(outcome)).toContain("caut.budget_breach");
  });

  test("action-arbitration: superseded_by safety arbitration recorded", () => {
    const outcome = readExpectedOutcome("action-arbitration");
    expect(actionKinds(outcome)).toContain("swarm.halt");
    expect(outcome.actionPlan?.requiresApproval ?? false).toBe(false);
    // The losing recovery plan must be marked superseded_by the winning
    // safety plan — that is the whole point of action arbitration.
    expect(
      postconditionChecks(outcome).some((c) => c.includes("superseded_by")),
    ).toBe(true);
    expect(
      postconditionChecks(outcome).some((c) => c.includes("status == superseded")),
    ).toBe(true);
  });

  test("stale-reservation: force-release gated by approval", () => {
    const outcome = readExpectedOutcome("stale-reservation");
    expect(actionKinds(outcome)).toContain("reservation.force_release");
    // Force-releasing a peer agent's reservation is destructive; §8.8
    // requires an approval before the executor proceeds.
    expect((outcome.approvalsRequested ?? []).length).toBeGreaterThan(0);
    expect(
      (outcome.approvalsRequested ?? []).some(
        (a) => a.scope === "reservation.force_release",
      ),
    ).toBe(true);
    // Postcondition must verify the reservation actually disappeared.
    expect(
      postconditionChecks(outcome).some(
        (c) => c.includes("reservation") && c.includes("absent"),
      ),
    ).toBe(true);
  });

  test("postcondition-failure: follow-up detection chains via sourceActionId", () => {
    const outcome = readExpectedOutcome("postcondition-failure");
    // §8.3.1 — when a postcondition fails the daemon emits a follow-up
    // detection that the next tend tick can pick up. The follow-up must
    // carry the sourceActionId so the chain is auditable.
    const followUp = (outcome.detections ?? []).find(
      (d) => d.kind === "action_plan.follow_up_detection",
    );
    expect(followUp).toBeDefined();
    expect(typeof followUp?.payload?.sourceActionId).toBe("string");
    expect((followUp?.payload?.sourceActionId as string).length).toBeGreaterThan(0);
    // The postcondition check explicitly references the chain.
    expect(
      postconditionChecks(outcome).some((c) => c.includes("sourceActionId")),
    ).toBe(true);
    // Plan.md §8.3.1 — rollback fires regardless of wake decision.
    expect(detectionKinds(outcome)).toContain("action_plan.rollback");
    expect(detectionKinds(outcome)).toContain("action_plan.postcondition_failed");
  });

  test("wedged-pane: kill + force-release with approvals", () => {
    const outcome = readExpectedOutcome("wedged-pane");
    expect(outcome.wakeAgent).toBe(true);
    const kinds = actionKinds(outcome);
    expect(kinds).toContain("agent.kill_wedged_process");
    expect(kinds).toContain("reservation.force_release");
    // Both destructive actions need approval.
    expect((outcome.approvalsRequested ?? []).length).toBeGreaterThanOrEqual(2);
  });

  test("rate-limited-with-caam: switches CAAM account behind approval", () => {
    const outcome = readExpectedOutcome("rate-limited-with-caam");
    expect(outcome.wakeAgent).toBe(true);
    expect(actionKinds(outcome)).toContain("caam.switch_account");
    expect((outcome.approvalsRequested ?? []).length).toBeGreaterThan(0);
  });

  test("rate-limited-no-caam: pauses agent (no CAAM alternative)", () => {
    const outcome = readExpectedOutcome("rate-limited-no-caam");
    expect(outcome.wakeAgent).toBe(true);
    expect(actionKinds(outcome)).toContain("agent.pause");
  });

  test("skill-drift: pauses agent with approval", () => {
    const outcome = readExpectedOutcome("skill-drift");
    expect(actionKinds(outcome)).toContain("agent.pause");
    expect((outcome.approvalsRequested ?? []).length).toBeGreaterThan(0);
  });

  test("missing-tool: deterministic no-wake surface", () => {
    const outcome = readExpectedOutcome("missing-tool");
    expect(outcome.wakeAgent).toBe(false);
    expect((outcome.actionPlan?.actions ?? []).length).toBe(0);
    expect(outcome.activityBehavior).toBe("surface");
  });

  test("commit-burst: silent-but-audited (Guardrail 10)", () => {
    const outcome = readExpectedOutcome("commit-burst");
    expect(outcome.wakeAgent).toBe(false);
    // Commit burst is high-volume but not surfaced — Guardrail 10 says
    // audit must still fire even when the Activity panel is quiet.
    expect(outcome.activityBehavior).toBe("audit_only");
  });

  test("healthy-hour: silent zero-cost tick", () => {
    const outcome = readExpectedOutcome("healthy-hour");
    expect(outcome.wakeAgent).toBe(false);
    expect((outcome.actionPlan?.actions ?? []).length).toBe(0);
    expect(outcome.activityBehavior).toBe("silent");
  });

  test("idle-but-not-stuck: silent observation only", () => {
    const outcome = readExpectedOutcome("idle-but-not-stuck");
    expect(outcome.wakeAgent).toBe(false);
    expect((outcome.actionPlan?.actions ?? []).length).toBe(0);
    expect(outcome.activityBehavior).toBe("silent");
  });
});
