import { agentFamilyTones, hoopoeTokens, statusTones } from "../../tokens/index.ts";
import type { AgentFamily, ToneToken } from "../../tokens/index.ts";

export type AgentTileHarness = Extract<AgentFamily, "claude" | "codex" | "gemini">;

export type AgentTileStatus = "working" | "idle" | "awaiting-review" | "wedged" | "rate-limited";

export type AgentTileBeadStatus =
  | "ready"
  | "claimed"
  | "in_progress"
  | "in_review"
  | "blocked"
  | "paused"
  | "closed";

export type AgentTileActionId =
  | "switch-account"
  | "resume-session"
  | "pause-and-notify"
  | "kill-and-reassign"
  | "send-marching-orders";

export interface AgentTileBeadClaim {
  readonly id: string;
  readonly title: string;
  readonly status: AgentTileBeadStatus;
}

export interface AgentTileDecision {
  readonly id: string;
  readonly label: string;
  readonly actor?: string;
  readonly occurredAtLabel?: string;
}

export interface AgentTileProps {
  readonly agentName: string;
  readonly harness: AgentTileHarness;
  readonly caamAccount: string;
  readonly status: AgentTileStatus;
  readonly currentBead?: AgentTileBeadClaim | null;
  readonly timeOnBeadLabel?: string | null;
  readonly recentDecisions?: readonly AgentTileDecision[];
  readonly selectedActionId?: AgentTileActionId | null;
  readonly onAction?: (action: AgentTileActionModel, event: MouseEvent) => void;
}

export interface AgentTileHarnessModel {
  readonly id: AgentTileHarness;
  readonly label: string;
  readonly shortLabel: string;
  readonly ariaLabel: string;
  readonly tone: ToneToken;
}

export interface AgentTileStatusModel {
  readonly id: AgentTileStatus;
  readonly label: string;
  readonly ariaLabel: string;
  readonly marker: string;
  readonly tone: ToneToken;
}

export interface AgentTileBeadModel {
  readonly id: string;
  readonly title: string;
  readonly status: AgentTileBeadStatus | "none";
  readonly statusLabel: string;
}

export interface AgentTileDecisionModel {
  readonly id: string;
  readonly label: string;
  readonly actor: string | null;
  readonly occurredAtLabel: string | null;
}

export interface AgentTileActionModel {
  readonly id: AgentTileActionId;
  readonly label: string;
  readonly ariaLabel: string;
  readonly destructive: boolean;
}

export interface AgentTileModel {
  readonly agentName: string;
  readonly harness: AgentTileHarnessModel;
  readonly caamAccount: string;
  readonly status: AgentTileStatusModel;
  readonly currentBead: AgentTileBeadModel;
  readonly timeOnBeadLabel: string;
  readonly recentDecisions: readonly AgentTileDecisionModel[];
  readonly actions: readonly AgentTileActionModel[];
  readonly selectedAction: AgentTileActionModel | null;
  readonly className: string;
  readonly style: Readonly<Record<string, string>>;
}

export const agentTileStatuses = [
  "working",
  "idle",
  "awaiting-review",
  "wedged",
  "rate-limited",
] as const satisfies readonly AgentTileStatus[];

export const agentTileActions = [
  action("switch-account", "Switch account", "Switch the CAAM account for this agent", false),
  action("resume-session", "Resume session", "Resume this agent session", false),
  action("pause-and-notify", "Pause and notify", "Pause this agent and notify the operator", false),
  action("kill-and-reassign", "Kill and reassign", "Stop this agent and reassign its bead", true),
  action(
    "send-marching-orders",
    "Send marching orders",
    "Send marching orders to this agent",
    false,
  ),
] as const satisfies readonly AgentTileActionModel[];

export function getAgentTileModel(props: AgentTileProps): AgentTileModel {
  const harness = getHarnessModel(props.harness);
  const status = getAgentTileStatusModel(props.status);
  const decisions = props.recentDecisions?.slice(0, 3).map(toDecisionModel) ?? [];
  const selectedAction =
    props.selectedActionId === undefined || props.selectedActionId === null
      ? null
      : getActionById(props.selectedActionId);

  return {
    agentName: props.agentName,
    harness,
    caamAccount: props.caamAccount,
    status,
    currentBead: getBeadModel(props.currentBead),
    timeOnBeadLabel: props.timeOnBeadLabel ?? "No active bead timer",
    recentDecisions: decisions,
    actions: agentTileActions,
    selectedAction,
    className: `hoopoe-agent-tile hoopoe-agent-tile--${props.harness} hoopoe-agent-tile--${props.status}`,
    style: agentTileStyle(),
  };
}

export function renderAgentTileElement(
  props: AgentTileProps,
  ownerDocument: Document = document,
): HTMLElement {
  const model = getAgentTileModel(props);
  const tile = ownerDocument.createElement("article");
  const header = ownerDocument.createElement("header");
  const harnessBadge = ownerDocument.createElement("span");
  const identity = ownerDocument.createElement("div");
  const name = ownerDocument.createElement("h3");
  const account = ownerDocument.createElement("p");
  const statusRow = ownerDocument.createElement("div");
  const statusBadge = ownerDocument.createElement("span");
  const bead = ownerDocument.createElement("section");
  const beadTitle = ownerDocument.createElement("p");
  const beadMeta = ownerDocument.createElement("p");
  const decisions = ownerDocument.createElement("section");
  const decisionsHeading = ownerDocument.createElement("h4");
  const decisionsList = ownerDocument.createElement("ul");
  const menu = ownerDocument.createElement("details");
  const menuLabel = ownerDocument.createElement("summary");
  const actions = ownerDocument.createElement("div");

  tile.className = model.className;
  tile.setAttribute("aria-label", `${model.agentName} agent tile`);
  assignStyle(tile, model.style);

  header.style.display = "grid";
  header.style.gridTemplateColumns = "40px minmax(0, 1fr)";
  header.style.alignItems = "center";
  header.style.gap = hoopoeTokens.spacing[3];

  harnessBadge.textContent = model.harness.shortLabel;
  harnessBadge.setAttribute("aria-label", model.harness.ariaLabel);
  assignStyle(harnessBadge, harnessBadgeStyle(model.harness.tone));

  identity.style.minWidth = "0";
  name.textContent = model.agentName;
  assignStyle(name, headingStyle("16px"));

  account.textContent = model.caamAccount;
  assignStyle(account, monoTextStyle());

  identity.append(name, account);
  header.append(harnessBadge, identity);

  statusRow.style.display = "flex";
  statusRow.style.alignItems = "center";
  statusRow.style.justifyContent = "space-between";
  statusRow.style.gap = hoopoeTokens.spacing[2];

  statusBadge.textContent = `${model.status.marker} ${model.status.label}`;
  statusBadge.setAttribute("role", "status");
  statusBadge.setAttribute("aria-label", model.status.ariaLabel);
  assignStyle(statusBadge, statusBadgeStyle(model.status.tone));

  const timer = ownerDocument.createElement("span");
  timer.textContent = model.timeOnBeadLabel;
  assignStyle(timer, timerStyle());
  statusRow.append(statusBadge, timer);

  bead.setAttribute("aria-label", "Current bead");
  assignStyle(bead, sectionStyle());
  beadTitle.textContent =
    model.currentBead.status === "none"
      ? model.currentBead.title
      : `${model.currentBead.id} ${model.currentBead.title}`;
  assignStyle(beadTitle, bodyTextStyle());
  beadMeta.textContent = model.currentBead.statusLabel;
  assignStyle(beadMeta, mutedTextStyle());
  bead.append(beadTitle, beadMeta);

  decisions.setAttribute("aria-label", "Recent decisions");
  assignStyle(decisions, sectionStyle());
  decisionsHeading.textContent = "Recent decisions";
  assignStyle(decisionsHeading, headingStyle("12px"));
  assignStyle(decisionsList, listStyle());

  for (const decision of model.recentDecisions) {
    decisionsList.append(renderDecisionElement(decision, ownerDocument));
  }

  if (model.recentDecisions.length === 0) {
    const empty = ownerDocument.createElement("li");
    empty.textContent = "No recent decisions";
    assignStyle(empty, mutedTextStyle());
    decisionsList.append(empty);
  }

  decisions.append(decisionsHeading, decisionsList);

  menuLabel.textContent = "Actions";
  assignStyle(menuLabel, menuLabelStyle());
  actions.style.display = "grid";
  actions.style.gap = hoopoeTokens.spacing[1.5];
  actions.style.marginTop = hoopoeTokens.spacing[2];

  for (const actionModel of model.actions) {
    actions.append(renderActionButton(actionModel, props.onAction, ownerDocument));
  }

  menu.append(menuLabel, actions);
  tile.append(header, statusRow, bead, decisions, menu);

  return tile;
}

function getHarnessModel(harness: AgentTileHarness): AgentTileHarnessModel {
  const labels: Record<AgentTileHarness, Pick<AgentTileHarnessModel, "label" | "shortLabel">> = {
    claude: { label: "Claude Code", shortLabel: "CC" },
    codex: { label: "Codex CLI", shortLabel: "CX" },
    gemini: { label: "Gemini CLI", shortLabel: "GM" },
  };

  const label = labels[harness];

  return {
    id: harness,
    label: label.label,
    shortLabel: label.shortLabel,
    ariaLabel: `${label.label} harness`,
    tone: agentFamilyTones[harness],
  };
}

function getAgentTileStatusModel(status: AgentTileStatus): AgentTileStatusModel {
  const model = statusModelById[status];

  if (model === undefined) {
    throw new Error(`Unsupported AgentTile status ${status}`);
  }

  return model;
}

function getBeadModel(bead: AgentTileProps["currentBead"]): AgentTileBeadModel {
  if (bead === undefined || bead === null) {
    return {
      id: "",
      title: "No bead claimed",
      status: "none",
      statusLabel: "Unassigned",
    };
  }

  return {
    id: bead.id,
    title: bead.title,
    status: bead.status,
    statusLabel: formatLabel(bead.status),
  };
}

function toDecisionModel(decision: AgentTileDecision): AgentTileDecisionModel {
  return {
    id: decision.id,
    label: decision.label,
    actor: decision.actor ?? null,
    occurredAtLabel: decision.occurredAtLabel ?? null,
  };
}

function getActionById(id: AgentTileActionId): AgentTileActionModel {
  const actionModel = agentTileActions.find((candidate) => candidate.id === id);

  if (actionModel === undefined) {
    throw new Error(`Unsupported AgentTile action ${id}`);
  }

  return actionModel;
}

function renderDecisionElement(
  decision: AgentTileDecisionModel,
  ownerDocument: Document,
): HTMLElement {
  const item = ownerDocument.createElement("li");
  const label = ownerDocument.createElement("span");
  const meta = ownerDocument.createElement("span");
  const metaParts = [decision.actor, decision.occurredAtLabel].filter(Boolean);

  item.style.display = "grid";
  item.style.gap = hoopoeTokens.spacing[0.5];

  label.textContent = decision.label;
  assignStyle(label, bodyTextStyle());

  meta.textContent = metaParts.length === 0 ? "Decision recorded" : metaParts.join(" / ");
  assignStyle(meta, mutedTextStyle());

  item.append(label, meta);
  return item;
}

function renderActionButton(
  actionModel: AgentTileActionModel,
  onAction: AgentTileProps["onAction"],
  ownerDocument: Document,
): HTMLButtonElement {
  const button = ownerDocument.createElement("button");
  button.type = "button";
  button.textContent = actionModel.label;
  button.dataset.action = actionModel.id;
  button.setAttribute("aria-label", actionModel.ariaLabel);
  assignStyle(button, actionButtonStyle(actionModel.destructive));

  if (onAction !== undefined) {
    button.addEventListener("click", (event) => onAction(actionModel, event));
  }

  return button;
}

function action(
  id: AgentTileActionId,
  label: string,
  ariaLabel: string,
  destructive: boolean,
): AgentTileActionModel {
  return { id, label, ariaLabel, destructive };
}

function formatLabel(value: string): string {
  const spaced = value.replaceAll("_", " ").replaceAll("-", " ");

  return spaced.replace(/\b\w/g, (match) => match.toUpperCase());
}

function agentTileStyle(): Readonly<Record<string, string>> {
  const surface = hoopoeTokens.color.surface.dark;

  return {
    display: "grid",
    gap: hoopoeTokens.spacing[4],
    width: "100%",
    minWidth: "280px",
    maxWidth: "420px",
    padding: hoopoeTokens.spacing[4],
    borderRadius: hoopoeTokens.radius.lg,
    border: `1px solid ${surface.border}`,
    background: surface.panel,
    color: surface.text,
    boxShadow: hoopoeTokens.shadow.soft,
    fontFamily: hoopoeTokens.typography.sans.join(", "),
    letterSpacing: "0",
  };
}

function harnessBadgeStyle(tone: ToneToken): Readonly<Record<string, string>> {
  return {
    display: "inline-grid",
    placeItems: "center",
    width: "40px",
    height: "40px",
    borderRadius: hoopoeTokens.radius.lg,
    border: `1px solid ${tone.border}`,
    background: tone.bg,
    color: tone.fg,
    fontSize: "12px",
    fontWeight: "800",
    lineHeight: "1",
  };
}

function statusBadgeStyle(tone: ToneToken): Readonly<Record<string, string>> {
  return {
    display: "inline-flex",
    alignItems: "center",
    gap: hoopoeTokens.spacing[1],
    minHeight: "24px",
    padding: "4px 8px",
    borderRadius: hoopoeTokens.radius.full,
    border: `1px solid ${tone.border}`,
    background: tone.bg,
    color: tone.fg,
    fontSize: "12px",
    fontWeight: "700",
    lineHeight: "1",
    whiteSpace: "nowrap",
  };
}

function sectionStyle(): Readonly<Record<string, string>> {
  return {
    display: "grid",
    gap: hoopoeTokens.spacing[1.5],
    paddingTop: hoopoeTokens.spacing[3],
    borderTop: `1px solid ${hoopoeTokens.color.surface.dark.borderSoft}`,
  };
}

function headingStyle(fontSize: string): Readonly<Record<string, string>> {
  return {
    margin: "0",
    color: hoopoeTokens.color.surface.dark.text,
    fontSize,
    fontWeight: "700",
    lineHeight: "1.2",
    letterSpacing: "0",
  };
}

function bodyTextStyle(): Readonly<Record<string, string>> {
  return {
    margin: "0",
    color: hoopoeTokens.color.surface.dark.text,
    fontSize: "13px",
    lineHeight: "1.35",
    letterSpacing: "0",
  };
}

function mutedTextStyle(): Readonly<Record<string, string>> {
  return {
    margin: "0",
    color: hoopoeTokens.color.surface.dark.textDim,
    fontSize: "12px",
    lineHeight: "1.35",
    letterSpacing: "0",
  };
}

function monoTextStyle(): Readonly<Record<string, string>> {
  return {
    margin: "2px 0 0",
    color: hoopoeTokens.color.surface.dark.textDim,
    fontFamily: hoopoeTokens.typography.mono.join(", "),
    fontSize: "11px",
    lineHeight: "1.35",
    letterSpacing: "0",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  };
}

function timerStyle(): Readonly<Record<string, string>> {
  return {
    color: hoopoeTokens.color.surface.dark.textDim,
    fontFamily: hoopoeTokens.typography.mono.join(", "),
    fontSize: "12px",
    lineHeight: "1",
    letterSpacing: "0",
    whiteSpace: "nowrap",
  };
}

function listStyle(): Readonly<Record<string, string>> {
  return {
    display: "grid",
    gap: hoopoeTokens.spacing[2],
    margin: "0",
    padding: "0",
    listStyle: "none",
  };
}

function menuLabelStyle(): Readonly<Record<string, string>> {
  return {
    cursor: "pointer",
    color: hoopoeTokens.color.surface.dark.text,
    fontSize: "12px",
    fontWeight: "700",
    lineHeight: "1.2",
    letterSpacing: "0",
  };
}

function actionButtonStyle(destructive: boolean): Readonly<Record<string, string>> {
  const tone = destructive ? statusTones.blocked : statusTones.ready;

  return {
    width: "100%",
    minHeight: "28px",
    padding: "5px 8px",
    borderRadius: hoopoeTokens.radius.md,
    border: `1px solid ${tone.border}`,
    background: "transparent",
    color: tone.fg,
    font: "inherit",
    fontSize: "12px",
    fontWeight: "650",
    lineHeight: "1.2",
    textAlign: "left",
    letterSpacing: "0",
    cursor: "pointer",
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

const statusModelById: Readonly<Record<AgentTileStatus, AgentTileStatusModel>> = {
  working: {
    id: "working",
    label: "Working",
    ariaLabel: "Agent status: working",
    marker: "W",
    tone: statusTones.running,
  },
  idle: {
    id: "idle",
    label: "Idle",
    ariaLabel: "Agent status: idle",
    marker: "I",
    tone: statusTones.muted,
  },
  "awaiting-review": {
    id: "awaiting-review",
    label: "Awaiting review",
    ariaLabel: "Agent status: awaiting review",
    marker: "V",
    tone: statusTones.in_review,
  },
  wedged: {
    id: "wedged",
    label: "Wedged",
    ariaLabel: "Agent status: wedged",
    marker: "!",
    tone: statusTones.blocked,
  },
  "rate-limited": {
    id: "rate-limited",
    label: "Rate limited",
    ariaLabel: "Agent status: rate limited",
    marker: "L",
    tone: statusTones.waiting_approval,
  },
};
