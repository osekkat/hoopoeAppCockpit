import { expect, test } from "bun:test";
import {
  COVERAGE_RAMP,
  HOOPOE_DESIGN_SYSTEM_PACKAGE_NAME,
  statusPillStatesByKind,
  agentFamilyTones,
  coverageThresholds,
  cssVariableThemes,
  priorityTones,
  statusTones,
  toolHealthTones,
} from "./index.ts";
import type { ToneToken } from "./tokens/index.ts";

test("design-system exposes package identity and coverage thresholds", () => {
  expect(HOOPOE_DESIGN_SYSTEM_PACKAGE_NAME).toBe("@hoopoe/design-system");
  expect(COVERAGE_RAMP.length).toBe(3);
  expect(COVERAGE_RAMP.map((stop) => stop.threshold)).toEqual([0, 60, 80]);
  expect(coverageThresholds).toEqual({ low: 0, medium: 60, high: 80 });
  expect(statusPillStatesByKind.bead).toContain("paused");
});

test("status, priority, agent, and tool tones meet AA text contrast", () => {
  for (const [family, tones] of Object.entries({
    status: statusTones,
    priority: priorityTones,
    agent: agentFamilyTones,
    toolHealth: toolHealthTones,
  })) {
    for (const [name, tone] of Object.entries<ToneToken>(tones)) {
      expect(contrastRatio(tone.fg, tone.bg), `${family}.${name}`).toBeGreaterThanOrEqual(
        4.5,
      );
    }
  }
});

test("theme.css exposes every dark-mode variable derived from tokens.ts", async () => {
  const themeCss = await Bun.file(new URL("./tokens/theme.css", import.meta.url)).text();

  for (const [name, value] of Object.entries(cssVariableThemes.dark)) {
    expect(normalizeCss(themeCss), name).toContain(
      `${normalizeCssValue(name)}:${normalizeCssValue(value)}`,
    );
  }
});

function contrastRatio(foreground: string, background: string): number {
  const fg = relativeLuminance(hexToRgb(foreground));
  const bg = relativeLuminance(hexToRgb(background));
  const lighter = Math.max(fg, bg);
  const darker = Math.min(fg, bg);

  return (lighter + 0.05) / (darker + 0.05);
}

function relativeLuminance([red, green, blue]: readonly [number, number, number]): number {
  const [r, g, b] = [red, green, blue].map((channel) => {
    const value = channel / 255;

    return value <= 0.03928
      ? value / 12.92
      : Math.pow((value + 0.055) / 1.055, 2.4);
  });

  if (r === undefined || g === undefined || b === undefined) {
    throw new Error("Invalid RGB tuple");
  }

  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function hexToRgb(value: string): [number, number, number] {
  const hex = value.replace("#", "");

  if (!/^[0-9A-Fa-f]{6}$/.test(hex)) {
    throw new Error(`Expected 6-digit hex color, received ${value}`);
  }

  return [
    Number.parseInt(hex.slice(0, 2), 16),
    Number.parseInt(hex.slice(2, 4), 16),
    Number.parseInt(hex.slice(4, 6), 16),
  ];
}

function normalizeCss(css: string): string {
  return css.replaceAll(/\s+/g, "").toUpperCase();
}

function normalizeCssValue(value: string): string {
  return value.replaceAll(/\s+/g, "").toUpperCase();
}
