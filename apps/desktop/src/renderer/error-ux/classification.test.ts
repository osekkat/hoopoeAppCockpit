import { describe, expect, test } from "bun:test";
import {
  ariaLiveFor,
  defaultActionLabel,
  defaultAutoDismissMs,
  defaultDismissible,
  deriveSeverity,
  deriveSurface,
  isBannerSurface,
  isInlinePillSurface,
  isModalSurface,
  isToastSurface,
} from "./classification.ts";
import type { ProblemEnvelope } from "./types.ts";

function envelope(overrides?: Partial<ProblemEnvelope>): ProblemEnvelope {
  return {
    type: "https://hoopoe.io/problems/test",
    title: "Test problem",
    status: 400,
    surface: "banner",
    actionability: "manual",
    user_message: "Test message",
    ...overrides,
  };
}

describe("hp-8dym :: classification", () => {
  test("deriveSeverity buckets HTTP status codes correctly", () => {
    expect(deriveSeverity(envelope({ status: 200 }))).toBe("info");
    expect(deriveSeverity(envelope({ status: 304 }))).toBe("info");
    expect(deriveSeverity(envelope({ status: 400, actionability: "reload" }))).toBe("warning");
    expect(deriveSeverity(envelope({ status: 422 }))).toBe("error");
    expect(deriveSeverity(envelope({ status: 423 }))).toBe("error");
    expect(deriveSeverity(envelope({ status: 500 }))).toBe("error");
  });

  test("4xx with actionability=manual escalates from warning to error", () => {
    expect(deriveSeverity(envelope({ status: 404, actionability: "manual" }))).toBe("error");
    expect(deriveSeverity(envelope({ status: 404, actionability: "reload" }))).toBe("warning");
  });

  test("explicit severity overrides any derivation", () => {
    expect(deriveSeverity(envelope({ status: 200 }), "blocking")).toBe("blocking");
    expect(deriveSeverity(envelope({ status: 500 }), "info")).toBe("info");
  });

  test("deriveSurface defaults to envelope.surface unless overridden", () => {
    expect(deriveSurface(envelope({ surface: "toast" }))).toBe("toast");
    expect(deriveSurface(envelope({ surface: "toast" }), "blocking_modal")).toBe(
      "blocking_modal",
    );
  });

  test("surface predicates correctly identify each surface", () => {
    expect(isToastSurface("toast")).toBe(true);
    expect(isBannerSurface("banner")).toBe(true);
    expect(isInlinePillSurface("inline_pill")).toBe(true);
    expect(isModalSurface("blocking_modal")).toBe(true);
    expect(isToastSurface("banner")).toBe(false);
  });

  test("ariaLiveFor uses polite for info and assertive for warning+", () => {
    expect(ariaLiveFor("info")).toBe("polite");
    expect(ariaLiveFor("warning")).toBe("assertive");
    expect(ariaLiveFor("error")).toBe("assertive");
    expect(ariaLiveFor("critical")).toBe("assertive");
    expect(ariaLiveFor("blocking")).toBe("assertive");
  });

  test("defaultDismissible is false only for blocking severity on blocking_modal", () => {
    expect(defaultDismissible("blocking", "blocking_modal")).toBe(false);
    expect(defaultDismissible("blocking", "toast")).toBe(true);
    expect(defaultDismissible("error", "blocking_modal")).toBe(true);
    expect(defaultDismissible("warning", "banner")).toBe(true);
    expect(defaultDismissible("blocking", "blocking_modal", true)).toBe(true);
  });

  test("defaultAutoDismissMs returns null for non-toast surfaces", () => {
    expect(defaultAutoDismissMs("warning", "banner")).toBeNull();
    expect(defaultAutoDismissMs("error", "blocking_modal")).toBeNull();
    expect(defaultAutoDismissMs("info", "inline_pill")).toBeNull();
  });

  test("defaultAutoDismissMs scales with severity for toast surfaces", () => {
    expect(defaultAutoDismissMs("info", "toast")).toBe(5_000);
    expect(defaultAutoDismissMs("warning", "toast")).toBe(7_000);
    expect(defaultAutoDismissMs("error", "toast")).toBe(10_000);
    expect(defaultAutoDismissMs("critical", "toast")).toBe(15_000);
    expect(defaultAutoDismissMs("blocking", "toast")).toBeNull();
  });

  test("defaultActionLabel covers all hp-g6sp actionabilities", () => {
    expect(defaultActionLabel("reload")).toBe("Reload");
    expect(defaultActionLabel("re-pair")).toBe("Re-pair");
    expect(defaultActionLabel("edit-deps")).toBe("Edit dependencies");
    expect(defaultActionLabel("switch-account")).toBe("Switch account");
    expect(defaultActionLabel("open-docs")).toBe("Open docs");
    expect(defaultActionLabel("manual")).toBe("Acknowledge");
  });
});
