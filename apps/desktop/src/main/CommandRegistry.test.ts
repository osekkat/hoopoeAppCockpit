import { expect, test } from "bun:test";
import {
  CommandRegistry,
  UnknownCommandError,
  UnknownContextKeyError,
} from "./CommandRegistry.ts";

test("CommandRegistry: register + dispatch round trip", async () => {
  const registry = new CommandRegistry();
  let called = false;
  registry.registerCommand({
    id: "stage.planning",
    title: "Open Planning",
    handler: () => {
      called = true;
      return "ok";
    },
  });
  const result = await registry.dispatch<unknown, string>("stage.planning", {});
  expect(called).toBe(true);
  expect(result).toBe("ok");
});

test("CommandRegistry: dispatching unknown command throws UnknownCommandError", async () => {
  const registry = new CommandRegistry();
  await expect(registry.dispatch("nope", {})).rejects.toBeInstanceOf(UnknownCommandError);
});

test("CommandRegistry: registering same id twice fails fast", () => {
  const registry = new CommandRegistry();
  registry.registerCommand({ id: "x", handler: () => null });
  expect(() => registry.registerCommand({ id: "x", handler: () => null })).toThrow();
});

test("CommandRegistry: unregister removes the command", () => {
  const registry = new CommandRegistry();
  const { unregister } = registry.registerCommand({ id: "x", handler: () => null });
  expect(registry.hasCommand("x")).toBe(true);
  unregister();
  expect(registry.hasCommand("x")).toBe(false);
});

test("CommandRegistry: validateContextKey throws UnknownContextKeyError on typo", () => {
  const registry = new CommandRegistry();
  registry.registerContextKeys(["stage.planning", "agent.selected"]);
  registry.validateContextKey("stage.planning");
  registry.validateContextKey("true");
  registry.validateContextKey("false");
  expect(() => registry.validateContextKey("agent.selceted")).toThrow(UnknownContextKeyError);
});

test("CommandRegistry: knownContextKeys returns sorted list", () => {
  const registry = new CommandRegistry();
  registry.registerContextKeys(["stage.swarm", "agent.selected", "stage.beads"]);
  expect(registry.knownContextKeys()).toEqual([
    "agent.selected",
    "stage.beads",
    "stage.swarm",
  ]);
});

test("CommandRegistry: listCommands returns sorted snapshot", () => {
  const registry = new CommandRegistry();
  registry.registerCommand({ id: "z", handler: () => null });
  registry.registerCommand({ id: "a", handler: () => null });
  registry.registerCommand({ id: "m", handler: () => null });
  expect(registry.listCommands().map((c) => c.id)).toEqual(["a", "m", "z"]);
});
