import { expect, test } from "bun:test";
import { getHealthKpiCardModel } from "./health-kpi-card.ts";

test("HealthKpiCard: numeric value renders with optional unit", () => {
  expect(
    getHealthKpiCardModel({ title: "Coverage", value: 73, unit: "%" }).valueLabel,
  ).toBe("73%");
  expect(
    getHealthKpiCardModel({ title: "Complexity", value: 12.4 }).valueLabel,
  ).toBe("12.4");
});

test("HealthKpiCard: positive delta is up; negative is down; zero is flat", () => {
  expect(
    getHealthKpiCardModel({ title: "Cov", value: 80, delta: 5, deltaUnit: "%" }).delta?.trend,
  ).toBe("up");
  expect(
    getHealthKpiCardModel({ title: "Cov", value: 80, delta: -3 }).delta?.trend,
  ).toBe("down");
  expect(
    getHealthKpiCardModel({ title: "Cov", value: 80, delta: 0 }).delta?.trend,
  ).toBe("flat");
});

test("HealthKpiCard: invertGoodTrend flips the tone for downward deltas", () => {
  // Coverage: up is good (default).
  const cov = getHealthKpiCardModel({ title: "Cov", value: 80, delta: 5 });
  // Complexity: down is good.
  const complexity = getHealthKpiCardModel({
    title: "Complexity",
    value: 12,
    delta: -2,
    invertGoodTrend: true,
  });
  expect(cov.delta?.tone.dot).toBe(complexity.delta?.tone.dot);
  // And in the opposite direction the tones disagree:
  const covDown = getHealthKpiCardModel({ title: "Cov", value: 80, delta: -3 });
  const complexityUp = getHealthKpiCardModel({
    title: "Complexity",
    value: 14,
    delta: 2,
    invertGoodTrend: true,
  });
  expect(covDown.delta?.tone.dot).toBe(complexityUp.delta?.tone.dot);
});

test("HealthKpiCard: delta label includes a non-color trend marker", () => {
  expect(
    getHealthKpiCardModel({ title: "x", value: 1, delta: 3 }).delta?.label,
  ).toContain("▲");
  expect(
    getHealthKpiCardModel({ title: "x", value: 1, delta: -3 }).delta?.label,
  ).toContain("▼");
});

test("HealthKpiCard: missing/non-finite delta yields delta: null", () => {
  expect(getHealthKpiCardModel({ title: "x", value: 1 }).delta).toBeNull();
  expect(
    getHealthKpiCardModel({ title: "x", value: 1, delta: Number.NaN }).delta,
  ).toBeNull();
});

test("HealthKpiCard: aria-label includes title, value, and delta", () => {
  const model = getHealthKpiCardModel({
    title: "Coverage",
    value: 73,
    unit: "%",
    delta: 2,
    deltaUnit: "%",
  });
  expect(model.ariaLabel).toContain("Coverage");
  expect(model.ariaLabel).toContain("73%");
  expect(model.ariaLabel).toContain("+2%");
});

test("HealthKpiCard: sparkline points pass through verbatim", () => {
  const points = [{ t: 1, v: 70 }, { t: 2, v: 73 }];
  expect(getHealthKpiCardModel({ title: "x", value: 73, sparkline: points }).sparkline).toEqual(
    points,
  );
});
