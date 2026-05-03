// Hoopoe-owned. Renderer-side command list that feeds the ⌘K palette.
//
// The palette MUST NOT carry a duplicate static list (Phase 1.5 acceptance
// hp-2qgx): every entry here is derived from the same `stageDefinitions`
// the navigation rail consumes, plus a small set of activity-panel actions
// whose IDs match `apps/desktop/src/main/keybindings/index.ts` defaults so
// keybinding-driven dispatch and palette-driven dispatch resolve to the
// same handler.
//
// The set of `whenContextKeys` referenced here MUST be a subset of
// `SHELL_PALETTE_CONTEXT_KEYS`. The design-system primitive throws when a
// command references an unknown key (Appendix B anti-pattern #7), so a
// typo fails loud at render time rather than evaluating to false.

import type { CommandPaletteCommand } from "@hoopoe/design-system";
import { stageDefinitions, type ShellRouteId } from "../../stages.ts";

export interface ShellCommandContext {
  readonly projectId: string | undefined;
  readonly navigateToStage: (projectId: string, stageId: ShellRouteId) => void;
  readonly openProjectPicker: () => void;
  readonly toggleActivityPanel: () => void;
  readonly setActivityPanelOpen: (open: boolean) => void;
  readonly closeCommandPalette: () => void;
}

export interface ShellCommand extends CommandPaletteCommand {
  readonly execute: (context: ShellCommandContext) => void;
}

export const SHELL_PALETTE_CONTEXT_KEYS = [
  "project.active",
  "activity.open",
  "stage.planning",
  "stage.beads",
  "stage.swarm",
  "stage.harden",
  "stage.diagnostics",
] as const;

export type ShellPaletteContextKey = (typeof SHELL_PALETTE_CONTEXT_KEYS)[number];

export type ShellPaletteContext = Readonly<Record<ShellPaletteContextKey, boolean>>;

const stageCommandIdByStageId: Record<ShellRouteId, string> = {
  plan: "stage.planning",
  bead: "stage.beads",
  swarm: "stage.swarm",
  harden: "stage.harden",
  diag: "stage.diagnostics",
};

const stageContextKeyByStageId: Record<ShellRouteId, ShellPaletteContextKey> = {
  plan: "stage.planning",
  bead: "stage.beads",
  swarm: "stage.swarm",
  harden: "stage.harden",
  diag: "stage.diagnostics",
};

const stageCategoryByStageId: Record<
  ShellRouteId,
  CommandPaletteCommand["category"]
> = {
  plan: "Plan",
  bead: "Beads",
  swarm: "Swarm",
  harden: "Diagnostics",
  diag: "Diagnostics",
};

const stageDefaultKeybindingByStageId: Partial<Record<ShellRouteId, string>> = {
  plan: "Cmd+1",
  bead: "Cmd+2",
  swarm: "Cmd+3",
  harden: "Cmd+4",
};

export function buildShellCommands(): readonly ShellCommand[] {
  const stageCommands = stageDefinitions.map<ShellCommand>((stage) => {
    const defaultKeybinding = stageDefaultKeybindingByStageId[stage.id];
    return {
      id: stageCommandIdByStageId[stage.id],
      title: `Go to ${stage.label}`,
      category: stageCategoryByStageId[stage.id],
      description: `Stage ${stage.number} — ${stage.verb}`,
      ...(defaultKeybinding !== undefined ? { defaultKeybinding } : {}),
      whenContextKeys: ["project.active"],
      execute(context) {
        if (context.projectId !== undefined) {
          context.navigateToStage(context.projectId, stage.id);
        } else {
          context.openProjectPicker();
        }
        context.closeCommandPalette();
      },
    };
  });

  const activityCommands: readonly ShellCommand[] = [
    {
      id: "activity.toggle",
      title: "Toggle Activity panel",
      category: "Activity",
      description: "Open or close the orchestrator activity drawer",
      defaultKeybinding: "Cmd+/",
      whenContextKeys: ["project.active"],
      execute(context) {
        context.toggleActivityPanel();
        context.closeCommandPalette();
      },
    },
    {
      id: "activity.close",
      title: "Close Activity panel",
      category: "Activity",
      description: "Hide the orchestrator activity drawer",
      whenContextKeys: ["activity.open"],
      execute(context) {
        context.setActivityPanelOpen(false);
        context.closeCommandPalette();
      },
    },
  ];

  const projectCommands: readonly ShellCommand[] = [
    {
      id: "project.open-picker",
      title: "Open Project picker",
      category: "Project",
      description: "Browse pinned and recent projects",
      execute(context) {
        context.openProjectPicker();
        context.closeCommandPalette();
      },
    },
  ];

  return [...projectCommands, ...stageCommands, ...activityCommands];
}

export function buildShellPaletteContext(input: {
  readonly projectId: string | undefined;
  readonly activeStageId: ShellRouteId | undefined;
  readonly activityPanelOpen: boolean;
}): ShellPaletteContext {
  const stageFlag = (target: ShellPaletteContextKey): boolean =>
    input.activeStageId !== undefined &&
    stageContextKeyByStageId[input.activeStageId] === target;

  return {
    "project.active": input.projectId !== undefined,
    "activity.open": input.activityPanelOpen,
    "stage.planning": stageFlag("stage.planning"),
    "stage.beads": stageFlag("stage.beads"),
    "stage.swarm": stageFlag("stage.swarm"),
    "stage.harden": stageFlag("stage.harden"),
    "stage.diagnostics": stageFlag("stage.diagnostics"),
  };
}
