import {
  renderStatusPillElement,
  statusPillStatesByKind,
} from "./status-pill.ts";
import type { StatusPillKind, StatusPillProps } from "./status-pill.ts";
import { hoopoeTokens } from "../tokens/index.ts";

const meta = {
  title: "Components/StatusPill",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const AllStates = {
  render: () => {
    const darkSurface = hoopoeTokens.color.surface.dark;
    const main = document.createElement("main");
    main.style.minHeight = "100vh";
    main.style.padding = "32px";
    main.style.background = darkSurface.base;
    main.style.color = darkSurface.text;
    main.style.fontFamily = hoopoeTokens.typography.sans.join(", ");

    const grid = document.createElement("section");
    grid.style.display = "grid";
    grid.style.gap = "24px";
    grid.style.maxWidth = "960px";

    for (const [kind, states] of Object.entries(statusPillStatesByKind)) {
      grid.append(section(kind as StatusPillKind, states));
    }

    main.append(grid);
    return main;
  },
};

function section(kind: StatusPillKind, states: readonly string[]): HTMLElement {
  const wrapper = document.createElement("section");
  const heading = document.createElement("h2");
  const row = document.createElement("div");

  heading.textContent = kind;
  heading.style.margin = "0 0 12px";
  heading.style.fontSize = "18px";
  heading.style.fontWeight = "650";

  row.style.display = "flex";
  row.style.flexWrap = "wrap";
  row.style.gap = "8px";

  for (const state of states) {
    row.append(renderStatusPillElement({ kind, state } as StatusPillProps));
  }

  wrapper.append(heading, row);
  return wrapper;
}
