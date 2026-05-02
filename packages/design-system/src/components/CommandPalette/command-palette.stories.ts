import { renderCommandPaletteElement } from "./command-palette.ts";
import type { CommandPaletteCommand, CommandPaletteProps } from "./command-palette.ts";

const meta = {
  title: "Components/CommandPalette",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const Recent = {
  render: () =>
    renderShell({
      query: "",
      recentCommandIds: ["swarm.broadcast", "planning.open"],
      context: enabledContext,
    }),
};

export const TypingMatches = {
  render: () =>
    renderShell({
      query: "swarm",
      context: enabledContext,
    }),
};

export const NoMatches = {
  render: () =>
    renderShell({
      query: "zzzz",
      context: enabledContext,
    }),
};

export const WhenFiltered = {
  render: () =>
    renderShell({
      query: "",
      context: disabledSwarmContext,
    }),
};

export const Empty = {
  render: () =>
    renderCommandPaletteElement({
      commands: [],
      query: "",
      context: enabledContext,
      knownContextKeys,
    }),
};

function renderShell(
  props: Pick<CommandPaletteProps, "query" | "context" | "recentCommandIds">,
): HTMLElement {
  return renderCommandPaletteElement({
    commands,
    knownContextKeys,
    ...props,
  });
}

const commands: readonly CommandPaletteCommand[] = [
  {
    id: "project.import",
    title: "Import Project",
    category: "Project",
    description: "Connect an existing repository to the cockpit",
    defaultKeybinding: "Cmd+I",
    whenContextKeys: ["project.active"],
  },
  {
    id: "planning.open",
    title: "Open Planning",
    category: "Plan",
    description: "Jump to stage 01",
    defaultKeybinding: "Cmd+1",
  },
  {
    id: "beads.ready",
    title: "Show Ready Beads",
    category: "Beads",
    description: "Filter the bead board to ready work",
    defaultKeybinding: "Cmd+2",
    whenContextKeys: ["project.active"],
  },
  {
    id: "swarm.broadcast",
    title: "Broadcast to Swarm",
    category: "Swarm",
    description: "Send marching orders to every active worker",
    whenContextKeys: ["swarm.active"],
  },
  {
    id: "activity.toggle",
    title: "Toggle Activity Panel",
    category: "Activity",
    description: "Open or close the cross-stage activity drawer",
    defaultKeybinding: "Cmd+/",
  },
  {
    id: "diagnostics.rawPane",
    title: "Show Raw Pane",
    category: "Diagnostics",
    description: "Open the audited diagnostics-only pane",
    whenContextKeys: ["stage.diagnostics"],
  },
];

const knownContextKeys = ["project.active", "swarm.active", "stage.diagnostics"] as const;

const enabledContext = {
  "project.active": true,
  "swarm.active": true,
  "stage.diagnostics": true,
} as const;

const disabledSwarmContext = {
  "project.active": true,
  "swarm.active": false,
  "stage.diagnostics": false,
} as const;
