// hp-9z45 — streaming bootstrap wizard step tests.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  BOOTSTRAP_STEP_CONFIGS,
  StepBootstrapStream,
  buildBootstrapCheckpointData,
  buildBootstrapFailure,
  invokeBootstrapStep,
  isBootstrapStepId,
  summarizeBootstrapEvents,
  type BootstrapStepBridge,
  type BootstrapStepId,
  type BootstrapStreamEvent,
} from "./StepBootstrapStream.tsx";

const SAMPLE_EVENTS: readonly BootstrapStreamEvent[] = [
  {
    seq: 1,
    phaseId: "ssh",
    label: "SSH identity",
    status: "passed",
    message: "Key accepted",
    evidenceRef: "bootstrap:run-1:1",
  },
  {
    seq: 2,
    phaseId: "acfs",
    label: "ACFS phase",
    status: "warning",
    message: "Optional cache warmup skipped",
    evidenceRef: "bootstrap:run-1:2",
  },
];

test("StepBootstrapStream: every hp-9z45 step renders a real component shell", () => {
  for (const stepId of Object.keys(BOOTSTRAP_STEP_CONFIGS) as BootstrapStepId[]) {
    const html = renderToStaticMarkup(
      <StepBootstrapStream
        runId="run-1"
        stepId={stepId}
        onComplete={() => undefined}
        onFailed={() => undefined}
      />,
    );
    expect(html).toContain(`data-testid="wizard-step-${stepId}"`);
    expect(html).toContain(BOOTSTRAP_STEP_CONFIGS[stepId].title);
    expect(html).toContain(BOOTSTRAP_STEP_CONFIGS[stepId].endpointLabel);
    expect(html).toContain(`data-testid="wizard-${stepId}-run"`);
    expect(html).not.toContain("The body of this step ships in a follow-up bead");
  }
});

test("StepBootstrapStream: renders streamed phases and doctor checks", () => {
  const html = renderToStaticMarkup(
    <StepBootstrapStream
      initialEvents={[
        ...SAMPLE_EVENTS,
        {
          seq: 3,
          phaseId: "doctor",
          label: "acfs doctor",
          status: "passed",
          doctor: [
            { id: "key", label: "SSH key", status: "ok", detail: "fingerprint matches" },
            { id: "caam", label: "CAAM", status: "warn", detail: "not configured yet" },
          ],
        },
      ]}
      runId="run-1"
      stepId="verify_key"
      onComplete={() => undefined}
      onFailed={() => undefined}
    />,
  );
  expect(html).toContain("SSH identity");
  expect(html).toContain("bootstrap:run-1:1");
  expect(html).toContain("acfs doctor");
  expect(html).toContain("fingerprint matches");
});

test("summarizeBootstrapEvents: counts terminal status and sequence cursor", () => {
  expect(summarizeBootstrapEvents([])).toEqual({
    status: "idle",
    total: 0,
    passed: 0,
    warning: 0,
    failed: 0,
    terminalSeq: 0,
  });
  expect(summarizeBootstrapEvents(SAMPLE_EVENTS)).toEqual({
    status: "warning",
    total: 2,
    passed: 1,
    warning: 1,
    failed: 0,
    terminalSeq: 2,
  });
  expect(
    summarizeBootstrapEvents([
      ...SAMPLE_EVENTS,
      { seq: 8, phaseId: "doctor", label: "doctor", status: "failed" },
    ]).status,
  ).toBe("failed");
});

test("buildBootstrapCheckpointData: freezes traceability without raw logs", () => {
  const data = buildBootstrapCheckpointData({
    stepId: "preflight",
    summary: "preflight passed",
    terminalSeq: 2,
    phases: SAMPLE_EVENTS,
    evidenceRefs: ["bootstrap:run-1:1", "bootstrap:run-1:2"],
    resumeHint: "/v1/bootstrap/acfs/resume",
  });
  expect(data).toEqual({
    stepId: "preflight",
    summary: "preflight passed",
    terminalSeq: 2,
    phaseCount: 2,
    failedPhaseIds: [],
    warningPhaseIds: ["acfs"],
    evidenceRefs: ["bootstrap:run-1:1", "bootstrap:run-1:2"],
    resumeHint: "/v1/bootstrap/acfs/resume",
  });
});

test("invokeBootstrapStep: dispatches through the typed bridge and streams events", async () => {
  const streamed: BootstrapStreamEvent[] = [];
  const bridge: BootstrapStepBridge = {
    preflight: async (input, sink) => {
      expect(input).toEqual({ runId: "run-1", stepId: "preflight" });
      for (const event of SAMPLE_EVENTS) sink(event);
      return { outcome: "completed", summary: "ok", events: SAMPLE_EVENTS };
    },
  };
  const result = await invokeBootstrapStep(
    bridge,
    { runId: "run-1", stepId: "preflight" },
    (event) => streamed.push(event),
  );
  expect(result.outcome).toBe("completed");
  expect(streamed.map((event) => event.seq)).toEqual([1, 2]);
});

test("invokeBootstrapStep: missing bridge yields a stable step failure", async () => {
  let thrown: Error | null = null;
  try {
    await invokeBootstrapStep(null, { runId: "run-1", stepId: "acfs_install" }, () => undefined);
  } catch (err) {
    thrown = err as Error;
  }
  expect(thrown?.message).toContain("/v1/bootstrap/acfs/start");
  const failure = buildBootstrapFailure("acfs_install", thrown?.message ?? "", [
    { seq: 1, phaseId: "install", label: "Install", status: "failed", message: "missing jsm" },
  ]);
  expect(failure).toEqual({
    code: "bootstrap_acfs_install_failed",
    message: "missing jsm",
  });
});

test("isBootstrapStepId: narrows exactly the hp-9z45 step set", () => {
  expect(isBootstrapStepId("preflight")).toBe(true);
  expect(isBootstrapStepId("acfs_install")).toBe(true);
  expect(isBootstrapStepId("reconnect")).toBe(true);
  expect(isBootstrapStepId("verify_key")).toBe(true);
  expect(isBootstrapStepId("status_check")).toBe(false);
});
