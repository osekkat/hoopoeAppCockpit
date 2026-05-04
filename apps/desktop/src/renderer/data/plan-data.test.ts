import { expect, test } from "bun:test";
import healthyHourPlan001 from "../../../../../packages/fixtures/scenarios/healthy-hour/plans/plan-001.bundle.json" with {
  type: "json",
};
import healthyHourPlanMeta from "../../../../../packages/fixtures/scenarios/healthy-hour/plans/meta.json" with {
  type: "json",
};
import {
  isPlanProjectId,
  type PlanArtifact,
  type PlanArtifactStatus,
  type PlanBundle,
  type PlanLockState,
} from "./plan-data.ts";

test("healthy-hour plan meta exposes the expected scenario + plan identifiers", () => {
  expect(healthyHourPlanMeta.scenarioId).toBe("healthy-hour");
  expect(healthyHourPlanMeta.plans).toHaveLength(2);
  expect(healthyHourPlanMeta.plans.map((plan) => plan.planId)).toEqual([
    "plan-001",
    "plan-002",
  ]);
  expect(healthyHourPlanMeta.plans[0]?.lockState).toBe("locked");
  expect(healthyHourPlanMeta.plans[0]?.active).toBe(true);
});

test("plan-001 bundle has all required §7.1 artifact kinds + a complete history", () => {
  const bundle = healthyHourPlan001 as unknown as PlanBundle;

  const kinds = new Set(bundle.artifacts.map((artifact) => artifact.kind));
  expect(kinds.has("plan")).toBe(true);
  expect(kinds.has("rough-idea")).toBe(true);
  expect(kinds.has("candidate")).toBe(true);
  expect(kinds.has("comparative-matrix")).toBe(true);
  expect(kinds.has("synthesis")).toBe(true);
  expect(kinds.has("fresh-eyes-critique")).toBe(true);
  expect(kinds.has("refinement-round")).toBe(true);
  expect(kinds.has("unresolved-decisions")).toBe(true);

  const candidateCount = bundle.artifacts.filter((artifact) => artifact.kind === "candidate")
    .length;
  expect(candidateCount).toBeGreaterThanOrEqual(3);

  const validStatuses: ReadonlySet<PlanArtifactStatus> = new Set([
    "queued",
    "running",
    "completed",
    "failed",
    "skipped",
  ]);
  for (const artifact of bundle.artifacts) {
    expect(validStatuses.has(artifact.status)).toBe(true);
  }

  expect(bundle.history.length).toBeGreaterThan(0);
  expect(bundle.history.some((entry) => entry.kind === "plan_locked")).toBe(true);
});

test("plan-001 candidates carry model + harness + CAAM metadata for the comparative matrix", () => {
  const bundle = healthyHourPlan001 as unknown as PlanBundle;
  const candidates = bundle.artifacts.filter(
    (artifact): artifact is PlanArtifact & { readonly kind: "candidate" } =>
      artifact.kind === "candidate",
  );
  for (const candidate of candidates) {
    expect(candidate.model).toBeTruthy();
    expect(candidate.harness).toBeTruthy();
    expect(candidate.caamAccount).toBeTruthy();
    expect(typeof candidate.latencyMs).toBe("number");
  }
});

test("plan-001 lock state is LOCKED so the editor must render read-only by default", () => {
  const bundle = healthyHourPlan001 as unknown as PlanBundle;
  const lockState: PlanLockState = bundle.lockState;
  expect(lockState).toBe("locked");
  expect(bundle.lockedAt).not.toBeNull();
});

test("isPlanProjectId gates the Mock Flywheel project ids the renderer wires up", () => {
  expect(isPlanProjectId("local-demo")).toBe(true);
  expect(isPlanProjectId("mock-flywheel-project")).toBe(true);
  expect(isPlanProjectId("real-vps-project")).toBe(false);
});
