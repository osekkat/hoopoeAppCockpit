// hp-70yz — Local-clone dirty banner.
//
// Shown above the active stage when the renderer's dirty-state store has
// a non-clean entry for the active project. Per plan.md §7.7:
//   "Local clone has unsaved changes — Hoopoe ignores local edits.
//    Make changes via the VPS (ssh or Cursor/VS Code Remote).
//    [Discard local changes] [Open clone in Finder]"
//
// Discard is a destructive action (`git reset --hard @{u} && git clean -fd`).
// It MUST be confirmed by the user; the explicit confirmation text reads
// the file counts so the user knows what they're throwing away.
//
// Reveal-in-Finder uses the existing `window.hoopoe.files.revealInFinder`
// preload channel. Discard requires a new preload channel + safe shell
// boundary (filed as a follow-up bead at hp-70yz close); for this commit
// the Discard button opens the confirmation dialog and reports a
// "Coming soon — pending discardLocalClone preload channel" notice
// rather than running an unsafe shell.

import { useEffect, useState } from "react";
import { AlertTriangle, FolderOpen, Trash2 } from "lucide-react";
import {
  selectDirtyState,
  useDirtyStore,
  type CloneDirtyState,
} from "./dirty-store.ts";
import "./DirtyBanner.css";

export interface DirtyBannerProps {
  /** Project the banner is rendering for. When null, the banner is
   *  hidden (project picker route). */
  readonly projectId: string | null;
  /** Override the file-reveal action (tests). Default uses
   *  `window.hoopoe.files.revealInFinder`. */
  readonly revealInFinder?: (path: string) => Promise<void> | void;
  /** Override the discard action (tests). Defaults to a stub that opens
   *  a "coming soon" notice (pending the discardLocalClone preload
   *  channel — see hp-70yz follow-up). */
  readonly discardLocalChanges?: (input: { projectId: string }) => Promise<void> | void;
  /** Path to the local clone (for the Reveal action). Optional; when
   *  omitted, the Reveal button is hidden. */
  readonly cloneRepoPath?: string;
}

export function DirtyBanner(props: DirtyBannerProps) {
  // The store hook is the live wire-up; tests inject `dirtyState` directly
  // via DirtyBannerView (below) to avoid Zustand SSR snapshot issues with
  // renderToStaticMarkup.
  const storeState = useDirtyStore((store) => selectDirtyState(store, props.projectId));
  return <DirtyBannerView {...props} dirtyState={storeState} />;
}

export interface DirtyBannerViewProps extends DirtyBannerProps {
  /** Pre-resolved dirty state (test seam). When null/undefined the
   *  banner renders nothing. */
  readonly dirtyState: CloneDirtyState | null;
}

export function DirtyBannerView({
  cloneRepoPath,
  discardLocalChanges,
  dirtyState,
  projectId,
  revealInFinder,
}: DirtyBannerViewProps) {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [discardError, setDiscardError] = useState<string | null>(null);

  // Auto-close the dialog if the project becomes clean while it's open.
  useEffect(() => {
    if (!dirtyState?.dirty) setConfirmOpen(false);
  }, [dirtyState?.dirty]);

  if (!projectId) return null;
  if (!dirtyState || !dirtyState.dirty) return null;

  // Capture the narrowed projectId so closures don't widen it back to
  // `string | null`.
  const activeProjectId: string = projectId;
  const summary = describeDirty(dirtyState);

  function handleReveal() {
    if (!cloneRepoPath) return;
    const reveal = revealInFinder ?? defaultRevealInFinder;
    void Promise.resolve(reveal(cloneRepoPath));
  }

  function handleDiscardClick() {
    setDiscardError(null);
    setConfirmOpen(true);
  }

  function handleConfirmDiscard() {
    setDiscardError(null);
    const discard = discardLocalChanges ?? defaultDiscardLocalChanges;
    Promise.resolve(discard({ projectId: activeProjectId }))
      .then(() => setConfirmOpen(false))
      .catch((err: Error) => setDiscardError(err.message));
  }

  return (
    <div
      aria-labelledby="hh-dirty-banner-title"
      className="hh-dirty-banner"
      data-testid="dirty-banner"
      role="alert"
    >
      <AlertTriangle aria-hidden="true" size={18} strokeWidth={2.1} />
      <div className="hh-dirty-banner-body">
        <strong id="hh-dirty-banner-title">Local clone has unsaved changes</strong>
        <p>
          Hoopoe ignores local edits. Make changes via the VPS (ssh or
          Cursor/VS Code Remote). <span className="hh-dirty-banner-counts">{summary}</span>
        </p>
      </div>
      <div className="hh-dirty-banner-actions">
        <button
          className="hh-dirty-banner-discard"
          data-testid="dirty-banner-discard"
          onClick={handleDiscardClick}
          type="button"
        >
          <Trash2 size={14} strokeWidth={2.1} />
          Discard local changes
        </button>
        {cloneRepoPath !== undefined ? (
          <button
            className="hh-dirty-banner-reveal"
            data-testid="dirty-banner-reveal"
            onClick={handleReveal}
            type="button"
          >
            <FolderOpen size={14} strokeWidth={2.1} />
            Open in Finder
          </button>
        ) : null}
      </div>
      {confirmOpen ? (
        <DiscardConfirmationDialog
          dirty={dirtyState}
          onCancel={() => setConfirmOpen(false)}
          onConfirm={handleConfirmDiscard}
          {...(discardError !== null ? { error: discardError } : {})}
        />
      ) : null}
    </div>
  );
}

interface DiscardConfirmationDialogProps {
  readonly dirty: CloneDirtyState;
  readonly onCancel: () => void;
  readonly onConfirm: () => void;
  readonly error?: string;
}

function DiscardConfirmationDialog({
  dirty,
  error,
  onCancel,
  onConfirm,
}: DiscardConfirmationDialogProps) {
  // The confirmation text spells out exactly what `git reset --hard @{u}
  // && git clean -fd` will throw away so the user can't accidentally
  // confirm. Per plan.md §5.2: destructive actions surface a typed
  // confirmation; here the typing is the explicit "Discard" button vs
  // the safe "Cancel" default. Pressing Escape cancels.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onCancel();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onCancel]);

  return (
    <div
      aria-labelledby="hh-discard-title"
      aria-modal="true"
      className="hh-dirty-banner-dialog"
      data-testid="dirty-banner-confirm"
      role="dialog"
    >
      <div className="hh-dirty-banner-dialog-card">
        <h2 id="hh-discard-title">Discard local changes?</h2>
        <p>
          This will run <code>git reset --hard @{`{u}`} &amp;&amp; git clean -fd</code> on the
          local clone. The following changes will be permanently deleted:
        </p>
        <ul>
          {dirty.modifiedCount > 0 ? (
            <li>{dirty.modifiedCount} modified file{dirty.modifiedCount === 1 ? "" : "s"}</li>
          ) : null}
          {dirty.untrackedCount > 0 ? (
            <li>{dirty.untrackedCount} untracked file{dirty.untrackedCount === 1 ? "" : "s"}</li>
          ) : null}
          {dirty.aheadCount > 0 ? (
            <li>{dirty.aheadCount} local commit{dirty.aheadCount === 1 ? "" : "s"} ahead of upstream</li>
          ) : null}
        </ul>
        <p className="hh-dirty-banner-warning">This cannot be undone.</p>
        {error !== undefined ? (
          <p className="hh-dirty-banner-error" role="alert" data-testid="dirty-banner-confirm-error">{error}</p>
        ) : null}
        <div className="hh-dirty-banner-dialog-actions">
          <button
            className="hh-dirty-banner-dialog-cancel"
            data-testid="dirty-banner-confirm-cancel"
            onClick={onCancel}
            type="button"
          >
            Cancel
          </button>
          <button
            className="hh-dirty-banner-dialog-confirm"
            data-testid="dirty-banner-confirm-discard"
            onClick={onConfirm}
            type="button"
          >
            Discard
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Pure formatters (testable) ───────────────────────────────────────────

export function describeDirty(state: CloneDirtyState): string {
  const parts: string[] = [];
  if (state.modifiedCount > 0) {
    parts.push(`${state.modifiedCount} modified`);
  }
  if (state.untrackedCount > 0) {
    parts.push(`${state.untrackedCount} untracked`);
  }
  if (state.aheadCount > 0) {
    parts.push(`ahead ${state.aheadCount}`);
  }
  if (state.behindCount > 0) {
    parts.push(`behind ${state.behindCount}`);
  }
  return parts.join(" · ");
}

// ── Default actions (production wiring) ──────────────────────────────────

interface FilesBridge {
  readonly revealInFinder?: (path: string) => Promise<void>;
}

interface BridgeShape {
  readonly files?: FilesBridge;
}

function defaultRevealInFinder(path: string): Promise<void> | void {
  if (typeof window === "undefined") return;
  const bridge = (window as Window & { readonly hoopoe?: BridgeShape }).hoopoe;
  const reveal = bridge?.files?.revealInFinder;
  if (typeof reveal === "function") return reveal(path);
}

function defaultDiscardLocalChanges(_input: { projectId: string }): Promise<void> {
  return Promise.reject(
    new Error(
      "Discard not yet wired — pending the hoopoe.clone.discardLocalChanges preload channel (follow-up bead).",
    ),
  );
}
