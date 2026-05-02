import { agentFamilyTones, statusTones } from "../../tokens/index.ts";
import type { AgentFamily, ToneToken } from "../../tokens/index.ts";
import {
  getPriorityChipModel,
  type PriorityChipModel,
  type PriorityChipProps,
} from "../PriorityChip/priority-chip.ts";

export type BeadCardVariant = "compact" | "expanded";

export type BeadCardStatus =
  | "ready"
  | "claimed"
  | "in_progress"
  | "in_review"
  | "blocked"
  | "paused"
  | "closed";

export interface BeadCardOwner {
  readonly agentName: string;
  readonly harness?: AgentFamily;
}

export interface BeadCardProps {
  readonly id: string;
  readonly title: string;
  readonly status: BeadCardStatus;
  readonly priority: PriorityChipProps["priority"];
  readonly owner?: BeadCardOwner | null;
  readonly filesTouched?: number;
  readonly ageLabel?: string | null;
  readonly variant?: BeadCardVariant;
  readonly ariaLabel?: string;
}

export interface BeadCardModel {
  readonly id: string;
  readonly title: string;
  readonly status: {
    readonly id: BeadCardStatus;
    readonly label: string;
    readonly tone: ToneToken;
    readonly marker: string;
  };
  readonly priorityChip: PriorityChipModel;
  readonly owner: {
    readonly agentName: string;
    readonly tone: ToneToken | null;
  } | null;
  readonly filesTouchedLabel: string | null;
  readonly ageLabel: string | null;
  readonly variant: BeadCardVariant;
  readonly ariaLabel: string;
}

const STATUS_LABELS: Record<BeadCardStatus, string> = {
  ready: "Ready",
  claimed: "Claimed",
  in_progress: "In progress",
  in_review: "In review",
  blocked: "Blocked",
  paused: "Paused",
  closed: "Closed",
};

const STATUS_MARKERS: Record<BeadCardStatus, string> = {
  ready: "○",
  claimed: "◐",
  in_progress: "▶",
  in_review: "◇",
  blocked: "■",
  paused: "‖",
  closed: "●",
};

export const beadCardStatuses: ReadonlyArray<BeadCardStatus> = [
  "ready",
  "claimed",
  "in_progress",
  "in_review",
  "blocked",
  "paused",
  "closed",
];

export function getBeadCardModel(props: BeadCardProps): BeadCardModel {
  const variant = props.variant ?? "expanded";
  const statusLabel = STATUS_LABELS[props.status];
  const statusTone = statusTones[props.status];
  const statusMarker = STATUS_MARKERS[props.status];
  const priorityChip = getPriorityChipModel({
    priority: props.priority,
    size: variant === "compact" ? "sm" : "md",
  });
  const owner =
    props.owner === undefined || props.owner === null
      ? null
      : {
          agentName: props.owner.agentName,
          tone: props.owner.harness ? agentFamilyTones[props.owner.harness] : null,
        };
  const filesTouchedLabel =
    typeof props.filesTouched === "number"
      ? props.filesTouched === 1
        ? "1 file"
        : `${props.filesTouched} files`
      : null;
  const ageLabel = props.ageLabel ?? null;
  const ariaLabel =
    props.ariaLabel ??
    `Bead ${props.id}: ${props.title}. ${statusLabel}. Priority ${props.priority.toUpperCase()}.`;
  return {
    id: props.id,
    title: props.title,
    status: { id: props.status, label: statusLabel, tone: statusTone, marker: statusMarker },
    priorityChip,
    owner,
    filesTouchedLabel,
    ageLabel,
    variant,
    ariaLabel,
  };
}
