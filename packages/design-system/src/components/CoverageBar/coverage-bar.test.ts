import { expect, test } from "bun:test";
import { classifyCoverageBand, getCoverageBarModel } from "./coverage-bar.ts";

test("CoverageBar: classifies bands at threshold boundaries", () => {
  expect(classifyCoverageBand(0)).toBe("low");
  expect(classifyCoverageBand(59)).toBe("low");
  expect(classifyCoverageBand(60)).toBe("medium");
  expect(classifyCoverageBand(79)).toBe("medium");
  expect(classifyCoverageBand(80)).toBe("high");
  expect(classifyCoverageBand(100)).toBe("high");
});

test("CoverageBar: clamps out-of-range percentages", () => {
  expect(getCoverageBarModel({ percent: -10 }).clampedPercent).toBe(0);
  expect(getCoverageBarModel({ percent: 150 }).clampedPercent).toBe(100);
  expect(getCoverageBarModel({ percent: Number.NaN }).clampedPercent).toBe(0);
});

test("CoverageBar: label and aria-label reflect the rounded percentage and band", () => {
  const model = getCoverageBarModel({ percent: 73.4 });
  expect(model.label).toBe("73%");
  expect(model.band).toBe("medium");
  expect(model.ariaLabel).toContain("73");
  expect(model.ariaLabel).toContain("medium");
});

test("CoverageBar: tones from the coverage ramp are wired up", () => {
  expect(getCoverageBarModel({ percent: 30 }).tone.bg).toStartWith("#");
  expect(getCoverageBarModel({ percent: 30 }).band).toBe("low");
  expect(getCoverageBarModel({ percent: 99 }).band).toBe("high");
});

test("CoverageBar: default gridlines fall at medium and high thresholds", () => {
  const model = getCoverageBarModel({ percent: 50 });
  expect(model.gridlines.map((g) => g.position)).toEqual([60, 80]);
  expect(model.gridlines[0]?.band).toBe("medium");
  expect(model.gridlines[1]?.band).toBe("high");
});

test("CoverageBar: hideLabel propagates to the model", () => {
  expect(getCoverageBarModel({ percent: 50, hideLabel: true }).hideLabel).toBe(true);
  expect(getCoverageBarModel({ percent: 50 }).hideLabel).toBe(false);
});
