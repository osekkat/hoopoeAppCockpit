import { useNavigate } from "@tanstack/react-router";
import {
  AlertTriangle,
  Check,
  ChevronDown,
  Circle,
  Eye,
  FolderPlus,
  GitBranch,
  Pause,
  Pin,
  Search,
  Settings,
  Users,
  X,
} from "lucide-react";
import { type KeyboardEvent, useEffect, useMemo, useState } from "react";
import { projectDisplayName } from "../stages.ts";
import { useShellUiStore, type ShellProjectSummary } from "../store.ts";
import {
  formatRelativeActivation,
  isProjectSwarmRunning,
  routeForStage,
  splitProjectSections,
} from "./project-switcher-model.ts";

export function ProjectSwitcher() {
  const navigate = useNavigate();
  const projects = useShellUiStore((state) => state.projects);
  const activeProjectId = useShellUiStore((state) => state.activeProjectId);
  const lastProjectId = useShellUiStore((state) => state.lastProjectId);
  const projectViewStateById = useShellUiStore((state) => state.projectViewStateById);
  const open = useShellUiStore((state) => state.projectSwitcherOpen);
  const searchTerm = useShellUiStore((state) => state.projectSearch);
  const pendingSwitchProjectId = useShellUiStore((state) => state.pendingSwitchProjectId);
  const setOpen = useShellUiStore((state) => state.setProjectSwitcherOpen);
  const setSearch = useShellUiStore((state) => state.setProjectSearch);
  const requestProjectSwitch = useShellUiStore((state) => state.requestProjectSwitch);
  const confirmPendingProjectSwitch = useShellUiStore(
    (state) => state.confirmPendingProjectSwitch,
  );
  const cancelPendingProjectSwitch = useShellUiStore(
    (state) => state.cancelPendingProjectSwitch,
  );
  const viewCurrentProjectSwarm = useShellUiStore((state) => state.viewCurrentProjectSwarm);
  const toggleProjectPin = useShellUiStore((state) => state.toggleProjectPin);
  const [highlightedProjectId, setHighlightedProjectId] = useState<string | null>(null);

  const activeProject =
    projects.find((project) => project.id === activeProjectId) ??
    projects.find((project) => project.id === lastProjectId) ??
    null;
  const pendingProject =
    projects.find((project) => project.id === pendingSwitchProjectId) ?? null;

  const sections = useMemo(
    () => splitProjectSections(projects, searchTerm),
    [projects, searchTerm],
  );
  const visibleProjects = useMemo(
    () => [...sections.pinned, ...sections.recent],
    [sections.pinned, sections.recent],
  );

  useEffect(() => {
    function onKeyDown(event: globalThis.KeyboardEvent) {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "p") {
        event.preventDefault();
        setOpen(true);
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [setOpen]);

  useEffect(() => {
    if (!open) {
      setHighlightedProjectId(null);
      return;
    }
    if (
      highlightedProjectId &&
      visibleProjects.some((project) => project.id === highlightedProjectId)
    ) {
      return;
    }
    setHighlightedProjectId(visibleProjects[0]?.id ?? null);
  }, [highlightedProjectId, open, visibleProjects]);

  function navigateToProject(projectId: string) {
    const stageId = projectViewStateById[projectId]?.lastStageId ?? "plan";
    void navigate({ to: routeForStage(stageId), params: { projectId } });
  }

  function selectProject(projectId: string) {
    const result = requestProjectSwitch(projectId);
    if (result === "switched" || result === "duplicate") {
      navigateToProject(projectId);
    }
  }

  function confirmSwitch(choice: "continue" | "pause-first") {
    const switchedProjectId = confirmPendingProjectSwitch(choice);
    if (switchedProjectId) navigateToProject(switchedProjectId);
  }

  function viewSourceSwarm() {
    const sourceProjectId = activeProject?.id;
    if (!sourceProjectId) return;
    viewCurrentProjectSwarm();
    void navigate({ to: "/$projectId/swarm", params: { projectId: sourceProjectId } });
  }

  function onPopoverKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === "Escape") {
      event.preventDefault();
      setOpen(false);
      cancelPendingProjectSwitch();
      return;
    }
    if (visibleProjects.length === 0) return;
    const currentIndex = Math.max(
      0,
      visibleProjects.findIndex((project) => project.id === highlightedProjectId),
    );
    if (event.key === "ArrowDown") {
      event.preventDefault();
      setHighlightedProjectId(
        visibleProjects[(currentIndex + 1) % visibleProjects.length]?.id ?? null,
      );
    }
    if (event.key === "ArrowUp") {
      event.preventDefault();
      setHighlightedProjectId(
        visibleProjects[
          (currentIndex - 1 + visibleProjects.length) % visibleProjects.length
        ]?.id ?? null,
      );
    }
    if (event.key === "Enter" && highlightedProjectId) {
      event.preventDefault();
      selectProject(highlightedProjectId);
    }
  }

  return (
    <div className="hh-project-switcher" onKeyDown={onPopoverKeyDown}>
      <button
        aria-expanded={open}
        aria-haspopup="dialog"
        className="hh-project-switcher-button"
        onClick={() => setOpen(!open)}
        type="button"
      >
        <span className="hh-project-dot" aria-hidden="true" />
        <span className="hh-project-switcher-copy">
          <strong>{activeProject?.name ?? projectDisplayName(undefined)}</strong>
          <span>
            {activeProject
              ? `${activeProject.branch} - ${activeProject.gitStatus}`
              : "No project selected"}
          </span>
        </span>
        <ChevronDown size={16} strokeWidth={2.1} />
      </button>

      {open ? (
        <div className="hh-project-popover" role="dialog" aria-label="Switch project">
          <label className="hh-project-search">
            <Search size={15} strokeWidth={2.1} aria-hidden="true" />
            <input
              autoFocus
              type="search"
              value={searchTerm}
              onChange={(event) => setSearch(event.currentTarget.value)}
              placeholder="Search projects"
              aria-label="Search projects"
            />
          </label>

          <ProjectSection
            activeProjectId={activeProject?.id ?? null}
            highlightedProjectId={highlightedProjectId}
            label="Pinned"
            projects={sections.pinned}
            onPin={toggleProjectPin}
            onSelect={selectProject}
          />
          <ProjectSection
            activeProjectId={activeProject?.id ?? null}
            highlightedProjectId={highlightedProjectId}
            label="Recent"
            projects={sections.recent}
            onPin={toggleProjectPin}
            onSelect={selectProject}
          />

          <footer className="hh-project-popover-footer">
            <button className="hh-text-button" type="button">
              <FolderPlus size={14} strokeWidth={2.1} />
              <span>Add project</span>
            </button>
            <button className="hh-text-button" type="button">
              <Settings size={14} strokeWidth={2.1} />
              <span>Manage</span>
            </button>
          </footer>
        </div>
      ) : null}

      {pendingProject && activeProject && isProjectSwarmRunning(activeProject) ? (
        <div
          className="hh-project-switch-modal"
          role="dialog"
          aria-modal="true"
          aria-label="Project switch confirmation"
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.stopPropagation();
              cancelPendingProjectSwitch();
            }
          }}
        >
          <div className="hh-project-switch-modal-card">
            <div className="hh-project-switch-modal-title">
              <AlertTriangle size={18} strokeWidth={2.1} />
              <strong>{activeProject.name} still has a running swarm</strong>
            </div>
            <p>
              {activeProject.swarm.activeAgents} agents and{" "}
              {activeProject.swarm.readyBeads} ready beads stay active on the VPS when
              you switch to {pendingProject.name}.
            </p>
            <div className="hh-project-switch-modal-actions">
              <button
                className="hh-text-button"
                onClick={() => confirmSwitch("continue")}
                type="button"
              >
                <Check size={14} strokeWidth={2.1} />
                <span>Continue</span>
              </button>
              <button
                className="hh-text-button"
                onClick={() => confirmSwitch("pause-first")}
                type="button"
              >
                <Pause size={14} strokeWidth={2.1} />
                <span>Pause first</span>
              </button>
              <button className="hh-text-button" onClick={viewSourceSwarm} type="button">
                <Eye size={14} strokeWidth={2.1} />
                <span>View swarm</span>
              </button>
              <button
                aria-label="Cancel project switch"
                className="hh-icon-button"
                onClick={cancelPendingProjectSwitch}
                type="button"
              >
                <X size={15} strokeWidth={2.1} />
              </button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function ProjectSection({
  activeProjectId,
  highlightedProjectId,
  label,
  projects,
  onPin,
  onSelect,
}: {
  readonly activeProjectId: string | null;
  readonly highlightedProjectId: string | null;
  readonly label: string;
  readonly projects: readonly ShellProjectSummary[];
  readonly onPin: (projectId: string) => void;
  readonly onSelect: (projectId: string) => void;
}) {
  if (projects.length === 0) return null;

  return (
    <section className="hh-project-section" aria-label={`${label} projects`}>
      <h2>{label}</h2>
      <div className="hh-project-rows">
        {projects.map((project) => (
          <div
            className="hh-project-row"
            data-active={project.id === activeProjectId}
            data-highlighted={project.id === highlightedProjectId}
            key={project.id}
          >
            <button
              className="hh-project-row-main"
              onClick={() => onSelect(project.id)}
              type="button"
            >
              <span className="hh-project-row-heading">
                <strong>{project.name}</strong>
                <span>{project.slug}</span>
              </span>
              <span className="hh-project-row-meta">
                <GitBranch size={13} strokeWidth={2.1} />
                <span>{project.branch}</span>
                <ProjectBadge label={project.gitStatus} />
                {project.swarm.status === "running" ? (
                  <ProjectBadge label={`${project.swarm.activeAgents} agents running`} />
                ) : null}
              </span>
              <span className="hh-project-row-footer">
                <ToolDot label="VPS" value={project.toolHealth.vps} />
                <ToolDot label="NTM" value={project.toolHealth.ntm} />
                <ToolDot label="Mail" value={project.toolHealth.mail} />
                <span>{formatRelativeActivation(project.lastActivatedAt)}</span>
              </span>
            </button>
            <button
              aria-label={`${project.pinned ? "Unpin" : "Pin"} ${project.name}`}
              className="hh-icon-button hh-project-pin"
              onClick={() => onPin(project.id)}
              type="button"
            >
              <Pin size={14} strokeWidth={project.pinned ? 2.6 : 2} />
            </button>
          </div>
        ))}
      </div>
    </section>
  );
}

function ProjectBadge({ label }: { readonly label: string }) {
  return <span className="hh-project-badge">{label}</span>;
}

function ToolDot({
  label,
  value,
}: {
  readonly label: string;
  readonly value: ShellProjectSummary["toolHealth"]["vps"];
}) {
  return (
    <span className="hh-tool-dot" data-health={value}>
      <Circle size={8} fill="currentColor" strokeWidth={0} aria-hidden="true" />
      <span>{label}</span>
    </span>
  );
}

export function ProjectRunningPill({
  project,
}: {
  readonly project: ShellProjectSummary | null;
}) {
  if (!project || project.swarm.status !== "running") return null;

  return (
    <span className="hh-topbar-pill hh-running-pill">
      <Users size={14} strokeWidth={2} />
      <span>Swarm</span>
      <strong>{project.swarm.activeAgents} running</strong>
    </span>
  );
}
