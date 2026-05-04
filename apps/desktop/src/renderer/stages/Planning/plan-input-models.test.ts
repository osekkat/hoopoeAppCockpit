// hp-z5r — plan-input model picker logic tests.

import { expect, test } from "bun:test";
import {
  EMPTY_PLAN_INPUT_DRAFT,
  MAX_COMPETING_CANDIDATES,
  PLAN_DEFAULT_COMPETITION,
  PLAN_DEFAULT_PRIMARY,
  PLAN_MODELS,
  PLAN_PROVIDER_IDS,
  PlanInputError,
  compileDraft,
  emptyPlanModelAvailability,
  findPlanModel,
  selectHoopoeDefaults,
  validatePlanModelSelection,
  type PlanModelAvailability,
  type PlanProviderId,
} from "./plan-input-models.ts";

function fullyAvailable(): PlanModelAvailability {
  return Object.fromEntries(PLAN_PROVIDER_IDS.map((id) => [id, true])) as PlanModelAvailability;
}

function only(...ids: readonly PlanProviderId[]): PlanModelAvailability {
  const map = emptyPlanModelAvailability() as Record<PlanProviderId, boolean>;
  for (const id of ids) map[id] = true;
  return map;
}

test("PLAN_MODELS lists all five providers in canonical order", () => {
  expect(PLAN_MODELS.map((m) => m.id)).toEqual([
    "chatgpt_pro_browser",
    "claude_max",
    "gpt_pro",
    "gemini_ultra",
    "grok_heavy",
  ]);
});

test("emptyPlanModelAvailability: every provider starts unavailable", () => {
  const avail = emptyPlanModelAvailability();
  for (const id of PLAN_PROVIDER_IDS) {
    expect(avail[id]).toBe(false);
  }
});

test("findPlanModel: throws on unknown id", () => {
  expect(() => findPlanModel("garbage" as PlanProviderId)).toThrow(/unknown_model/);
});

test("findPlanModel: returns the canonical option", () => {
  expect(findPlanModel("claude_max").label).toBe("Claude Opus (Claude Max)");
});

test("selectHoopoeDefaults: full availability picks the canonical 4-way lineup", () => {
  const lineup = selectHoopoeDefaults(fullyAvailable());
  expect(lineup.primary).toBe(PLAN_DEFAULT_PRIMARY);
  expect(lineup.competition).toEqual(PLAN_DEFAULT_COMPETITION);
});

test("selectHoopoeDefaults: missing primary falls back to first available", () => {
  const lineup = selectHoopoeDefaults(only("claude_max", "gemini_ultra"));
  expect(lineup.primary).toBe("claude_max");
  expect(lineup.competition).toEqual(["gemini_ultra"]);
});

test("selectHoopoeDefaults: never includes primary in competition", () => {
  const lineup = selectHoopoeDefaults(fullyAvailable());
  expect(lineup.competition).not.toContain(lineup.primary);
});

test("selectHoopoeDefaults: caps competition at MAX_COMPETING_CANDIDATES", () => {
  const lineup = selectHoopoeDefaults(fullyAvailable());
  expect(lineup.competition.length).toBe(MAX_COMPETING_CANDIDATES);
});

test("selectHoopoeDefaults: throws on no subscriptions", () => {
  expect(() => selectHoopoeDefaults(emptyPlanModelAvailability())).toThrow(/no_subscription/);
});

test("validatePlanModelSelection: valid lineup returns []", () => {
  const issues = validatePlanModelSelection(
    { primary: "chatgpt_pro_browser", competition: ["claude_max", "gemini_ultra"] },
    fullyAvailable(),
  );
  expect(issues).toEqual([]);
});

test("validatePlanModelSelection: catches unavailable primary", () => {
  const issues = validatePlanModelSelection(
    { primary: "grok_heavy", competition: [] },
    only("claude_max"),
  );
  expect(issues.map((i) => i.code)).toContain("primary_unavailable");
});

test("validatePlanModelSelection: catches unavailable candidate", () => {
  const issues = validatePlanModelSelection(
    { primary: "claude_max", competition: ["grok_heavy"] },
    only("claude_max"),
  );
  expect(issues.map((i) => i.code)).toContain("candidate_unavailable");
});

test("validatePlanModelSelection: catches candidate-is-primary", () => {
  const issues = validatePlanModelSelection(
    { primary: "claude_max", competition: ["claude_max"] },
    fullyAvailable(),
  );
  expect(issues.map((i) => i.code)).toContain("candidate_is_primary");
});

test("validatePlanModelSelection: catches duplicate candidate", () => {
  const issues = validatePlanModelSelection(
    { primary: "chatgpt_pro_browser", competition: ["claude_max", "claude_max"] },
    fullyAvailable(),
  );
  expect(issues.map((i) => i.code)).toContain("duplicate_candidate");
});

test("validatePlanModelSelection: catches > MAX_COMPETING_CANDIDATES", () => {
  const issues = validatePlanModelSelection(
    {
      primary: "chatgpt_pro_browser",
      competition: ["claude_max", "gpt_pro", "gemini_ultra", "grok_heavy"],
    },
    fullyAvailable(),
  );
  expect(issues.map((i) => i.code)).toContain("too_many_candidates");
});

test("compileDraft: empty description fails fast", () => {
  const result = compileDraft({
    draft: { ...EMPTY_PLAN_INPUT_DRAFT, description: "   " },
    availability: fullyAvailable(),
    projectHasCodebase: false,
  });
  expect(result.ok).toBe(false);
  if (!result.ok) {
    expect(result.issues[0]?.code).toBe("empty_description");
  }
});

test("compileDraft: hoopoeChoosesModels overrides selection with defaults", () => {
  const draft = {
    ...EMPTY_PLAN_INPUT_DRAFT,
    description: "build a thing",
    hoopoeChoosesModels: true,
    selection: { primary: "grok_heavy" as PlanProviderId, competition: [] },
  };
  const result = compileDraft({
    draft,
    availability: fullyAvailable(),
    projectHasCodebase: false,
  });
  expect(result.ok).toBe(true);
  if (result.ok) {
    expect(result.request.primary).toBe(PLAN_DEFAULT_PRIMARY);
    expect(result.request.competition).toEqual(PLAN_DEFAULT_COMPETITION);
  }
});

test("compileDraft: surfaces no_subscription when nothing available", () => {
  const result = compileDraft({
    draft: {
      ...EMPTY_PLAN_INPUT_DRAFT,
      description: "build a thing",
      hoopoeChoosesModels: true,
    },
    availability: emptyPlanModelAvailability(),
    projectHasCodebase: false,
  });
  expect(result.ok).toBe(false);
  if (!result.ok) {
    expect(result.issues[0]?.code).toBe("no_subscription");
  }
});

test("compileDraft: respects user selection when hoopoeChoosesModels is false", () => {
  const draft = {
    ...EMPTY_PLAN_INPUT_DRAFT,
    description: "build a thing",
    hoopoeChoosesModels: false,
    selection: {
      primary: "claude_max" as PlanProviderId,
      competition: ["gemini_ultra"] as PlanProviderId[],
    },
  };
  const result = compileDraft({
    draft,
    availability: fullyAvailable(),
    projectHasCodebase: false,
  });
  expect(result.ok).toBe(true);
  if (result.ok) {
    expect(result.request.primary).toBe("claude_max");
    expect(result.request.competition).toEqual(["gemini_ultra"]);
  }
});

test("compileDraft: projectHasCodebase flag flows through", () => {
  const draft = { ...EMPTY_PLAN_INPUT_DRAFT, description: "build" };
  const withCodebase = compileDraft({ draft, availability: fullyAvailable(), projectHasCodebase: true });
  const withoutCodebase = compileDraft({ draft, availability: fullyAvailable(), projectHasCodebase: false });
  expect(withCodebase.ok && withCodebase.request.attachExistingCodebase).toBe(true);
  expect(withoutCodebase.ok && withoutCodebase.request.attachExistingCodebase).toBe(false);
});

test("compileDraft: kickoffMode flows through", () => {
  const result = compileDraft({
    draft: { ...EMPTY_PLAN_INPUT_DRAFT, description: "x", kickoffMode: "first_shot" },
    availability: fullyAvailable(),
    projectHasCodebase: false,
  });
  expect(result.ok && result.request.kickoffMode).toBe("first_shot");
});

test("PlanInputError: stable name + carries code", () => {
  const err = new PlanInputError("invalid", "test");
  expect(err.name).toBe("PlanInputError");
  expect(err.code).toBe("invalid");
});
