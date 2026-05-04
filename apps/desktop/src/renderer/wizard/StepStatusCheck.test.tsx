// hp-zsp1 - status-check wizard step tests.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { CapabilityRegistry, ToolId, ToolReport } from "@hoopoe/schemas";
import {
  StepStatusCheck,
  buildStatusCheckCheckpointData,
  deriveStatusCheckResult,
  type StatusCheckBridge,
} from "./StepStatusCheck.tsx";

test("StepStatusCheck: renders a real step shell and run affordance", () => {
  const html = renderToStaticMarkup(
    <StepStatusCheck onComplete={() => undefined} onFailed={() => undefined} />,
  );
  expect(html).toContain('data-testid="wizard-step-status_check"');
  expect(html).toContain("Status check");
  expect(html).toContain('data-testid="wizard-status-check-run"');
  expect(html).not.toContain("The body of this step ships in a follow-up bead");
});

test("deriveStatusCheckResult: summarizes required tools and CAAM subscription access", () => {
  const result = deriveStatusCheckResult(
    registryFixture({
      git: okReport("git"),
      br: okReport("br"),
      bv: okReport("bv"),
      ntm: okReport("ntm"),
      agent_mail: okReport("agent_mail"),
      caam: okReport("caam"),
      jsm: degradedReport("jsm"),
      jfp: okReport("jfp"),
      oracle: missingReport("oracle"),
      rch: okReport("rch"),
    }),
  );
  expect(result.summary).toBe("1 required tools missing");
  expect(result.tools.find((tool) => tool.id === "jsm")?.status).toBe("warning");
  expect(result.tools.find((tool) => tool.id === "oracle")?.status).toBe("missing");
  expect(result.subscription.status).toBe("ready");
});

test("buildStatusCheckCheckpointData: freezes summary without raw capability maps", () => {
  const selection = deriveStatusCheckResult(
    registryFixture({
      git: okReport("git"),
      br: okReport("br"),
      bv: okReport("bv"),
      ntm: okReport("ntm"),
      agent_mail: okReport("agent_mail"),
      caam: degradedReport("caam"),
      jsm: okReport("jsm"),
      jfp: okReport("jfp"),
      oracle: okReport("oracle"),
      rch: okReport("rch"),
    }),
  );
  const data = buildStatusCheckCheckpointData(selection);
  expect(data.subscriptionStatus).toBe("warning");
  expect(data.readyTools).toContain("git");
  expect(data.warningTools).toEqual(["caam"]);
  expect(data.missingTools).toEqual([]);
});

test("StatusCheckBridge: load contract returns the canonical selection shape", async () => {
  const bridge: StatusCheckBridge = {
    load: async () =>
      deriveStatusCheckResult(
        registryFixture({
          git: okReport("git"),
          br: okReport("br"),
          bv: okReport("bv"),
          ntm: okReport("ntm"),
          agent_mail: okReport("agent_mail"),
          caam: okReport("caam"),
          jsm: okReport("jsm"),
          jfp: okReport("jfp"),
          oracle: okReport("oracle"),
          rch: okReport("rch"),
        }),
      ),
  };
  const result = await bridge.load();
  expect(result.summary).toBe("Tool inventory passed");
  expect(result.tools).toHaveLength(10);
});

function registryFixture(tools: Partial<Record<ToolId, ToolReport>>): CapabilityRegistry {
  return {
    schemaVersion: 1,
    snapshotAt: "2026-05-04T07:00:00Z",
    daemonApiVersion: "0.1.0",
    fixturesVersion: "test",
    tools,
  };
}

function okReport(tool: ToolId): ToolReport {
  return {
    tool,
    version: "1.0.0",
    source: "fixture",
    lastCheckedAt: "2026-05-04T07:00:00Z",
    fixturesVersion: "test",
    capabilities: {
      [`${tool}.probe`]: { status: "ok" },
      [`${tool}.read`]: { status: "ok" },
    },
  };
}

function degradedReport(tool: ToolId): ToolReport {
  return {
    ...okReport(tool),
    capabilities: {
      [`${tool}.probe`]: { status: "degraded", notes: "fallback transport" },
      [`${tool}.read`]: { status: "ok" },
    },
  };
}

function missingReport(tool: ToolId): ToolReport {
  return {
    ...okReport(tool),
    capabilities: {
      [`${tool}.probe`]: { status: "missing", notes: "binary not found" },
    },
  };
}
