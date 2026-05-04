// hp-1fd1 — cache view model tests.

import { expect, test } from "bun:test";
import {
  CAP_HARD_MAX_BYTES,
  CloneActionsBridgeUnavailableError,
  STUB_CLONE_ACTIONS_BRIDGE,
  ReadOnlyCloneMirrorError,
  formatBytes,
  formatRelativeTime,
  resolveCloneActionsBridge,
  sortCacheRows,
  totalCacheBytes,
  validateCapOverride,
  type CloneCacheRow,
  type CloneCacheSort,
} from "./cache-view-model.ts";

function row(input: Partial<CloneCacheRow> & Pick<CloneCacheRow, "projectId">): CloneCacheRow {
  return {
    displayName: input.displayName ?? input.projectId,
    originRemote: input.originRemote ?? `git@github.com:org/${input.projectId}.git`,
    syncStatus: input.syncStatus ?? "synced",
    sizeBytes: input.sizeBytes ?? 0,
    lastSyncedAt: input.lastSyncedAt ?? null,
    lastAccessedAt: input.lastAccessedAt ?? "2026-05-04T00:00:00.000Z",
    capsOverride: input.capsOverride ?? null,
    authMissing: input.authMissing ?? false,
    ...input,
  };
}

test("formatBytes: handles B / KB / MB / GB scales + edge cases", () => {
  expect(formatBytes(0)).toBe("0 B");
  expect(formatBytes(1)).toBe("1 B");
  expect(formatBytes(1023)).toBe("1023 B");
  expect(formatBytes(1024)).toBe("1.0 KB");
  expect(formatBytes(1024 * 5.5)).toBe("5.5 KB");
  expect(formatBytes(1024 * 100)).toBe("100 KB");
  expect(formatBytes(1024 * 1024)).toBe("1.0 MB");
  expect(formatBytes(1024 * 1024 * 250)).toBe("250 MB");
  expect(formatBytes(1024 * 1024 * 1024)).toBe("1.00 GB");
  expect(formatBytes(1024 * 1024 * 1024 * 12.5)).toBe("12.5 GB");
});

test("formatBytes: invalid input renders as em dash", () => {
  expect(formatBytes(-1)).toBe("—");
  expect(formatBytes(NaN)).toBe("—");
  expect(formatBytes(Infinity)).toBe("—");
});

test("formatRelativeTime: null + invalid → em dash", () => {
  expect(formatRelativeTime(null)).toBe("—");
  expect(formatRelativeTime("not-a-date")).toBe("—");
});

test("formatRelativeTime: scales (just now / Nm / Nh / Nd)", () => {
  const now = () => new Date("2026-05-04T12:00:00.000Z");
  expect(formatRelativeTime("2026-05-04T11:59:30.000Z", now)).toBe("just now");
  expect(formatRelativeTime("2026-05-04T11:55:00.000Z", now)).toBe("5m ago");
  expect(formatRelativeTime("2026-05-04T09:00:00.000Z", now)).toBe("3h ago");
  expect(formatRelativeTime("2026-05-01T12:00:00.000Z", now)).toBe("3d ago");
});

test("sortCacheRows: by name asc + desc", () => {
  const rows = [row({ projectId: "p", displayName: "Charlie" }), row({ projectId: "p2", displayName: "Alpha" }), row({ projectId: "p3", displayName: "Bravo" })];
  const ascSort: CloneCacheSort = { key: "name", dir: "asc" };
  const ascResult = sortCacheRows(rows, ascSort);
  expect(ascResult.map((r) => r.displayName)).toEqual(["Alpha", "Bravo", "Charlie"]);
  const descResult = sortCacheRows(rows, { key: "name", dir: "desc" });
  expect(descResult.map((r) => r.displayName)).toEqual(["Charlie", "Bravo", "Alpha"]);
});

test("sortCacheRows: by size desc", () => {
  const rows = [
    row({ projectId: "small", sizeBytes: 100 }),
    row({ projectId: "big", sizeBytes: 1_000_000 }),
    row({ projectId: "medium", sizeBytes: 50_000 }),
  ];
  const sorted = sortCacheRows(rows, { key: "size", dir: "desc" });
  expect(sorted.map((r) => r.projectId)).toEqual(["big", "medium", "small"]);
});

test("sortCacheRows: stable on ties (preserves insertion order)", () => {
  const rows = [
    row({ projectId: "a", sizeBytes: 100 }),
    row({ projectId: "b", sizeBytes: 100 }),
    row({ projectId: "c", sizeBytes: 100 }),
  ];
  const sorted = sortCacheRows(rows, { key: "size", dir: "asc" });
  expect(sorted.map((r) => r.projectId)).toEqual(["a", "b", "c"]);
});

test("sortCacheRows: by lastSynced (null sorts as oldest)", () => {
  const rows = [
    row({ projectId: "synced-recent", lastSyncedAt: "2026-05-04T10:00:00.000Z" }),
    row({ projectId: "synced-old", lastSyncedAt: "2026-05-01T10:00:00.000Z" }),
    row({ projectId: "never", lastSyncedAt: null }),
  ];
  const ascResult = sortCacheRows(rows, { key: "lastSynced", dir: "asc" });
  expect(ascResult[0]?.projectId).toBe("never");
  expect(ascResult[1]?.projectId).toBe("synced-old");
  expect(ascResult[2]?.projectId).toBe("synced-recent");
});

test("sortCacheRows: by status alphabetic", () => {
  const rows = [
    row({ projectId: "e", syncStatus: "error" }),
    row({ projectId: "s", syncStatus: "synced" }),
    row({ projectId: "u", syncStatus: "uncloned" }),
    row({ projectId: "f", syncStatus: "fetching" }),
  ];
  const sorted = sortCacheRows(rows, { key: "status", dir: "asc" });
  expect(sorted.map((r) => r.syncStatus)).toEqual(["error", "fetching", "synced", "uncloned"]);
});

test("totalCacheBytes: sums up + ignores negatives", () => {
  expect(totalCacheBytes([])).toBe(0);
  expect(totalCacheBytes([
    row({ projectId: "a", sizeBytes: 100 }),
    row({ projectId: "b", sizeBytes: 200 }),
    row({ projectId: "c", sizeBytes: -50 }),
  ])).toBe(300);
});

test("validateCapOverride: happy path + non-positive sizes", () => {
  expect(validateCapOverride({ softCapBytes: 1024, hardCapBytes: 2048 })).toBeNull();
  expect(validateCapOverride({ softCapBytes: 0, hardCapBytes: 1 })?.code).toBe("soft_too_low");
  expect(validateCapOverride({ softCapBytes: 1, hardCapBytes: 0 })?.code).toBe("soft_too_low");
});

test("validateCapOverride: hard cap < soft cap is rejected", () => {
  const issue = validateCapOverride({ softCapBytes: 2_000, hardCapBytes: 1_000 });
  expect(issue?.code).toBe("hard_lt_soft");
});

test("validateCapOverride: exceeds CAP_HARD_MAX_BYTES is rejected", () => {
  const issue = validateCapOverride({ softCapBytes: 1024, hardCapBytes: CAP_HARD_MAX_BYTES + 1 });
  expect(issue?.code).toBe("hard_too_high");
  expect(issue?.message).toContain("50.0 GB");
});

test("CAP_HARD_MAX_BYTES: 50 GB ceiling", () => {
  expect(CAP_HARD_MAX_BYTES).toBe(50 * 1024 * 1024 * 1024);
});

test("STUB_CLONE_ACTIONS_BRIDGE: every method rejects with the bridge-unavailable error", async () => {
  for (const action of ["clearLocalClone", "revealInFinder", "openInTerminal", "setCapOverride"] as const) {
    let captured: Error | null = null;
    try {
      await (STUB_CLONE_ACTIONS_BRIDGE as unknown as Record<string, (input: unknown) => Promise<void>>)[action]!({
        projectId: "p",
      });
    } catch (err) {
      captured = err as Error;
    }
    expect(captured).toBeInstanceOf(CloneActionsBridgeUnavailableError);
    expect(captured?.message).toContain(action);
    expect(captured?.message.toLowerCase()).toContain("not available");
  }
});

test("resolveCloneActionsBridge: falls back to typed unavailable bridge without preload channels", async () => {
  const bridge = resolveCloneActionsBridge({});
  let captured: Error | null = null;
  try {
    await bridge.revealInFinder({ projectId: "p" });
  } catch (err) {
    captured = err as Error;
  }
  expect(captured).toBeInstanceOf(CloneActionsBridgeUnavailableError);
});

test("resolveCloneActionsBridge: adapts window.hoopoe.clone preload channels", async () => {
  const calls: string[] = [];
  const bridge = resolveCloneActionsBridge({
    window: {
      hoopoe: {
        clone: {
          revealInFinder: async ({ projectId }) => {
            calls.push(`reveal:${projectId}`);
          },
          openInTerminal: async ({ projectId }) => {
            calls.push(`terminal:${projectId}`);
          },
          setCapOverride: async ({ capsOverride, projectId }) => {
            calls.push(`caps:${projectId}:${capsOverride?.softCapBytes ?? "default"}`);
          },
        },
      },
    },
  });

  let clearError: Error | null = null;
  try {
    await bridge.clearLocalClone({ projectId: "alpha" });
  } catch (err) {
    clearError = err as Error;
  }
  expect(clearError).toBeInstanceOf(ReadOnlyCloneMirrorError);
  await bridge.revealInFinder({ projectId: "beta" });
  await bridge.openInTerminal({ projectId: "gamma" });
  await bridge.setCapOverride({
    projectId: "delta",
    capsOverride: { softCapBytes: 1024, hardCapBytes: 2048 },
  });
  await bridge.setCapOverride({ projectId: "epsilon", capsOverride: null });

  expect(calls).toEqual([
    "reveal:beta",
    "terminal:gamma",
    "caps:delta:1024",
    "caps:epsilon:default",
  ]);
});
