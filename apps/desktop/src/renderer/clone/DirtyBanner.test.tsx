// hp-70yz — DirtyBanner render tests.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import { DirtyBanner, DirtyBannerView, describeDirty, useDirtyStore } from "./index.ts";

beforeEach(() => {
  useDirtyStore.getState().clear();
});

afterEach(() => {
  useDirtyStore.getState().clear();
});

test("DirtyBanner: returns null when projectId is null", () => {
  const html = renderToStaticMarkup(<DirtyBanner projectId={null} />);
  expect(html).toBe("");
});

test("DirtyBanner: returns null when no entry has been recorded", () => {
  const html = renderToStaticMarkup(<DirtyBanner projectId="p1" />);
  expect(html).toBe("");
});

test("DirtyBanner: returns null when entry exists but is clean", () => {
  useDirtyStore.getState().recordEvent("p1", {
    dirty: false,
    modifiedCount: 0,
    untrackedCount: 0,
    aheadCount: 0,
    behindCount: 0,
  });
  const html = renderToStaticMarkup(<DirtyBanner projectId="p1" />);
  expect(html).toBe("");
});

test("DirtyBannerView: renders when project is dirty + surfaces counts + actions", () => {
  // Use the presentational form so we can pass dirtyState directly —
  // Zustand's hook returns the SSR snapshot in renderToStaticMarkup
  // which doesn't see post-mount state mutations.
  const html = renderToStaticMarkup(
    <DirtyBannerView
      cloneRepoPath="/Users/me/repo"
      dirtyState={{
        dirty: true,
        modifiedCount: 3,
        untrackedCount: 2,
        aheadCount: 1,
        behindCount: 0,
      }}
      projectId="p1"
    />,
  );
  expect(html).toContain("data-testid=\"dirty-banner\"");
  expect(html).toContain("Local clone has unsaved changes");
  expect(html).toContain("Hoopoe ignores local edits");
  expect(html).toContain("3 modified");
  expect(html).toContain("2 untracked");
  expect(html).toContain("ahead 1");
  // Both action buttons present.
  expect(html).toContain("data-testid=\"dirty-banner-discard\"");
  expect(html).toContain("data-testid=\"dirty-banner-reveal\"");
});

test("DirtyBannerView: hides Reveal button when no cloneRepoPath supplied", () => {
  const html = renderToStaticMarkup(
    <DirtyBannerView
      dirtyState={{
        dirty: true,
        modifiedCount: 1,
        untrackedCount: 0,
        aheadCount: 0,
        behindCount: 0,
      }}
      projectId="p1"
    />,
  );
  expect(html).toContain("data-testid=\"dirty-banner\"");
  expect(html).toContain("data-testid=\"dirty-banner-discard\"");
  expect(html).not.toContain("data-testid=\"dirty-banner-reveal\"");
});

test("DirtyBannerView: confirmation dialog is NOT rendered until Discard is clicked", () => {
  const html = renderToStaticMarkup(
    <DirtyBannerView
      dirtyState={{
        dirty: true,
        modifiedCount: 1,
        untrackedCount: 0,
        aheadCount: 0,
        behindCount: 0,
      }}
      projectId="p1"
    />,
  );
  expect(html).not.toContain("data-testid=\"dirty-banner-confirm\"");
});

test("DirtyBannerView: returns null when projectId is null even with dirty state", () => {
  const html = renderToStaticMarkup(
    <DirtyBannerView
      dirtyState={{
        dirty: true,
        modifiedCount: 1,
        untrackedCount: 0,
        aheadCount: 0,
        behindCount: 0,
      }}
      projectId={null}
    />,
  );
  expect(html).toBe("");
});

test("DirtyBanner (wired): pulls from store; null when no entry", () => {
  // The wired form delegates to DirtyBannerView. With no recordEvent,
  // the store returns null → component renders nothing.
  const html = renderToStaticMarkup(<DirtyBanner projectId="p1" />);
  expect(html).toBe("");
});

test("describeDirty: composes counts in the display order", () => {
  expect(
    describeDirty({
      dirty: true,
      modifiedCount: 3,
      untrackedCount: 2,
      aheadCount: 1,
      behindCount: 4,
    }),
  ).toBe("3 modified · 2 untracked · ahead 1 · behind 4");
});

test("describeDirty: omits zero buckets", () => {
  expect(
    describeDirty({
      dirty: true,
      modifiedCount: 0,
      untrackedCount: 5,
      aheadCount: 0,
      behindCount: 0,
    }),
  ).toBe("5 untracked");
});

test("describeDirty: returns empty string when nothing dirty (defensive)", () => {
  expect(
    describeDirty({
      dirty: false,
      modifiedCount: 0,
      untrackedCount: 0,
      aheadCount: 0,
      behindCount: 0,
    }),
  ).toBe("");
});
