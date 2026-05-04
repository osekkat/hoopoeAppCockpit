// hp-o6q — wizard step machine tests.

import { expect, test } from "bun:test";
import {
  WIZARD_STEP_IDS,
  appendCheckpoint,
  applicableStepsFor,
  canonicalNextStep,
  computeWizardState,
  lastCheckpointForStep,
  recordPath,
  startRun,
} from "./index.ts";

test("applicableStepsFor: unknown path returns minimal [path, success]", () => {
  expect(applicableStepsFor(null)).toEqual(["path", "success"]);
});

test("applicableStepsFor: existing_vps + provision_new walk every step", () => {
  expect(applicableStepsFor("existing_vps")).toEqual(WIZARD_STEP_IDS);
  expect(applicableStepsFor("provision_new")).toEqual(WIZARD_STEP_IDS);
});

test("applicableStepsFor: local_demo skips VPS-bound steps", () => {
  const steps = applicableStepsFor("local_demo");
  expect(steps).toContain("path");
  expect(steps).toContain("ssh_key");
  expect(steps).toContain("extensions");
  expect(steps).toContain("success");
  expect(steps).not.toContain("vps_connect");
  expect(steps).not.toContain("preflight");
  expect(steps).not.toContain("acfs_install");
});

test("computeWizardState: fresh run with no checkpoints starts on `path`", () => {
  const run = startRun({ runId: "r" });
  const state = computeWizardState(run);
  expect(state.currentStep).toBe("path");
  expect(state.terminal).toBe(false);
  expect(state.resumable).toBe(false);
  expect(state.completedSteps).toEqual([]);
});

test("computeWizardState: completed `path` advances current to next applicable step", () => {
  let run = recordPath(startRun({ runId: "r" }), "existing_vps");
  run = appendCheckpoint(run, { stepId: "path", outcome: "completed" });
  const state = computeWizardState(run);
  expect(state.currentStep).toBe("ssh_key");
  expect(state.completedSteps).toEqual(["path"]);
});

test("computeWizardState: failed checkpoint pins current step + resumable=true", () => {
  let run = recordPath(startRun({ runId: "r" }), "existing_vps");
  run = appendCheckpoint(run, { stepId: "path", outcome: "completed" });
  run = appendCheckpoint(run, {
    stepId: "ssh_key",
    outcome: "failed",
    failure: { code: "keygen_failed", message: "ssh-keygen exited 1" },
  });
  const state = computeWizardState(run);
  expect(state.currentStep).toBe("ssh_key");
  expect(state.resumable).toBe(true);
  expect(state.lastCheckpoint?.failure?.code).toBe("keygen_failed");
});

test("computeWizardState: skipped checkpoints count as completed for advancement", () => {
  let run = recordPath(startRun({ runId: "r" }), "local_demo");
  run = appendCheckpoint(run, { stepId: "path", outcome: "completed", data: { path: "local_demo" } });
  run = appendCheckpoint(run, { stepId: "ssh_key", outcome: "skipped" });
  // local_demo's applicable steps are [path, ssh_key, extensions, success];
  // both completed/skipped → current is `extensions`.
  expect(computeWizardState(run).currentStep).toBe("extensions");
});

test("computeWizardState: terminal when every applicable step is completed/skipped", () => {
  let run = recordPath(startRun({ runId: "r" }), "local_demo");
  run = appendCheckpoint(run, { stepId: "path", outcome: "completed" });
  run = appendCheckpoint(run, { stepId: "ssh_key", outcome: "skipped" });
  run = appendCheckpoint(run, { stepId: "extensions", outcome: "completed" });
  run = appendCheckpoint(run, { stepId: "success", outcome: "completed" });
  const state = computeWizardState(run);
  expect(state.currentStep).toBe("success");
  expect(state.terminal).toBe(true);
});

test("computeWizardState: latest checkpoint wins for a step (retries override prior failure)", () => {
  let run = recordPath(startRun({ runId: "r" }), "existing_vps");
  run = appendCheckpoint(run, { stepId: "path", outcome: "completed" });
  run = appendCheckpoint(run, {
    stepId: "ssh_key",
    outcome: "failed",
    failure: { code: "k", message: "first try" },
  });
  // User retries successfully.
  run = appendCheckpoint(run, { stepId: "ssh_key", outcome: "completed" });
  const state = computeWizardState(run);
  expect(state.currentStep).toBe("rent_vps");
  expect(state.completedSteps).toEqual(["path", "ssh_key"]);
});

test("lastCheckpointForStep: returns null when the step hasn't appeared", () => {
  const run = startRun({ runId: "r" });
  expect(lastCheckpointForStep(run.checkpoints, "preflight")).toBeNull();
});

test("canonicalNextStep: walks the canonical order ignoring path applicability", () => {
  expect(canonicalNextStep("path")).toBe("ssh_key");
  expect(canonicalNextStep("ssh_key")).toBe("rent_vps");
  expect(canonicalNextStep("success")).toBeNull();
});
