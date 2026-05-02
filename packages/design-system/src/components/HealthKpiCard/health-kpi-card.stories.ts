import { hoopoeTokens } from "../../tokens/index.ts";
import { getHealthKpiCardModel } from "./health-kpi-card.ts";
import type { HealthKpiCardProps, HealthKpiSparklinePoint } from "./health-kpi-card.ts";

const meta = {
  title: "Components/HealthKpiCard",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const HealthGrid = {
  render: () => {
    const shell = document.createElement("main");
    const grid = document.createElement("section");

    shell.style.minHeight = "100vh";
    shell.style.padding = "32px";
    shell.style.background = hoopoeTokens.color.surface.dark.baseDeep;
    shell.style.fontFamily = hoopoeTokens.typography.sans.join(", ");

    grid.style.display = "grid";
    grid.style.gridTemplateColumns = "repeat(auto-fit, minmax(220px, 1fr))";
    grid.style.gap = hoopoeTokens.spacing[3];
    grid.style.maxWidth = "960px";

    for (const kpi of kpis) {
      grid.append(renderHealthKpiCard(kpi));
    }

    shell.append(grid);
    return shell;
  },
};

function renderHealthKpiCard(props: HealthKpiCardProps): HTMLElement {
  const model = getHealthKpiCardModel(props);
  const card = document.createElement("article");
  const title = document.createElement("h3");
  const value = document.createElement("div");

  card.setAttribute("aria-label", model.ariaLabel);
  card.style.display = "grid";
  card.style.gap = hoopoeTokens.spacing[3];
  card.style.padding = hoopoeTokens.spacing[4];
  card.style.borderRadius = hoopoeTokens.radius.lg;
  card.style.border = `1px solid ${hoopoeTokens.color.surface.dark.border}`;
  card.style.background = hoopoeTokens.color.surface.dark.panel;
  card.style.color = hoopoeTokens.color.surface.dark.text;
  card.style.boxShadow = hoopoeTokens.shadow.soft;

  title.textContent = model.title;
  title.style.margin = "0";
  title.style.color = hoopoeTokens.color.surface.dark.textDim;
  title.style.fontSize = "12px";
  title.style.fontWeight = "800";
  title.style.letterSpacing = "0";

  value.textContent = model.valueLabel;
  value.style.fontSize = "30px";
  value.style.fontWeight = "850";
  value.style.lineHeight = "1";
  value.style.letterSpacing = "0";

  card.append(title, value);

  if (model.delta !== null) {
    const delta = document.createElement("span");
    delta.textContent = model.delta.label;
    delta.style.width = "fit-content";
    delta.style.padding = "4px 8px";
    delta.style.borderRadius = hoopoeTokens.radius.full;
    delta.style.border = `1px solid ${model.delta.tone.border}`;
    delta.style.background = model.delta.tone.bg;
    delta.style.color = model.delta.tone.fg;
    delta.style.fontSize = "12px";
    delta.style.fontWeight = "800";
    card.append(delta);
  }

  if (model.sparkline.length > 0) {
    card.append(renderSparkline(model.sparkline));
  }

  if (model.subtitle !== null) {
    const subtitle = document.createElement("p");
    subtitle.textContent = model.subtitle;
    subtitle.style.margin = "0";
    subtitle.style.color = hoopoeTokens.color.surface.dark.textDim;
    subtitle.style.fontSize = "12px";
    subtitle.style.lineHeight = "1.35";
    card.append(subtitle);
  }

  return card;
}

function renderSparkline(points: ReadonlyArray<HealthKpiSparklinePoint>): SVGElement {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  const polyline = document.createElementNS("http://www.w3.org/2000/svg", "polyline");
  const xs = points.map((point) => point.t);
  const ys = points.map((point) => point.v);
  const minX = Math.min(...xs);
  const maxX = Math.max(...xs);
  const minY = Math.min(...ys);
  const maxY = Math.max(...ys);
  const width = 180;
  const height = 48;
  const rangeX = Math.max(maxX - minX, 1);
  const rangeY = Math.max(maxY - minY, 1);
  const path = points
    .map((point) => {
      const x = ((point.t - minX) / rangeX) * width;
      const y = height - ((point.v - minY) / rangeY) * height;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(" ");

  svg.setAttribute("viewBox", `0 0 ${width} ${height}`);
  svg.setAttribute("role", "img");
  svg.setAttribute("aria-label", "Sparkline trend");
  svg.style.width = "100%";
  svg.style.height = "48px";
  polyline.setAttribute("points", path);
  polyline.setAttribute("fill", "none");
  polyline.setAttribute("stroke", hoopoeTokens.color.brand.russetDark);
  polyline.setAttribute("stroke-width", "3");
  polyline.setAttribute("stroke-linecap", "round");
  polyline.setAttribute("stroke-linejoin", "round");
  svg.append(polyline);
  return svg;
}

const sparkline: readonly HealthKpiSparklinePoint[] = [
  { t: 1, v: 68 },
  { t: 2, v: 70 },
  { t: 3, v: 69 },
  { t: 4, v: 73 },
  { t: 5, v: 78 },
];

const kpis: readonly HealthKpiCardProps[] = [
  {
    title: "Coverage",
    value: 78,
    unit: "%",
    delta: 5,
    deltaUnit: "%",
    sparkline,
    subtitle: "Merged test evidence from the current branch.",
  },
  {
    title: "Hotspots",
    value: 12,
    delta: -3,
    invertGoodTrend: true,
    sparkline: [
      { t: 1, v: 18 },
      { t: 2, v: 15 },
      { t: 3, v: 12 },
    ],
    subtitle: "Weighted by churn and failing checks.",
  },
  {
    title: "Open Findings",
    value: 4,
    delta: 0,
    subtitle: "Critical and important findings only.",
  },
];
