import { renderApprovalDialogElement } from "./approval-dialog.ts";
import type { ApprovalDialogProps } from "./approval-dialog.ts";

const meta = {
  title: "Components/ApprovalDialog",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const HoopoePolicy = {
  render: () => renderApprovalDialogElement(policyApproval),
};

export const DcgHighRisk = {
  render: () => renderApprovalDialogElement(dcgApproval),
};

export const SlbCritical = {
  render: () => renderApprovalDialogElement(slbApproval),
};

const policyApproval: ApprovalDialogProps = {
  approvalId: "approval-policy-17",
  requestedActionSummary: "Allow snapshot-health to run in the isolated health worktree.",
  sourceRule: "hoopoe-policy",
  riskClass: "low",
  evidenceChips: [
    { id: "readonly", label: "read-only", tone: "neutral" },
    { id: "health-worktree", label: "isolated worktree", tone: "neutral" },
  ],
  selectedScope: "once",
  selectedExpiry: "15m",
  decisionActor: "operator",
  note: "",
  targetLabel: "snapshot-health",
};

const dcgApproval: ApprovalDialogProps = {
  approvalId: "approval-dcg-42",
  requestedActionSummary: "Permit force-release of a stale file reservation after TTL expiry.",
  sourceRule: "dcg",
  riskClass: "high",
  evidenceChips: [
    { id: "ttl", label: "TTL expired 18m ago", tone: "warning" },
    { id: "holder", label: "agent offline", tone: "warning" },
    { id: "mail", label: "notice will be sent", tone: "neutral" },
  ],
  selectedScope: "this-bead",
  selectedExpiry: "1h",
  decisionActor: "operator",
  note: "Release only this bead's stale lock.",
  targetLabel: "reservation.force_release",
};

const slbApproval: ApprovalDialogProps = {
  approvalId: "approval-slb-9",
  requestedActionSummary: "Two-person approval required before stopping a wedged agent process.",
  sourceRule: "slb",
  riskClass: "critical",
  evidenceChips: [
    { id: "wedged", label: "wedged 27m", tone: "danger" },
    { id: "bead", label: "hp-z1x", tone: "neutral" },
    { id: "replacement", label: "reassign prepared", tone: "warning" },
  ],
  selectedScope: "once",
  selectedExpiry: "15m",
  decisionActor: "second-operator",
  note: "Require reassignment confirmation.",
  targetLabel: "agent.kill_and_reassign",
  disabledDecisions: ["extend"],
};
