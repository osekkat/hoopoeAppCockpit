// Storybook stories for the Planning stage.
//
// Storybook runner is wired in `packages/design-system` and currently re-exports
// shared stories from each stage's `src/`. These stories describe the canonical
// states for the Planning workspace (locked plan, draft plan, comparative
// matrix, history timeline) so visual regressions surface during Round-0 review.
//
// The stories deliberately export plain metadata + builder functions instead of
// React story components so they remain consumable from both the Storybook
// runner (when it lands) and headless visual regression tools.

import type { PlanArtifact, PlanHistoryEntry, PlanSummary } from "../../data/plan-data.ts";

export const PLANNING_STAGE_STORY_TITLE = "Stages/Planning";

export const planningStageLockedFixture = {
  storyName: "Locked plan with full artifact set",
  plan: {
    planId: "plan-001",
    title: "Hoopoe v1 — cockpit MVP",
    version: 7,
    lockState: "locked",
    lockedAt: "2026-05-03T22:14:00.000Z",
    branch: "main",
    active: true,
  } satisfies PlanSummary,
  artifactCount: 10,
  candidateCount: 3,
  historyCount: 9,
  expectedReadOnly: true,
};

export const planningStageDraftFixture = {
  storyName: "Draft plan with partial artifacts",
  plan: {
    planId: "plan-002",
    title: "Onboarding wizard polish",
    version: 2,
    lockState: "draft",
    lockedAt: null,
    branch: "main",
    active: false,
  } satisfies PlanSummary,
  artifactCount: 3,
  candidateCount: 1,
  historyCount: 2,
  expectedReadOnly: false,
};

export const planningStageComparativeMatrixFixture = {
  storyName: "Comparative matrix with synced scroll",
  candidates: [
    {
      path: "candidates/claude.md",
      kind: "candidate",
      status: "completed",
      label: "Claude (Opus 4.7)",
      content: "# Claude\n\nLong content for scroll testing...\n".repeat(20),
      model: "claude-opus-4-7",
      harness: "claude-code",
    },
    {
      path: "candidates/gpt.md",
      kind: "candidate",
      status: "completed",
      label: "GPT-5 Pro (Codex)",
      content: "# GPT\n\nLong content for scroll testing...\n".repeat(20),
      model: "gpt-5-pro",
      harness: "codex-cli",
    },
    {
      path: "candidates/gemini.md",
      kind: "candidate",
      status: "completed",
      label: "Gemini 3 Pro",
      content: "# Gemini\n\nLong content for scroll testing...\n".repeat(20),
      model: "gemini-3-pro",
      harness: "gemini-cli",
    },
  ] satisfies readonly PlanArtifact[],
  syncedScrollDefault: true,
  highlightDiffDefault: true,
};

export const planningStageHistoryFixture = {
  storyName: "History timeline (newest first)",
  history: [
    { ts: "2026-05-01T10:00:00.000Z", kind: "plan_created", actor: "user" },
    {
      ts: "2026-05-01T10:14:00.000Z",
      kind: "candidate_generated",
      actor: "claude-opus-4-7",
      latencyMs: 42180,
    },
    {
      ts: "2026-05-03T22:14:00.000Z",
      kind: "plan_locked",
      actor: "user",
      version: 7,
    },
  ] satisfies readonly PlanHistoryEntry[],
  expectedSortDirection: "newest-first" as const,
};

export const planningStageEmptyFixture = {
  storyName: "Empty state when no plans exist",
  plans: [] satisfies readonly PlanSummary[],
  expectedMessage: "No plans yet.",
};
