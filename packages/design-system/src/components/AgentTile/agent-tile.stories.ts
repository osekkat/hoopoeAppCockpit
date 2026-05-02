import { hoopoeTokens } from "../../tokens/index.ts";
import { agentTileStatuses, renderAgentTileElement } from "./agent-tile.ts";
import type {
  AgentTileBeadClaim,
  AgentTileHarness,
  AgentTileProps,
  AgentTileStatus,
} from "./agent-tile.ts";

const meta = {
  title: "Components/AgentTile",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const SwarmGrid = {
  render: () => {
    const main = document.createElement("main");
    const grid = document.createElement("section");

    main.style.minHeight = "100vh";
    main.style.padding = "32px";
    main.style.background = hoopoeTokens.color.surface.dark.baseDeep;
    main.style.color = hoopoeTokens.color.surface.dark.text;
    main.style.fontFamily = hoopoeTokens.typography.sans.join(", ");

    grid.style.display = "grid";
    grid.style.gridTemplateColumns = "repeat(auto-fit, minmax(280px, 1fr))";
    grid.style.gap = "16px";
    grid.style.maxWidth = "1180px";

    for (const [index, status] of agentTileStatuses.entries()) {
      grid.append(renderAgentTileElement(tileForStatus(status, index)));
    }

    main.append(grid);
    return main;
  },
};

function tileForStatus(status: AgentTileStatus, index: number): AgentTileProps {
  const harnesses: readonly AgentTileHarness[] = ["claude", "codex", "gemini"];
  const harness = harnesses[index % harnesses.length] ?? "codex";
  const agentNames = ["BlueHill", "GreenBear", "FuchsiaPond", "PurpleBear", "FuchsiaStone"];
  const beadStatusByAgentStatus: Record<AgentTileStatus, AgentTileBeadClaim | null> = {
    working: {
      id: "hp-8sm",
      title: "AgentTile primitive",
      status: "in_progress",
    },
    idle: null,
    "awaiting-review": {
      id: "hp-i62",
      title: "Reusable component set",
      status: "in_review",
    },
    wedged: {
      id: "hp-z1x",
      title: "Desktop stage shell",
      status: "blocked",
    },
    "rate-limited": {
      id: "hp-6f4",
      title: "Command palette registry",
      status: "paused",
    },
  };

  return {
    agentName: agentNames[index] ?? "HoopoeAgent",
    harness,
    caamAccount: `${harness}-max.account.${index + 1}`,
    status,
    currentBead: beadStatusByAgentStatus[status],
    timeOnBeadLabel: status === "idle" ? "No active bead" : `${(index + 1) * 18}m`,
    recentDecisions: [
      { id: `${status}-1`, label: "opened bead context", actor: "orchestrator-chat" },
      { id: `${status}-2`, label: "reported latest test signal", occurredAtLabel: "8m ago" },
      { id: `${status}-3`, label: "updated agent mail thread", occurredAtLabel: "3m ago" },
    ],
  };
}
