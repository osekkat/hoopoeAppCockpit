import {
  hoopoeTokens,
  statusTones,
  toolHealthTones,
} from "../tokens/index.ts";
import type { StatusTone, ToneToken, ToolHealthTone } from "../tokens/index.ts";

export type StatusPillKind = "bead" | "job" | "approval" | "capability" | "tool";
export type StatusPillSize = "sm" | "md";

export type BeadStatus =
  | "ready"
  | "claimed"
  | "in_progress"
  | "in_review"
  | "closed"
  | "blocked"
  | "paused";
export type JobStatus =
  | "queued"
  | "running"
  | "waiting_approval"
  | "canceling"
  | "succeeded"
  | "failed"
  | "interrupted";
export type ApprovalStatus =
  | "pending"
  | "approved"
  | "denied"
  | "expired"
  | "superseded_by";
export type CapabilityStatus = "ok" | "degraded" | "missing" | "blocked-by-policy";
export type ToolStatus = ToolHealthTone;

export type StatusPillProps =
  | BaseStatusPillProps<"bead", BeadStatus>
  | BaseStatusPillProps<"job", JobStatus>
  | BaseStatusPillProps<"approval", ApprovalStatus>
  | BaseStatusPillProps<"capability", CapabilityStatus>
  | BaseStatusPillProps<"tool", ToolStatus>;

export interface StatusPillModel {
  readonly kind: StatusPillKind;
  readonly state: string;
  readonly size: StatusPillSize;
  readonly label: string;
  readonly ariaLabel: string;
  readonly marker: string;
  readonly tone: ToneToken;
  readonly className: string;
  readonly style: Readonly<Record<string, string>>;
  readonly markerStyle: Readonly<Record<string, string>>;
}

export const statusPillStatesByKind = {
  bead: ["ready", "claimed", "in_progress", "in_review", "closed", "blocked", "paused"],
  job: [
    "queued",
    "running",
    "waiting_approval",
    "canceling",
    "succeeded",
    "failed",
    "interrupted",
  ],
  approval: ["pending", "approved", "denied", "expired", "superseded_by"],
  capability: ["ok", "degraded", "missing", "blocked-by-policy"],
  tool: ["green", "yellow", "red"],
} as const satisfies Record<StatusPillKind, readonly string[]>;

export function getStatusPillModel(props: StatusPillProps): StatusPillModel {
  const size = props.size ?? "md";
  const label = props.label ?? formatStatusLabel(props.state);
  const tone = getStatusPillTone(props.kind, props.state);
  const marker = markerForStatus(props.kind, props.state);

  return {
    kind: props.kind,
    state: props.state,
    size,
    label,
    ariaLabel: props.ariaLabel ?? `${formatStatusLabel(props.kind)} status: ${label}`,
    marker,
    tone,
    className: `hoopoe-status-pill hoopoe-status-pill--${props.kind} hoopoe-status-pill--${size}`,
    style: statusPillStyle(tone, size),
    markerStyle: statusPillMarkerStyle(tone, size),
  };
}

export function renderStatusPillElement(
  props: StatusPillProps,
  ownerDocument: Document = document,
): HTMLElement {
  const model = getStatusPillModel(props);
  const pill = ownerDocument.createElement("span");
  const marker = ownerDocument.createElement("span");
  const label = ownerDocument.createElement("span");

  pill.className = model.className;
  pill.setAttribute("role", "status");
  pill.setAttribute("aria-label", model.ariaLabel);
  assignStyle(pill, model.style);

  marker.textContent = model.marker;
  marker.setAttribute("aria-hidden", "true");
  assignStyle(marker, model.markerStyle);

  label.textContent = model.label;
  pill.append(marker, label);

  return pill;
}

function getStatusPillTone(kind: StatusPillKind, state: string): ToneToken {
  if (kind === "tool") {
    return toolHealthTones[state as ToolStatus];
  }

  return statusTones[state as StatusTone];
}

function statusPillStyle(
  tone: ToneToken,
  size: StatusPillSize,
): Readonly<Record<string, string>> {
  const sizeTokens = size === "sm" ? smallSizeTokens : mediumSizeTokens;

  return {
    display: "inline-grid",
    gridTemplateColumns: "auto 1fr",
    alignItems: "center",
    gap: hoopoeTokens.spacing[1],
    width: "fit-content",
    minHeight: sizeTokens.minHeight,
    padding: sizeTokens.padding,
    borderRadius: hoopoeTokens.radius.full,
    border: `1px solid ${tone.border}`,
    background: tone.bg,
    color: tone.fg,
    fontFamily: hoopoeTokens.typography.sans.join(", "),
    fontSize: sizeTokens.fontSize,
    fontWeight: "650",
    lineHeight: "1",
    letterSpacing: "0",
    whiteSpace: "nowrap",
  };
}

function statusPillMarkerStyle(
  tone: ToneToken,
  size: StatusPillSize,
): Readonly<Record<string, string>> {
  const sizeTokens = size === "sm" ? smallSizeTokens : mediumSizeTokens;

  return {
    display: "inline-grid",
    placeItems: "center",
    minWidth: sizeTokens.markerSize,
    height: sizeTokens.markerSize,
    borderRadius: hoopoeTokens.radius.full,
    background: tone.dot,
    color: "#FFFFFF",
    fontSize: sizeTokens.markerFontSize,
    fontWeight: "800",
    lineHeight: "1",
  };
}

function markerForStatus(kind: StatusPillKind, state: string): string {
  const marker = statusMarkers[`${kind}:${state}`];

  if (marker === undefined) {
    throw new Error(`Unsupported StatusPill state ${kind}:${state}`);
  }

  return marker;
}

function formatStatusLabel(value: string): string {
  const spaced = value.replaceAll("_", " ").replaceAll("-", " ");

  return spaced.replace(/\b\w/g, (match) => match.toUpperCase());
}

function assignStyle(element: HTMLElement, styles: Readonly<Record<string, string>>): void {
  for (const [name, value] of Object.entries(styles)) {
    element.style.setProperty(kebabCase(name), value);
  }
}

function kebabCase(value: string): string {
  return value.replace(/[A-Z]/g, (match) => `-${match.toLowerCase()}`);
}

interface BaseStatusPillProps<TKind extends StatusPillKind, TState extends string> {
  readonly kind: TKind;
  readonly state: TState;
  readonly size?: StatusPillSize;
  readonly label?: string;
  readonly ariaLabel?: string;
}

const mediumSizeTokens = {
  minHeight: "24px",
  padding: "3px 8px 3px 4px",
  fontSize: "12px",
  markerSize: "18px",
  markerFontSize: "9px",
} as const;

const smallSizeTokens = {
  minHeight: "20px",
  padding: "2px 7px 2px 3px",
  fontSize: "11px",
  markerSize: "15px",
  markerFontSize: "8px",
} as const;

const statusMarkers: Readonly<Record<string, string>> = {
  "bead:ready": "R",
  "bead:claimed": "C",
  "bead:in_progress": "W",
  "bead:in_review": "V",
  "bead:closed": "D",
  "bead:blocked": "!",
  "bead:paused": "P",
  "job:queued": "Q",
  "job:running": "W",
  "job:waiting_approval": "A",
  "job:canceling": "X",
  "job:succeeded": "D",
  "job:failed": "!",
  "job:interrupted": "I",
  "approval:pending": "P",
  "approval:approved": "A",
  "approval:denied": "N",
  "approval:expired": "X",
  "approval:superseded_by": "S",
  "capability:ok": "O",
  "capability:degraded": "~",
  "capability:missing": "?",
  "capability:blocked-by-policy": "!",
  "tool:green": "G",
  "tool:yellow": "Y",
  "tool:red": "R",
};
