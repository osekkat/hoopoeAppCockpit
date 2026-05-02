import {
  coverageRamp,
  coverageThresholds,
} from "../../tokens/index.ts";
import type { CoverageBand, CoverageRampStop, ToneToken } from "../../tokens/index.ts";

export type CoverageBarSize = "sm" | "md" | "lg";

export interface CoverageBarProps {
  readonly percent: number;
  readonly size?: CoverageBarSize;
  /** Optional override for the threshold gridlines (defaults: 60, 80). */
  readonly gridlines?: ReadonlyArray<number>;
  /** Hide the percentage label (used in compact rows). */
  readonly hideLabel?: boolean;
  readonly ariaLabel?: string;
}

export interface CoverageBarGridline {
  readonly position: number;
  readonly band: CoverageBand;
}

export interface CoverageBarModel {
  readonly percent: number;
  readonly clampedPercent: number;
  readonly band: CoverageBand;
  readonly tone: ToneToken;
  readonly size: CoverageBarSize;
  readonly label: string;
  readonly ariaLabel: string;
  readonly gridlines: ReadonlyArray<CoverageBarGridline>;
  readonly hideLabel: boolean;
}

export function classifyCoverageBand(percent: number): CoverageBand {
  if (percent >= coverageThresholds.high) return "high";
  if (percent >= coverageThresholds.medium) return "medium";
  return "low";
}

function rampStopFor(band: CoverageBand): CoverageRampStop {
  const stop = coverageRamp.find((s) => s.label === band);
  if (!stop) {
    throw new Error(`Coverage ramp missing band: ${band}`);
  }
  return stop;
}

function clampPercent(value: number): number {
  if (Number.isNaN(value)) return 0;
  if (value < 0) return 0;
  if (value > 100) return 100;
  return value;
}

const DEFAULT_GRIDLINES: ReadonlyArray<number> = [
  coverageThresholds.medium,
  coverageThresholds.high,
];

export function getCoverageBarModel(props: CoverageBarProps): CoverageBarModel {
  const clampedPercent = clampPercent(props.percent);
  const band = classifyCoverageBand(clampedPercent);
  const tone = rampStopFor(band);
  const size = props.size ?? "md";
  const label = `${Math.round(clampedPercent)}%`;
  const ariaLabel =
    props.ariaLabel ??
    `Coverage ${Math.round(clampedPercent)} percent (${band})`;
  const gridlines: ReadonlyArray<CoverageBarGridline> = (
    props.gridlines ?? DEFAULT_GRIDLINES
  ).map((position) => ({
    position: clampPercent(position),
    band: classifyCoverageBand(position),
  }));
  return {
    percent: props.percent,
    clampedPercent,
    band,
    tone,
    size,
    label,
    ariaLabel,
    gridlines,
    hideLabel: Boolean(props.hideLabel),
  };
}
