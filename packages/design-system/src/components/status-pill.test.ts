import { expect, test } from "bun:test";
import {
  getStatusPillModel,
  statusPillStatesByKind,
} from "./status-pill.ts";
import type { StatusPillKind, StatusPillProps } from "./status-pill.ts";

test("StatusPill covers every declared kind and state", () => {
  for (const [kind, states] of Object.entries(statusPillStatesByKind)) {
    for (const state of states) {
      const model = getStatusPillModel({ kind, state } as StatusPillProps);

      expect(model.kind).toBe(kind);
      expect(model.state).toBe(state);
      expect(model.label.length).toBeGreaterThan(0);
      expect(model.ariaLabel).toContain("status");
      expect(model.marker.length).toBeGreaterThan(0);
      expect(model.tone.bg).toStartWith("#");
      expect(model.tone.fg).toStartWith("#");
    }
  }
});

test("StatusPill exposes a non-color marker and stable size styles", () => {
  const small = getStatusPillModel({
    kind: "capability",
    state: "blocked-by-policy",
    size: "sm",
  });
  const medium = getStatusPillModel({
    kind: "job",
    state: "waiting_approval",
    size: "md",
  });

  expect(small.marker).toBe("!");
  expect(small.markerStyle.minWidth).toBe("15px");
  expect(medium.marker).toBe("A");
  expect(medium.markerStyle.minWidth).toBe("18px");
});

test("StatusPill supports explicit labels and aria labels", () => {
  const model = getStatusPillModel({
    kind: "bead",
    state: "in_progress",
    label: "Working B-142",
    ariaLabel: "Bead B-142 is currently being worked",
  });

  expect(model.label).toBe("Working B-142");
  expect(model.ariaLabel).toBe("Bead B-142 is currently being worked");
});

test("StatusPill rejects unsupported state combinations", () => {
  expect(() =>
    getStatusPillModel({
      kind: "tool" as StatusPillKind,
      state: "running",
    } as StatusPillProps),
  ).toThrow("Unsupported StatusPill state tool:running");
});
