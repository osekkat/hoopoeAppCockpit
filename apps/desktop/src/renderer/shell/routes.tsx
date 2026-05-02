import { Link, useParams } from "@tanstack/react-router";
import { useEffect } from "react";
import {
  defaultProjectId,
  getStageDefinition,
  projectDisplayName,
  type ShellRouteId,
} from "../stages.ts";
import { useShellUiStore } from "../store.ts";
import { EmptyStage } from "./empty-stage.tsx";
import { StageHeader } from "./stage-header.tsx";

export function ProjectPickerRoute() {
  const rememberProject = useShellUiStore((state) => state.rememberProject);

  return (
    <section className="hh-project-picker" aria-labelledby="project-picker-title">
      <div>
        <span className="hh-stage-kicker">PROJECTS</span>
        <h1 id="project-picker-title">Local demo</h1>
      </div>
      <div className="hh-project-list">
        <Link
          className="hh-project-card"
          onClick={() => rememberProject(defaultProjectId)}
          params={{ projectId: defaultProjectId }}
          to="/$projectId/plan"
        >
          <span className="hh-project-card-title">Local demo</span>
          <span className="hh-project-card-meta">Fixture-backed cockpit</span>
        </Link>
        <button className="hh-project-card hh-project-card-muted" type="button">
          <span className="hh-project-card-title">Add project</span>
          <span className="hh-project-card-meta">Pending daemon pairing</span>
        </button>
      </div>
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
      <EmptyStage stageId={stageId} />
    </section>
  );
}
