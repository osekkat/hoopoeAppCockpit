// hp-z5r — PlanInputChatBox render tests.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { PlanInputChatBox } from "./PlanInputChatBox.tsx";
import {
  EMPTY_PLAN_INPUT_DRAFT,
  PLAN_PROVIDER_IDS,
  emptyPlanModelAvailability,
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

test("PlanInputChatBox: renders the textarea + all 5 model rows", () => {
  const html = renderToStaticMarkup(<PlanInputChatBox availability={fullyAvailable()} />);
  expect(html).toContain("data-testid=\"plan-input\"");
  expect(html).toContain("data-testid=\"plan-input-textarea\"");
  for (const id of PLAN_PROVIDER_IDS) {
    expect(html).toContain(`data-testid="plan-input-primary-${id}"`);
    expect(html).toContain(`data-testid="plan-input-competitor-${id}"`);
  }
});

test("PlanInputChatBox: surfaces the no-subscription banner when nothing is available", () => {
  const html = renderToStaticMarkup(<PlanInputChatBox />);
  expect(html).toContain("data-testid=\"plan-input-no-subscription\"");
  expect(html).toContain("No qualifying subscription configured");
  // Submit button is disabled in this state.
  expect(html).toMatch(/data-testid="plan-input-submit"[^>]*disabled=""/);
});

test("PlanInputChatBox: hides the no-subscription banner when at least one provider is available", () => {
  const html = renderToStaticMarkup(<PlanInputChatBox availability={only("claude_max")} />);
  expect(html).not.toContain("data-testid=\"plan-input-no-subscription\"");
});

test("PlanInputChatBox: 'Let Hoopoe choose' is on by default", () => {
  const html = renderToStaticMarkup(<PlanInputChatBox availability={fullyAvailable()} />);
  expect(html).toContain("data-testid=\"plan-input-hoopoe-choose\"");
  // The toggle's checkbox is checked by default — easier to grep around the label.
  const togglePos = html.indexOf("data-testid=\"plan-input-hoopoe-choose\"");
  const sliceAround = html.slice(togglePos, togglePos + 500);
  expect(sliceAround).toContain("checked");
});

test("PlanInputChatBox: kickoff toggle defaults to clarify", () => {
  const html = renderToStaticMarkup(<PlanInputChatBox availability={fullyAvailable()} />);
  // The label's data-selected attribute renders BEFORE the data-testid
  // — match on the full label tag for clarify (selected) vs first_shot
  // (not selected).
  expect(html).toMatch(/data-selected="true"[^>]*data-testid="plan-input-kickoff-clarify"/);
  expect(html).toMatch(/data-selected="false"[^>]*data-testid="plan-input-kickoff-first_shot"/);
});

test("PlanInputChatBox: codebase banner only renders when projectHasCodebase=true", () => {
  const without = renderToStaticMarkup(<PlanInputChatBox availability={fullyAvailable()} />);
  expect(without).not.toContain("data-testid=\"plan-input-codebase\"");
  const withCodebase = renderToStaticMarkup(
    <PlanInputChatBox availability={fullyAvailable()} projectHasCodebase />,
  );
  expect(withCodebase).toContain("data-testid=\"plan-input-codebase\"");
  expect(withCodebase).toContain("existing-codebase context bundle");
});

test("PlanInputChatBox: summary footer reflects the canonical defaults when Hoopoe-chooses is on", () => {
  const html = renderToStaticMarkup(<PlanInputChatBox availability={fullyAvailable()} />);
  expect(html).toContain("data-testid=\"plan-input-summary\"");
  // Primary is the canonical default (ChatGPT Pro).
  expect(html).toContain("ChatGPT Pro (Oracle)");
  // All three competition defaults appear.
  expect(html).toContain("Claude Opus");
  expect(html).toContain("Gemini 3 Pro");
  expect(html).toContain("Grok Heavy");
});

test("PlanInputChatBox: respects initialDraft when supplied", () => {
  const html = renderToStaticMarkup(
    <PlanInputChatBox
      availability={fullyAvailable()}
      initialDraft={{
        ...EMPTY_PLAN_INPUT_DRAFT,
        description: "Build a tabbed inbox.",
        hoopoeChoosesModels: false,
        selection: { primary: "claude_max", competition: ["gemini_ultra"] },
        kickoffMode: "first_shot",
      }}
    />,
  );
  expect(html).toContain("Build a tabbed inbox.");
  // Summary footer reflects the override.
  expect(html).toContain("Claude Opus (Claude Max)");
  // Kickoff first_shot is selected (label's data-selected="true" sits
  // before the data-testid in the rendered tag).
  expect(html).toMatch(/data-selected="true"[^>]*data-testid="plan-input-kickoff-first_shot"/);
});

test("PlanInputChatBox: when only some providers are available, others are marked unavailable", () => {
  const html = renderToStaticMarkup(<PlanInputChatBox availability={only("claude_max")} />);
  // Claude row carries data-available="true" (it's the available one).
  expect(html).toMatch(/data-available="true"[^>]*data-testid="plan-input-primary-claude_max"/);
  // Grok row carries data-available="false" + the explainer.
  expect(html).toMatch(/data-available="false"[^>]*data-testid="plan-input-primary-grok_heavy"/);
  expect(html).toContain("subscription not configured");
});

test("PlanInputChatBox: textarea includes the description from initialDraft + placeholder text", () => {
  const html = renderToStaticMarkup(
    <PlanInputChatBox availability={fullyAvailable()} />,
  );
  expect(html).toContain("placeholder=\"Describe what you want to build");
});

test("PlanInputChatBox: form has stable test-id for top-level + submit button", () => {
  const html = renderToStaticMarkup(<PlanInputChatBox availability={fullyAvailable()} />);
  expect(html).toContain("data-testid=\"plan-input-form\"");
  expect(html).toContain("data-testid=\"plan-input-submit\"");
  expect(html).toContain("Generate plans");
});
