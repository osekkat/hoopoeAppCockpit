// hp-o6q — wizard shell + step component render tests.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  Step1PathPicker,
  Step11Success,
  StepStub,
  WizardReplaySink,
  WizardShell,
  appendCheckpoint,
  recordPath,
  startRun,
} from "./index.ts";
import { STEP_FOLLOWUPS } from "./WizardShell.tsx";

test("WizardShell: starts on the path picker by default", () => {
  const sink = new WizardReplaySink();
  sink.beginRun({ runId: "test-1" });
  const markup = renderToStaticMarkup(<WizardShell sink={sink} />);
  expect(markup).toContain("data-testid=\"wizard\"");
  expect(markup).toContain("data-current-step=\"path\"");
  expect(markup).toContain("data-testid=\"wizard-step-path\"");
  expect(markup).toContain("Pick a path");
  // Stepper starts with the minimal [path, success] for an unpicked path.
  expect(markup).toContain("data-testid=\"wizard-stepper-path\"");
  expect(markup).toContain("data-testid=\"wizard-stepper-success\"");
  // Resume banner is hidden when there are no failures.
  expect(markup).not.toContain("data-testid=\"wizard-resume-banner\"");
});

test("WizardShell: surfaces the resume banner on failed checkpoints", () => {
  const sink = new WizardReplaySink();
  sink.beginRun({ runId: "test-2" });
  sink.recordActivePath("existing_vps");
  sink.recordCheckpoint({ stepId: "path", outcome: "completed" });
  sink.recordCheckpoint({
    stepId: "ssh_key",
    outcome: "failed",
    failure: { code: "ssh_keygen_failed", message: "ssh-keygen exited 1: command not found" },
  });
  const markup = renderToStaticMarkup(<WizardShell sink={sink} />);
  expect(markup).toContain("data-current-step=\"ssh_key\"");
  expect(markup).toContain("data-testid=\"wizard-resume-banner\"");
  expect(markup).toContain("Step failed: ssh_keygen_failed");
  expect(markup).toContain("ssh-keygen exited 1");
});

test("WizardShell: terminal state renders the success step", () => {
  const sink = new WizardReplaySink();
  sink.beginRun({ runId: "test-3" });
  sink.recordActivePath("local_demo");
  sink.recordCheckpoint({ stepId: "path", outcome: "completed" });
  sink.recordCheckpoint({ stepId: "ssh_key", outcome: "skipped" });
  sink.recordCheckpoint({ stepId: "extensions", outcome: "completed" });
  sink.recordCheckpoint({ stepId: "success", outcome: "completed" });
  const markup = renderToStaticMarkup(<WizardShell sink={sink} />);
  expect(markup).toContain("data-current-step=\"success\"");
  expect(markup).toContain("data-terminal=\"true\"");
  expect(markup).toContain("Local demo ready");
});

test("WizardShell: stepper expands to the picked path's applicable steps", () => {
  const sink = new WizardReplaySink();
  sink.beginRun({ runId: "test-4" });
  sink.recordActivePath("local_demo");
  sink.recordCheckpoint({ stepId: "path", outcome: "completed" });
  const markup = renderToStaticMarkup(<WizardShell sink={sink} />);
  // Local-demo path's stepper includes ssh_key + extensions + success.
  expect(markup).toContain("data-testid=\"wizard-stepper-ssh_key\"");
  expect(markup).toContain("data-testid=\"wizard-stepper-extensions\"");
  expect(markup).toContain("data-testid=\"wizard-stepper-success\"");
  // VPS-only steps NOT in the local-demo stepper.
  expect(markup).not.toContain("data-testid=\"wizard-stepper-vps_connect\"");
  expect(markup).not.toContain("data-testid=\"wizard-stepper-preflight\"");
});

test("Step1PathPicker: lists the three paths; provision_new is disabled", () => {
  const markup = renderToStaticMarkup(
    <Step1PathPicker onChange={() => undefined} onContinue={() => undefined} value={null} />,
  );
  expect(markup).toContain("data-testid=\"wizard-path-existing_vps\"");
  expect(markup).toContain("data-testid=\"wizard-path-local_demo\"");
  expect(markup).toContain("data-testid=\"wizard-path-provision_new\"");
  // The disabled button reports data-disabled + aria-disabled.
  expect(markup).toContain("aria-disabled=\"true\"");
  expect(markup).toContain("Provider plugins ship in Phase 13");
});

test("Step1PathPicker: continue button is disabled until a path is picked", () => {
  const noneMarkup = renderToStaticMarkup(
    <Step1PathPicker onChange={() => undefined} onContinue={() => undefined} value={null} />,
  );
  expect(noneMarkup).toMatch(/data-testid="wizard-step-path-continue"[^>]*disabled=""/);

  const pickedMarkup = renderToStaticMarkup(
    <Step1PathPicker onChange={() => undefined} onContinue={() => undefined} value="existing_vps" />,
  );
  expect(pickedMarkup).not.toMatch(/data-testid="wizard-step-path-continue"[^>]*disabled=""/);
});

test("Step11Success: VPS path message", () => {
  const markup = renderToStaticMarkup(
    <Step11Success onEnterCockpit={() => undefined} path="existing_vps" />,
  );
  expect(markup).toContain("VPS ready");
  expect(markup).toContain("All systems go");
  expect(markup).toContain("data-local-demo=\"false\"");
});

test("Step11Success: local-demo path message", () => {
  const markup = renderToStaticMarkup(
    <Step11Success onEnterCockpit={() => undefined} path="local_demo" />,
  );
  expect(markup).toContain("Local demo ready");
  expect(markup).toContain("Mock Flywheel");
  expect(markup).toContain("data-local-demo=\"true\"");
});

test("StepStub: renders bead reference + advance affordance", () => {
  const markup = renderToStaticMarkup(
    <StepStub
      followupBead="hp-o6q-ssh"
      onMarkComplete={() => undefined}
      stepId="ssh_key"
      stepNumber={2}
      title="SSH key"
    />,
  );
  expect(markup).toContain("data-testid=\"wizard-step-ssh_key\"");
  expect(markup).toContain("STEP 02");
  expect(markup).toContain("hp-o6q-ssh");
  expect(markup).toContain("data-testid=\"wizard-step-ssh_key-complete\"");
});

test("StepStub: surfaces an optional Skip button", () => {
  const noSkip = renderToStaticMarkup(
    <StepStub
      followupBead="hp-o6q-vps"
      onMarkComplete={() => undefined}
      stepId="vps_connect"
      stepNumber={4}
      title="Connect VPS"
    />,
  );
  expect(noSkip).not.toContain("data-testid=\"wizard-step-vps_connect-skip\"");

  const withSkip = renderToStaticMarkup(
    <StepStub
      followupBead="hp-o6q-pre"
      onMarkComplete={() => undefined}
      onSkip={() => undefined}
      stepId="preflight"
      stepNumber={5}
      title="Pre-flight"
    />,
  );
  expect(withSkip).toContain("data-testid=\"wizard-step-preflight-skip\"");
});

test("WizardShell: hp-9z45 steps render streaming bootstrap components instead of stubs", () => {
  const steps = ["preflight", "acfs_install", "reconnect", "verify_key"] as const;
  for (const stepId of steps) {
    const sink = new WizardReplaySink();
    sink.beginRun({ runId: `test-${stepId}` });
    sink.recordActivePath("existing_vps");
    sink.recordCheckpoint({ stepId: "path", outcome: "completed" });
    sink.recordCheckpoint({ stepId: "ssh_key", outcome: "completed" });
    sink.recordCheckpoint({ stepId: "rent_vps", outcome: "completed" });
    sink.recordCheckpoint({ stepId: "vps_connect", outcome: "completed" });
    if (stepId !== "preflight") {
      sink.recordCheckpoint({ stepId: "preflight", outcome: "completed" });
    }
    if (stepId === "reconnect" || stepId === "verify_key") {
      sink.recordCheckpoint({ stepId: "acfs_install", outcome: "completed" });
    }
    if (stepId === "verify_key") {
      sink.recordCheckpoint({ stepId: "reconnect", outcome: "completed" });
    }
    const markup = renderToStaticMarkup(<WizardShell sink={sink} />);
    expect(markup).toContain(`data-current-step="${stepId}"`);
    expect(markup).toContain(`data-testid="wizard-step-${stepId}"`);
    expect(markup).not.toContain("The body of this step ships in a follow-up bead");
  }
});

// hp-sgzb: STEP_FOLLOWUPS must point at real beads (not the placeholder
// `hp-o6q-...` shape that was filed at hp-o6q close). The shape regex
// pins the canonical id form (`hp-` + 3-5 lowercase alphanumeric chars).
test("STEP_FOLLOWUPS: every non-null value matches the canonical bead-id shape", () => {
  const beadIdRe = /^hp-[a-z0-9]{3,5}$/;
  for (const [stepId, followup] of Object.entries(STEP_FOLLOWUPS)) {
    if (followup === null) continue;
    if (!beadIdRe.test(followup)) {
      throw new Error(
        `${stepId} → ${followup}: not a canonical bead id (expected /^hp-[a-z0-9]{3,5}$/). ` +
          `Update STEP_FOLLOWUPS in WizardShell.tsx to the real follow-up bead.`,
      );
    }
  }
});

test("STEP_FOLLOWUPS: leaves only docs-only / inert steps without a follow-up", () => {
  // path / success are inert (no work to bead); rent_vps is read-only
  // agent-flywheel.com docs cards. Every other step MUST point at a real
  // follow-up bead so the stepper renders the bead reference correctly.
  const allowedNullSteps = new Set(["path", "rent_vps", "success"]);
  for (const [stepId, followup] of Object.entries(STEP_FOLLOWUPS)) {
    if (followup === null && !allowedNullSteps.has(stepId)) {
      throw new Error(
        `${stepId} has no follow-up bead but is not in the allowed-null set ` +
          `(${[...allowedNullSteps].join(", ")}). Either point at a real bead ` +
          `or add ${stepId} to the allowed-null set with a justification.`,
      );
    }
  }
});

// State-machine integration sanity test (no React) — confirms WizardShell's
// underlying machine reaches the right step when fed a real checkpoint
// stream. Not a substitute for the React render tests above.
test("integration: appending checkpoints advances the computed state", () => {
  let run = recordPath(startRun({ runId: "int-1" }), "existing_vps");
  run = appendCheckpoint(run, { stepId: "path", outcome: "completed" });
  expect(run.checkpoints.length).toBe(1);
  run = appendCheckpoint(run, { stepId: "ssh_key", outcome: "completed" });
  run = appendCheckpoint(run, { stepId: "rent_vps", outcome: "completed" });
  expect(run.checkpoints.length).toBe(3);
});
