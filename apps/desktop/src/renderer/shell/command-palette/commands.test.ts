import { expect, test } from "bun:test";
import { getCommandPaletteModel } from "@hoopoe/design-system";
import {
  buildShellCommands,
  buildShellPaletteContext,
  SHELL_PALETTE_CONTEXT_KEYS,
  type ShellCommandContext,
} from "./commands.ts";

const noopContext: ShellCommandContext = {
  projectId: undefined,
  navigateToStage: () => undefined,
  openProjectPicker: () => undefined,
  toggleActivityPanel: () => undefined,
  setActivityPanelOpen: () => undefined,
  closeCommandPalette: () => undefined,
};

test("buildShellCommands derives stage commands from stageDefinitions", () => {
  const commands = buildShellCommands();
  const ids = commands.map((command) => command.id);

  expect(ids).toContain("stage.planning");
  expect(ids).toContain("stage.beads");
  expect(ids).toContain("stage.swarm");
  expect(ids).toContain("stage.harden");
  expect(ids).toContain("stage.diagnostics");
  expect(ids).toContain("activity.toggle");
  expect(ids).toContain("project.open-picker");
});

test("stage commands gate on project.active so they hide without a project", () => {
  const commands = buildShellCommands();
  const model = getCommandPaletteModel({
    commands,
    query: "",
    context: buildShellPaletteContext({
      projectId: undefined,
      activeStageId: undefined,
      activityPanelOpen: false,
    }),
    knownContextKeys: SHELL_PALETTE_CONTEXT_KEYS,
  });
  const visibleIds = model.items.map((item) => item.command.id);

  expect(visibleIds).not.toContain("stage.planning");
  expect(visibleIds).toContain("project.open-picker");
});

test("when a project is active, stage commands surface in the palette", () => {
  const commands = buildShellCommands();
  const model = getCommandPaletteModel({
    commands,
    query: "swarm",
    context: buildShellPaletteContext({
      projectId: "local-demo",
      activeStageId: "plan",
      activityPanelOpen: false,
    }),
    knownContextKeys: SHELL_PALETTE_CONTEXT_KEYS,
  });

  expect(model.items[0]?.command.id).toBe("stage.swarm");
});

test("activity.close hides until the panel is open", () => {
  const commands = buildShellCommands();
  const closedContext = buildShellPaletteContext({
    projectId: "local-demo",
    activeStageId: "plan",
    activityPanelOpen: false,
  });
  const closedModel = getCommandPaletteModel({
    commands,
    query: "close activity",
    context: closedContext,
    knownContextKeys: SHELL_PALETTE_CONTEXT_KEYS,
  });

  expect(closedModel.items.map((item) => item.command.id)).not.toContain("activity.close");

  const openContext = buildShellPaletteContext({
    projectId: "local-demo",
    activeStageId: "plan",
    activityPanelOpen: true,
  });
  const openModel = getCommandPaletteModel({
    commands,
    query: "close activity",
    context: openContext,
    knownContextKeys: SHELL_PALETTE_CONTEXT_KEYS,
  });

  expect(openModel.items.map((item) => item.command.id)).toContain("activity.close");
});

test("executing a stage command navigates to the matching route", () => {
  const commands = buildShellCommands();
  const stageCommand = commands.find((command) => command.id === "stage.swarm");
  if (!stageCommand) throw new Error("stage.swarm command missing");

  const calls: { projectId: string; stageId: string }[] = [];
  let closeCount = 0;
  stageCommand.execute({
    ...noopContext,
    projectId: "local-demo",
    navigateToStage: (projectId, stageId) => {
      calls.push({ projectId, stageId });
    },
    closeCommandPalette: () => {
      closeCount += 1;
    },
  });

  expect(calls).toEqual([{ projectId: "local-demo", stageId: "swarm" }]);
  expect(closeCount).toBe(1);
});

test("executing a stage command without a project opens the project picker", () => {
  const commands = buildShellCommands();
  const stageCommand = commands.find((command) => command.id === "stage.beads");
  if (!stageCommand) throw new Error("stage.beads command missing");

  let openCount = 0;
  let navigateCount = 0;
  stageCommand.execute({
    ...noopContext,
    projectId: undefined,
    navigateToStage: () => {
      navigateCount += 1;
    },
    openProjectPicker: () => {
      openCount += 1;
    },
  });

  expect(navigateCount).toBe(0);
  expect(openCount).toBe(1);
});

test("buildShellPaletteContext only marks the active stage flag true", () => {
  const context = buildShellPaletteContext({
    projectId: "local-demo",
    activeStageId: "swarm",
    activityPanelOpen: false,
  });

  expect(context["stage.swarm"]).toBe(true);
  expect(context["stage.planning"]).toBe(false);
  expect(context["stage.beads"]).toBe(false);
  expect(context["project.active"]).toBe(true);
  expect(context["activity.open"]).toBe(false);
});
