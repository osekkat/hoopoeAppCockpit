import { hoopoeTokens } from "../../tokens/index.ts";
import { getCoverageBarModel } from "./coverage-bar.ts";
import type { CoverageBarProps } from "./coverage-bar.ts";

const meta = {
  title: "Components/CoverageBar",
  parameters: { layout: "centered" },
};

export default meta;

export const Ramp = {
  render: () => {
    const stack = document.createElement("section");
    stack.style.display = "grid";
    stack.style.gap = hoopoeTokens.spacing[4];
    stack.style.width = "420px";
    stack.style.padding = hoopoeTokens.spacing[6];
    stack.style.borderRadius = hoopoeTokens.radius.lg;
    stack.style.background = hoopoeTokens.color.surface.dark.panel;
    stack.style.fontFamily = hoopoeTokens.typography.sans.join(", ");

    for (const props of coverageExamples) {
      stack.append(renderCoverageBar(props));
    }

    return stack;
  },
};

export const Compact = {
  render: () => renderCoverageBar({ percent: 87, size: "sm", hideLabel: true }),
};

function renderCoverageBar(props: CoverageBarProps): HTMLElement {
  const model = getCoverageBarModel(props);
  const wrap = document.createElement("div");
  const label = document.createElement("div");
  const track = document.createElement("div");
  const fill = document.createElement("div");

  wrap.setAttribute("aria-label", model.ariaLabel);
  wrap.style.display = "grid";
  wrap.style.gap = hoopoeTokens.spacing[2];

  label.textContent = model.hideLabel ? model.band : `${model.label} ${model.band}`;
  label.style.color = hoopoeTokens.color.surface.dark.textDim;
  label.style.fontSize = "12px";
  label.style.fontWeight = "800";
  label.style.letterSpacing = "0";

  track.style.position = "relative";
  track.style.height = model.size === "lg" ? "14px" : model.size === "sm" ? "6px" : "10px";
  track.style.overflow = "hidden";
  track.style.borderRadius = hoopoeTokens.radius.full;
  track.style.border = `1px solid ${hoopoeTokens.color.surface.dark.border}`;
  track.style.background = hoopoeTokens.color.surface.dark.panelAlt;

  fill.style.width = `${model.clampedPercent}%`;
  fill.style.height = "100%";
  fill.style.background = model.tone.dot;
  fill.style.borderRadius = hoopoeTokens.radius.full;
  track.append(fill);

  for (const gridline of model.gridlines) {
    const line = document.createElement("span");
    line.style.position = "absolute";
    line.style.left = `${gridline.position}%`;
    line.style.top = "0";
    line.style.bottom = "0";
    line.style.width = "1px";
    line.style.background = "rgba(255,255,255,0.42)";
    track.append(line);
  }

  if (!model.hideLabel) {
    wrap.append(label);
  }
  wrap.append(track);
  return wrap;
}

const coverageExamples: readonly CoverageBarProps[] = [
  { percent: 38, size: "lg" },
  { percent: 64, size: "md" },
  { percent: 92, size: "md" },
];
