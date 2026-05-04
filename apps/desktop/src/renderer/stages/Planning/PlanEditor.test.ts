import { describe, expect, test } from "bun:test";
import { planEditorSaveKeyBinding } from "./PlanEditor.tsx";

describe("planEditorSaveKeyBinding", () => {
  test("uses the current save handler instead of the handler captured at mount", () => {
    const calls: string[] = [];
    const saveHandlerRef = {
      current: () => calls.push("initial"),
    };
    const binding = planEditorSaveKeyBinding(saveHandlerRef);

    saveHandlerRef.current = () => calls.push("latest");

    expect(binding.run()).toBe(true);
    expect(calls).toEqual(["latest"]);
  });
});
