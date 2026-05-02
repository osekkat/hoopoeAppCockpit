import { expect, test } from "bun:test";
import { agentTileActions, agentTileStatuses, getAgentTileModel } from "./agent-tile.ts";
import type { AgentTileActionId, AgentTileProps } from "./agent-tile.ts";

const baseAgentTile: AgentTileProps = {
  agentName: "BlueHill",
  harness: "codex",
  caamAccount: "codex-pro.account.alpha",
  status: "working",
  currentBead: {
    id: "hp-8sm",
    title: "Build AgentTile primitive",
    status: "in_progress",
  },
  timeOnBeadLabel: "42m",
  recentDecisions: [
    { id: "decision-1", label: "claimed hp-8sm", actor: "BlueHill", occurredAtLabel: "42m ago" },
    { id: "decision-2", label: "reserved AgentTile files", actor: "BlueHill" },
  ],
};

test("AgentTile maps every status to a visible non-color marker", () => {
  for (const status of agentTileStatuses) {
    const model = getAgentTileModel({ ...baseAgentTile, status });

    expect(model.status.id).toBe(status);
    expect(model.status.label.length).toBeGreaterThan(0);
    expect(model.status.ariaLabel).toContain("Agent status");
    expect(model.status.marker.length).toBeGreaterThan(0);
    expect(model.status.tone.bg).toStartWith("#");
  }
});

test("AgentTile exposes the required context-menu actions in stable order", () => {
  const model = getAgentTileModel(baseAgentTile);
  const actionIds = model.actions.map((action) => action.id);

  expect(actionIds).toEqual([
    "switch-account",
    "resume-session",
    "pause-and-notify",
    "kill-and-reassign",
    "send-marching-orders",
  ] satisfies AgentTileActionId[]);
  expect(agentTileActions).toHaveLength(5);
  expect(model.selectedAction).toBeNull();
});

test("AgentTile caps recent decisions at three items", () => {
  const model = getAgentTileModel({
    ...baseAgentTile,
    recentDecisions: [
      { id: "decision-1", label: "one" },
      { id: "decision-2", label: "two" },
      { id: "decision-3", label: "three" },
      { id: "decision-4", label: "four" },
    ],
  });

  expect(model.recentDecisions.map((decision) => decision.id)).toEqual([
    "decision-1",
    "decision-2",
    "decision-3",
  ]);
});

test("AgentTile model includes harness, CAAM account, bead, status, timer, and action state", () => {
  const model = getAgentTileModel({
    ...baseAgentTile,
    selectedActionId: "resume-session",
  });

  expect(model.harness.shortLabel).toBe("CX");
  expect(model.caamAccount).toBe("codex-pro.account.alpha");
  expect(model.currentBead.id).toBe("hp-8sm");
  expect(model.currentBead.statusLabel).toBe("In Progress");
  expect(model.status.marker).toBe("W");
  expect(model.timeOnBeadLabel).toBe("42m");
  expect(model.actions).toHaveLength(5);
  expect(model.selectedAction?.id).toBe("resume-session");
});

test("AgentTile implementation does not import the raw-pane component", async () => {
  const source = await Bun.file(new URL("./agent-tile.ts", import.meta.url)).text();

  expect(source).not.toContain("TerminalPane");
  expect(source).not.toContain("xterm");
  expect(source).not.toContain("scrollback");
});
