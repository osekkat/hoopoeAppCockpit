import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  FIRST_ONBOARDING_TOUR_STEP_ID,
  nextOnboardingTourStepId,
  previousOnboardingTourStepId,
} from "../store.ts";
import { ONBOARDING_TOUR_STEPS, OnboardingTour } from "./OnboardingTour.tsx";

test("onboarding tour model keeps a stable stage walkthrough order", () => {
  expect(FIRST_ONBOARDING_TOUR_STEP_ID).toBe("topbar");
  expect(ONBOARDING_TOUR_STEPS.map((step) => step.id)).toEqual([
    "topbar",
    "activity",
    "stages",
    "planning",
    "beads",
    "swarm",
    "hardening",
  ]);
  expect(nextOnboardingTourStepId("topbar")).toBe("activity");
  expect(previousOnboardingTourStepId("topbar")).toBe("topbar");
  expect(nextOnboardingTourStepId("hardening")).toBe("hardening");
});

test("OnboardingTour: renders the current step, progress, and skip control", () => {
  const html = renderToStaticMarkup(
    <OnboardingTour
      open={true}
      stepId="activity"
      onBack={() => undefined}
      onClose={() => undefined}
      onComplete={() => undefined}
      onNext={() => undefined}
      onSkip={() => undefined}
    />,
  );

  expect(html).toContain('data-testid="onboarding-tour"');
  expect(html).toContain('data-step="activity"');
  expect(html).toContain("Keep the audit trail open");
  expect(html).toContain('data-testid="onboarding-tour-skip"');
  expect(html).toContain("Step 2 of 7");
});

test("OnboardingTour: hidden state renders nothing", () => {
  const html = renderToStaticMarkup(
    <OnboardingTour
      open={false}
      stepId="topbar"
      onBack={() => undefined}
      onClose={() => undefined}
      onComplete={() => undefined}
      onNext={() => undefined}
      onSkip={() => undefined}
    />,
  );

  expect(html).toBe("");
});
