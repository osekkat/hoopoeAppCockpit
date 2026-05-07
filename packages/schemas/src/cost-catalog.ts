// hp-mbty engine slice: source-of-truth runtime helper for the §13 cost
// catalog mirrored from agent-flywheel.com. Loaded by the wizard
// "Subscription requirement" step, the new-plan empty state, and the
// project-settings subscription docs page (hp-mbty UI surfaces, follow-up).
//
// File layout:
//   packages/schemas/cost-catalog.json   — JSON source of truth (this slice)
//   packages/schemas/src/cost-catalog.ts — runtime types + validator (this slice)
//
// Per hp-mbty acceptance: the renderer derives every cost-card row from this
// catalog and never fabricates per-call USD numbers (§13 "no token-cost
// theatre"). When `caut` cannot measure a provider's monthly tier, the
// catalog row reports `monthlyUsdMin: 0, monthlyUsdMax: 0` and the UI surface
// must display "unmeasured" (§1.4 inspectability), never a synthesized value.

import catalogJson from "../cost-catalog.json" with { type: "json" };

export type CostCatalogTier = "core" | "recommended" | "optional";

export type CostCatalogRequiredFor = "planning" | "swarm" | "tending";

export interface CostCatalogRow {
  readonly product: string;
  readonly displayName: string;
  readonly monthlyUsdMin: number;
  readonly monthlyUsdMax: number;
  readonly providerUrl: string;
  readonly requiredFor: readonly CostCatalogRequiredFor[];
  readonly tier: CostCatalogTier;
  readonly notes: string;
}

export interface CostCatalog {
  readonly version: string;
  readonly source: string;
  readonly rows: readonly CostCatalogRow[];
  readonly allInMonthlyUsdMin: number;
  readonly allInMonthlyUsdMax: number;
  readonly comparison: string;
}

const TIERS: ReadonlySet<CostCatalogTier> = new Set(["core", "recommended", "optional"]);

const REQUIRED_FOR: ReadonlySet<CostCatalogRequiredFor> = new Set([
  "planning",
  "swarm",
  "tending",
]);

function isRow(value: unknown): value is CostCatalogRow {
  if (value === null || typeof value !== "object") return false;
  const r = value as Record<string, unknown>;
  if (
    typeof r.product !== "string" ||
    typeof r.displayName !== "string" ||
    typeof r.monthlyUsdMin !== "number" ||
    typeof r.monthlyUsdMax !== "number" ||
    typeof r.providerUrl !== "string" ||
    typeof r.notes !== "string"
  ) {
    return false;
  }
  if (typeof r.tier !== "string" || !TIERS.has(r.tier as CostCatalogTier)) return false;
  if (!Array.isArray(r.requiredFor) || r.requiredFor.length === 0) return false;
  for (const entry of r.requiredFor) {
    if (typeof entry !== "string" || !REQUIRED_FOR.has(entry as CostCatalogRequiredFor)) {
      return false;
    }
  }
  return r.monthlyUsdMin >= 0 && r.monthlyUsdMax >= r.monthlyUsdMin;
}

function isCatalog(value: unknown): value is CostCatalog {
  if (value === null || typeof value !== "object") return false;
  const c = value as Record<string, unknown>;
  if (
    typeof c.version !== "string" ||
    typeof c.source !== "string" ||
    typeof c.allInMonthlyUsdMin !== "number" ||
    typeof c.allInMonthlyUsdMax !== "number" ||
    typeof c.comparison !== "string"
  ) {
    return false;
  }
  if (!Array.isArray(c.rows) || c.rows.length === 0) return false;
  for (const row of c.rows) {
    if (!isRow(row)) return false;
  }
  return c.allInMonthlyUsdMin >= 0 && c.allInMonthlyUsdMax >= c.allInMonthlyUsdMin;
}

export function loadCostCatalog(): CostCatalog {
  if (!isCatalog(catalogJson)) {
    throw new Error("cost-catalog.json failed runtime validation");
  }
  return catalogJson;
}

export function rowsRequiredFor(
  catalog: CostCatalog,
  capability: CostCatalogRequiredFor,
): readonly CostCatalogRow[] {
  return catalog.rows.filter((row) => row.requiredFor.includes(capability));
}

export function isCoreTier(row: CostCatalogRow): boolean {
  return row.tier === "core";
}

export function isMeasured(row: CostCatalogRow): boolean {
  return row.monthlyUsdMax > 0;
}
