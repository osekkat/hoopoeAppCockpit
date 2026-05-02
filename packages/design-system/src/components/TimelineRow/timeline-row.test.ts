import { expect, test } from "bun:test";
import { getTimelineRowModel, timelineRowKinds } from "./timeline-row.ts";

test("TimelineRow: every kind has a label, marker, and ariaLabel", () => {
  for (const kind of timelineRowKinds) {
    const model = getTimelineRowModel({
      id: `row-${kind}`,
      kind,
      timestampLabel: "12:01",
      actor: { id: "u1", displayName: "GreenBear", kind: "agent", harness: "claude" },
      summary: `event of kind ${kind}`,
    });
    expect(model.kindLabel.length).toBeGreaterThan(0);
    expect(model.kindMarker.length).toBeGreaterThan(0);
    expect(model.ariaLabel).toContain(kind === "mail" ? "Mail" : model.kindLabel);
    expect(model.ariaLabel).toContain("GreenBear");
    expect(model.ariaLabel).toContain("12:01");
  }
});

test("TimelineRow: agent harness produces a tone; user gets muted; system gets null", () => {
  const agentModel = getTimelineRowModel({
    id: "r1",
    kind: "agent-decision",
    timestampLabel: "12:01",
    actor: { id: "a1", displayName: "GreenBear", kind: "agent", harness: "claude" },
    summary: "claimed bead",
  });
  const userModel = getTimelineRowModel({
    id: "r2",
    kind: "user-message",
    timestampLabel: "12:02",
    actor: { id: "u1", displayName: "Ops", kind: "user" },
    summary: "@orchestrator status?",
  });
  const systemModel = getTimelineRowModel({
    id: "r3",
    kind: "audit",
    timestampLabel: "12:03",
    actor: { id: "s1", displayName: "audit-log", kind: "system" },
    summary: "settings changed",
  });
  expect(agentModel.actor.tone?.dot).toStartWith("#");
  expect(userModel.actor.tone?.bg).toStartWith("#");
  expect(systemModel.actor.tone).toBeNull();
});

test("TimelineRow: pills, inlinePreview, clickTarget propagate", () => {
  const model = getTimelineRowModel({
    id: "r1",
    kind: "build",
    timestampLabel: "12:01",
    actor: { id: "s", displayName: "rch", kind: "system" },
    summary: "build green",
    pills: [{ id: "p1", label: "rch" }],
    inlinePreview: "tail of log",
    clickTarget: "diagnostics://build/123",
  });
  expect(model.pills).toHaveLength(1);
  expect(model.pills[0]?.label).toBe("rch");
  expect(model.inlinePreview).toBe("tail of log");
  expect(model.clickTarget).toBe("diagnostics://build/123");
});

test("TimelineRow: unread defaults to false", () => {
  expect(
    getTimelineRowModel({
      id: "r1",
      kind: "mail",
      timestampLabel: "12:01",
      actor: { id: "a", displayName: "x", kind: "agent" },
      summary: "y",
    }).unread,
  ).toBe(false);
});
