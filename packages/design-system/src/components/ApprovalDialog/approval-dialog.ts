import { hoopoeTokens, statusTones } from "../../tokens/index.ts";
import type { ToneToken } from "../../tokens/index.ts";

export type ApprovalDialogSourceRule = "hoopoe-policy" | "dcg" | "slb";
export type ApprovalDialogRiskClass = "low" | "medium" | "high" | "critical";
export type ApprovalDialogScope = "once" | "this-bead" | "this-swarm" | "this-project-session";
export type ApprovalDialogExpiry = "15m" | "1h" | "4h" | "end-of-session";
export type ApprovalDialogDecision = "approve" | "deny" | "extend";

export interface ApprovalDialogEvidenceChip {
  readonly id: string;
  readonly label: string;
  readonly tone?: "neutral" | "warning" | "danger";
}

export interface ApprovalDialogProps {
  readonly approvalId: string;
  readonly requestedActionSummary: string;
  readonly sourceRule: ApprovalDialogSourceRule;
  readonly riskClass: ApprovalDialogRiskClass;
  readonly evidenceChips: readonly ApprovalDialogEvidenceChip[];
  readonly selectedScope?: ApprovalDialogScope;
  readonly selectedExpiry?: ApprovalDialogExpiry;
  readonly decisionActor: string;
  readonly note: string;
  readonly targetLabel?: string;
  readonly disabledDecisions?: readonly ApprovalDialogDecision[];
  readonly onFieldChange?: (change: ApprovalDialogFieldChange, event: Event) => void;
  readonly onDecision?: (decision: ApprovalDialogDecisionPayload, event: MouseEvent) => void;
}

export interface ApprovalDialogFieldChange {
  readonly field: "scope" | "expiry" | "decisionActor" | "note";
  readonly value: string;
}

export interface ApprovalDialogOption<TValue extends string> {
  readonly value: TValue;
  readonly label: string;
  readonly description: string;
}

export interface ApprovalDialogActionModel {
  readonly decision: ApprovalDialogDecision;
  readonly label: string;
  readonly ariaLabel: string;
  readonly disabled: boolean;
  readonly destructive: boolean;
}

export interface ApprovalDialogDecisionPayload {
  readonly approvalId: string;
  readonly decision: ApprovalDialogDecision;
  readonly scope: ApprovalDialogScope;
  readonly expiry: ApprovalDialogExpiry;
  readonly decisionActor: string;
  readonly note: string;
}

export interface ApprovalDialogModel {
  readonly approvalId: string;
  readonly requestedActionSummary: string;
  readonly sourceRule: ApprovalDialogSourceRule;
  readonly sourceRuleLabel: string;
  readonly riskClass: ApprovalDialogRiskClass;
  readonly riskLabel: string;
  readonly riskTone: ToneToken;
  readonly targetLabel: string | null;
  readonly evidenceChips: readonly Required<ApprovalDialogEvidenceChip>[];
  readonly selectedScope: ApprovalDialogScope;
  readonly selectedExpiry: ApprovalDialogExpiry;
  readonly decisionActor: string;
  readonly note: string;
  readonly scopeOptions: readonly ApprovalDialogOption<ApprovalDialogScope>[];
  readonly expiryOptions: readonly ApprovalDialogOption<ApprovalDialogExpiry>[];
  readonly actions: readonly ApprovalDialogActionModel[];
  readonly className: string;
  readonly style: Readonly<Record<string, string>>;
}

export const approvalDialogScopeOptions = [
  option("once", "Once", "Allow this single approval request only"),
  option("this-bead", "This bead", "Allow matching requests while this bead is active"),
  option("this-swarm", "This swarm", "Allow matching requests during this swarm session"),
  option(
    "this-project-session",
    "This project session",
    "Allow matching requests until this project session ends",
  ),
] as const satisfies readonly ApprovalDialogOption<ApprovalDialogScope>[];

export const approvalDialogExpiryOptions = [
  option("15m", "15 minutes", "Expire soon if no action follows"),
  option("1h", "1 hour", "Expire after a short work window"),
  option("4h", "4 hours", "Expire after the current work block"),
  option("end-of-session", "End of session", "Expire when this session ends"),
] as const satisfies readonly ApprovalDialogOption<ApprovalDialogExpiry>[];

export function getApprovalDialogModel(props: ApprovalDialogProps): ApprovalDialogModel {
  const disabledDecisions = new Set(props.disabledDecisions ?? []);
  const selectedScope = props.selectedScope ?? "once";
  const selectedExpiry = props.selectedExpiry ?? "15m";

  return {
    approvalId: props.approvalId,
    requestedActionSummary: props.requestedActionSummary,
    sourceRule: props.sourceRule,
    sourceRuleLabel: sourceRuleLabels[props.sourceRule],
    riskClass: props.riskClass,
    riskLabel: riskLabels[props.riskClass],
    riskTone: riskToneByClass[props.riskClass],
    targetLabel: props.targetLabel ?? null,
    evidenceChips: props.evidenceChips.map(normalizeEvidenceChip),
    selectedScope,
    selectedExpiry,
    decisionActor: props.decisionActor,
    note: props.note,
    scopeOptions: approvalDialogScopeOptions,
    expiryOptions: approvalDialogExpiryOptions,
    actions: approvalDialogActions(disabledDecisions),
    className: `hoopoe-approval-dialog hoopoe-approval-dialog--${props.riskClass}`,
    style: dialogShellStyle(),
  };
}

export function createApprovalDialogDecisionPayload(
  model: ApprovalDialogModel,
  decision: ApprovalDialogDecision,
): ApprovalDialogDecisionPayload {
  return {
    approvalId: model.approvalId,
    decision,
    scope: model.selectedScope,
    expiry: model.selectedExpiry,
    decisionActor: model.decisionActor.trim(),
    note: model.note.trim(),
  };
}

export function renderApprovalDialogElement(
  props: ApprovalDialogProps,
  ownerDocument: Document = document,
): HTMLElement {
  const model = getApprovalDialogModel(props);
  const shell = ownerDocument.createElement("section");
  const panel = ownerDocument.createElement("div");
  const header = ownerDocument.createElement("header");
  const title = ownerDocument.createElement("h2");
  const risk = ownerDocument.createElement("span");
  const summary = ownerDocument.createElement("p");
  const sourceRow = ownerDocument.createElement("div");
  const evidence = ownerDocument.createElement("div");
  const scope = renderSelectField(
    "scope",
    "Scope",
    model.selectedScope,
    model.scopeOptions,
    props.onFieldChange,
    ownerDocument,
  );
  const expiry = renderSelectField(
    "expiry",
    "Expiry",
    model.selectedExpiry,
    model.expiryOptions,
    props.onFieldChange,
    ownerDocument,
  );
  const actor = renderTextField(
    "decisionActor",
    "Decision actor",
    model.decisionActor,
    props.onFieldChange,
    ownerDocument,
  );
  const note = renderNoteField(model.note, props.onFieldChange, ownerDocument);
  const actionRow = ownerDocument.createElement("footer");

  shell.className = model.className;
  shell.setAttribute("role", "dialog");
  shell.setAttribute("aria-modal", "true");
  shell.setAttribute("aria-label", "Approval request");
  assignStyle(shell, model.style);

  assignStyle(panel, panelStyle());

  header.style.display = "grid";
  header.style.gridTemplateColumns = "minmax(0, 1fr) auto";
  header.style.alignItems = "start";
  header.style.gap = hoopoeTokens.spacing[3];

  title.textContent = "Approval Required";
  assignStyle(title, headingStyle("16px"));

  risk.textContent = model.riskLabel;
  assignStyle(risk, pillStyle(model.riskTone));
  header.append(title, risk);

  summary.textContent = model.requestedActionSummary;
  assignStyle(summary, summaryStyle());

  sourceRow.style.display = "flex";
  sourceRow.style.flexWrap = "wrap";
  sourceRow.style.gap = hoopoeTokens.spacing[2];
  sourceRow.append(
    renderMetaChip(`Source: ${model.sourceRuleLabel}`, ownerDocument),
    renderMetaChip(`Approval: ${model.approvalId}`, ownerDocument),
  );
  if (model.targetLabel !== null) {
    sourceRow.append(renderMetaChip(model.targetLabel, ownerDocument));
  }

  evidence.setAttribute("aria-label", "Evidence");
  evidence.style.display = "flex";
  evidence.style.flexWrap = "wrap";
  evidence.style.gap = hoopoeTokens.spacing[2];
  for (const chip of model.evidenceChips) {
    evidence.append(renderEvidenceChip(chip, ownerDocument));
  }

  actionRow.style.display = "flex";
  actionRow.style.flexWrap = "wrap";
  actionRow.style.justifyContent = "end";
  actionRow.style.gap = hoopoeTokens.spacing[2];

  for (const action of model.actions) {
    actionRow.append(renderDecisionButton(action, model, props.onDecision, ownerDocument));
  }

  panel.append(
    header,
    summary,
    sourceRow,
    evidence,
    fieldsGrid([scope, expiry, actor], ownerDocument),
    note,
    actionRow,
  );
  shell.append(panel);
  return shell;
}

function normalizeEvidenceChip(
  chip: ApprovalDialogEvidenceChip,
): Required<ApprovalDialogEvidenceChip> {
  return {
    id: chip.id,
    label: chip.label,
    tone: chip.tone ?? "neutral",
  };
}

function approvalDialogActions(
  disabledDecisions: ReadonlySet<ApprovalDialogDecision>,
): readonly ApprovalDialogActionModel[] {
  return [
    {
      decision: "approve",
      label: "Approve",
      ariaLabel: "Approve this request",
      disabled: disabledDecisions.has("approve"),
      destructive: false,
    },
    {
      decision: "deny",
      label: "Deny",
      ariaLabel: "Deny this request",
      disabled: disabledDecisions.has("deny"),
      destructive: true,
    },
    {
      decision: "extend",
      label: "Extend",
      ariaLabel: "Extend this request",
      disabled: disabledDecisions.has("extend"),
      destructive: false,
    },
  ];
}

function renderSelectField<TValue extends ApprovalDialogScope | ApprovalDialogExpiry>(
  field: "scope" | "expiry",
  labelText: string,
  selectedValue: TValue,
  options: readonly ApprovalDialogOption<TValue>[],
  onFieldChange: ApprovalDialogProps["onFieldChange"],
  ownerDocument: Document,
): HTMLElement {
  const label = ownerDocument.createElement("label");
  const span = ownerDocument.createElement("span");
  const select = ownerDocument.createElement("select");

  assignStyle(label, fieldStyle());
  span.textContent = labelText;
  assignStyle(span, labelStyle());

  select.name = field;
  select.value = selectedValue;
  assignStyle(select, controlStyle());
  select.addEventListener("change", (event) => {
    onFieldChange?.({ field, value: (event.currentTarget as HTMLSelectElement).value }, event);
  });

  for (const optionModel of options) {
    const optionElement = ownerDocument.createElement("option");
    optionElement.value = optionModel.value;
    optionElement.textContent = optionModel.label;
    optionElement.selected = optionModel.value === selectedValue;
    select.append(optionElement);
  }

  label.append(span, select);
  return label;
}

function renderTextField(
  field: "decisionActor",
  labelText: string,
  value: string,
  onFieldChange: ApprovalDialogProps["onFieldChange"],
  ownerDocument: Document,
): HTMLElement {
  const label = ownerDocument.createElement("label");
  const span = ownerDocument.createElement("span");
  const input = ownerDocument.createElement("input");

  assignStyle(label, fieldStyle());
  span.textContent = labelText;
  assignStyle(span, labelStyle());

  input.name = field;
  input.value = value;
  input.autocomplete = "off";
  assignStyle(input, controlStyle());
  input.addEventListener("input", (event) => {
    onFieldChange?.({ field, value: (event.currentTarget as HTMLInputElement).value }, event);
  });

  label.append(span, input);
  return label;
}

function renderNoteField(
  value: string,
  onFieldChange: ApprovalDialogProps["onFieldChange"],
  ownerDocument: Document,
): HTMLElement {
  const label = ownerDocument.createElement("label");
  const span = ownerDocument.createElement("span");
  const textarea = ownerDocument.createElement("textarea");

  assignStyle(label, fieldStyle());
  span.textContent = "Note";
  assignStyle(span, labelStyle());

  textarea.name = "note";
  textarea.value = value;
  textarea.rows = 4;
  assignStyle(textarea, textAreaStyle());
  textarea.addEventListener("input", (event) => {
    onFieldChange?.(
      { field: "note", value: (event.currentTarget as HTMLTextAreaElement).value },
      event,
    );
  });

  label.append(span, textarea);
  return label;
}

function renderDecisionButton(
  action: ApprovalDialogActionModel,
  model: ApprovalDialogModel,
  onDecision: ApprovalDialogProps["onDecision"],
  ownerDocument: Document,
): HTMLButtonElement {
  const button = ownerDocument.createElement("button");
  button.type = "button";
  button.textContent = action.label;
  button.disabled = action.disabled;
  button.dataset.decision = action.decision;
  button.setAttribute("aria-label", action.ariaLabel);
  assignStyle(button, decisionButtonStyle(action));
  button.addEventListener("click", (event) => {
    onDecision?.(createApprovalDialogDecisionPayload(model, action.decision), event);
  });
  return button;
}

function fieldsGrid(fields: readonly HTMLElement[], ownerDocument: Document): HTMLElement {
  const grid = ownerDocument.createElement("div");
  grid.style.display = "grid";
  grid.style.gridTemplateColumns = "repeat(auto-fit, minmax(180px, 1fr))";
  grid.style.gap = hoopoeTokens.spacing[3];
  grid.append(...fields);
  return grid;
}

function renderMetaChip(label: string, ownerDocument: Document): HTMLElement {
  const chip = ownerDocument.createElement("span");
  chip.textContent = label;
  assignStyle(chip, metaChipStyle());
  return chip;
}

function renderEvidenceChip(
  chip: Required<ApprovalDialogEvidenceChip>,
  ownerDocument: Document,
): HTMLElement {
  const element = ownerDocument.createElement("span");
  element.textContent = chip.label;
  assignStyle(element, pillStyle(evidenceToneByChipTone[chip.tone]));
  return element;
}

function option<TValue extends string>(
  value: TValue,
  label: string,
  description: string,
): ApprovalDialogOption<TValue> {
  return { value, label, description };
}

function dialogShellStyle(): Readonly<Record<string, string>> {
  return {
    position: "fixed",
    inset: "0",
    display: "grid",
    placeItems: "center",
    padding: "24px",
    background: "rgba(15, 15, 17, 0.64)",
    color: hoopoeTokens.color.surface.dark.text,
    fontFamily: hoopoeTokens.typography.sans.join(", "),
    letterSpacing: "0",
  };
}

function panelStyle(): Readonly<Record<string, string>> {
  return {
    display: "grid",
    gap: hoopoeTokens.spacing[4],
    width: "min(680px, 100%)",
    maxHeight: "min(720px, calc(100vh - 48px))",
    overflow: "auto",
    padding: hoopoeTokens.spacing[4],
    borderRadius: hoopoeTokens.radius.lg,
    border: `1px solid ${hoopoeTokens.color.surface.dark.border}`,
    background: hoopoeTokens.color.surface.dark.panel,
    boxShadow: hoopoeTokens.shadow.glass,
  };
}

function headingStyle(fontSize: string): Readonly<Record<string, string>> {
  return {
    margin: "0",
    color: hoopoeTokens.color.surface.dark.text,
    fontSize,
    fontWeight: "750",
    lineHeight: "1.2",
    letterSpacing: "0",
  };
}

function summaryStyle(): Readonly<Record<string, string>> {
  return {
    margin: "0",
    color: hoopoeTokens.color.surface.dark.text,
    fontSize: "14px",
    fontWeight: "600",
    lineHeight: "1.45",
    letterSpacing: "0",
  };
}

function fieldStyle(): Readonly<Record<string, string>> {
  return {
    display: "grid",
    gap: hoopoeTokens.spacing[1.5],
    minWidth: "0",
  };
}

function labelStyle(): Readonly<Record<string, string>> {
  return {
    color: hoopoeTokens.color.surface.dark.textDim,
    fontSize: "12px",
    fontWeight: "700",
    lineHeight: "1.2",
    letterSpacing: "0",
  };
}

function controlStyle(): Readonly<Record<string, string>> {
  return {
    width: "100%",
    minHeight: "36px",
    padding: "7px 9px",
    borderRadius: hoopoeTokens.radius.md,
    border: `1px solid ${hoopoeTokens.color.surface.dark.border}`,
    background: hoopoeTokens.color.surface.dark.panelAlt,
    color: hoopoeTokens.color.surface.dark.text,
    font: "inherit",
    fontSize: "13px",
    lineHeight: "1.2",
    letterSpacing: "0",
  };
}

function textAreaStyle(): Readonly<Record<string, string>> {
  return {
    ...controlStyle(),
    minHeight: "88px",
    resize: "vertical",
    lineHeight: "1.4",
  };
}

function pillStyle(tone: ToneToken): Readonly<Record<string, string>> {
  return {
    display: "inline-flex",
    alignItems: "center",
    width: "fit-content",
    minHeight: "24px",
    padding: "4px 8px",
    borderRadius: hoopoeTokens.radius.full,
    border: `1px solid ${tone.border}`,
    background: tone.bg,
    color: tone.fg,
    fontSize: "12px",
    fontWeight: "750",
    lineHeight: "1",
    letterSpacing: "0",
    whiteSpace: "nowrap",
  };
}

function metaChipStyle(): Readonly<Record<string, string>> {
  return {
    display: "inline-flex",
    alignItems: "center",
    minHeight: "22px",
    padding: "3px 7px",
    borderRadius: hoopoeTokens.radius.full,
    border: `1px solid ${hoopoeTokens.color.surface.dark.borderSoft}`,
    background: hoopoeTokens.color.surface.dark.panelAlt,
    color: hoopoeTokens.color.surface.dark.textDim,
    fontSize: "11px",
    fontWeight: "700",
    lineHeight: "1",
    letterSpacing: "0",
    whiteSpace: "nowrap",
  };
}

function decisionButtonStyle(action: ApprovalDialogActionModel): Readonly<Record<string, string>> {
  const tone = action.destructive ? statusTones.blocked : statusTones.approved;

  return {
    minHeight: "34px",
    padding: "7px 12px",
    borderRadius: hoopoeTokens.radius.md,
    border: `1px solid ${tone.border}`,
    background: action.disabled ? "transparent" : tone.bg,
    color: action.disabled ? hoopoeTokens.color.surface.dark.textMute : tone.fg,
    font: "inherit",
    fontSize: "13px",
    fontWeight: "750",
    lineHeight: "1.2",
    letterSpacing: "0",
    cursor: action.disabled ? "not-allowed" : "pointer",
  };
}

function assignStyle(element: HTMLElement, styles: Readonly<Record<string, string>>): void {
  for (const [name, value] of Object.entries(styles)) {
    element.style.setProperty(kebabCase(name), value);
  }
}

function kebabCase(value: string): string {
  return value.replace(/[A-Z]/g, (match) => `-${match.toLowerCase()}`);
}

const sourceRuleLabels: Readonly<Record<ApprovalDialogSourceRule, string>> = {
  "hoopoe-policy": "Hoopoe policy",
  dcg: "DCG",
  slb: "SLB",
};

const riskLabels: Readonly<Record<ApprovalDialogRiskClass, string>> = {
  low: "Low risk",
  medium: "Medium risk",
  high: "High risk",
  critical: "Critical risk",
};

const riskToneByClass: Readonly<Record<ApprovalDialogRiskClass, ToneToken>> = {
  low: statusTones.approved,
  medium: statusTones.waiting_approval,
  high: statusTones.blocked,
  critical: statusTones.failed,
};

const evidenceToneByChipTone: Readonly<
  Record<Required<ApprovalDialogEvidenceChip>["tone"], ToneToken>
> = {
  neutral: statusTones.muted,
  warning: statusTones.waiting_approval,
  danger: statusTones.blocked,
};
