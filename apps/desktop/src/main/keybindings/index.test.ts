import {
  mkdtempSync,
  rmSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, expect, test } from "bun:test";
import { CommandRegistry, UnknownContextKeyError } from "../CommandRegistry.ts";
import {
  KeybindingsManager,
  collectAstIdentifiers,
} from "./index.ts";
import type { ShortcutContext } from "../../vendored/t3code/keybindings/evaluator.ts";

let workDir: string;
let configPath: string;

function newRegistryWithCommands(): CommandRegistry {
  const registry = new CommandRegistry();
  registry.registerCommand({ id: "command-palette.open", handler: () => "palette" });
  registry.registerCommand({ id: "stage.planning", handler: () => "planning" });
  registry.registerCommand({ id: "stage.beads", handler: () => "beads" });
  registry.registerCommand({ id: "stage.swarm", handler: () => "swarm" });
  registry.registerCommand({ id: "stage.harden", handler: () => "harden" });
  registry.registerCommand({ id: "activity.toggle", handler: () => "activity" });
  return registry;
}

beforeEach(() => {
  workDir = mkdtempSync(join(tmpdir(), "hoopoe-keybindings-"));
  configPath = join(workDir, "keybindings.json");
});

afterEach(() => {
  rmSync(workDir, { recursive: true, force: true });
});

test("KeybindingsManager: defaults dispatch via the CommandRegistry", async () => {
  const registry = newRegistryWithCommands();
  const manager = new KeybindingsManager({ configPath, registry, platform: "darwin" });
  const fired = await manager.dispatch(
    { key: "k", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false },
    {},
  );
  expect(fired).toBe("command-palette.open");
});

test("KeybindingsManager: user override beats default (last-rule-wins)", async () => {
  writeFileSync(
    configPath,
    JSON.stringify(
      [{ key: "cmd+k", command: "stage.planning" }],
      null,
      2,
    ),
    "utf8",
  );
  const registry = newRegistryWithCommands();
  const manager = new KeybindingsManager({ configPath, registry, platform: "darwin" });
  const fired = await manager.dispatch(
    { key: "k", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false },
    {},
  );
  expect(fired).toBe("stage.planning");
});

test("KeybindingsManager: when-clause gates dispatch", async () => {
  writeFileSync(
    configPath,
    JSON.stringify(
      [
        { key: "cmd+1", command: "stage.beads", when: "stage.planning" },
      ],
      null,
      2,
    ),
    "utf8",
  );
  const registry = newRegistryWithCommands();
  const manager = new KeybindingsManager({ configPath, registry, platform: "darwin" });

  const event = { key: "1", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false };

  const planningCtx: ShortcutContext = { "stage.planning": true };
  expect(await manager.dispatch(event, planningCtx)).toBe("stage.beads");

  const beadsCtx: ShortcutContext = { "stage.beads": true };
  // The default cmd+1 is `stage.planning`; user override only fires under
  // `when: stage.planning`, so the default takes effect when that's false.
  expect(await manager.dispatch(event, beadsCtx)).toBe("stage.planning");
});

test("KeybindingsManager: unknown when-clause context key throws UnknownContextKeyError", () => {
  writeFileSync(
    configPath,
    JSON.stringify(
      [{ key: "cmd+1", command: "stage.planning", when: "stage.planing" }],
      null,
      2,
    ),
    "utf8",
  );
  const registry = newRegistryWithCommands();
  expect(
    () => new KeybindingsManager({ configPath, registry, platform: "darwin" }),
  ).toThrow(UnknownContextKeyError);
});

test("KeybindingsManager: malformed config falls back to defaults + warns", () => {
  writeFileSync(configPath, "{not valid json", "utf8");
  const warnings: string[] = [];
  const registry = newRegistryWithCommands();
  const manager = new KeybindingsManager({
    configPath,
    registry,
    platform: "darwin",
    logger: {
      warn: (message) => warnings.push(message),
      info() {},
    },
  });
  expect(manager.current().length).toBeGreaterThan(0);
  expect(warnings).toContain("keybindings.read-failed");
});

test("KeybindingsManager: command not registered → dispatch logs warning, returns null", async () => {
  writeFileSync(
    configPath,
    JSON.stringify(
      [{ key: "cmd+9", command: "command.does.not.exist" }],
      null,
      2,
    ),
    "utf8",
  );
  const warnings: string[] = [];
  const registry = newRegistryWithCommands();
  const manager = new KeybindingsManager({
    configPath,
    registry,
    platform: "darwin",
    logger: {
      warn: (message) => warnings.push(message),
      info() {},
    },
  });
  const fired = await manager.dispatch(
    { key: "9", metaKey: true, ctrlKey: false, shiftKey: false, altKey: false },
    {},
  );
  expect(fired).toBeNull();
  expect(warnings).toContain("keybindings.command-not-registered");
});

test("KeybindingsManager: reloadNow re-reads file and notifies subscribers", async () => {
  writeFileSync(configPath, JSON.stringify([], null, 2), "utf8");
  const registry = newRegistryWithCommands();
  const manager = new KeybindingsManager({ configPath, registry, platform: "darwin" });
  const seen: number[] = [];
  manager.subscribe((resolved) => {
    seen.push(resolved.length);
  });
  writeFileSync(
    configPath,
    JSON.stringify(
      [{ key: "cmd+9", command: "stage.planning" }],
      null,
      2,
    ),
    "utf8",
  );
  manager.reloadNow();
  expect(seen.length).toBe(1);
  expect(seen[0]).toBeGreaterThan(0);
});

test("collectAstIdentifiers: walks the AST and dedupes", () => {
  const ids = collectAstIdentifiers({
    type: "and",
    left: { type: "identifier", name: "stage.planning" },
    right: {
      type: "or",
      left: { type: "not", node: { type: "identifier", name: "agent.selected" } },
      right: { type: "identifier", name: "stage.planning" },
    },
  });
  expect([...ids].toSorted()).toEqual(["agent.selected", "stage.planning"]);
});
