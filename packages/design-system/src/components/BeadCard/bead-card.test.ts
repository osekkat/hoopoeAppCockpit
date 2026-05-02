import { expect, test } from "bun:test";
import { beadCardStatuses, getBeadCardModel } from "./bead-card.ts";

test("BeadCard: every status maps to a label, tone, and non-color marker", () => {
  for (const status of beadCardStatuses) {
    const model = getBeadCardModel({
      id: "hp-test",
      title: "Test bead",
      status,
      priority: "p1",
    });
    expect(model.status.label.length).toBeGreaterThan(0);
    expect(model.status.marker.length).toBeGreaterThan(0);
    expect(model.status.tone.bg).toStartWith("#");
  }
});

test("BeadCard: priorityChip is composed via PriorityChip primitive", () => {
  const model = getBeadCardModel({
    id: "hp-test",
    title: "Test bead",
    status: "ready",
    priority: "p0",
  });
  expect(model.priorityChip.priority).toBe("p0");
  expect(model.priorityChip.marker).toBe("!!");
});

test("BeadCard: filesTouched label uses singular vs plural", () => {
  expect(
    getBeadCardModel({
      id: "hp-test",
      title: "x",
      status: "ready",
      priority: "p2",
      filesTouched: 0,
    }).filesTouchedLabel,
  ).toBe("0 files");
  expect(
    getBeadCardModel({
      id: "hp-test",
      title: "x",
      status: "ready",
      priority: "p2",
      filesTouched: 1,
    }).filesTouchedLabel,
  ).toBe("1 file");
  expect(
    getBeadCardModel({
      id: "hp-test",
      title: "x",
      status: "ready",
      priority: "p2",
      filesTouched: 7,
    }).filesTouchedLabel,
  ).toBe("7 files");
});

test("BeadCard: owner harness produces a tone; missing harness leaves tone null", () => {
  expect(
    getBeadCardModel({
      id: "hp-test",
      title: "x",
      status: "ready",
      priority: "p2",
      owner: { agentName: "GreenBear", harness: "claude" },
    }).owner?.tone?.dot,
  ).toStartWith("#");
  expect(
    getBeadCardModel({
      id: "hp-test",
      title: "x",
      status: "ready",
      priority: "p2",
      owner: { agentName: "GreenBear" },
    }).owner?.tone,
  ).toBeNull();
});

test("BeadCard: compact variant downsizes the priority chip", () => {
  expect(
    getBeadCardModel({
      id: "hp-test",
      title: "x",
      status: "ready",
      priority: "p1",
      variant: "compact",
    }).priorityChip.size,
  ).toBe("sm");
});

test("BeadCard: aria-label is descriptive by default", () => {
  const model = getBeadCardModel({
    id: "hp-test",
    title: "Vendor t3code",
    status: "in_progress",
    priority: "p0",
  });
  expect(model.ariaLabel).toContain("hp-test");
  expect(model.ariaLabel).toContain("Vendor t3code");
  expect(model.ariaLabel).toContain("In progress");
  expect(model.ariaLabel).toContain("P0");
});
