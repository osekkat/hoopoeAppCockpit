// hp-qj9o - fixture-backed e2e coverage for the renderer onboarding stream.

import { afterEach, describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import acfsDoctor from "../../../../../packages/fixtures/phase0-2026-05-02/acfs/doctor.json" with { type: "json" };
import acfsDoctorStatus from "../../../../../packages/fixtures/phase0-2026-05-02/acfs/doctor-status.json" with { type: "json" };
import {
  StepBootstrapStream,
  buildBootstrapCheckpointData,
  invokeBootstrapStep,
  summarizeBootstrapEvents,
  type BootstrapDoctorCheck,
  type BootstrapEventSink,
  type BootstrapStepBridge,
  type BootstrapStepId,
  type BootstrapStepResult,
  type BootstrapStepRunInput,
  type BootstrapStreamEvent,
} from "./StepBootstrapStream.tsx";
import {
  assertNoProductionEndpoints,
  createPhase1TestLogger,
  type Phase1StructuredTestLogger,
} from "../../test-utils/structured-test-logger.ts";
import {
  assertNoUnredactedSecrets,
  bootMockFlywheel,
  getEmittedEvents,
  type ReplayClient,
} from "../../test-utils/replay/index.ts";

type TestWindow = typeof globalThis & {
  window?: {
    hoopoe?: {
      bootstrap?: BootstrapStepBridge;
    };
  };
};

interface FixtureBootstrapDaemon {
  readonly request: (
    method: "bootstrap.preflight" | "bootstrap.verify_key",
    input: BootstrapStepRunInput,
    sink: BootstrapEventSink,
  ) => Promise<BootstrapStepResult>;
}

interface DoctorFixture {
  readonly checks: readonly {
    readonly id: string;
    readonly label: string;
    readonly status: "pass" | "warn" | "fail";
    readonly details?: string;
  }[];
  readonly summary: {
    readonly pass: number;
    readonly warn: number;
    readonly fail: number;
  };
}

const globalWithWindow = globalThis as TestWindow;
const doctor = acfsDoctor as DoctorFixture;

afterEach(() => {
  delete globalWithWindow.window;
});

describe("StepBootstrapStream e2e", () => {
  test("streams renderer onboarding through preload bridge into Phase 0 mock daemon", async () => {
    const logger = createPhase1TestLogger({
      suite: "renderer.onboarding.bootstrap",
      testName: "StepBootstrapStream.preload-to-daemon",
      correlationId: "hp-qj9o-bootstrap-e2e",
      now: () => new Date("2026-05-04T00:00:00.000Z"),
    });
    const runId = "hp-qj9o-phase0-fresh";
    let client: ReplayClient | null = null;

    logger.start({
      fixture: "phase0-2026-05-02/scenarios/fresh",
      flow: "StepBootstrapStream -> window.hoopoe.bootstrap -> fixture daemon",
    });
    logger.phase("setup", { guard: "no production endpoints" });
    assertNoProductionEndpoints({
      urls: ["fixture://phase0-2026-05-02/scenarios/fresh", "http://127.0.0.1:0"],
    });

    try {
      client = bootMockFlywheel({
        scenario: "fresh",
        now: () => Date.parse("2026-05-04T00:00:00.000Z"),
      });
      const daemon = createFixtureBootstrapDaemon(client, logger);
      globalWithWindow.window = {
        hoopoe: {
          bootstrap: createBootstrapPreloadBridge(daemon, logger),
        },
      };

      logger.snapshot("mock-daemon.boot", {
        scenarioId: client.scenarioId(),
        capturedAt: client.scenario().snapshot.meta.capturedAt,
        declaredAdapters: client.declaredAdapters(),
      });

      logger.phase("act", { stepId: "preflight" });
      const preflightEmptyMarkup = renderToStaticMarkup(
        <StepBootstrapStream
          runId={runId}
          stepId="preflight"
          onComplete={() => undefined}
          onFailed={() => undefined}
        />,
      );
      logger.snapshot("renderer.preflight.empty", {
        hasRunButton: preflightEmptyMarkup.includes('data-testid="wizard-preflight-run"'),
        waitsForBridge: preflightEmptyMarkup.includes("Waiting for the bootstrap preload bridge."),
        summary: summarizeBootstrapEvents([]),
      });
      expect(preflightEmptyMarkup).toContain('data-testid="wizard-step-preflight"');
      expect(preflightEmptyMarkup).toContain('data-testid="wizard-preflight-run"');
      expect(preflightEmptyMarkup).not.toContain("Waiting for the bootstrap preload bridge.");
      expect(summarizeBootstrapEvents([]).status).toBe("idle");

      const bridge = globalWithWindow.window?.hoopoe?.bootstrap ?? null;
      const preflightEvents: BootstrapStreamEvent[] = [];
      const preflightResult = await invokeBootstrapStep(
        bridge,
        { runId, stepId: "preflight" },
        (event) => preflightEvents.push(event),
      );
      const preflightSummary = summarizeBootstrapEvents(preflightEvents);
      logger.assertion("preflight.stream.completed", {
        outcome: preflightResult.outcome,
        eventCount: preflightEvents.length,
        summary: preflightSummary,
      });
      expect(preflightResult.outcome).toBe("completed");
      expect(preflightSummary.status).toBe("passed");
      expect(preflightEvents.map((event) => event.phaseId)).toEqual([
        "daemon-health",
        "git-status",
        "agent-mail",
      ]);

      const preflightRenderedMarkup = renderToStaticMarkup(
        <StepBootstrapStream
          initialEvents={preflightEvents}
          runId={runId}
          stepId="preflight"
          onComplete={() => undefined}
          onFailed={() => undefined}
        />,
      );
      const preflightCheckpoint = buildBootstrapCheckpointData({
        stepId: "preflight",
        summary: preflightResult.summary,
        terminalSeq: preflightSummary.terminalSeq,
        phases: preflightEvents,
        evidenceRefs: preflightResult.evidenceRefs ?? [],
      });
      logger.snapshot("renderer.preflight.completed", {
        checkpoint: preflightCheckpoint,
        containsEvidence: preflightRenderedMarkup.includes("phase0:fresh:git.status_porcelain"),
      });
      expect(preflightRenderedMarkup).toContain('data-status="passed"');
      expect(preflightRenderedMarkup).toContain("Daemon health");
      expect(preflightRenderedMarkup).toContain("Git status");
      expect(preflightRenderedMarkup).toContain("Agent Mail inbox");
      expect(preflightRenderedMarkup).toContain("phase0:fresh:git.status_porcelain");
      expect(preflightCheckpoint.phaseCount).toBe(preflightEvents.length);
      expect(preflightCheckpoint.failedPhaseIds).toEqual([]);

      logger.phase("act", { stepId: "verify_key" });
      const verifyEvents: BootstrapStreamEvent[] = [];
      const verifyResult = await invokeBootstrapStep(
        bridge,
        { runId, stepId: "verify_key", sinceSeq: preflightSummary.terminalSeq },
        (event) => verifyEvents.push(event),
      );
      const verifySummary = summarizeBootstrapEvents(verifyEvents);
      logger.assertion("verify-key.stream.completed", {
        outcome: verifyResult.outcome,
        eventCount: verifyEvents.length,
        summary: verifySummary,
      });
      expect(verifyResult.outcome).toBe("completed");
      expect(verifySummary.status).toBe("warning");
      expect(verifyEvents).toHaveLength(1);
      expect(verifyEvents[0]?.doctor?.some((check) => check.status === "warn")).toBe(true);

      const verifyRenderedMarkup = renderToStaticMarkup(
        <StepBootstrapStream
          initialEvents={verifyEvents}
          runId={runId}
          stepId="verify_key"
          onComplete={() => undefined}
          onFailed={() => undefined}
        />,
      );
      const verifyCheckpoint = buildBootstrapCheckpointData({
        stepId: "verify_key",
        summary: verifyResult.summary,
        terminalSeq: verifySummary.terminalSeq,
        phases: verifyEvents,
        evidenceRefs: verifyResult.evidenceRefs ?? [],
      });
      logger.snapshot("renderer.verify-key.completed", {
        checkpoint: verifyCheckpoint,
        renderedDoctorWarnings: verifyRenderedMarkup.includes("MCP Agent Mail"),
      });
      expect(verifyRenderedMarkup).toContain('data-status="warning"');
      expect(verifyRenderedMarkup).toContain("acfs doctor");
      expect(verifyRenderedMarkup).toContain("Tailscale");
      expect(verifyRenderedMarkup).toContain("MCP Agent Mail");
      expect(verifyRenderedMarkup).toContain("phase0:acfs.doctor:2026-05-03T22:11:48Z");
      expect(verifyCheckpoint.warningPhaseIds).toEqual(["acfs-doctor"]);

      logger.phase("assert", { boundary: "fixture daemon" });
      const adapterCalls = client
        .recordedCalls()
        .map((call) => `${call.adapter}.${call.method}`);
      const bootstrapEventCount = getEmittedEvents(client).filter(
        (event) => event.channel === "bootstrap",
      ).length;
      logger.assertion("daemon.adapter-contract", {
        adapterCalls,
        bootstrapEventCount,
      });
      expect(adapterCalls).toContain("health.probe");
      expect(adapterCalls).toContain("git.status_porcelain");
      expect(adapterCalls).toContain("agent_mail.fetch_inbox");
      expect(adapterCalls).toContain("health.acfs_doctor");
      expect(bootstrapEventCount).toBe(preflightEvents.length + verifyEvents.length);
      assertNoUnredactedSecrets(getEmittedEvents(client));

      logger.phase("assert", { boundary: "structured logs" });
      const logLines = logger.jsonLines().split("\n").filter(Boolean);
      expect(logLines.length).toBeGreaterThan(10);
      expect(logLines.some((line) => line.includes('"phase":"setup"'))).toBe(true);
      expect(logLines.some((line) => line.includes('"event":"test.assertion"'))).toBe(true);
      logger.end("passed", {
        assertions: logLines.length,
        emittedEvents: getEmittedEvents(client).length,
      });
    } catch (err) {
      logger.end("failed", { message: (err as Error).message });
      throw err;
    } finally {
      logger.phase("teardown", { clientOpen: client !== null });
      client?.close();
    }
  });
});

function createBootstrapPreloadBridge(
  daemon: FixtureBootstrapDaemon,
  logger: Phase1StructuredTestLogger,
): BootstrapStepBridge {
  return {
    preflight: async (input, sink) => {
      logger.snapshot("preload.dispatch", {
        method: "bootstrap.preflight",
        runId: input.runId,
        stepId: input.stepId,
      });
      return await daemon.request("bootstrap.preflight", input, sink);
    },
    verifyKey: async (input, sink) => {
      logger.snapshot("preload.dispatch", {
        method: "bootstrap.verify_key",
        runId: input.runId,
        stepId: input.stepId,
        sinceSeq: input.sinceSeq ?? null,
      });
      return await daemon.request("bootstrap.verify_key", input, sink);
    },
  };
}

function createFixtureBootstrapDaemon(
  client: ReplayClient,
  logger: Phase1StructuredTestLogger,
): FixtureBootstrapDaemon {
  return {
    request: async (method, input, sink) => {
      logger.snapshot("daemon.receive", { method, runId: input.runId, stepId: input.stepId });
      const events =
        method === "bootstrap.preflight"
          ? buildPreflightEvents(client)
          : buildVerifyKeyEvents(client);

      for (const event of events) {
        client.emit({
          channel: "bootstrap",
          seq: 0,
          ts: event.recordedAt ?? client.scenario().snapshot.meta.capturedAt,
          type: "bootstrap.phase",
          payload: {
            runId: input.runId,
            stepId: input.stepId,
            phaseId: event.phaseId,
            status: event.status,
            evidenceRef: event.evidenceRef ?? null,
          },
        });
        sink(event);
      }

      const summary = summarizeBootstrapEvents(events);
      logger.snapshot("daemon.emit", {
        method,
        status: summary.status,
        eventCount: events.length,
        terminalSeq: summary.terminalSeq,
      });
      return {
        outcome: summary.status === "failed" ? "failed" : "completed",
        summary:
          method === "bootstrap.preflight"
            ? "Pre-flight checks passed from Phase 0 fixture replay."
            : "ACFS doctor completed from Phase 0 fixture replay.",
        events,
        evidenceRefs: events
          .map((event) => event.evidenceRef)
          .filter((ref): ref is string => typeof ref === "string" && ref.length > 0),
      };
    },
  };
}

function buildPreflightEvents(client: ReplayClient): BootstrapStreamEvent[] {
  const health = client.health();
  const healthCapture = client.callAdapter("health", "probe");
  client.callAdapter("git", "status_porcelain");
  client.callAdapter("agent_mail", "fetch_inbox");
  const gitStatus = client.getAdapterInvocation("git", "status_porcelain");
  const mailHelp = client.getAdapterInvocation("agent_mail", "help");
  const ts = client.scenario().snapshot.meta.capturedAt;

  return [
    {
      seq: 1,
      phaseId: "daemon-health",
      label: "Daemon health",
      status: health.status === "ok" ? "passed" : "failed",
      message: `Fixture daemon responded from ${health.environment}.`,
      detail:
        healthCapture?.present === true
          ? "Health adapter present in Phase 0 snapshot."
          : "Health probe synthesized by fixture daemon.",
      evidenceRef: "phase0:fresh:health.probe",
      recordedAt: ts,
    },
    {
      seq: 2,
      phaseId: "git-status",
      label: "Git status",
      status: gitStatus?.exit === 0 ? "passed" : "failed",
      message: "Desktop preload bridge reached the daemon-side Git adapter contract.",
      detail: `stdoutBytes=${gitStatus?.stdoutBytes ?? 0}`,
      evidenceRef: "phase0:fresh:git.status_porcelain",
      recordedAt: ts,
    },
    {
      seq: 3,
      phaseId: "agent-mail",
      label: "Agent Mail inbox",
      status: mailHelp?.exit === 0 ? "passed" : "warning",
      message: "Agent Mail adapter capture is available without using MCP Agent Mail.",
      detail: `stdoutBytes=${mailHelp?.stdoutBytes ?? 0}`,
      evidenceRef: "phase0:fresh:agent_mail.help",
      recordedAt: ts,
    },
  ];
}

function buildVerifyKeyEvents(client: ReplayClient): BootstrapStreamEvent[] {
  client.callAdapter("health", "acfs_doctor");
  const warningChecks = doctor.checks.filter((check) => check.status === "warn");
  const representativePasses = doctor.checks
    .filter(
      (check) =>
        check.status === "pass" &&
        (check.id.startsWith("tool.") || check.id.startsWith("stack.")),
    )
    .slice(0, 6);
  const doctorChecks = [...representativePasses, ...warningChecks].map(toBootstrapDoctorCheck);
  const status = doctor.summary.fail > 0 ? "failed" : doctor.summary.warn > 0 ? "warning" : "passed";

  return [
    {
      seq: 4,
      phaseId: "acfs-doctor",
      label: "acfs doctor",
      status,
      message: `${doctor.summary.pass} passed, ${doctor.summary.warn} warnings, ${doctor.summary.fail} failed.`,
      detail: `capturedAt=${acfsDoctorStatus.capturedAt}`,
      evidenceRef: `phase0:acfs.doctor:${acfsDoctorStatus.capturedAt}`,
      doctor: doctorChecks,
      recordedAt: acfsDoctorStatus.capturedAt,
    },
  ];
}

function toBootstrapDoctorCheck(check: DoctorFixture["checks"][number]): BootstrapDoctorCheck {
  return {
    id: check.id,
    label: check.label,
    status: check.status === "pass" ? "ok" : check.status === "warn" ? "warn" : "fail",
    ...(check.details !== undefined && check.details.length > 0 ? { detail: check.details } : {}),
  };
}
