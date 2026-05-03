// `@hoopoe/fixture-replay` — stage taxonomy + adapter→stage mapping (hp-q3t).
//
// The cockpit is organized into four `STAGE N — VERB` chrome blocks
// (plan.md §7). For replay-driven tests, "reaching a stage" means the
// adapters that back that stage's UI have been touched.
//
// Mapping (kept narrow on purpose — overlap is fine since multiple stages
// can share an adapter; see plan.md §1.1 source-of-truth table):
//
//   planning   → oracle (planning rounds), agent_mail (orchestrator chat)
//   beads      → br, bv
//   swarm      → ntm, caam, agent_mail
//   hardening  → ubs, health
//
// agent_mail is intentionally listed under planning + swarm because it
// powers both the user↔orchestrator chat (planning) and the agent↔agent
// mail panel (swarm). Tests that expect a *narrow* stage match should
// instead assert on a specific adapter via `expectAdapterCalled`.

import type { AdapterSlug } from "@hoopoe/fixtures";

export const STAGE_IDS = ["planning", "beads", "swarm", "hardening"] as const;
export type StageId = (typeof STAGE_IDS)[number];

export const STAGE_ADAPTERS: Readonly<Record<StageId, readonly AdapterSlug[]>> = {
  planning: ["oracle", "agent_mail"],
  beads: ["br", "bv"],
  swarm: ["ntm", "caam", "agent_mail"],
  hardening: ["ubs", "health"],
};

export function isStageId(value: string): value is StageId {
  return (STAGE_IDS as readonly string[]).includes(value);
}

export function stageForAdapter(adapter: AdapterSlug): readonly StageId[] {
  const stages: StageId[] = [];
  for (const stage of STAGE_IDS) {
    if (STAGE_ADAPTERS[stage].includes(adapter)) {
      stages.push(stage);
    }
  }
  return stages;
}
