import { statusTones, toolHealthTones } from "../../tokens/index.ts";
import type { ToneToken } from "../../tokens/index.ts";

export type HealthKpiTrend = "up" | "down" | "flat";

export interface HealthKpiSparklinePoint {
  readonly t: number;
  readonly v: number;
}

export interface HealthKpiCardProps {
  readonly title: string;
  readonly value: number | string;
  readonly unit?: string | null;
  readonly delta?: number | null;
  readonly deltaUnit?: string | null;
  /** Optional explicit trend; otherwise derived from `delta`. */
  readonly trend?: HealthKpiTrend;
  /** Inverts the "good direction" — for metrics like complexity where
   * a downward delta is the desirable trend. */
  readonly invertGoodTrend?: boolean;
  readonly sparkline?: ReadonlyArray<HealthKpiSparklinePoint>;
  readonly subtitle?: string | null;
  readonly ariaLabel?: string;
}

export interface HealthKpiCardModel {
  readonly title: string;
  readonly valueLabel: string;
  readonly subtitle: string | null;
  readonly delta: {
    readonly raw: number;
    readonly label: string;
    readonly trend: HealthKpiTrend;
    readonly tone: ToneToken;
  } | null;
  readonly sparkline: ReadonlyArray<HealthKpiSparklinePoint>;
  readonly ariaLabel: string;
}

const TREND_MARKERS: Record<HealthKpiTrend, string> = {
  up: "▲",
  flat: "▶",
  down: "▼",
};

function deriveTrend(delta: number): HealthKpiTrend {
  if (delta > 0) return "up";
  if (delta < 0) return "down";
  return "flat";
}

function toneForTrend(trend: HealthKpiTrend, invertGoodTrend: boolean): ToneToken {
  if (trend === "flat") return statusTones.muted;
  const isGood = invertGoodTrend ? trend === "down" : trend === "up";
  return isGood ? toolHealthTones.green : toolHealthTones.red;
}

function formatValue(value: number | string, unit: string | null | undefined): string {
  const stringValue =
    typeof value === "number" ? formatNumber(value) : value;
  if (unit && unit.length > 0) return `${stringValue}${unit}`;
  return stringValue;
}

function formatNumber(value: number): string {
  if (!Number.isFinite(value)) return "—";
  if (Number.isInteger(value)) return value.toString();
  return value.toFixed(1);
}

export function getHealthKpiCardModel(props: HealthKpiCardProps): HealthKpiCardModel {
  const valueLabel = formatValue(props.value, props.unit ?? null);
  const subtitle = props.subtitle ?? null;
  const sparkline = props.sparkline ?? [];

  let deltaModel: HealthKpiCardModel["delta"] = null;
  if (typeof props.delta === "number" && Number.isFinite(props.delta)) {
    const trend = props.trend ?? deriveTrend(props.delta);
    const sign = props.delta > 0 ? "+" : props.delta < 0 ? "" : "±";
    const formatted = `${sign}${formatNumber(props.delta)}${props.deltaUnit ?? ""}`;
    const marker = TREND_MARKERS[trend];
    deltaModel = {
      raw: props.delta,
      label: `${marker} ${formatted}`,
      trend,
      tone: toneForTrend(trend, Boolean(props.invertGoodTrend)),
    };
  }

  const ariaLabel =
    props.ariaLabel ??
    `${props.title}: ${valueLabel}${
      deltaModel ? `, ${deltaModel.label}` : ""
    }`;

  return {
    title: props.title,
    valueLabel,
    subtitle,
    delta: deltaModel,
    sparkline,
    ariaLabel,
  };
}
