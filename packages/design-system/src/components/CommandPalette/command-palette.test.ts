import { expect, test } from "bun:test";
import {
  UnknownCommandPaletteContextKeyError,
  getCommandPaletteModel,
  moveCommandPaletteSelection,
} from "./command-palette.ts";
import type { CommandPaletteCommand } from "./command-palette.ts";

const commands: readonly CommandPaletteCommand[] = [
  {
    id: "project.import",
    title: "Import Project",
    category: "Project",
    description: "Connect an existing repository",
    defaultKeybinding: "Cmd+I",
    whenContextKeys: ["project.active"],
  },
  {
    id: "planning.open",
    title: "Open Planning",
    category: "Plan",
    description: "Jump to the planning stage",
    defaultKeybinding: "Cmd+1",
  },
  {
    id: "swarm.broadcast",
    title: "Broadcast to Swarm",
    category: "Swarm",
    description: "Send marching orders to every active worker",
    whenContextKeys: ["swarm.active"],
  },
  {
    id: "diagnostics.rawPane",
    title: "Show Raw Pane",
    category: "Diagnostics",
    description: "Open the audited diagnostics pane",
    whenContextKeys: ["stage.diagnostics"],
  },
];

const context = {
  "project.active": true,
  "swarm.active": false,
  "stage.diagnostics": false,
} as const;

test("CommandPalette filters commands by registered context keys", () => {
  const model = getCommandPaletteModel({
    commands,
    query: "",
    context,
    knownContextKeys: Object.keys(context),
  });

  expect(model.items.map((item) => item.command.id)).toEqual(["planning.open", "project.import"]);
  expect(model.filteredCommandIds).toEqual(["swarm.broadcast", "diagnostics.rawPane"]);
});

test("CommandPalette rejects unknown when-clause context keys", () => {
  expect(() =>
    getCommandPaletteModel({
      commands: [
        {
          id: "bad.command",
          title: "Bad Command",
          category: "Diagnostics",
          whenContextKeys: ["agent.selceted"],
        },
      ],
      query: "",
      context,
      knownContextKeys: Object.keys(context),
    }),
  ).toThrow(UnknownCommandPaletteContextKeyError);
});

test("CommandPalette fuzzy-searches across title, id, and description", () => {
  const model = getCommandPaletteModel({
    commands,
    query: "march",
    context: { ...context, "swarm.active": true },
    knownContextKeys: Object.keys(context),
  });

  expect(model.items.map((item) => item.command.id)).toEqual(["swarm.broadcast"]);
  expect(model.items[0]?.descriptionMatchRanges.length).toBeGreaterThan(0);
});

test("CommandPalette highlight ranges map back to the original title", () => {
  const model = getCommandPaletteModel({
    commands,
    query: "planning",
    context,
    knownContextKeys: Object.keys(context),
  });

  expect(model.items[0]?.command.id).toBe("planning.open");
  expect(model.items[0]?.titleMatchRanges).toEqual([{ start: 5, end: 13 }]);
});

test("CommandPalette puts recently used commands first when query is empty", () => {
  const model = getCommandPaletteModel({
    commands,
    query: "",
    context,
    knownContextKeys: Object.keys(context),
    recentCommandIds: ["project.import"],
  });

  expect(model.items[0]?.command.id).toBe("project.import");
  expect(model.items[0]?.recentRank).toBe(0);
});

test("CommandPalette selection movement wraps through visible results", () => {
  const model = getCommandPaletteModel({
    commands,
    query: "",
    context,
    knownContextKeys: Object.keys(context),
    activeCommandId: "project.import",
  });

  expect(moveCommandPaletteSelection(model, "next")).toBe("planning.open");
  expect(moveCommandPaletteSelection(model, "previous")).toBe("planning.open");
});

test("CommandPalette exposes no-match and no-command empty states", () => {
  const noMatches = getCommandPaletteModel({
    commands,
    query: "zzzz",
    context,
    knownContextKeys: Object.keys(context),
  });
  const noCommands = getCommandPaletteModel({
    commands: [],
    query: "",
    context,
  });

  expect(noMatches.emptyState).toBe("no-matches");
  expect(noCommands.emptyState).toBe("no-commands");
});
