import { expect, test } from "bun:test";
import {
  isCoreTier,
  isMeasured,
  loadCostCatalog,
  rowsRequiredFor,
  type CostCatalog,
} from "./cost-catalog.ts";

test("cost catalog loads and passes runtime validation", () => {
  const catalog: CostCatalog = loadCostCatalog();
  expect(catalog.version).toBe("1.0.0");
  expect(catalog.rows.length).toBeGreaterThanOrEqual(4);
  expect(catalog.allInMonthlyUsdMin).toBe(440);
  expect(catalog.allInMonthlyUsdMax).toBe(656);
});

test("catalog mirrors the §13 README cost table products", () => {
  const catalog = loadCostCatalog();
  const products = catalog.rows.map((row) => row.product).sort();
  expect(products).toEqual(["chatgpt-pro", "claude-max", "gemini-ultra", "vps"]);
});

test("VPS row matches the README $40-56 range and is core tier", () => {
  const catalog = loadCostCatalog();
  const vps = catalog.rows.find((row) => row.product === "vps");
  expect(vps).toBeDefined();
  if (!vps) return;
  expect(vps.monthlyUsdMin).toBe(40);
  expect(vps.monthlyUsdMax).toBe(56);
  expect(vps.tier).toBe("core");
  expect(vps.requiredFor).toContain("planning");
  expect(vps.requiredFor).toContain("swarm");
  expect(vps.requiredFor).toContain("tending");
});

test("Claude Max row covers the standard + power tiers", () => {
  const catalog = loadCostCatalog();
  const claude = catalog.rows.find((row) => row.product === "claude-max");
  expect(claude).toBeDefined();
  if (!claude) return;
  expect(claude.monthlyUsdMin).toBe(200);
  expect(claude.monthlyUsdMax).toBe(400);
  expect(claude.tier).toBe("core");
});

test("ChatGPT Pro is recommended (drives Oracle for planning) — not core", () => {
  const catalog = loadCostCatalog();
  const chatgpt = catalog.rows.find((row) => row.product === "chatgpt-pro");
  expect(chatgpt).toBeDefined();
  if (!chatgpt) return;
  expect(chatgpt.tier).toBe("recommended");
  expect(chatgpt.requiredFor).toEqual(["planning"]);
});

test("Gemini Ultra row reports unmeasured cost (§1.4 inspectability)", () => {
  const catalog = loadCostCatalog();
  const gemini = catalog.rows.find((row) => row.product === "gemini-ultra");
  expect(gemini).toBeDefined();
  if (!gemini) return;
  expect(gemini.tier).toBe("optional");
  expect(isMeasured(gemini)).toBe(false);
});

test("rowsRequiredFor filters by capability", () => {
  const catalog = loadCostCatalog();
  const planning = rowsRequiredFor(catalog, "planning");
  const planningProducts = planning.map((row) => row.product).sort();
  expect(planningProducts).toEqual(["chatgpt-pro", "claude-max", "gemini-ultra", "vps"]);

  const tending = rowsRequiredFor(catalog, "tending");
  const tendingProducts = tending.map((row) => row.product).sort();
  expect(tendingProducts).toEqual(["claude-max", "vps"]);
});

test("isCoreTier flags exactly the core rows", () => {
  const catalog = loadCostCatalog();
  const core = catalog.rows.filter(isCoreTier).map((row) => row.product).sort();
  expect(core).toEqual(["claude-max", "vps"]);
});

test("all-in range covers at least the sum of core minimums", () => {
  const catalog = loadCostCatalog();
  const coreMin = catalog.rows
    .filter(isCoreTier)
    .reduce((sum, row) => sum + row.monthlyUsdMin, 0);
  expect(catalog.allInMonthlyUsdMin).toBeGreaterThanOrEqual(coreMin);
});
