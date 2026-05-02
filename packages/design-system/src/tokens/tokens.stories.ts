import {
  agentFamilyTones,
  coverageRamp,
  hoopoeTokens,
  priorityTones,
  statusTones,
  toolHealthTones,
} from "./index.ts";

const meta = {
  title: "Tokens/Hoopoe",
  parameters: { layout: "fullscreen" },
};

export default meta;

export const Overview = {
  render: () => {
    const darkSurface = hoopoeTokens.color.surface.dark;
    const sansFontFamily = hoopoeTokens.typography.sans.join(", ");
    const main = document.createElement("main");
    main.style.minHeight = "100vh";
    main.style.padding = "32px";
    main.style.background = darkSurface.base;
    main.style.color = darkSurface.text;
    main.style.fontFamily = sansFontFamily;

    const grid = document.createElement("section");
    grid.style.display = "grid";
    grid.style.gap = "24px";
    grid.style.maxWidth = "1120px";
    grid.append(
      section("Status", statusTones),
      section("Priority", priorityTones),
      section("Agents", agentFamilyTones),
      section("Tool Health", toolHealthTones),
      section(
        "Coverage",
        Object.fromEntries(
          coverageRamp.map((stop) => [
            `${stop.label} ${stop.threshold}%`,
            {
              bg: stop.bg,
              fg: stop.fg,
              border: stop.border,
              dot: stop.dot,
            },
          ]),
        ),
      ),
    );
    main.append(grid);

    return main;
  },
};

function section(title: string, tones: Record<string, TokenSwatch>): HTMLElement {
  const wrapper = document.createElement("section");
  const heading = document.createElement("h2");
  const swatches = document.createElement("div");

  heading.textContent = title;
  heading.style.margin = "0 0 12px";
  heading.style.fontSize = "18px";
  heading.style.fontWeight = "650";

  swatches.style.display = "grid";
  swatches.style.gridTemplateColumns = "repeat(auto-fill, minmax(180px, 1fr))";
  swatches.style.gap = "10px";

  for (const [name, tone] of Object.entries(tones)) {
    swatches.append(swatch(name, tone));
  }

  wrapper.append(heading, swatches);
  return wrapper;
}

function swatch(name: string, tone: TokenSwatch): HTMLElement {
  const tile = document.createElement("div");
  const dot = document.createElement("span");
  const label = document.createElement("span");

  tile.style.display = "grid";
  tile.style.gridTemplateColumns = "14px 1fr";
  tile.style.alignItems = "center";
  tile.style.gap = "10px";
  tile.style.minHeight = "42px";
  tile.style.padding = "8px 10px";
  tile.style.borderRadius = "8px";
  tile.style.border = `1px solid ${tone.border}`;
  tile.style.background = tone.bg;
  tile.style.color = tone.fg;
  tile.style.fontSize = "12px";
  tile.style.fontWeight = "650";

  dot.style.width = "10px";
  dot.style.height = "10px";
  dot.style.borderRadius = "999px";
  dot.style.background = tone.dot;

  label.textContent = name;
  tile.append(dot, label);

  return tile;
}

interface TokenSwatch {
  readonly bg: string;
  readonly fg: string;
  readonly border: string;
  readonly dot: string;
}
