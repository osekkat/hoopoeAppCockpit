import { Link, useParams } from "@tanstack/react-router";
import { FolderPlus, Route, Server } from "lucide-react";
import { useEffect, useState } from "react";
import {
  defaultProjectId,
  getStageDefinition,
  projectDisplayName,
  type ShellRouteId,
} from "../stages.ts";
import { DiagnosticsStage } from "../diagnostics/index.ts";
import { ProjectEntry } from "../projects/index.ts";
import { BeadsStage } from "../stages/Beads/BeadsStage.tsx";
import { PlanningStage } from "../stages/Planning/PlanningStage.tsx";
import { SwarmStage } from "../stages/Swarm/SwarmStage.tsx";
import { StateSurface } from "../state-view/index.ts";
import { useShellUiStore } from "../store.ts";
import { formatRelativeActivation, routeForStage } from "../topbar/project-switcher-model.ts";
import { EmptyStage } from "./empty-stage.tsx";
import { StageHeader } from "./stage-header.tsx";

export function ProjectPickerRoute() {
  const projects = useShellUiStore((state) => state.projects);
  const projectViewStateById = useShellUiStore((state) => state.projectViewStateById);
  const rememberProject = useShellUiStore((state) => state.rememberProject);
  const [entryOpen, setEntryOpen] = useState(false);

  return (
    <section className="hh-project-picker" aria-labelledby="project-picker-title">
      <div>
        <span className="hh-stage-kicker">PROJECTS</span>
        <h1 id="project-picker-title">Local demo</h1>
      </div>
      {projects.length === 0 ? (
        <StateSurface
          variant="empty"
          eyebrow="Projects"
          icon={<FolderPlus size={18} strokeWidth={2.1} />}
          title="No projects yet"
          description="Connect an existing VPS or add a repo so Hoopoe can mirror canonical Flywheel state."
          details={[
            "Existing-VPS onboarding comes first.",
            "Projects need an origin remote before agents can push branches.",
          ]}
          actions={[
            {
              label: "Connect VPS",
              href: "/first-run",
              icon: <Server size={13} strokeWidth={2.1} />,
              variant: "primary",
              testId: "project-picker-connect-vps",
            },
            {
              label: "Add project",
              icon: <Route size={13} strokeWidth={2.1} />,
              onClick: () => setEntryOpen(true),
              testId: "project-picker-empty-add",
            },
          ]}
          testId="project-picker-empty"
        />
      ) : null}
      <div className="hh-project-list">
        {projects.map((project) => {
          const restoredStage =
            projectViewStateById[project.id]?.lastStageId ?? "plan";

          return (
            <Link
              className="hh-project-card"
              key={project.id}
              onClick={() => rememberProject(project.id)}
              params={{ projectId: project.id }}
              to={routeForStage(restoredStage)}
            >
              <span className="hh-project-card-title">{project.name}</span>
              <span className="hh-project-card-meta">
                {project.branch} - {formatRelativeActivation(project.lastActivatedAt)}
              </span>
            </Link>
          );
        })}
        <button
          aria-expanded={entryOpen}
          className="hh-project-card hh-project-card-muted"
          data-testid="project-picker-add"
          onClick={() => setEntryOpen((open) => !open)}
          type="button"
        >
          <span className="hh-project-card-title">{entryOpen ? "Close project entry" : "Add project"}</span>
          <span className="hh-project-card-meta">
            {entryOpen ? "Hide import / create / clone" : "Import, create, or clone"}
          </span>
        </button>
      </div>
      {entryOpen ? (
        <ProjectEntry
          onProjectReady={(input) => {
            // Daemon-side wiring (LilacBear) returns the new id; the
            // store is refreshed from daemon state via TanStack Query
            // invalidation so we just close the panel.
            rememberProject(input.projectId);
            setEntryOpen(false);
          }}
        />
      ) : null}
    </section>
  );
}

export function StageRoute({ stageId }: { readonly stageId: ShellRouteId }) {
  const params = useParams({ strict: false });
  const projectId = typeof params.projectId === "string" ? params.projectId : defaultProjectId;
  const projectName = projectDisplayName(projectId);
  const stage = getStageDefinition(stageId);
  const rememberProject = useShellUiStore((state) => state.rememberProject);
  const rememberStage = useShellUiStore((state) => state.rememberStage);

  useEffect(() => {
    rememberProject(projectId);
    rememberStage(stageId);
  }, [projectId, rememberProject, rememberStage, stageId]);

  return (
    <section className="hh-stage-route">
      <StageHeader
        stage={stage}
        projectName={projectName}
        breadcrumb={stageId === "diag" ? ["Diagnostics"] : [stage.label]}
      />
      {stageId === "plan" ? (
        <PlanningStage projectId={projectId} />
      ) : stageId === "bead" ? (
        <BeadsStage projectId={projectId} />
      ) : stageId === "swarm" ? (
        <SwarmStage projectId={projectId} />
      ) : stageId === "diag" ? (
        <DiagnosticsStage projectId={projectId} />
      ) : (
        <EmptyStage stageId={stageId} />
      )}
    </section>
  );
}
