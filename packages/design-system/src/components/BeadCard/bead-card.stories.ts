import { hoopoeTokens } from "../../tokens/index.ts";
import { beadCardStatuses, getBeadCardModel } from "./bead-card.ts";
import type { BeadCardProps } from "./bead-card.ts";

const meta = {
  title: "Components/BeadCard",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const KanbanColumn = {
  render: () => {
    const shell = renderShell();
    const column = document.createElement("section");
    const heading = document.createElement("h2");

    column.style.display = "grid";
    column.style.gap = hoopoeTokens.spacing[3];
    column.style.width = "min(420px, 100%)";

    heading.textContent = "Ready frontier";
    heading.style.margin = "0";
    heading.style.fontSize = "14px";
    heading.style.letterSpacing = "0";
    heading.style.color = hoopoeTokens.color.surface.dark.textDim;

    column.append(heading);
    for (const bead of expandedBeads) {
      column.append(renderBeadCard(bead));
    }

    shell.append(column);
    return shell;
  },
};

export const DagCompact = {
  render: () => {
    const shell = renderShell();
    const grid = document.createElement("section");

    grid.style.display = "grid";
    grid.style.gridTemplateColumns = "repeat(auto-fit, minmax(210px, 1fr))";
    grid.style.gap = hoopoeTokens.spacing[3];
    grid.style.width = "min(960px, 100%)";

    for (const [index, status] of beadCardStatuses.entries()) {
      grid.append(
        renderBeadCard({
          id: `hp-${index + 101}`,
          title: `${status.replace(/_/g, " ")} bead`,
          status,
          priority: index < 2 ? "p0" : index < 4 ? "p1" : "p2",
          owner: index % 2 === 0 ? { agentName: "BlueHill", harness: "codex" } : null,
          filesTouched: index + 1,
          ageLabel: `${index + 2}h`,
          variant: "compact",
        }),
      );
    }

    shell.append(grid);
    return shell;
  },
};

function renderBeadCard(props: BeadCardProps): HTMLElement {
  const model = getBeadCardModel(props);
  const card = document.createElement("article");
  const top = document.createElement("div");
  const id = document.createElement("span");
  const priority = document.createElement("span");
  const title = document.createElement("h3");
  const meta = document.createElement("div");
  const status = document.createElement("span");

  card.setAttribute("aria-label", model.ariaLabel);
  card.style.display = "grid";
  card.style.gap = hoopoeTokens.spacing[3];
  card.style.padding =
    model.variant === "compact" ? hoopoeTokens.spacing[3] : hoopoeTokens.spacing[4];
  card.style.borderRadius = hoopoeTokens.radius.lg;
  card.style.border = `1px solid ${hoopoeTokens.color.surface.dark.border}`;
  card.style.background = hoopoeTokens.color.surface.dark.panel;
  card.style.boxShadow = hoopoeTokens.shadow.soft;
  card.style.color = hoopoeTokens.color.surface.dark.text;
  card.style.minWidth = "0";

  top.style.display = "flex";
  top.style.alignItems = "center";
  top.style.justifyContent = "space-between";
  top.style.gap = hoopoeTokens.spacing[2];

  id.textContent = model.id;
  id.style.color = hoopoeTokens.color.surface.dark.textDim;
  id.style.fontSize = "12px";
  id.style.fontWeight = "800";
  id.style.letterSpacing = "0";

  priority.textContent = `${model.priorityChip.marker} ${model.priorityChip.label}`;
  priority.setAttribute("aria-label", model.priorityChip.ariaLabel);
  priority.style.padding = "4px 8px";
  priority.style.borderRadius = hoopoeTokens.radius.full;
  priority.style.border = `1px solid ${model.priorityChip.tone.border}`;
  priority.style.background = model.priorityChip.tone.bg;
  priority.style.color = model.priorityChip.tone.fg;
  priority.style.fontSize = model.priorityChip.size === "sm" ? "11px" : "12px";
  priority.style.fontWeight = "800";
  priority.style.lineHeight = "1";
  priority.style.whiteSpace = "nowrap";

  top.append(id, priority);

  title.textContent = model.title;
  title.style.margin = "0";
  title.style.fontSize = model.variant === "compact" ? "13px" : "15px";
  title.style.lineHeight = "1.3";
  title.style.letterSpacing = "0";

  meta.style.display = "flex";
  meta.style.flexWrap = "wrap";
  meta.style.gap = hoopoeTokens.spacing[2];

  status.textContent = `${model.status.marker} ${model.status.label}`;
  status.style.padding = "4px 8px";
  status.style.borderRadius = hoopoeTokens.radius.full;
  status.style.border = `1px solid ${model.status.tone.border}`;
  status.style.background = model.status.tone.bg;
  status.style.color = model.status.tone.fg;
  status.style.fontSize = "12px";
  status.style.fontWeight = "800";
  meta.append(status);

  if (model.owner !== null) {
    meta.append(renderMeta(model.owner.agentName, model.owner.tone?.bg ?? null));
  }
  if (model.filesTouchedLabel !== null) {
    meta.append(renderMeta(model.filesTouchedLabel, null));
  }
  if (model.ageLabel !== null) {
    meta.append(renderMeta(model.ageLabel, null));
  }

  card.append(top, title, meta);
  return card;
}

function renderMeta(label: string, background: string | null): HTMLElement {
  const item = document.createElement("span");
  item.textContent = label;
  item.style.padding = "4px 7px";
  item.style.borderRadius = hoopoeTokens.radius.full;
  item.style.border = `1px solid ${hoopoeTokens.color.surface.dark.borderSoft}`;
  item.style.background = background ?? hoopoeTokens.color.surface.dark.panelAlt;
  item.style.color = hoopoeTokens.color.surface.dark.textDim;
  item.style.fontSize = "11px";
  item.style.fontWeight = "700";
  return item;
}

function renderShell(): HTMLElement {
  const shell = document.createElement("main");
  shell.style.minHeight = "100vh";
  shell.style.padding = "32px";
  shell.style.background = hoopoeTokens.color.surface.dark.baseDeep;
  shell.style.fontFamily = hoopoeTokens.typography.sans.join(", ");
  return shell;
}

const expandedBeads: readonly BeadCardProps[] = [
  {
    id: "hp-i62",
    title: "Reusable component set",
    status: "in_progress",
    priority: "p1",
    owner: { agentName: "BlueHill", harness: "codex" },
    filesTouched: 6,
    ageLabel: "42m",
  },
  {
    id: "hp-z1x",
    title: "Desktop stage shell",
    status: "blocked",
    priority: "p0",
    owner: { agentName: "FuchsiaPond", harness: "claude" },
    filesTouched: 14,
    ageLabel: "2h",
  },
  {
    id: "hp-wle",
    title: "Mock Flywheel fixture corpus",
    status: "closed",
    priority: "p1",
    owner: { agentName: "FuchsiaStone", harness: "claude" },
    filesTouched: 1,
    ageLabel: "done",
  },
];
