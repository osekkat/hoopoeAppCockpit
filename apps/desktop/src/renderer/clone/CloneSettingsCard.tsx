// hp-1fd1 — Local-clone settings card.
//
// Per plan.md §7.7 'DISK HYGIENE' + 'AUTHENTICATION':
//   - Total cache view: list of clones with size, last-fetched,
//     last-accessed; sortable.
//   - Per-project actions: Reveal in Finder, Open in terminal, caps
//     override. The legacy Clear action now returns a read-only error
//     rather than mutating the desktop mirror.
//   - Per-project cap override editor (soft/hard, validated).
//   - Auth-fallback warning when initial clone fails with auth_missing.
//
// The card is presentation-only. Side effects flow through the
// CloneActionsBridge contract. Production
// resolves the bridge from `window.hoopoe.clone.*`; tests may inject a
// custom bridge or fall back to the typed unavailable stub.

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
} from "lucide-react";
import {
  CAP_HARD_MAX_BYTES,
  DEFAULT_CACHE_SORT,
  formatBytes,
  formatRelativeTime,
  resolveCloneActionsBridge,
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
  /** Bridge for destructive actions. Defaults to the Electron preload
   *  bridge when present, otherwise to a typed unavailable stub. */
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
  actions = resolveCloneActionsBridge(),
  defaultCaps,
  initialSort = DEFAULT_CACHE_SORT,
  now,
  rows,
}: CloneSettingsCardProps) {
  const [sort, setSort] = useState<CloneCacheSort>(initialSort);
  const [actionState, setActionState] = useState<ActionState>(EMPTY_ACTION_STATE);
  const [editingCapsId, setEditingCapsId] = useState<string | null>(null);

  const sortedRows = useMemo(() => sortCacheRows(rows, sort), [rows, sort]);
  const total = useMemo(() => totalCacheBytes(rows), [rows]);

  function flipSort(key: CloneCacheSortKey): void {
    if (sort.key === key) {
      setSort({ key, dir: sort.dir === "asc" ? "desc" : "asc" });
    } else {
      setSort({ key, dir: key === "name" || key === "status" ? "asc" : "desc" });
    }
  }

  async function runAction(
    projectId: string,
    action: "reveal" | "terminal",
  ): Promise<void> {
    const actionId = `${projectId}:${action}`;
    setActionState((prev) => ({
      busy: new Set(prev.busy).add(actionId),
      error: { ...prev.error, [actionId]: "" },
    }));
    try {
      if (action === "reveal") await actions.revealInFinder({ projectId });
      else await actions.openInTerminal({ projectId });
      setActionState((prev) => {
        const busy = new Set(prev.busy);
        busy.delete(actionId);
        const error = { ...prev.error };
        delete error[actionId];
        return { busy, error };
      });
    } catch (err) {
      setActionState((prev) => {
        const busy = new Set(prev.busy);
        busy.delete(actionId);
        return { busy, error: { ...prev.error, [actionId]: (err as Error).message } };
      });
    }
  }

  async function saveCapOverride(
    projectId: string,
    capsOverride: CapOverrideForm | null,
  ): Promise<void> {
    try {
      await actions.setCapOverride({ projectId, capsOverride });
      setEditingCapsId(null);
    } catch (err) {
      setActionState((prev) => ({
        ...prev,
        error: { ...prev.error, [`${projectId}:caps`]: (err as Error).message },
      }));
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
            <code> ~/Library/Application Support/Hoopoe/projects/</code>. The
            mirror is read-only; inspect it in Finder and make code changes
            through the VPS.
          </p>
        </div>
      </header>

      <div className="hh-clone-settings-summary" data-testid="clone-settings-summary">
        <strong>{rows.length}</strong> clone{rows.length === 1 ? "" : "s"} ·{" "}
        <strong>{formatBytes(total)}</strong> on disk · soft cap{" "}
        <strong>{formatBytes(defaultCaps.softCapBytes)}</strong> · hard cap{" "}
        <strong>{formatBytes(defaultCaps.hardCapBytes)}</strong>
      </div>

      {rows.length === 0 ? (
        <div className="hh-clone-settings-empty" data-testid="clone-settings-empty">
          No projects have a local clone yet. Import a project to start the cache.
        </div>
      ) : (
        <div className="hh-clone-settings-table-wrap">
          <table className="hh-clone-settings-table">
            <thead>
              <tr>
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
                  onCapsSave={(caps) => saveCapOverride(row.projectId, caps)}
                  row={row}
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
  readonly editingCaps: boolean;
  readonly defaultCaps: { readonly softCapBytes: number; readonly hardCapBytes: number };
  readonly actionState: ActionState;
  readonly now?: () => Date;
  readonly onAction: (action: "reveal" | "terminal") => void;
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
  row,
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
          <td colSpan={6}>
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
