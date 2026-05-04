import { hoopoeTokens } from "../../tokens/index.ts";
import { renderStageHeaderElement } from "./stage-header.ts";

const darkSurface = hoopoeTokens.color.surface.dark;

const meta = {
  title: "Components/StageHeader",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const Planning = {
  render: () =>
    renderShell(
      renderStageHeaderElement({
        stageId: "plan",
        projectName: "Local demo",
        breadcrumb: ["Planning"],
        subtitle: "Candidate plans, comparative matrix, synthesis, and refinement rounds.",
        actions: [{ id: "import", label: "Import Plan", tone: "primary" }],
      }),
    ),
};

export const SwarmWithActions = {
  render: () =>
    renderShell(
      renderStageHeaderElement({
        stageId: "swarm",
        projectName: "Local demo",
        breadcrumb: ["Swarm", "phase-2"],
        activeActionId: "broadcast",
        actions: [
          { id: "broadcast", label: "Broadcast", tone: "primary" },
          { id: "pause", label: "Pause", tone: "secondary" },
          { id: "halt", label: "Halt", tone: "danger" },
        ],
      }),
    ),
};

export const Diagnostics = {
  render: () =>
    renderShell(
      renderStageHeaderElement({
        stageId: "diagnostics",
        projectName: "Local demo",
        breadcrumb: [{ label: "Diagnostics", href: "/diag" }, "Audit log"],
        title: "Audit Log",
        subtitle: "Filter and inspect redacted audit entries without exposing raw panes by default.",
      }),
    ),
};

function renderShell(header: HTMLElement): HTMLElement {
  const shell = document.createElement("main");

  shell.style.minHeight = "100vh";
  shell.style.background = darkSurface.baseDeep;
  shell.append(header);

  return shell;
}
