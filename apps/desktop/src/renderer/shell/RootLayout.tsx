import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { Activity, Command, PanelRightClose, PanelRightOpen } from "lucide-react";
import { useEffect, useRef } from "react";
import {
  stageDefinitions,
  stageForPathname,
  topBarPlaceholders,
} from "../stages.ts";
import { useShellUiStore } from "../store.ts";
import { ProjectRunningPill, ProjectSwitcher } from "../topbar/ProjectSwitcher.tsx";
import { ActivityPanel } from "./activity-panel.tsx";

export function RootLayout() {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const params = useParams({ strict: false });
  const projectId = typeof params.projectId === "string" ? params.projectId : undefined;
  const activeStage = stageForPathname(pathname);
  const activityPanelOpen = useShellUiStore((state) => state.activityPanelOpen);
  const toggleActivityPanel = useShellUiStore((state) => state.toggleActivityPanel);
  const setActivityPanelOpen = useShellUiStore((state) => state.setActivityPanelOpen);
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
            {topBarPlaceholders.map((item) => {
              const Icon = item.icon;
              return (
                <span className="hh-topbar-pill" key={item.label}>
                  <Icon size={14} strokeWidth={2} />
                  <span>{item.label}</span>
                  <strong>{item.value}</strong>
                </span>
              );
            })}
          </div>

          <div className="hh-topbar-actions">
            <button className="hh-icon-button" type="button" aria-label="Open command palette">
              <Command size={17} strokeWidth={2.1} />
            </button>
            <button
              aria-expanded={activityPanelOpen}
              aria-label={activityPanelOpen ? "Close Activity panel" : "Open Activity panel"}
              className="hh-icon-button"
              onClick={toggleActivityPanel}
              type="button"
            >
              {activityPanelOpen ? (
                <PanelRightClose size={17} strokeWidth={2.1} />
              ) : (
                <PanelRightOpen size={17} strokeWidth={2.1} />
              )}
            </button>
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
          <Outlet />
        </main>
      </section>

      <ActivityPanel
        open={activityPanelOpen}
        onClose={() => setActivityPanelOpen(false)}
        icon={<Activity size={16} strokeWidth={2.1} />}
      />
    </div>
  );
}
