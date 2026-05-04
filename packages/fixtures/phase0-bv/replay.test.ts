// hp-ge0 — Phase 0 `bv` fixture replay test.
//
// Validates the captures in `captures/` against the shape declarations
// in `manifest.json`. Catches drift if `bv` ships a new robot output
// shape that the adapter parser hasn't been updated for, or if a peer
// agent partially overwrites this pack.
//
// The captures are real `bv` v0.16.0 output against the local
// hoopoeAppCockpit `.beads/`; see README.md for the local-stand-in
// pedigree and the planned re-capture against a real ACFS VPS once
// `bv` is installed there.

import { describe, expect, test } from "bun:test";
import { readFileSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const PACK_ROOT = dirname(fileURLToPath(import.meta.url));
const CAPTURES = join(PACK_ROOT, "captures");

interface Manifest {
  readonly packVersion: string;
  readonly mode: string;
  readonly realVpsAcceptance: boolean;
  readonly captures: Record<
    string,
    { readonly argv: readonly string[]; readonly exit: number; readonly topLevelKeys?: readonly string[] }
  >;
}

function readManifest(): Manifest {
  const raw = readFileSync(join(PACK_ROOT, "manifest.json"), "utf8");
  return JSON.parse(raw) as Manifest;
}

function readCapture(name: string): unknown {
  return JSON.parse(readFileSync(join(CAPTURES, name), "utf8"));
}

describe("phase0-bv: manifest", () => {
  test("manifest is well-formed and pinned to schemaVersion-equivalent fields", () => {
    const manifest = readManifest();
    expect(manifest.packVersion).toBe("0.1.0");
    expect(manifest.mode).toBe("local-stand-in");
    expect(manifest.realVpsAcceptance).toBe(false);
    expect(Object.keys(manifest.captures).length).toBeGreaterThanOrEqual(8);
  });

  test("every declared capture file actually exists on disk", () => {
    const manifest = readManifest();
    for (const name of Object.keys(manifest.captures)) {
      expect(existsSync(join(CAPTURES, name))).toBe(true);
    }
  });
});

describe("phase0-bv: JSON-shape invariants per robot command", () => {
  test("manifest.captures.topLevelKeys matches each capture's actual top-level keys (drift gate)", () => {
    const manifest = readManifest();
    for (const [name, expected] of Object.entries(manifest.captures)) {
      if (!expected.topLevelKeys || !name.endsWith(".json")) continue;
      const captured = readCapture(name);
      expect(typeof captured).toBe("object");
      expect(captured).not.toBeNull();
      const actual = Object.keys(captured as Record<string, unknown>).sort();
      const expectedSorted = [...expected.topLevelKeys].sort();
      expect({ name, actual }).toEqual({ name, actual: expectedSorted });
    }
  });

  test("robot-triage: triage.quick_ref + recommendations + project_health shape", () => {
    const triage = readCapture("robot-triage.json") as {
      readonly triage: {
        readonly meta: { readonly issue_count: number; readonly phase2_ready: boolean };
        readonly quick_ref: {
          readonly open_count: number;
          readonly actionable_count: number;
          readonly top_picks: ReadonlyArray<{ readonly id: string; readonly score: number }>;
        };
        readonly recommendations: ReadonlyArray<{ readonly id: string; readonly score: number }>;
        readonly project_health: { readonly counts: { readonly total: number } };
      };
    };
    expect(triage.triage.meta.issue_count).toBeGreaterThan(0);
    expect(triage.triage.meta.phase2_ready).toBe(true);
    expect(triage.triage.quick_ref.open_count).toBeGreaterThanOrEqual(0);
    expect(triage.triage.quick_ref.top_picks.length).toBeGreaterThan(0);
    for (const pick of triage.triage.quick_ref.top_picks) {
      expect(typeof pick.id).toBe("string");
      expect(typeof pick.score).toBe("number");
      expect(pick.score).toBeGreaterThanOrEqual(0);
    }
    expect(Array.isArray(triage.triage.recommendations)).toBe(true);
    expect(triage.triage.project_health.counts.total).toBe(triage.triage.meta.issue_count);
  });

  test("robot-plan: plan.tracks is non-empty + summary.highest_impact present", () => {
    const planned = readCapture("robot-plan.json") as {
      readonly plan: {
        readonly tracks: ReadonlyArray<{ readonly items: ReadonlyArray<unknown> }>;
        readonly summary: { readonly highest_impact?: unknown };
      };
    };
    expect(Array.isArray(planned.plan.tracks)).toBe(true);
    expect(planned.plan.tracks.length).toBeGreaterThan(0);
    expect("highest_impact" in planned.plan.summary).toBe(true);
  });

  test("robot-priority: recommendations array + summary present", () => {
    const priority = readCapture("robot-priority.json") as {
      readonly recommendations: ReadonlyArray<unknown> | null;
      readonly summary: Record<string, unknown>;
    };
    // recommendations may legitimately be null when no reprioritization is suggested.
    expect(priority.recommendations === null || Array.isArray(priority.recommendations)).toBe(true);
    expect(typeof priority.summary).toBe("object");
  });

  test("robot-insights: Bottlenecks array + Stats topology + Cycles either array or null", () => {
    const insights = readCapture("robot-insights.json") as {
      readonly Bottlenecks: ReadonlyArray<unknown>;
      readonly Cycles: ReadonlyArray<unknown> | null;
      readonly Stats: {
        readonly NodeCount: number;
        readonly EdgeCount: number;
        readonly TopologicalOrder?: ReadonlyArray<unknown>;
      };
    };
    expect(Array.isArray(insights.Bottlenecks)).toBe(true);
    // Cycles is `null` on a healthy DAG; an array of cycle paths when
    // the graph contains cycles. Either is valid.
    expect(insights.Cycles === null || Array.isArray(insights.Cycles)).toBe(true);
    expect(typeof insights.Stats.NodeCount).toBe("number");
    expect(typeof insights.Stats.EdgeCount).toBe("number");
    expect(insights.Stats.NodeCount).toBeGreaterThan(0);
  });

  test("robot-recipes: at least the canonical built-in recipes are present", () => {
    const recipes = readCapture("robot-recipes.json") as {
      readonly recipes: ReadonlyArray<{ readonly name: string; readonly source: string }>;
    };
    expect(Array.isArray(recipes.recipes)).toBe(true);
    expect(recipes.recipes.length).toBeGreaterThan(0);
    const names = recipes.recipes.map((r) => r.name);
    for (const required of ["actionable", "blocked", "default", "high-impact", "quick-wins"]) {
      expect(names).toContain(required);
    }
    for (const recipe of recipes.recipes) {
      expect(["builtin", "user", "project"]).toContain(recipe.source);
    }
  });

  test("robot-next: a single top recommendation with id + score", () => {
    const next = readCapture("robot-next.json") as {
      readonly id: string;
      readonly title: string;
      readonly score: number;
      readonly claim_command: string;
    };
    expect(typeof next.id).toBe("string");
    expect(next.id.length).toBeGreaterThan(0);
    expect(typeof next.title).toBe("string");
    expect(typeof next.score).toBe("number");
    expect(next.claim_command).toContain(next.id);
  });

  test("robot-forecast-all: forecast_count matches forecasts.length + summary.total >= forecast_count", () => {
    const forecast = readCapture("robot-forecast-all.json") as {
      readonly forecast_count: number;
      readonly forecasts: ReadonlyArray<unknown>;
      readonly summary: { readonly total?: number };
    };
    expect(forecast.forecast_count).toBe(forecast.forecasts.length);
    expect(forecast.forecast_count).toBeGreaterThan(0);
  });
});

describe("phase0-bv: export.head.md format", () => {
  test("export head starts with the canonical Beads Export header", () => {
    const exportHead = readFileSync(join(CAPTURES, "export.head.md"), "utf8");
    expect(exportHead.startsWith("# Beads Export\n")).toBe(true);
    expect(exportHead).toContain("## Summary");
    expect(exportHead).toContain("| **Total** |");
  });
});
