import { hoopoeTokens, statusTones } from "../../tokens/index.ts";
import { getTimelineRowModel, timelineRowKinds } from "./timeline-row.ts";
import type { TimelineRowProps } from "./timeline-row.ts";

const meta = {
  title: "Components/TimelineRow",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const ActivityPanel = {
  render: () => {
    const shell = document.createElement("main");
    const list = document.createElement("section");

    shell.style.minHeight = "100vh";
    shell.style.padding = "32px";
    shell.style.background = hoopoeTokens.color.surface.dark.baseDeep;
    shell.style.fontFamily = hoopoeTokens.typography.sans.join(", ");

    list.style.display = "grid";
    list.style.gap = hoopoeTokens.spacing[2];
    list.style.maxWidth = "820px";

    for (const [index, kind] of timelineRowKinds.entries()) {
      list.append(renderTimelineRow(rowForKind(kind, index)));
    }

    shell.append(list);
    return shell;
  },
};

function renderTimelineRow(props: TimelineRowProps): HTMLElement {
  const model = getTimelineRowModel(props);
  const row = document.createElement("article");
  const marker = document.createElement("span");
  const body = document.createElement("div");
  const heading = document.createElement("div");
  const summary = document.createElement("p");
  const pills = document.createElement("div");

  row.setAttribute("aria-label", model.ariaLabel);
  row.style.display = "grid";
  row.style.gridTemplateColumns = "32px minmax(0, 1fr)";
  row.style.gap = hoopoeTokens.spacing[3];
  row.style.padding = hoopoeTokens.spacing[3];
  row.style.borderRadius = hoopoeTokens.radius.lg;
  row.style.border = `1px solid ${
    model.unread ? statusTones.waiting_approval.border : hoopoeTokens.color.surface.dark.border
  }`;
  row.style.background = model.unread
    ? hoopoeTokens.color.surface.dark.glass
    : hoopoeTokens.color.surface.dark.panel;
  row.style.color = hoopoeTokens.color.surface.dark.text;

  marker.textContent = model.kindMarker;
  marker.style.display = "grid";
  marker.style.placeItems = "center";
  marker.style.width = "28px";
  marker.style.height = "28px";
  marker.style.borderRadius = hoopoeTokens.radius.full;
  marker.style.background = model.actor.tone?.bg ?? hoopoeTokens.color.surface.dark.panelAlt;
  marker.style.color = model.actor.tone?.fg ?? hoopoeTokens.color.surface.dark.textDim;
  marker.style.fontWeight = "900";

  body.style.display = "grid";
  body.style.gap = hoopoeTokens.spacing[2];
  body.style.minWidth = "0";

  heading.textContent = `${model.kindLabel} - ${model.actor.displayName} - ${model.timestampLabel}`;
  heading.style.color = hoopoeTokens.color.surface.dark.textDim;
  heading.style.fontSize = "12px";
  heading.style.fontWeight = "800";
  heading.style.letterSpacing = "0";

  summary.textContent = model.summary;
  summary.style.margin = "0";
  summary.style.fontSize = "14px";
  summary.style.lineHeight = "1.35";

  pills.style.display = "flex";
  pills.style.flexWrap = "wrap";
  pills.style.gap = hoopoeTokens.spacing[2];
  for (const pill of model.pills) {
    const chip = document.createElement("span");
    chip.textContent = pill.label;
    chip.style.padding = "4px 7px";
    chip.style.borderRadius = hoopoeTokens.radius.full;
    chip.style.border = `1px solid ${
      pill.tone?.border ?? hoopoeTokens.color.surface.dark.borderSoft
    }`;
    chip.style.background = pill.tone?.bg ?? hoopoeTokens.color.surface.dark.panelAlt;
    chip.style.color = pill.tone?.fg ?? hoopoeTokens.color.surface.dark.textDim;
    chip.style.fontSize = "11px";
    chip.style.fontWeight = "800";
    pills.append(chip);
  }

  body.append(heading, summary);
  if (model.inlinePreview !== null) {
    const preview = document.createElement("code");
    preview.textContent = model.inlinePreview;
    preview.style.padding = hoopoeTokens.spacing[2];
    preview.style.borderRadius = hoopoeTokens.radius.md;
    preview.style.background = hoopoeTokens.color.surface.dark.panelAlt;
    preview.style.color = hoopoeTokens.color.surface.dark.textDim;
    preview.style.fontFamily = hoopoeTokens.typography.mono.join(", ");
    preview.style.fontSize = "12px";
    body.append(preview);
  }
  if (model.pills.length > 0) {
    body.append(pills);
  }

  row.append(marker, body);
  return row;
}

function rowForKind(kind: TimelineRowProps["kind"], index: number): TimelineRowProps {
  return {
    id: `event-${kind}`,
    kind,
    timestampLabel: `${index + 1}m ago`,
    actor: actorForIndex(index),
    summary: summaryByKind[kind],
    pills: [
      { id: "bead", label: "hp-i62", tone: statusTones.in_progress },
      { id: "source", label: kind },
    ],
    inlinePreview:
      kind === "build" ? "rch exec -- bun run --cwd packages/design-system test" : null,
    clickTarget: `activity://${kind}/event-${index}`,
    unread: index === 0,
  };
}

function actorForIndex(index: number): TimelineRowProps["actor"] {
  if (index % 4 === 0) {
    return { id: "BlueHill", displayName: "BlueHill", kind: "agent", harness: "codex" };
  }
  if (index % 4 === 1) {
    return { id: "operator", displayName: "Operator", kind: "user" };
  }
  if (index % 4 === 2) {
    return { id: "orchestrator-chat", displayName: "orchestrator-chat", kind: "orchestrator" };
  }
  return { id: "system", displayName: "Hoopoe", kind: "system" };
}

const summaryByKind: Record<TimelineRowProps["kind"], string> = {
  mail: "Completion note posted to the bead thread.",
  reservation: "Exclusive component-directory reservation granted.",
  build: "Design-system package tests passed through rch.",
  approval: "Approval request collected a decision and note.",
  audit: "Guardrail-sensitive action recorded in the audit log.",
  "agent-decision": "Agent selected the next unblocked primitive.",
  "user-message": "Operator redirected the design-system queue.",
  "orchestrator-reply": "Orchestrator acknowledged current swarm state.",
};
