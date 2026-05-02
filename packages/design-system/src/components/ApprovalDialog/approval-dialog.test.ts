import { expect, test } from "bun:test";
import {
  approvalDialogExpiryOptions,
  approvalDialogScopeOptions,
  createApprovalDialogDecisionPayload,
  getApprovalDialogModel,
} from "./approval-dialog.ts";
import type { ApprovalDialogProps } from "./approval-dialog.ts";

const baseApproval: ApprovalDialogProps = {
  approvalId: "approval-123",
  requestedActionSummary: "Allow BlueHill to extend the reservation for hp-8gg.",
  sourceRule: "dcg",
  riskClass: "high",
  evidenceChips: [
    { id: "bead", label: "hp-8gg", tone: "neutral" },
    { id: "scope", label: "exclusive reservation", tone: "warning" },
  ],
  selectedScope: "this-bead",
  selectedExpiry: "1h",
  decisionActor: "operator",
  note: "Allow while the component is being committed.",
  targetLabel: "packages/design-system/src/components/ApprovalDialog/**",
};

test("ApprovalDialog model exposes required approval fields", () => {
  const model = getApprovalDialogModel(baseApproval);

  expect(model.approvalId).toBe("approval-123");
  expect(model.requestedActionSummary).toContain("BlueHill");
  expect(model.sourceRuleLabel).toBe("DCG");
  expect(model.riskLabel).toBe("High risk");
  expect(model.evidenceChips.map((chip) => chip.label)).toEqual([
    "hp-8gg",
    "exclusive reservation",
  ]);
  expect(model.targetLabel).toContain("ApprovalDialog");
});

test("ApprovalDialog has stable scope and expiry option sets", () => {
  expect(approvalDialogScopeOptions.map((option) => option.value)).toEqual([
    "once",
    "this-bead",
    "this-swarm",
    "this-project-session",
  ]);
  expect(approvalDialogExpiryOptions.map((option) => option.value)).toEqual([
    "15m",
    "1h",
    "4h",
    "end-of-session",
  ]);
});

test("ApprovalDialog creates typed decision payloads without action execution fields", () => {
  const model = getApprovalDialogModel({
    ...baseApproval,
    decisionActor: "  operator  ",
    note: "  approved for current bead  ",
  });
  const payload = createApprovalDialogDecisionPayload(model, "approve");

  expect(payload).toEqual({
    approvalId: "approval-123",
    decision: "approve",
    scope: "this-bead",
    expiry: "1h",
    decisionActor: "operator",
    note: "approved for current bead",
  });
  expect(Object.keys(payload)).not.toContain("execute");
  expect(Object.keys(payload)).not.toContain("command");
});

test("ApprovalDialog supports disabled approve deny extend buttons", () => {
  const model = getApprovalDialogModel({
    ...baseApproval,
    disabledDecisions: ["approve", "extend"],
  });

  expect(model.actions.map((action) => [action.decision, action.disabled])).toEqual([
    ["approve", true],
    ["deny", false],
    ["extend", true],
  ]);
});

test("ApprovalDialog defaults missing optional values conservatively", () => {
  const model = getApprovalDialogModel({
    approvalId: "approval-456",
    requestedActionSummary: "Permit a low-risk read-only diagnostics check.",
    sourceRule: "hoopoe-policy",
    riskClass: "low",
    evidenceChips: [{ id: "readonly", label: "read-only" }],
    decisionActor: "",
    note: "",
  });

  expect(model.selectedScope).toBe("once");
  expect(model.selectedExpiry).toBe("15m");
  expect(model.evidenceChips[0]?.tone).toBe("neutral");
  expect(model.actions).toHaveLength(3);
});
