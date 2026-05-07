// hp-hle — StageGate covers the four FeatureRender states.
//
// bun:test has no DOM, so we render via react-dom/server and assert on
// the static HTML output. The pure decision helpers are exercised
// directly without React.

import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  type CapabilityRegistry,
  type CapabilityStatus,
} from "../../capabilities/index.ts";
import {
  decideStageGate,
  worstFeatureDecision,
} from "../data/capability-data.ts";
import { getStageDefinition } from "../stages.ts";
import { StageGate } from "./StageGate.tsx";

function registryWith(
  overrides: Readonly<Record<string, CapabilityStatus>>,
): CapabilityRegistry {
  // Build a registry where every requested capRef has the supplied status.
  const tools: Record<string, {
    tool: string;
    version: string;
    source: string;
    capabilities: Record<string, { status: CapabilityStatus }>;
    lastCheckedAt: string;
    fixturesVersion: string;
  }> = {};
  for (const [capRef, status] of Object.entries(overrides)) {
    const tool = capRef.split(".")[0]!;
    if (!tools[tool]) {
      tools[tool] = {
        tool,
        version: "0.0.0",
        source: "test",
        capabilities: {},
        lastCheckedAt: "2026-05-07T00:00:00Z",
        fixturesVersion: "test",
      };
    }
    tools[tool].capabilities[capRef] = { status };
  }
  return {
    schemaVersion: 1,
    snapshotAt: "2026-05-07T00:00:00Z",
    daemonApiVersion: "0.1.0",
    fixturesVersion: "test",
    tools: tools as CapabilityRegistry["tools"],
  };
}

const beadStage = getStageDefinition("bead");
const swarmStage = getStageDefinition("swarm");
const planStage = getStageDefinition("plan");

describe("hp-hle — decideStageGate worst-of resolution", () => {
  test("stages with no required features resolve to null (always available)", () => {
    const registry = registryWith({});
    expect(decideStageGate(registry, planStage)).toBeNull();
  });

  test("blocked-by-policy outranks every other state", () => {
    const registry = registryWith({
      "ntm.robot.snapshot": "blocked-by-policy",
      "git.status.read": "missing",
      "git.push": "ok",
    });
    const decision = decideStageGate(registry, swarmStage);
    expect(decision).not.toBeNull();
    expect(decision!.render).toBe("blocked-by-policy");
  });

  test("unavailable outranks degraded", () => {
    const registry = registryWith({
      "ntm.robot.snapshot": "missing",
      "git.status.read": "degraded",
      "git.push": "ok",
    });
    const decision = decideStageGate(registry, swarmStage);
    expect(decision!.render).toBe("unavailable");
  });

  test("degraded surfaces when only optional or non-required caps are degraded", () => {
    const registry = registryWith({
      "ntm.robot.snapshot": "degraded",
      "git.status.read": "ok",
      "git.push": "ok",
    });
    const decision = decideStageGate(registry, swarmStage);
    expect(decision!.render).toBe("degraded");
  });

  test("available when every required capability resolves OK", () => {
    const registry = registryWith({
      "ntm.robot.snapshot": "ok",
      "ntm.panes.stream": "ok",
      "git.status.read": "ok",
      "git.push": "ok",
    });
    const decision = decideStageGate(registry, swarmStage);
    expect(decision!.render).toBe("available");
  });

  test("worstFeatureDecision returns null on empty input", () => {
    expect(worstFeatureDecision([])).toBeNull();
  });
});

describe("hp-hle — StageGate render output", () => {
  test("renders children when decision is available", () => {
    const registry = registryWith({ "br.issues.read": "ok", "bv.robot.triage": "ok" });
    const html = renderToStaticMarkup(
      <StageGate stage={beadStage} registry={registry}>
        <span data-testid="bead-children">stage body</span>
      </StageGate>,
    );
    expect(html).toContain("bead-children");
    expect(html).not.toContain("stage-gate-degraded");
    expect(html).not.toContain("stage-gate-unavailable");
    expect(html).not.toContain("stage-gate-blocked");
  });

  test("renders banner + children when decision is degraded", () => {
    const registry = registryWith({
      "br.issues.read": "ok",
      "bv.robot.triage": "degraded",
    });
    const html = renderToStaticMarkup(
      <StageGate stage={beadStage} registry={registry}>
        <span data-testid="bead-children">stage body</span>
      </StageGate>,
    );
    expect(html).toContain("stage-gate-degraded-bead");
    expect(html).toContain("bead-children");
    expect(html).toContain("degraded mode");
  });

  test("renders unavailable surface (no children) when required cap missing", () => {
    const registry = registryWith({
      "br.issues.read": "missing",
      "bv.robot.triage": "ok",
    });
    const html = renderToStaticMarkup(
      <StageGate stage={beadStage} registry={registry}>
        <span data-testid="bead-children">stage body</span>
      </StageGate>,
    );
    expect(html).toContain("stage-gate-unavailable-bead");
    expect(html).not.toContain("bead-children");
    expect(html).toContain("br.issues.read");
  });

  test("renders blocked-by-policy surface (no children) when required cap blocked", () => {
    const registry = registryWith({
      "br.issues.read": "blocked-by-policy",
      "bv.robot.triage": "ok",
    });
    const html = renderToStaticMarkup(
      <StageGate stage={beadStage} registry={registry}>
        <span data-testid="bead-children">stage body</span>
      </StageGate>,
    );
    expect(html).toContain("stage-gate-blocked-bead");
    expect(html).not.toContain("bead-children");
    expect(html).toContain("blocked by policy");
  });

  test("stages with no required features always render children (planning)", () => {
    const registry = registryWith({});
    const html = renderToStaticMarkup(
      <StageGate stage={planStage} registry={registry}>
        <span data-testid="plan-children">plan body</span>
      </StageGate>,
    );
    expect(html).toContain("plan-children");
  });
});
