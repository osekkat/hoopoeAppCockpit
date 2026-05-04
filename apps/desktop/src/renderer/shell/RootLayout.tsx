import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { Activity, Command, PanelRightClose, PanelRightOpen } from "lucide-react";
import { useCallback, useEffect, useRef } from "react";
import { ActivityDrawer, useActivityStore } from "../activity/index.ts";
import { DirtyBanner, DirtyBannerSubscription } from "../clone/index.ts";
import { ErrorUxRoot } from "../error-ux/index.ts";
import "../error-ux/error-ux.css";
import {
  stageDefinitions,
  stageForPathname,
} from "../stages.ts";
import { useShellUiStore } from "../store.ts";
import {
  BeadsPulsePill,
  CodeHealthPill,
  ProjectRunningPill,
  ProjectSwitcher,
  SubscriptionPill,
  SwarmStatePill,
  ToolHealthPill,
} from "../topbar/index.ts";
import "../topbar/topbar-pills.css";
import { CommandPaletteHost } from "./command-palette/CommandPaletteHost.tsx";

export function RootLayout() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const params = useParams({ strict: false });
  const projectId = typeof params.projectId === "string" ? params.projectId : undefined;
  const activeStage = stageForPathname(pathname);
  const activityPanelOpen = useShellUiStore((state) => state.activityPanelOpen);
  const toggleActivityPanel = useShellUiStore((state) => state.toggleActivityPanel);
  const setActivityPanelOpen = useShellUiStore((state) => state.setActivityPanelOpen);
  const commandPaletteOpen = useShellUiStore((state) => state.commandPaletteOpen);
  const setCommandPaletteOpen = useShellUiStore((state) => state.setCommandPaletteOpen);
  const toggleCommandPalette = useShellUiStore((state) => state.toggleCommandPalette);
  const rememberStageScroll = useShellUiStore((state) => state.rememberStageScroll);
  const projectViewStateById = useShellUiStore((state) => state.projectViewStateById);
  const projects = useShellUiStore((state) => state.projects);
  const activeProjectId = useShellUiStore((state) => state.activeProjectId);
  const stageContentRef = useRef<HTMLElement | null>(null);
  const scrollSaveTimerRef = useRef<number | null>(null);
  const topbarProjectId = projectId ?? activeProjectId ?? undefined;
  const activeProject =
    projects.find((project) => project.id === topbarProjectId) ?? null;

  useEffect(() => {
    if (!projectId || !activeStage) return;
    const scrollY =
      projectViewStateById[projectId]?.stageScrollYByStage[activeStage.id] ?? 0;
    stageContentRef.current?.scrollTo({ top: scrollY });
  }, [activeStage, projectId, projectViewStateById, pathname]);

  useEffect(() => {
    return () => {
      if (scrollSaveTimerRef.current !== null) {
        window.clearTimeout(scrollSaveTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      const modPressed = event.metaKey || event.ctrlKey;
      const isPaletteShortcut =
        modPressed && (event.key === "k" || event.key === "K");
      const isFallbackShortcut =
        modPressed && event.shiftKey && (event.key === "p" || event.key === "P");
      // hp-1r4: ⌘/ (or Ctrl+/) toggles the Activity drawer.
      const isActivityShortcut = modPressed && event.key === "/";

      if (isActivityShortcut) {
        event.preventDefault();
        toggleActivityPanel();
        return;
      }
      if (isPaletteShortcut || isFallbackShortcut) {
        event.preventDefault();
        toggleCommandPalette();
      }
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [toggleActivityPanel, toggleCommandPalette]);

  const closeCommandPalette = useCallback(() => {
    setCommandPaletteOpen(false);
  }, [setCommandPaletteOpen]);

  return (
    <div className="hh-shell" data-activity-open={activityPanelOpen}>
      <aside className="hh-stage-rail" aria-label="Hoopoe stages">
        <div className="hh-brand-lockup">
          <div className="hh-brand-mark" aria-hidden="true">
            H
          </div>
          <div>
            <div className="hh-brand-name">Hoopoe</div>
            <div className="hh-brand-subtitle">Cockpit</div>
          </div>
        </div>

        <nav className="hh-stage-nav" aria-label="Stage routes">
          {stageDefinitions.map((stage) => {
            const Icon = stage.icon;
            const active = activeStage?.id === stage.id;

            if (!projectId) {
              return (
                <span className="hh-stage-link hh-stage-link-disabled" key={stage.id}>
                  <Icon size={18} strokeWidth={2.1} />
                  <span>{stage.label}</span>
                </span>
              );
            }

            return (
              <Link
                activeOptions={{ exact: true }}
                className="hh-stage-link"
                data-active={active}
                key={stage.id}
                params={{ projectId }}
                to={stage.routeTo}
              >
                <Icon size={18} strokeWidth={2.1} />
                <span>{stage.label}</span>
              </Link>
            );
          })}
        </nav>
      </aside>

      <section className="hh-main-region">
        <header className="hh-topbar">
          <ProjectSwitcher />

          <div className="hh-topbar-status" aria-label="Project status">
            <ProjectRunningPill project={activeProject} />
            <ToolHealthPill project={activeProject} />
            <SwarmStatePill project={activeProject} />
            <BeadsPulsePill project={activeProject} />
            <CodeHealthPill project={activeProject} />
            <SubscriptionPill project={activeProject} />
          </div>

          <div className="hh-topbar-actions">
            <button
              aria-expanded={commandPaletteOpen}
              aria-haspopup="dialog"
              aria-label="Open command palette"
              className="hh-icon-button"
              onClick={toggleCommandPalette}
              type="button"
            >
              <Command size={17} strokeWidth={2.1} />
            </button>
            <ActivityToggleButton
              open={activityPanelOpen}
              onClick={toggleActivityPanel}
            />
          </div>
        </header>

        <main
          className="hh-stage-content"
          ref={stageContentRef}
          onScroll={(event) => {
            if (projectId && activeStage) {
              const scrollY = event.currentTarget.scrollTop;
              const stageId = activeStage.id;
              if (scrollSaveTimerRef.current !== null) {
                window.clearTimeout(scrollSaveTimerRef.current);
              }
              scrollSaveTimerRef.current = window.setTimeout(() => {
                rememberStageScroll(projectId, stageId, scrollY);
                scrollSaveTimerRef.current = null;
              }, 250);
            }
          }}
        >
          <DirtyBanner
            projectId={topbarProjectId ?? null}
            {...(activeProject?.rootPath ? { cloneRepoPath: activeProject.rootPath } : {})}
          />
          <Outlet />
        </main>
      </section>

      <ActivityDrawer
        open={activityPanelOpen}
        onClose={() => setActivityPanelOpen(false)}
        icon={<Activity size={16} strokeWidth={2.1} />}
      />

      <CommandPaletteHost
        open={commandPaletteOpen}
        pathname={pathname}
        projectId={projectId}
        onClose={closeCommandPalette}
      />

      <ErrorUxRoot />
      <DirtyBannerSubscription />
    </div>
  );
}

interface ActivityToggleButtonProps {
  readonly open: boolean;
  readonly onClick: () => void;
}

/** hp-1r4: top-right Activity toggle. Shows the unread count from the
 *  activity store as a persistent badge so the user can see urgent
 *  events from any stage without opening the drawer. */
function ActivityToggleButton({ open, onClick }: ActivityToggleButtonProps) {
  const unreadCount = useActivityStore((state) => state.unreadCount);
  const Icon = open ? PanelRightClose : PanelRightOpen;
  return (
    <button
      aria-expanded={open}
      aria-label={open ? "Close Activity drawer" : `Open Activity drawer${unreadCount > 0 ? ` (${unreadCount} unread)` : ""}`}
      className="hh-icon-button hh-activity-toggle"
      data-unread={unreadCount > 0}
      onClick={onClick}
      type="button"
    >
      <Icon size={17} strokeWidth={2.1} />
      {unreadCount > 0 && !open && (
        <span aria-hidden="true" className="hh-activity-toggle-badge">
          {unreadCount > 99 ? "99+" : unreadCount}
        </span>
      )}
    </button>
  );
}
