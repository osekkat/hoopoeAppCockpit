import { describe, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { createErrorBus } from "./errorBus.ts";
import { BannerStack } from "./surfaces/Banner.tsx";
import { BlockingModal } from "./surfaces/BlockingModal.tsx";
import { InlinePill } from "./surfaces/InlinePill.tsx";
import { ToastStack } from "./surfaces/Toast.tsx";
import type { ProblemEnvelope, PublishedError } from "./types.ts";

function envelope(overrides?: Partial<ProblemEnvelope>): ProblemEnvelope {
  return {
    type: "https://hoopoe.io/problems/test",
    title: "Test problem",
    status: 422,
    surface: "toast",
    actionability: "manual",
    user_message: "Test user-facing message",
    ...overrides,
  };
}

function published(overrides?: Partial<PublishedError>): PublishedError {
  return {
    id: "err-1",
    source: "test-source",
    severity: "warning",
    surface: "toast",
    publishedAt: 0,
    coalescedCount: 1,
    envelope: envelope(),
    hints: { dismissible: true },
    ...overrides,
  };
}

describe("hp-8dym :: ToastStack", () => {
  test("renders one DOM toast per error and respects severity classnames + ARIA roles", () => {
    const bus = createErrorBus();
    const errors: readonly PublishedError[] = [
      published({ id: "err-1", severity: "info", surface: "toast" }),
      published({
        id: "err-2",
        severity: "error",
        surface: "toast",
        envelope: envelope({ status: 500 }),
      }),
    ];
    const markup = renderToStaticMarkup(
      <ToastStack bus={bus} errors={errors} reducedMotion={false} />,
    );

    expect(markup).toContain('data-testid="error-toast-stack"');
    expect(markup).toContain('data-testid="error-toast-err-1"');
    expect(markup).toContain('data-testid="error-toast-err-2"');
    expect(markup).toContain("hh-error-toast-info");
    expect(markup).toContain("hh-error-toast-error");
    expect(markup).toContain('role="status"');
    expect(markup).toContain('role="alert"');
  });

  test('renders the "+ N more" overflow when errors exceed MAX_VISIBLE_TOASTS', () => {
    const bus = createErrorBus();
    const errors: readonly PublishedError[] = Array.from({ length: 7 }, (_, index) =>
      published({
        id: `err-${index + 1}`,
        envelope: envelope({ type: `https://hoopoe.io/problems/x-${index}` }),
      }),
    );
    const markup = renderToStaticMarkup(
      <ToastStack bus={bus} errors={errors} reducedMotion={false} />,
    );

    expect(markup).toContain('data-testid="error-toast-overflow"');
    expect(markup).toContain("+ 2 more");
  });

  test("renders coalesced count when an error has been deduped", () => {
    const bus = createErrorBus();
    const errors: readonly PublishedError[] = [
      published({ coalescedCount: 4 }),
    ];
    const markup = renderToStaticMarkup(
      <ToastStack bus={bus} errors={errors} reducedMotion={false} />,
    );
    expect(markup).toContain("×4");
  });

  test("reduced-motion class flag disables animation styling", () => {
    const bus = createErrorBus();
    const markup = renderToStaticMarkup(
      <ToastStack bus={bus} errors={[]} reducedMotion={true} />,
    );
    expect(markup).toContain("hh-error-toast-stack-reduced-motion");
  });
});

describe("hp-8dym :: BannerStack", () => {
  test("renders nothing when no errors are passed", () => {
    const bus = createErrorBus();
    const markup = renderToStaticMarkup(<BannerStack bus={bus} errors={[]} />);
    expect(markup).toBe("");
  });

  test("each banner has region role + aria-label combining severity + title", () => {
    const bus = createErrorBus();
    const errors: readonly PublishedError[] = [
      published({
        id: "err-1",
        severity: "warning",
        surface: "banner",
        envelope: envelope({ surface: "banner", title: "VPS daemon version mismatch" }),
      }),
    ];
    const markup = renderToStaticMarkup(<BannerStack bus={bus} errors={errors} />);
    expect(markup).toContain('role="region"');
    expect(markup).toContain('aria-label="warning — VPS daemon version mismatch"');
  });

  test('renders "Show all banners" overflow for >MAX_VISIBLE_BANNERS', () => {
    const bus = createErrorBus();
    const errors: readonly PublishedError[] = Array.from({ length: 5 }, (_, index) =>
      published({
        id: `err-${index + 1}`,
        surface: "banner",
        envelope: envelope({ surface: "banner", type: `https://hoopoe.io/problems/b-${index}` }),
      }),
    );
    const markup = renderToStaticMarkup(<BannerStack bus={bus} errors={errors} />);
    expect(markup).toContain('data-testid="error-banner-overflow"');
    expect(markup).toContain("Show all banners (2 more)");
  });
});

describe("hp-8dym :: InlinePill", () => {
  test("renders children when no matching capability error is in the bus", () => {
    const markup = renderToStaticMarkup(
      <InlinePill errors={[]} capabilityId="caam">
        <button type="button">Launch swarm</button>
      </InlinePill>,
    );
    expect(markup).toContain("Launch swarm");
    expect(markup).not.toContain("hh-error-inline-pill");
  });

  test("renders the pill (replacing children) when a matching inline_pill error is active", () => {
    const errors: readonly PublishedError[] = [
      published({
        id: "err-1",
        surface: "inline_pill",
        envelope: envelope({ surface: "inline_pill", title: "CAAM unavailable" }),
        hints: { capabilityId: "caam" },
      }),
    ];
    const markup = renderToStaticMarkup(
      <InlinePill errors={errors} capabilityId="caam">
        <button type="button">Launch swarm</button>
      </InlinePill>,
    );
    expect(markup).toContain('data-testid="error-inline-pill-caam"');
    expect(markup).toContain("CAAM unavailable");
    expect(markup).not.toContain("Launch swarm");
  });

  test("ignores capability errors that don't match the pill's capabilityId", () => {
    const errors: readonly PublishedError[] = [
      published({
        id: "err-1",
        surface: "inline_pill",
        envelope: envelope({ surface: "inline_pill" }),
        hints: { capabilityId: "ntm" },
      }),
    ];
    const markup = renderToStaticMarkup(
      <InlinePill errors={errors} capabilityId="caam">
        <button type="button">Launch swarm</button>
      </InlinePill>,
    );
    expect(markup).toContain("Launch swarm");
    expect(markup).not.toContain("hh-error-inline-pill");
  });
});

describe("hp-8dym :: BlockingModal", () => {
  test("renders nothing when no modal errors are present", () => {
    const bus = createErrorBus();
    const markup = renderToStaticMarkup(<BlockingModal bus={bus} errors={[]} />);
    expect(markup).toBe("");
  });

  test("renders dialog with role + aria-modal + labelledby/describedby", () => {
    const bus = createErrorBus();
    const errors: readonly PublishedError[] = [
      published({
        id: "err-9",
        severity: "blocking",
        surface: "blocking_modal",
        envelope: envelope({ surface: "blocking_modal", title: "Plan locked" }),
        hints: { dismissible: false },
      }),
    ];
    const markup = renderToStaticMarkup(<BlockingModal bus={bus} errors={errors} />);

    expect(markup).toContain('role="dialog"');
    expect(markup).toContain('aria-modal="true"');
    expect(markup).toContain('aria-labelledby="error-modal-title-err-9"');
    expect(markup).toContain('aria-describedby="error-modal-detail-err-9"');
    expect(markup).toContain("Plan locked");
  });

  test("hides Cancel button when error is non-dismissible", () => {
    const bus = createErrorBus();
    const errors: readonly PublishedError[] = [
      published({
        id: "err-9",
        severity: "blocking",
        surface: "blocking_modal",
        envelope: envelope({ surface: "blocking_modal" }),
        hints: { dismissible: false },
      }),
    ];
    const markup = renderToStaticMarkup(<BlockingModal bus={bus} errors={errors} />);
    expect(markup).not.toContain('data-testid="error-modal-cancel-err-9"');
    expect(markup).toContain('data-testid="error-modal-primary-err-9"');
  });
});
