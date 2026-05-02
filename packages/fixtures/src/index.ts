// `@hoopoe/fixtures` — Mock Flywheel corpus.
//
// Phase 0 (hp-7cs / hp-6v3 / hp-78m / hp-wle / hp-d54 / hp-pl5o) captures
// real ACFS-VPS JSON snapshots and parser fixtures for Git, beads, `bv`
// triage/plan/insights/diff, NTM, Agent Mail, reservations, and health.
// They feed Mock Flywheel Mode (`plan.md §13`), the daemon's adapter
// contract tests (`plan.md §18.3`), and the tending evaluation harness
// (`plan.md §8.8`).
//
// For hp-xru this file is a placeholder so the workspace has something to
// type-check. The real fixture loader and corpus index land in Phase 0.

export const HOOPOE_FIXTURES_PACKAGE_NAME = "@hoopoe/fixtures";

export type FixtureKind =
  | "git_status"
  | "br_list"
  | "bv_triage"
  | "bv_plan"
  | "bv_insights"
  | "bv_diff"
  | "ntm_snapshot"
  | "agent_mail_dump"
  | "file_reservations"
  | "health_lizard"
  | "ru_sync"
  | "ru_status"
  | "ru_list"
  | "ru_prune";
