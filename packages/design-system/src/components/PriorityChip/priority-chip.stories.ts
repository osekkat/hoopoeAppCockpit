import { hoopoeTokens } from "../../tokens/index.ts";
import { getPriorityChipModel, priorityChipVariants } from "./priority-chip.ts";
import type { PriorityChipProps } from "./priority-chip.ts";

const meta = {
  title: "Components/PriorityChip",
  parameters: { layout: "centered" },
};

export default meta;

export const AllPriorities = {
  render: () => {
    const row = document.createElement("section");
    row.style.display = "flex";
    row.style.flexWrap = "wrap";
    row.style.gap = hoopoeTokens.spacing[2];
    row.style.padding = hoopoeTokens.spacing[6];
    row.style.background = hoopoeTokens.color.surface.dark.baseDeep;
    row.style.fontFamily = hoopoeTokens.typography.sans.join(", ");

    for (const priority of priorityChipVariants) {
      row.append(renderPriorityChip({ priority }));
    }

    return row;
  },
};

export const Small = {
  render: () => renderPriorityChip({ priority: "p0", size: "sm", label: "P0 critical path" }),
};

function renderPriorityChip(props: PriorityChipProps): HTMLElement {
  const model = getPriorityChipModel(props);
  const chip = document.createElement("span");

  chip.textContent = `${model.marker} ${model.label}`;
  chip.setAttribute("aria-label", model.ariaLabel);
  chip.style.display = "inline-flex";
  chip.style.alignItems = "center";
  chip.style.minHeight = model.size === "sm" ? "22px" : "26px";
  chip.style.padding = model.size === "sm" ? "3px 7px" : "5px 9px";
  chip.style.borderRadius = hoopoeTokens.radius.full;
  chip.style.border = `1px solid ${model.tone.border}`;
  chip.style.background = model.tone.bg;
  chip.style.color = model.tone.fg;
  chip.style.fontFamily = hoopoeTokens.typography.sans.join(", ");
  chip.style.fontSize = model.size === "sm" ? "11px" : "12px";
  chip.style.fontWeight = "800";
  chip.style.lineHeight = "1";
  chip.style.letterSpacing = "0";

  return chip;
}
