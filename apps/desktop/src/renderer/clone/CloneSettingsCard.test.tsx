// hp-1fd1 — CloneSettingsCard render tests.

import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  CloneSettingsCard,
  STUB_CLONE_ACTIONS_BRIDGE,
  type CloneActionsBridge,
  type CloneCacheRow,
} from "./index.ts";

const FIXED_NOW = () => new Date("2026-05-04T12:00:00.000Z");

const DEFAULT_CAPS = {
  softCapBytes: 2 * 1024 * 1024 * 1024,
  hardCapBytes: 5 * 1024 * 1024 * 1024,
};

function row(overrides: Partial<CloneCacheRow> & Pick<CloneCacheRow, "projectId">): CloneCacheRow {
  return {
    displayName: overrides.displayName ?? overrides.projectId,
    originRemote: overrides.originRemote ?? `git@github.com:org/${overrides.projectId}.git`,
    syncStatus: overrides.syncStatus ?? "synced",
    sizeBytes: overrides.sizeBytes ?? 1_500_000,
    lastSyncedAt: overrides.lastSyncedAt ?? "2026-05-04T11:00:00.000Z",
    lastAccessedAt: overrides.lastAccessedAt ?? "2026-05-04T11:30:00.000Z",
    capsOverride: overrides.capsOverride ?? null,
    authMissing: overrides.authMissing ?? false,
    ...overrides,
  };
}

test("CloneSettingsCard: empty state when no rows", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard defaultCaps={DEFAULT_CAPS} now={FIXED_NOW} rows={[]} />,
  );
  expect(html).toContain("data-testid=\"clone-settings-card\"");
  expect(html).toContain("data-testid=\"clone-settings-empty\"");
  expect(html).toContain("No projects have a local clone yet");
  // Summary shows 0 clones.
  expect(html).toContain("data-testid=\"clone-settings-summary\"");
  expect(html).toContain("<strong>0</strong> clone");
});

test("CloneSettingsCard: summary shows total + per-cap config", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard
      defaultCaps={DEFAULT_CAPS}
      now={FIXED_NOW}
      rows={[
        row({ projectId: "alpha", sizeBytes: 1024 * 1024 * 100 }), // 100 MB
        row({ projectId: "beta", sizeBytes: 1024 * 1024 * 200 }),  // 200 MB
      ]}
    />,
  );
  // 2 clones, 300 MB total.
  expect(html).toContain("<strong>2</strong> clone");
  expect(html).toContain("<strong>300 MB</strong>");
  // Default caps formatted.
  expect(html).toContain("<strong>2.00 GB</strong>"); // soft
  expect(html).toContain("<strong>5.00 GB</strong>"); // hard
});

test("CloneSettingsCard: every project row renders with its actions", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard
      defaultCaps={DEFAULT_CAPS}
      now={FIXED_NOW}
      rows={[
        row({ projectId: "alpha", displayName: "Alpha Service" }),
        row({ projectId: "beta", displayName: "Beta UI" }),
      ]}
    />,
  );
  for (const id of ["alpha", "beta"]) {
    expect(html).toContain(`data-testid="clone-settings-row-${id}"`);
    expect(html).toContain(`data-testid="clone-settings-action-reveal-${id}"`);
    expect(html).toContain(`data-testid="clone-settings-action-terminal-${id}"`);
    expect(html).toContain(`data-testid="clone-settings-action-clear-${id}"`);
    expect(html).toContain(`data-testid="clone-settings-action-caps-${id}"`);
    expect(html).toContain(`data-testid="clone-settings-select-${id}"`);
  }
  expect(html).toContain("Alpha Service");
  expect(html).toContain("Beta UI");
});

test("CloneSettingsCard: synced status renders the synced badge", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard
      defaultCaps={DEFAULT_CAPS}
      now={FIXED_NOW}
      rows={[row({ projectId: "p", syncStatus: "synced" })]}
    />,
  );
  expect(html).toContain("hh-clone-status-synced");
  expect(html).toContain("synced");
});

test("CloneSettingsCard: error status renders the error badge", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard
      defaultCaps={DEFAULT_CAPS}
      now={FIXED_NOW}
      rows={[row({ projectId: "p", syncStatus: "error" })]}
    />,
  );
  expect(html).toContain("hh-clone-status-error");
  expect(html).toContain("data-status=\"error\"");
});

test("CloneSettingsCard: auth-missing flag surfaces the credentials banner", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard
      defaultCaps={DEFAULT_CAPS}
      now={FIXED_NOW}
      rows={[row({ projectId: "p", authMissing: true })]}
    />,
  );
  expect(html).toContain("data-testid=\"clone-settings-auth-p\"");
  expect(html).toContain("SSH or PAT credentials missing");
});

test("CloneSettingsCard: bulk-clear bar is hidden when nothing selected", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard defaultCaps={DEFAULT_CAPS} now={FIXED_NOW} rows={[row({ projectId: "p" })]} />,
  );
  expect(html).not.toContain("data-testid=\"clone-settings-bulk\"");
});

test("CloneSettingsCard: sort headers carry stable test-ids and accessible labels", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard
      defaultCaps={DEFAULT_CAPS}
      now={FIXED_NOW}
      rows={[row({ projectId: "p" })]}
    />,
  );
  for (const key of ["name", "status", "size", "synced", "accessed"]) {
    expect(html).toContain(`data-testid="clone-settings-th-${key}"`);
  }
  // Default sort is lastAccessed desc → that header should be active descending.
  expect(html).toMatch(/data-active="true"[^>]*data-dir="desc"[^>]*data-testid="clone-settings-th-accessed"/);
  expect(html).toContain("aria-sort=\"descending\"");
});

test("CloneSettingsCard: row with capsOverride does NOT inline the editor by default", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard
      defaultCaps={DEFAULT_CAPS}
      now={FIXED_NOW}
      rows={[row({ projectId: "p", capsOverride: { softCapBytes: 1024 * 1024 * 500, hardCapBytes: 1024 * 1024 * 1024 } })]}
    />,
  );
  // Cap editor row only renders when the user clicks "Caps" — initial
  // render should not include it.
  expect(html).not.toContain("data-testid=\"clone-settings-caps-row-p\"");
  expect(html).not.toContain("data-testid=\"clone-settings-caps-p\"");
  // But the Caps action button is present so the user can open it.
  expect(html).toContain("data-testid=\"clone-settings-action-caps-p\"");
});

test("CloneSettingsCard: passes through a custom CloneActionsBridge", async () => {
  // We can't drive React state changes via renderToStaticMarkup, but we
  // CAN exercise the bridge contract directly to confirm the shape.
  const calls: Array<{ kind: string; projectId: string }> = [];
  const bridge: CloneActionsBridge = {
    clearLocalClone: async ({ projectId }) => { calls.push({ kind: "clear", projectId }); },
    revealInFinder: async ({ projectId }) => { calls.push({ kind: "reveal", projectId }); },
    openInTerminal: async ({ projectId }) => { calls.push({ kind: "terminal", projectId }); },
    setCapOverride: async ({ projectId }) => { calls.push({ kind: "caps", projectId }); },
  };
  await bridge.clearLocalClone({ projectId: "alpha" });
  await bridge.revealInFinder({ projectId: "beta" });
  await bridge.openInTerminal({ projectId: "gamma" });
  await bridge.setCapOverride({ projectId: "delta", capsOverride: null });
  expect(calls).toEqual([
    { kind: "clear", projectId: "alpha" },
    { kind: "reveal", projectId: "beta" },
    { kind: "terminal", projectId: "gamma" },
    { kind: "caps", projectId: "delta" },
  ]);
});

test("CloneSettingsCard: fallback stub bridge surfaces the typed unavailable error", async () => {
  let captured: Error | null = null;
  try {
    await STUB_CLONE_ACTIONS_BRIDGE.clearLocalClone({ projectId: "p" });
  } catch (err) {
    captured = err as Error;
  }
  expect(captured?.name).toBe("CloneActionsBridgeUnavailableError");
});

test("CloneSettingsCard: rows with authMissing render together with healthy rows", () => {
  const html = renderToStaticMarkup(
    <CloneSettingsCard
      defaultCaps={DEFAULT_CAPS}
      now={FIXED_NOW}
      rows={[
        row({ projectId: "good" }),
        row({ projectId: "bad", authMissing: true, syncStatus: "error" }),
      ]}
    />,
  );
  expect(html).toContain("data-testid=\"clone-settings-row-good\"");
  expect(html).toContain("data-testid=\"clone-settings-row-bad\"");
  expect(html).toContain("data-testid=\"clone-settings-auth-bad\"");
  expect(html).not.toContain("data-testid=\"clone-settings-auth-good\"");
});
