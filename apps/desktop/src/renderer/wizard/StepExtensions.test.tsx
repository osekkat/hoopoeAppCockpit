// hp-zsp1 - extensions wizard step tests.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import type { CapabilityRegistry, ToolId, ToolReport } from "@hoopoe/schemas";
import {
  StepExtensions,
  buildExtensionsCheckpointData,
  deriveExtensionsResult,
  type ExtensionsBridge,
} from "./StepExtensions.tsx";

test("StepExtensions: renders the four-extension verification shell", () => {
  const html = renderToStaticMarkup(
    <StepExtensions onComplete={() => undefined} onFailed={() => undefined} />,
  );
  expect(html).toContain('data-testid="wizard-step-extensions"');
  expect(html).toContain("Hoopoe extensions");
  expect(html).toContain('data-testid="wizard-extensions-run"');
  expect(html).not.toContain("The body of this step ships in a follow-up bead");
});

test("deriveExtensionsResult: handles daemon, oracle, skill loader, and git auth", () => {
  const result = deriveExtensionsResult({
    healthOk: true,
    version: "0.1.0-test",
    capabilities: registryFixture({
      oracle: okReport("oracle"),
      jsm: missingReport("jsm"),
      jfp: okReport("jfp"),
      git: degradedReport("git"),
    }),
  });
  expect(result.summary).toBe("Extensions verified with warnings");
  expect(result.substeps.map((substep) => substep.id)).toEqual([
    "daemon_service",
    "oracle_browser",
    "skill_loader",
    "github_auth",
  ]);
  expect(result.substeps.find((substep) => substep.id === "skill_loader")?.status).toBe("warning");
  expect(result.substeps.find((substep) => substep.id === "github_auth")?.status).toBe("warning");
});

test("buildExtensionsCheckpointData: stores traceability ids only", () => {
  const result = deriveExtensionsResult({
    healthOk: false,
    version: "",
    capabilities: registryFixture({
      oracle: okReport("oracle"),
      jsm: okReport("jsm"),
      jfp: okReport("jfp"),
      git: missingReport("git"),
    }),
  });
  const data = buildExtensionsCheckpointData(result);
  expect(data.readySubsteps).toEqual(["oracle_browser", "skill_loader"]);
  expect(data.missingSubsteps).toEqual(["daemon_service", "github_auth"]);
});

test("ExtensionsBridge: verify contract returns the canonical selection shape", async () => {
  const bridge: ExtensionsBridge = {
    verify: async () =>
      deriveExtensionsResult({
        healthOk: true,
        version: "0.1.0",
        capabilities: registryFixture({
          oracle: okReport("oracle"),
          jsm: okReport("jsm"),
          jfp: okReport("jfp"),
          git: okReport("git"),
        }),
      }),
  };
  const result = await bridge.verify();
  expect(result.summary).toBe("Extensions verified");
  expect(result.substeps.every((substep) => substep.status === "ready")).toBe(true);
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
