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
      actions={[{ label: "Retry", icon: <RotateCcw size={12} />, variant: "primary" }]}
    />,
  );

  expect(markup).toContain("No matching events");
  expect(markup).toContain('data-action-variant="primary"');
  expect(markup).toContain("Retry");
});
