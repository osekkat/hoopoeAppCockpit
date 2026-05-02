import { expect, test } from "bun:test";
import {
  getPriorityChipModel,
  priorityChipVariants,
} from "./priority-chip.ts";

test("PriorityChip: every variant produces a non-empty label, marker, and tone", () => {
  for (const variant of priorityChipVariants) {
    const model = getPriorityChipModel({ priority: variant });
    expect(model.label.length).toBeGreaterThan(0);
    expect(model.marker.length).toBeGreaterThan(0);
    expect(model.tone.bg).toStartWith("#");
    expect(model.tone.fg).toStartWith("#");
  }
});

test("PriorityChip: P0 marker is louder than lower priorities", () => {
  expect(getPriorityChipModel({ priority: "p0" }).marker).toBe("!!");
  expect(getPriorityChipModel({ priority: "p1" }).marker).toBe("!");
  expect(getPriorityChipModel({ priority: "p4" }).marker).toBe("·");
});

test("PriorityChip: ariaLabel is descriptive and includes the priority code", () => {
  const model = getPriorityChipModel({ priority: "p0" });
  expect(model.ariaLabel).toContain("P0");
  expect(model.ariaLabel.toLowerCase()).toContain("critical");
});

test("PriorityChip: explicit label override wins over default", () => {
  const model = getPriorityChipModel({ priority: "p2", label: "Medium · ship this week" });
  expect(model.label).toBe("Medium · ship this week");
});

test("PriorityChip: size defaults to md", () => {
  expect(getPriorityChipModel({ priority: "p1" }).size).toBe("md");
  expect(getPriorityChipModel({ priority: "p1", size: "sm" }).size).toBe("sm");
});
