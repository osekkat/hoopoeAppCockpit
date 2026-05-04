// hp-1fd1 — Pure model for the local-clone settings/cache view.
//
// The Settings card consumes a list of CloneCacheRow entries (one per
// project) + the global cap config + a bridge for the destructive
// actions (Clear / Reveal / Open in terminal). The bridge defaults to
// stubs that throw a typed "not yet wired" error pending the
// `hoopoe.clone.*` preload channels (filed as a follow-up bead).
//
// All sort + format helpers live here as pure functions so the
// rendering tests don't have to deal with dates / Intl quirks.

// Local redeclarations of the engine types — canonical definitions live
// in electron/clone/types.ts (hp-2n1). The renderer tsconfig now
// includes electron/ (hp-5jtf), but we keep this module's type surface
// inlined to avoid coupling the renderer build to the electron tree.
export interface CloneCapConfig {
  readonly softCapBytes: number;
  readonly hardCapBytes: number;
}

export type CloneSyncStatus =
  | "uncloned"
  | "cloning"
  | "fetching"
  | "synced"
  | "error";

export interface CloneCacheRow {
  /** Stable Hoopoe project id. */
  readonly projectId: string;
  /** Human-readable name (defaults to projectId basename). */
  readonly displayName: string;
  /** Origin remote URL — shown next to the name for clarity. */
  readonly originRemote: string;
  /** Sync status from CloneState (uncloned/cloning/fetching/synced/error). */
  readonly syncStatus: CloneSyncStatus;
  /** Recorded clone size on disk (bytes). 0 when uncloned. */
  readonly sizeBytes: number;
  /** RFC3339 timestamp of the last successful fetch. */
  readonly lastSyncedAt: string | null;
  /** RFC3339 timestamp of the last user-driven open of the clone. */
  readonly lastAccessedAt: string;
  /** Per-project cap override; null = use global default. */
  readonly capsOverride: CloneCapConfig | null;
  /** Auth-missing fault stamps the row with a separate banner CTA. */
  readonly authMissing: boolean;
}

export type CloneCacheSortKey = "name" | "size" | "lastSynced" | "lastAccessed" | "status";
export type CloneCacheSortDir = "asc" | "desc";

export interface CloneCacheSort {
  readonly key: CloneCacheSortKey;
  readonly dir: CloneCacheSortDir;
}

export const DEFAULT_CACHE_SORT: CloneCacheSort = { key: "lastAccessed", dir: "desc" };

/** Sort a row list by the given key. The comparator is stable; rows
 *  with equal keys preserve insertion order. */
export function sortCacheRows(
  rows: readonly CloneCacheRow[],
  sort: CloneCacheSort,
): readonly CloneCacheRow[] {
  const indexed = rows.map((row, index) => ({ row, index }));
  indexed.sort((a, b) => {
    const cmp = compareRows(a.row, b.row, sort.key);
    if (cmp !== 0) return sort.dir === "asc" ? cmp : -cmp;
    // Stable: preserve original insertion order on ties.
    return a.index - b.index;
  });
  return indexed.map((entry) => entry.row);
}

function compareRows(a: CloneCacheRow, b: CloneCacheRow, key: CloneCacheSortKey): number {
  switch (key) {
    case "name":
      return a.displayName.localeCompare(b.displayName);
    case "size":
      return a.sizeBytes - b.sizeBytes;
    case "lastSynced":
      return parseTs(a.lastSyncedAt) - parseTs(b.lastSyncedAt);
    case "lastAccessed":
      return parseTs(a.lastAccessedAt) - parseTs(b.lastAccessedAt);
    case "status":
      return a.syncStatus.localeCompare(b.syncStatus);
    default:
      return 0;
  }
}

function parseTs(value: string | null): number {
  if (!value) return -Infinity;
  const t = Date.parse(value);
  return Number.isNaN(t) ? -Infinity : t;
}

/** Sum of sizes across the row list. */
export function totalCacheBytes(rows: readonly CloneCacheRow[]): number {
  let total = 0;
  for (const row of rows) total += Math.max(0, row.sizeBytes);
  return total;
}

/** Formats bytes as B / KB / MB / GB. Pure; tests assert exact output. */
export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  const kb = bytes / 1024;
  if (kb < 1024) return `${kb < 10 ? kb.toFixed(1) : Math.round(kb).toString()} KB`;
  const mb = kb / 1024;
  if (mb < 1024) return `${mb < 10 ? mb.toFixed(1) : Math.round(mb).toString()} MB`;
  const gb = mb / 1024;
  return `${gb < 10 ? gb.toFixed(2) : gb.toFixed(1)} GB`;
}

/** Formats an RFC3339 timestamp as a relative duration ("3m ago", "2d ago").
 *  When `value` is null, returns "—". `now` is injectable for tests. */
export function formatRelativeTime(value: string | null, now: () => Date = () => new Date()): string {
  if (!value) return "—";
  const ts = Date.parse(value);
  if (Number.isNaN(ts)) return "—";
  const deltaMs = Math.max(0, now().getTime() - ts);
  const minute = 60 * 1000;
  const hour = 60 * minute;
  const day = 24 * hour;
  if (deltaMs < minute) return "just now";
  if (deltaMs < hour) return `${Math.round(deltaMs / minute)}m ago`;
  if (deltaMs < day) return `${Math.round(deltaMs / hour)}h ago`;
  return `${Math.round(deltaMs / day)}d ago`;
}

// ── Cap-override validation (renderer-side mirror of disk.ts validateCaps) ──

export interface CapOverrideForm {
  readonly softCapBytes: number;
  readonly hardCapBytes: number;
}

export interface CapOverrideError {
  readonly code: "soft_too_low" | "hard_lt_soft" | "hard_too_high";
  readonly message: string;
}

/** Hard ceiling so the user can't accidentally type "100GB" and brick
 *  their disk. Above this requires editing the JSON file by hand. */
export const CAP_HARD_MAX_BYTES = 50 * 1024 * 1024 * 1024;

export function validateCapOverride(form: CapOverrideForm): CapOverrideError | null {
  if (!Number.isFinite(form.softCapBytes) || form.softCapBytes <= 0) {
    return { code: "soft_too_low", message: "Soft cap must be positive." };
  }
  if (!Number.isFinite(form.hardCapBytes) || form.hardCapBytes <= 0) {
    return { code: "soft_too_low", message: "Hard cap must be positive." };
  }
  if (form.hardCapBytes < form.softCapBytes) {
    return { code: "hard_lt_soft", message: "Hard cap must be >= soft cap." };
  }
  if (form.hardCapBytes > CAP_HARD_MAX_BYTES) {
    return {
      code: "hard_too_high",
      message: `Hard cap must be <= ${formatBytes(CAP_HARD_MAX_BYTES)}; edit the JSON file to override.`,
    };
  }
  return null;
}

// ── Bridge contract for the destructive actions ───────────────────────────

export interface CloneActionsBridge {
  /** Delete the local-clone directory; on next access Hoopoe re-clones. */
  readonly clearLocalClone: (input: { readonly projectId: string }) => Promise<void>;
  /** macOS Reveal-in-Finder. */
  readonly revealInFinder: (input: { readonly projectId: string }) => Promise<void>;
  /** Open the clone directory in the user's default terminal app. */
  readonly openInTerminal: (input: { readonly projectId: string }) => Promise<void>;
  /** Persist a per-project cap override. Pass null to remove the
   *  override and fall back to the global cap config. */
  readonly setCapOverride: (input: {
    readonly projectId: string;
    readonly capsOverride: CapOverrideForm | null;
  }) => Promise<void>;
}

/** Default bridge — every action throws a typed error pointing at the
 *  follow-up bead that wires the preload channels. UI uses this so the
 *  card stays exercisable even when the preload integration isn't wired. */
export class CloneActionsBridgeUnavailableError extends Error {
  override readonly name = "CloneActionsBridgeUnavailableError";
  constructor(action: string) {
    super(
      `Hoopoe clone actions bridge not yet wired for "${action}" — pending the hp-58wp / clone-actions preload channels.`,
    );
  }
}

export const STUB_CLONE_ACTIONS_BRIDGE: CloneActionsBridge = {
  clearLocalClone: () => Promise.reject(new CloneActionsBridgeUnavailableError("clearLocalClone")),
  revealInFinder: () => Promise.reject(new CloneActionsBridgeUnavailableError("revealInFinder")),
  openInTerminal: () => Promise.reject(new CloneActionsBridgeUnavailableError("openInTerminal")),
  setCapOverride: () => Promise.reject(new CloneActionsBridgeUnavailableError("setCapOverride")),
};
