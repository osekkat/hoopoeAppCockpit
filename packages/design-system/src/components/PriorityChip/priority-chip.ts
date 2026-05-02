import { priorityTones } from "../../tokens/index.ts";
import type { PriorityChipVariant, ToneToken } from "../../tokens/index.ts";

export type PriorityChipSize = "sm" | "md";

export interface PriorityChipProps {
  readonly priority: PriorityChipVariant;
  readonly size?: PriorityChipSize;
  readonly label?: string;
  readonly ariaLabel?: string;
}

export interface PriorityChipModel {
  readonly priority: PriorityChipVariant;
  readonly size: PriorityChipSize;
  readonly label: string;
  readonly ariaLabel: string;
  /** Visible non-color marker so the chip is distinguishable for users with
   * impaired color perception. P0 = `!!`, P1 = `!`, …  */
  readonly marker: string;
  readonly tone: ToneToken;
}

export const priorityChipVariants: ReadonlyArray<PriorityChipVariant> = [
  "p0",
  "p1",
  "p2",
  "p3",
  "p4",
];

const PRIORITY_LABELS: Record<PriorityChipVariant, string> = {
  p0: "P0 · critical",
  p1: "P1 · high",
  p2: "P2 · medium",
  p3: "P3 · low",
  p4: "P4 · backlog",
};

const PRIORITY_MARKERS: Record<PriorityChipVariant, string> = {
  p0: "!!",
  p1: "!",
  p2: "•",
  p3: "·",
  p4: "·",
};

export function getPriorityChipModel(props: PriorityChipProps): PriorityChipModel {
  const size = props.size ?? "md";
  const label = props.label ?? PRIORITY_LABELS[props.priority];
  const marker = PRIORITY_MARKERS[props.priority];
  const ariaLabel = props.ariaLabel ?? `Priority ${props.priority.toUpperCase()}: ${label}`;
  const tone = priorityTones[props.priority];
  return {
    priority: props.priority,
    size,
    label,
    ariaLabel,
    marker,
    tone,
  };
}
