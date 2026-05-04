// hp-sgy — ErrorBoundary tests.
//
// `bun:test` runs without a DOM and without react-test-renderer (per
// the workspace's auto-memory `bun_test_no_dom` feedback). The tests
// mix two strategies that match the rest of the renderer test suite:
//
//   1. `renderToStaticMarkup` for happy-path / fallback-with-injected-state
//      snapshots — the same pattern used by every other renderer test.
//   2. Direct lifecycle-method invocation on a manually-constructed
//      class instance for the throw-then-catch pathway. React's class
//      contract is just `getDerivedStateFromError` (static) +
//      `componentDidCatch` + `render` — none of those need a real
//      reconciler to verify behaviorally.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { ErrorBoundary } from "./ErrorBoundary.tsx";
import { createErrorBus } from "./errorBus.ts";

// Subclass exposes a way to seed the error state without going through
// React reconciliation (which renderToStaticMarkup does not implement
// for thrown render errors — it just rethrows).
class SeededErrorBoundary extends ErrorBoundary {
  constructor(props: ConstructorParameters<typeof ErrorBoundary>[0] & {
    readonly seedError?: Error;
    readonly seedComponentStack?: string | null;
  }) {
    super(props);
    if (props.seedError) {
      this.state = {
        error: props.seedError,
        componentStack: props.seedComponentStack ?? null,
      };
    }
  }
}

beforeEach(() => {
  // Tests don't actually load the DOM, but the boundary's reload path
  // touches `window.location.reload`. Provide a minimal stub so the
  // production-mode reload-button test doesn't crash before the seam
  // (`onReload` prop) gets a chance to take over.
  (globalThis as { window?: unknown }).window = {
    location: { reload: () => {} },
  };
});

afterEach(() => {
  delete (globalThis as { window?: unknown }).window;
});

test("renderToStaticMarkup: passes children through when no error", () => {
  const html = renderToStaticMarkup(
    <ErrorBoundary>
      <span data-testid="safe">hello world</span>
    </ErrorBoundary>,
  );
  expect(html).toContain("hello world");
  expect(html).not.toContain("error-boundary-root");
});

test("getDerivedStateFromError: surfaces the error in state", () => {
  const next = ErrorBoundary.getDerivedStateFromError(new Error("boom"));
  expect(next.error).toBeInstanceOf(Error);
  expect(next.error?.message).toBe("boom");
});

test("renderToStaticMarkup: dev fallback shows error name + stack + reset button", () => {
  const err = new Error("boom");
  err.stack = "Error: boom\n    at fixtureSite (file.tsx:1:1)";
  const html = renderToStaticMarkup(
    <SeededErrorBoundary forceDev seedError={err} seedComponentStack="\n    in Boom\n    in Root">
      <span>hidden</span>
    </SeededErrorBoundary>,
  );
  expect(html).toContain('data-testid="error-boundary-root"');
  expect(html).toContain('data-testid="error-boundary-detail"');
  expect(html).toContain('data-testid="error-boundary-reset"');
  expect(html).toContain('data-testid="error-boundary-reload"');
  expect(html).toContain("Error");
  expect(html).toContain("boom");
  expect(html).toContain("fixtureSite");
  expect(html).toContain("in Boom");
});

test("renderToStaticMarkup: prod fallback omits stack details and the reset button", () => {
  const err = new Error("boom");
  const html = renderToStaticMarkup(
    <SeededErrorBoundary forceDev={false} seedError={err}>
      <span>hidden</span>
    </SeededErrorBoundary>,
  );
  expect(html).toContain('data-testid="error-boundary-root"');
  expect(html).not.toContain('data-testid="error-boundary-detail"');
  expect(html).not.toContain('data-testid="error-boundary-reset"');
  expect(html).toContain('data-testid="error-boundary-reload"');
  // The detail line that would carry the stack must not leak in prod.
  expect(html).not.toContain("boom");
});

test("componentDidCatch: publishes a renderer.boundary error to the supplied bus", () => {
  const bus = createErrorBus();
  // React's contract: instantiate with props, then call lifecycle
  // methods directly. No reconciler needed for behavioral assertions.
  const boundary = new ErrorBoundary({
    bus,
    children: null,
  });
  boundary.componentDidCatch(new Error("boom"), { componentStack: "\n    in X" });
  const snapshot = bus.getSnapshot();
  expect(snapshot.errors.length).toBe(1);
  const published = snapshot.errors[0]!;
  expect(published.source).toBe("renderer.boundary");
  expect(published.severity).toBe("blocking");
  expect(published.envelope.type).toBe("https://hoopoe.io/problems/renderer.crash");
  expect(published.envelope.detail).toBe("boom");
  expect(published.envelope.surface).toBe("blocking_modal");
});

test("componentDidCatch: a throwing bus does not propagate (boundary keeps the user safe)", () => {
  const angryBus = {
    publish: () => {
      throw new Error("bus down");
    },
    dismiss: () => {},
    dismissAll: () => {},
    subscribe: () => () => {},
    getSnapshot: () => ({ errors: [] }),
    reset: () => {},
  };
  const boundary = new ErrorBoundary({
    bus: angryBus,
    children: null,
  });
  // Must not rethrow.
  boundary.componentDidCatch(new Error("boom"), { componentStack: null });
});

test("reload action: clicking the rendered button invokes the onReload seam", () => {
  let calls = 0;
  const boundary = new SeededErrorBoundary({
    forceDev: true,
    onReload: () => calls++,
    seedError: new Error("boom"),
    children: null,
  });
  const tree = boundary.render() as {
    readonly props: { readonly children: { readonly props: { readonly children: ReadonlyArray<unknown> } } };
  };
  // root → card → [title, message, detail?, actions]
  const card = tree.props.children;
  const actions = (card.props.children as ReadonlyArray<unknown>).find(
    (child): child is { readonly props: { readonly children: ReadonlyArray<{ readonly props: { readonly "data-testid"?: string; readonly onClick?: () => void } }> } } =>
      Boolean(child) && typeof child === "object" && "props" in (child as object) &&
      (child as { props: { className?: string } }).props.className === "hh-error-boundary-actions",
  );
  if (!actions) throw new Error("actions container not found in fallback tree");
  const reload = actions.props.children.find(
    (button) => button.props["data-testid"] === "error-boundary-reload",
  );
  if (!reload) throw new Error("reload button not found in fallback tree");
  reload.props.onClick?.();
  expect(calls).toBe(1);
});
