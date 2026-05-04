// hp-1fd1 — Local-clone settings card.
//
// Per plan.md §7.7 'DISK HYGIENE' + 'AUTHENTICATION':
//   - Total cache view: list of clones with size, last-fetched,
//     last-accessed; sortable; multi-select clear.
//   - Per-project actions: Clear local clone, Reveal in Finder, Open in
//     terminal.
//   - Per-project cap override editor (soft/hard, validated).
//   - Auth-fallback warning when initial clone fails with auth_missing.
//
// The card is presentation-only. The destructive actions (Clear / cap
// override save) flow through the CloneActionsBridge contract. Default
// bridge throws the typed CloneActionsBridgeUnavailableError pending
// the `hoopoe.clone.*` preload channels.

import { useMemo, useState } from "react";
import {
  AlertTriangle,
  ArrowDown,
  ArrowUp,
  CheckCircle2,
  CircleHelp,
  FolderOpen,
  KeyRound,
  Loader2,
  Settings,
  Terminal,
  Trash2,
} from "lucide-react";
import {
  CAP_HARD_MAX_BYTES,
  DEFAULT_CACHE_SORT,
  STUB_CLONE_ACTIONS_BRIDGE,
  formatBytes,
  formatRelativeTime,
  sortCacheRows,
  totalCacheBytes,
  validateCapOverride,
  type CapOverrideForm,
  type CloneActionsBridge,
  type CloneCacheRow,
  type CloneCacheSort,
  type CloneCacheSortKey,
} from "./cache-view-model.ts";
import "./CloneSettingsCard.css";

export interface CloneSettingsCardProps {
  /** All projects' cache rows. Empty list renders the "no clones yet"
   *  empty state. */
  readonly rows: readonly CloneCacheRow[];
  /** Default cap config used when a row has capsOverride === null. */
  readonly defaultCaps: { readonly softCapBytes: number; readonly hardCapBytes: number };
  /** Bridge for destructive actions. Defaults to a stub that throws
   *  CloneActionsBridgeUnavailableError so the card renders without
   *  requiring the preload channels to be wired. */
  readonly actions?: CloneActionsBridge;
  /** Initial sort. Default: lastAccessed desc. */
  readonly initialSort?: CloneCacheSort;
  /** `now()` injection for stable test rendering. */
  readonly now?: () => Date;
}

interface ActionState {
  readonly busy: Set<string>;
  readonly error: Record<string, string>;
}

const EMPTY_ACTION_STATE: ActionState = { busy: new Set(), error: {} };

export function CloneSettingsCard({
  actions = STUB_CLONE_ACTIONS_BRIDGE,
  defaultCaps,
  initialSort = DEFAULT_CACHE_SORT,
  now,
  rows,
}: CloneSettingsCardProps) {
  const [sort, setSort] = useState<CloneCacheSort>(initialSort);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [actionState, setActionState] = useState<ActionState>(EMPTY_ACTION_STATE);
  const [editingCapsId, setEditingCapsId] = useState<string | null>(null);

  const sortedRows = useMemo(() => sortCacheRows(rows, sort), [rows, sort]);
  const total = useMemo(() => totalCacheBytes(rows), [rows]);
  const selectedSize = useMemo(
    () => totalCacheBytes(rows.filter((r) => selected.has(r.projectId))),
    [rows, selected],
  );

  function flipSort(key: CloneCacheSortKey): void {
    if (sort.key === key) {
      setSort({ key, dir: sort.dir === "asc" ? "desc" : "asc" });
    } else {
      setSort({ key, dir: key === "name" || key === "status" ? "asc" : "desc" });
    }
  }

  function toggleSelected(projectId: string): void {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(projectId)) next.delete(projectId);
      else next.add(projectId);
      return next;
    });
  }

  function selectAll(): void {
    setSelected(new Set(rows.map((r) => r.projectId)));
  }

  function deselectAll(): void {
    setSelected(new Set());
  }

  async function runAction(
    projectId: string,
    action: "clear" | "reveal" | "terminal",
  ): Promise<void> {
    const actionId = `${projectId}:${action}`;
    setActionState((prev) => ({
      busy: new Set(prev.busy).add(actionId),
      error: { ...prev.error, [actionId]: "" },
    }));
    try {
      if (action === "clear") await actions.clearLocalClone({ projectId });
      else if (action === "reveal") await actions.revealInFinder({ projectId });
      else await actions.openInTerminal({ projectId });
      setActionState((prev) => {
        const busy = new Set(prev.busy);
        busy.delete(actionId);
        const error = { ...prev.error };
        delete error[actionId];
        return { busy, error };
      });
      if (action === "clear") {
        setSelected((prev) => {
          const next = new Set(prev);
          next.delete(projectId);
          return next;
        });
      }
    } catch (err) {
      setActionState((prev) => {
        const busy = new Set(prev.busy);
        busy.delete(actionId);
        return { busy, error: { ...prev.error, [actionId]: (err as Error).message } };
      });
    }
  }

  async function clearMultiple(): Promise<void> {
    const ids = Array.from(selected);
    for (const id of ids) {
      // Run sequentially so the user sees per-row feedback. A concurrent
      // version would obscure which clone failed.
      // eslint-disable-next-line no-await-in-loop
      await runAction(id, "clear");
    }
  }

  return (
    <section
      aria-labelledby="hh-clone-settings-title"
      className="hh-clone-settings-card"
      data-testid="clone-settings-card"
    >
      <header className="hh-clone-settings-header">
        <Settings size={18} strokeWidth={2.1} />
        <div>
          <h2 id="hh-clone-settings-title">Local clone cache</h2>
          <p>
            Hoopoe maintains a sync-driven Git mirror of every project under
            <code> ~/Library/Application Support/Hoopoe/projects/</code>. Clear a
            project's clone to free disk space; the next access re-clones.
          </p>
        </div>
      </header>

      <div className="hh-clone-settings-summary" data-testid="clone-settings-summary">
        <strong>{rows.length}</strong> clone{rows.length === 1 ? "" : "s"} ·{" "}
        <strong>{formatBytes(total)}</strong> on disk · soft cap{" "}
        <strong>{formatBytes(defaultCaps.softCapBytes)}</strong> · hard cap{" "}
        <strong>{formatBytes(defaultCaps.hardCapBytes)}</strong>
      </div>

      {selected.size > 0 ? (
        <div
          aria-live="polite"
          className="hh-clone-settings-bulk"
          data-testid="clone-settings-bulk"
        >
          <span>
            {selected.size} selected · {formatBytes(selectedSize)} would be cleared
          </span>
          <div className="hh-clone-settings-bulk-actions">
            <button
              data-testid="clone-settings-bulk-deselect"
              onClick={deselectAll}
              type="button"
            >
              Deselect all
            </button>
            <button
              className="hh-clone-settings-danger"
              data-testid="clone-settings-bulk-clear"
              onClick={() => void clearMultiple()}
              type="button"
            >
              <Trash2 size={13} strokeWidth={2.1} aria-hidden="true" />
              Clear selected
            </button>
          </div>
        </div>
      ) : null}

      {rows.length === 0 ? (
        <div className="hh-clone-settings-empty" data-testid="clone-settings-empty">
          No projects have a local clone yet. Import a project to start the cache.
        </div>
      ) : (
        <div className="hh-clone-settings-table-wrap">
          <table className="hh-clone-settings-table">
            <thead>
              <tr>
                <th aria-label="Select">
                  <input
                    aria-label="Select all clones"
                    checked={selected.size === rows.length && rows.length > 0}
                    data-testid="clone-settings-select-all"
                    onChange={(event) => (event.target.checked ? selectAll() : deselectAll())}
                    type="checkbox"
                  />
                </th>
                <SortHeader sort={sort} sortKey="name" label="Project" onClick={flipSort} testId="clone-settings-th-name" />
                <SortHeader sort={sort} sortKey="status" label="Status" onClick={flipSort} testId="clone-settings-th-status" />
                <SortHeader sort={sort} sortKey="size" label="Size" onClick={flipSort} testId="clone-settings-th-size" />
                <SortHeader sort={sort} sortKey="lastSynced" label="Last synced" onClick={flipSort} testId="clone-settings-th-synced" />
                <SortHeader sort={sort} sortKey="lastAccessed" label="Last accessed" onClick={flipSort} testId="clone-settings-th-accessed" />
                <th aria-label="Actions">Actions</th>
              </tr>
            </thead>
            <tbody>
              {sortedRows.map((row) => (
                <CacheRow
                  actionState={actionState}
                  defaultCaps={defaultCaps}
                  editingCaps={editingCapsId === row.projectId}
                  key={row.projectId}
                  {...(now !== undefined ? { now } : {})}
                  onAction={(action) => void runAction(row.projectId, action)}
                  onEditCaps={() => setEditingCapsId(row.projectId)}
                  onCapsClose={() => setEditingCapsId(null)}
                  onCapsSave={async (caps) => {
                    try {
                      await actions.setCapOverride({ projectId: row.projectId, capsOverride: caps });
                      setEditingCapsId(null);
                    } catch (err) {
                      setActionState((prev) => ({
                        ...prev,
                        error: { ...prev.error, [`${row.projectId}:caps`]: (err as Error).message },
                      }));
                    }
                  }}
                  onSelectionToggle={() => toggleSelected(row.projectId)}
                  row={row}
                  selected={selected.has(row.projectId)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

interface SortHeaderProps {
  readonly sort: CloneCacheSort;
  readonly sortKey: CloneCacheSortKey;
  readonly label: string;
  readonly onClick: (key: CloneCacheSortKey) => void;
  readonly testId: string;
}

function SortHeader({ label, onClick, sort, sortKey, testId }: SortHeaderProps) {
  const isActive = sort.key === sortKey;
  return (
    <th aria-sort={isActive ? (sort.dir === "asc" ? "ascending" : "descending") : "none"}>
      <button
        className="hh-clone-settings-sort"
        data-active={isActive}
        data-dir={isActive ? sort.dir : undefined}
        data-testid={testId}
        onClick={() => onClick(sortKey)}
        type="button"
      >
        {label}
        {isActive ? (
          sort.dir === "asc" ? (
            <ArrowUp aria-hidden="true" size={11} strokeWidth={2.4} />
          ) : (
            <ArrowDown aria-hidden="true" size={11} strokeWidth={2.4} />
          )
        ) : null}
      </button>
    </th>
  );
}

interface CacheRowProps {
  readonly row: CloneCacheRow;
  readonly selected: boolean;
  readonly editingCaps: boolean;
  readonly defaultCaps: { readonly softCapBytes: number; readonly hardCapBytes: number };
  readonly actionState: ActionState;
  readonly now?: () => Date;
  readonly onSelectionToggle: () => void;
  readonly onAction: (action: "clear" | "reveal" | "terminal") => void;
  readonly onEditCaps: () => void;
  readonly onCapsClose: () => void;
  readonly onCapsSave: (caps: CapOverrideForm | null) => Promise<void>;
}

function CacheRow({
  actionState,
  defaultCaps,
  editingCaps,
  now,
  onAction,
  onCapsClose,
  onCapsSave,
  onEditCaps,
  onSelectionToggle,
  row,
  selected,
}: CacheRowProps) {
  const busy = (action: string): boolean => actionState.busy.has(`${row.projectId}:${action}`);
  const error = (action: string): string | undefined => actionState.error[`${row.projectId}:${action}`];
  const effectiveCaps = row.capsOverride ?? defaultCaps;

  return (
    <>
      <tr
        data-status={row.syncStatus}
        data-testid={`clone-settings-row-${row.projectId}`}
      >
        <td>
          <input
            aria-label={`Select ${row.displayName}`}
            checked={selected}
            data-testid={`clone-settings-select-${row.projectId}`}
            onChange={onSelectionToggle}
            type="checkbox"
          />
        </td>
        <td className="hh-clone-settings-name">
          <div>
            <strong>{row.displayName}</strong>
            <span>{row.originRemote}</span>
          </div>
          {row.authMissing ? (
            <p className="hh-clone-settings-auth" data-testid={`clone-settings-auth-${row.projectId}`}>
              <KeyRound aria-hidden="true" size={12} strokeWidth={2.1} />
              SSH or PAT credentials missing — initial clone failed. Check your
              git credential helper, then re-trigger the clone from this row.
            </p>
          ) : null}
        </td>
        <td>
          <StatusBadge status={row.syncStatus} />
        </td>
        <td>{formatBytes(row.sizeBytes)}</td>
        <td>{formatRelativeTime(row.lastSyncedAt, now)}</td>
        <td>{formatRelativeTime(row.lastAccessedAt, now)}</td>
        <td>
          <div className="hh-clone-settings-row-actions">
            <ActionButton
              busy={busy("reveal")}
              {...(error("reveal") !== undefined ? { error: error("reveal")! } : {})}
              icon={<FolderOpen size={13} strokeWidth={2.1} />}
              label="Reveal"
              onClick={() => onAction("reveal")}
              testId={`clone-settings-action-reveal-${row.projectId}`}
            />
            <ActionButton
              busy={busy("terminal")}
              {...(error("terminal") !== undefined ? { error: error("terminal")! } : {})}
              icon={<Terminal size={13} strokeWidth={2.1} />}
              label="Terminal"
              onClick={() => onAction("terminal")}
              testId={`clone-settings-action-terminal-${row.projectId}`}
            />
            <ActionButton
              busy={busy("clear")}
              danger
              {...(error("clear") !== undefined ? { error: error("clear")! } : {})}
              icon={<Trash2 size={13} strokeWidth={2.1} />}
              label="Clear"
              onClick={() => onAction("clear")}
              testId={`clone-settings-action-clear-${row.projectId}`}
            />
            <ActionButton
              busy={false}
              icon={<Settings size={13} strokeWidth={2.1} />}
              label="Caps"
              onClick={editingCaps ? onCapsClose : onEditCaps}
              testId={`clone-settings-action-caps-${row.projectId}`}
            />
          </div>
        </td>
      </tr>
      {editingCaps ? (
        <tr data-testid={`clone-settings-caps-row-${row.projectId}`}>
          <td colSpan={7}>
            <CapEditor
              defaults={defaultCaps}
              {...(row.capsOverride !== null ? { initial: row.capsOverride } : {})}
              onCancel={onCapsClose}
              onSave={onCapsSave}
              projectId={row.projectId}
              {...(error("caps") !== undefined ? { saveError: error("caps")! } : {})}
            />
            <p className="hh-clone-settings-caps-effective">
              Effective: soft <strong>{formatBytes(effectiveCaps.softCapBytes)}</strong> ·
              hard <strong>{formatBytes(effectiveCaps.hardCapBytes)}</strong>
            </p>
          </td>
        </tr>
      ) : null}
    </>
  );
}

function StatusBadge({ status }: { readonly status: CloneCacheRow["syncStatus"] }) {
  const Icon = status === "synced" ? CheckCircle2 : status === "error" ? AlertTriangle : status === "uncloned" ? CircleHelp : Loader2;
  return (
    <span className={`hh-clone-settings-status hh-clone-status-${status}`} data-status={status}>
      <Icon size={12} strokeWidth={2.1} aria-hidden="true" />
      {status}
    </span>
  );
}

interface ActionButtonProps {
  readonly icon: React.ReactNode;
  readonly label: string;
  readonly onClick: () => void;
  readonly busy: boolean;
  readonly error?: string;
  readonly danger?: boolean;
  readonly testId: string;
}

function ActionButton({ busy, danger, error, icon, label, onClick, testId }: ActionButtonProps) {
  return (
    <span className="hh-clone-settings-action-wrap">
      <button
        className="hh-clone-settings-action"
        data-busy={busy}
        data-danger={danger ?? false}
        data-testid={testId}
        disabled={busy}
        onClick={onClick}
        type="button"
      >
        {busy ? <Loader2 className="hh-spin" size={13} strokeWidth={2.1} /> : icon}
        {label}
      </button>
      {error ? (
        <small className="hh-clone-settings-action-error" data-testid={`${testId}-error`} role="alert">
          {error}
        </small>
      ) : null}
    </span>
  );
}

interface CapEditorProps {
  readonly projectId: string;
  readonly defaults: { readonly softCapBytes: number; readonly hardCapBytes: number };
  readonly initial?: CapOverrideForm;
  readonly onCancel: () => void;
  readonly onSave: (caps: CapOverrideForm | null) => Promise<void>;
  readonly saveError?: string;
}

function CapEditor({ defaults, initial, onCancel, onSave, projectId, saveError }: CapEditorProps) {
  const [softMb, setSoftMb] = useState<string>(
    String(Math.round((initial?.softCapBytes ?? defaults.softCapBytes) / (1024 * 1024))),
  );
  const [hardMb, setHardMb] = useState<string>(
    String(Math.round((initial?.hardCapBytes ?? defaults.hardCapBytes) / (1024 * 1024))),
  );
  const [busy, setBusy] = useState(false);

  const form: CapOverrideForm = {
    softCapBytes: Number.parseInt(softMb, 10) * 1024 * 1024,
    hardCapBytes: Number.parseInt(hardMb, 10) * 1024 * 1024,
  };
  const issue = validateCapOverride(form);

  async function save(remove: boolean) {
    if (!remove && issue) return;
    setBusy(true);
    try {
      await onSave(remove ? null : form);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      className="hh-clone-settings-caps"
      data-testid={`clone-settings-caps-${projectId}`}
    >
      <fieldset disabled={busy}>
        <legend>Per-project cap override</legend>
        <label>
          Soft cap (MB)
          <input
            data-testid={`clone-settings-caps-soft-${projectId}`}
            inputMode="numeric"
            min={1}
            max={Math.round(CAP_HARD_MAX_BYTES / (1024 * 1024))}
            onChange={(event) => setSoftMb(event.target.value)}
            type="number"
            value={softMb}
          />
        </label>
        <label>
          Hard cap (MB)
          <input
            data-testid={`clone-settings-caps-hard-${projectId}`}
            inputMode="numeric"
            min={1}
            max={Math.round(CAP_HARD_MAX_BYTES / (1024 * 1024))}
            onChange={(event) => setHardMb(event.target.value)}
            type="number"
            value={hardMb}
          />
        </label>
      </fieldset>
      {issue ? (
        <p
          className="hh-clone-settings-caps-issue"
          data-testid={`clone-settings-caps-issue-${projectId}`}
          role="alert"
        >
          {issue.message}
        </p>
      ) : null}
      {saveError ? (
        <p
          className="hh-clone-settings-caps-issue"
          data-testid={`clone-settings-caps-save-error-${projectId}`}
          role="alert"
        >
          {saveError}
        </p>
      ) : null}
      <div className="hh-clone-settings-caps-actions">
        <button data-testid={`clone-settings-caps-remove-${projectId}`} onClick={() => void save(true)} type="button">
          Remove override
        </button>
        <button data-testid={`clone-settings-caps-cancel-${projectId}`} onClick={onCancel} type="button">
          Cancel
        </button>
        <button
          className="hh-clone-settings-caps-save"
          data-testid={`clone-settings-caps-save-${projectId}`}
          disabled={issue !== null}
          onClick={() => void save(false)}
          type="button"
        >
          Save override
        </button>
      </div>
    </div>
  );
}
