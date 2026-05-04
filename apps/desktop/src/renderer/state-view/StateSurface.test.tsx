import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { RotateCcw } from "lucide-react";
import { StateSurface } from "./StateSurface.tsx";

test("StateSurface renders loading state with polite live region and skeleton", () => {
  const markup = renderToStaticMarkup(
    <StateSurface
      variant="loading"
      title="Loading plans"
      description="Fetching plan artifacts and history for this project."
      testId="loading-state"
    />,
  );

  expect(markup).toContain('data-variant="loading"');
  expect(markup).toContain('aria-live="polite"');
  expect(markup).toContain("hh-state-surface-skeleton");
  expect(markup).toContain("Loading plans");
});

test("StateSurface renders errors as assertive alerts", () => {
  const markup = renderToStaticMarkup(
    <StateSurface
      variant="error"
      title="Daemon data unavailable"
      description="Reconnect the daemon or open Diagnostics."
    />,
  );

  expect(markup).toContain('role="alert"');
  expect(markup).toContain('aria-live="assertive"');
  expect(markup).toContain("Daemon data unavailable");
});

test("StateSurface renders optional actions without requiring handlers", () => {
  const markup = renderToStaticMarkup(
    <StateSurface
      variant="empty"
      title="No matching events"
      description="Clear filters to restore the activity timeline."
      details={["Agent Mail events will appear here.", "Filters can hide older automation."]}
      actions={[{ label: "Retry", icon: <RotateCcw size={12} />, variant: "primary" }]}
    />,
  );

  expect(markup).toContain("No matching events");
  expect(markup).toContain('data-action-variant="primary"');
  expect(markup).toContain("Retry");
  expect(markup).toContain("Agent Mail events will appear here.");
});

test("StateSurface renders href actions as links", () => {
  const markup = renderToStaticMarkup(
    <StateSurface
      variant="degraded"
      title="Daemon disconnected"
      description="Open Diagnostics to repair the tunnel."
      actions={[
        {
          label: "Open Diagnostics",
          href: "/local-demo/diag",
          testId: "open-diagnostics",
        },
      ]}
    />,
  );

  expect(markup).toContain('href="/local-demo/diag"');
  expect(markup).toContain('data-testid="open-diagnostics"');
});
