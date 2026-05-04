// hp-70yz — Local-clone dirty banner.
//
// Shown above the active stage when the renderer's dirty-state store has
// a non-clean entry for the active project. Per plan.md §7.7:
//   "Local clone has unsaved changes — Hoopoe ignores local edits.
//    Make changes via the VPS (ssh or Cursor/VS Code Remote).
//    [Open clone in Finder]"
//
// hp-hde4 removed the destructive Discard action. The desktop mirror is
// read-only; users can reveal it in Finder to inspect or manually repair
// local edits outside Hoopoe, while Git writes continue to run on the VPS.

import { AlertTriangle, FolderOpen } from "lucide-react";
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
  dirtyState,
  projectId,
  revealInFinder,
}: DirtyBannerViewProps) {
  if (!projectId) return null;
  if (!dirtyState || !dirtyState.dirty) return null;

  const summary = describeDirty(dirtyState);

  function handleReveal() {
    if (!cloneRepoPath) return;
    const reveal = revealInFinder ?? defaultRevealInFinder;
    void Promise.resolve(reveal(cloneRepoPath));
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
          Cursor/VS Code Remote), or repair this mirror outside Hoopoe.{" "}
          <span className="hh-dirty-banner-counts">{summary}</span>
        </p>
      </div>
      {cloneRepoPath !== undefined ? (
        <div className="hh-dirty-banner-actions">
          <button
            className="hh-dirty-banner-reveal"
            data-testid="dirty-banner-reveal"
            onClick={handleReveal}
            type="button"
          >
            <FolderOpen size={14} strokeWidth={2.1} />
            Open in Finder
          </button>
        </div>
      ) : null}
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
