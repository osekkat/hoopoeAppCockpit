import { expect, test } from "bun:test";
import {
  buildTerminalPaneAuditEntry,
  getTerminalPaneModel,
} from "./terminal-pane.ts";

test("TerminalPane: visibility clamps to hidden without audit acknowledgement", () => {
  const model = getTerminalPaneModel({
    surface: "diagnostics",
    agentId: "agent-1",
    userId: "operator",
    visibility: "revealed",
    auditOnRevealAcknowledged: false,
  });
  expect(model.visibility).toBe("hidden");
  expect(model.policyBanner.auditMustFire).toBe(false);
});

test("TerminalPane: visibility honored when audit is acknowledged", () => {
  const model = getTerminalPaneModel({
    surface: "diagnostics",
    agentId: "agent-1",
    userId: "operator",
    visibility: "revealed",
    auditOnRevealAcknowledged: true,
  });
  expect(model.visibility).toBe("revealed");
  expect(model.policyBanner.auditMustFire).toBe(true);
  expect(model.policyBanner.text.toLowerCase()).toContain("audit");
});

test("TerminalPane: hidden state surfaces the Guardrail #12 banner", () => {
  const model = getTerminalPaneModel({
    surface: "diagnostics",
    agentId: "agent-1",
    userId: "operator",
    visibility: "hidden",
    auditOnRevealAcknowledged: false,
  });
  expect(model.policyBanner.text).toContain("Guardrail #12");
});

test("TerminalPane: aria-label changes with visibility", () => {
  const hidden = getTerminalPaneModel({
    surface: "diagnostics",
    agentId: "agent-1",
    userId: "operator",
    visibility: "hidden",
    auditOnRevealAcknowledged: false,
  });
  const revealed = getTerminalPaneModel({
    surface: "diagnostics",
    agentId: "agent-1",
    userId: "operator",
    visibility: "revealed",
    auditOnRevealAcknowledged: true,
  });
  expect(hidden.ariaLabel).toContain("hidden");
  expect(revealed.ariaLabel).toContain("revealed");
  expect(revealed.ariaLabel).toContain("operator");
});

test("buildTerminalPaneAuditEntry: produces an entry with ISO timestamp", () => {
  const entry = buildTerminalPaneAuditEntry({
    action: "show-raw-pane",
    agentId: "agent-1",
    userId: "operator",
    reason: "wedged-pane investigation",
    clock: () => new Date("2026-05-02T22:00:00.000Z"),
  });
  expect(entry.action).toBe("show-raw-pane");
  expect(entry.capturedAt).toBe("2026-05-02T22:00:00.000Z");
  expect(entry.reason).toBe("wedged-pane investigation");
});

test("TerminalPane: surface field is locked to 'diagnostics' (compile-time guardrail)", () => {
  // This test exists to remind future agents that broadening the surface
  // type would defeat Guardrail #12. The fact that the type only accepts
  // "diagnostics" is the actual enforcement; this assertion is a smoke.
  const model = getTerminalPaneModel({
    surface: "diagnostics",
    agentId: "agent-1",
    userId: "operator",
    visibility: "hidden",
    auditOnRevealAcknowledged: false,
  });
  expect(model.surface).toBe("diagnostics");
});
