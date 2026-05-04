import { AlertCircle, CheckCircle2, CircleDotDashed, ListChecks, Workflow } from "lucide-react";
import { useState } from "react";
import { useBeadsStageQuery } from "../../data/stage-data.ts";
import { BeadsDagView } from "./BeadsDagView.tsx";
import "./BeadsStage.css";

type BeadView = "list" | "dag";

export function BeadsStage({ projectId }: { readonly projectId: string }) {
  const query = useBeadsStageQuery(projectId);
  const [view, setView] = useState<BeadView>("list");

  if (query.isLoading) {
    return <div className="hh-live-stage hh-live-stage-loading">Loading Mock Flywheel beads...</div>;
  }

  if (query.isError || !query.data) {
    return (
      <div className="hh-live-stage hh-live-stage-error" role="status">
        <AlertCircle size={18} strokeWidth={2.1} />
        <span>Daemon data is not available for this project yet.</span>
      </div>
    );
  }

  const { data } = query;

  return (
    <div className="hh-live-stage hh-beads-stage" data-testid="mock-beads-stage">
      <section className="hh-fixture-strip" aria-label="Mock Flywheel source">
        <span>{data.source.scenarioId}</span>
        <strong>{data.source.fixturesVersion}</strong>
        <span>{data.source.transport}</span>
        <span>{data.total} beads</span>
      </section>

      <section className="hh-beads-summary" aria-label="Bead status summary">
        {data.statusCounts.map((item) => (
          <article className="hh-beads-summary-card" key={item.status}>
            <span>{item.status}</span>
            <strong>{item.count}</strong>
          </article>
        ))}
      </section>

      <section className="hh-beads-view-toggle" role="tablist" aria-label="Bead view">
        <button
          type="button"
          role="tab"
          aria-selected={view === "list"}
          className={`hh-beads-view-tab${view === "list" ? " hh-beads-view-tab-active" : ""}`}
          onClick={() => setView("list")}
          data-testid="beads-view-list"
        >
          <ListChecks size={13} strokeWidth={2.1} />
          <span>List</span>
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={view === "dag"}
          className={`hh-beads-view-tab${view === "dag" ? " hh-beads-view-tab-active" : ""}`}
          onClick={() => setView("dag")}
          data-testid="beads-view-dag"
        >
          <Workflow size={13} strokeWidth={2.1} />
          <span>DAG</span>
        </button>
      </section>

      {view === "list" ? (
        <section className="hh-beads-list" aria-label="Mock Flywheel bead list">
          <div className="hh-stage-section-title">
            <ListChecks size={17} strokeWidth={2.1} />
            <h2>Bead board</h2>
          </div>
          {data.beads.map((bead) => (
            <article className="hh-bead-row" key={bead.id}>
              <div className="hh-bead-row-icon" aria-hidden="true">
                {bead.status === "closed" ? (
                  <CheckCircle2 size={17} strokeWidth={2.1} />
                ) : (
                  <CircleDotDashed size={17} strokeWidth={2.1} />
                )}
              </div>
              <div className="hh-bead-row-main">
                <div className="hh-bead-row-title">
                  <code>{bead.id}</code>
                  <strong>{bead.title}</strong>
                </div>
                <p>{bead.descriptionSnippet || "No description in fixture."}</p>
              </div>
              <div className="hh-bead-row-meta">
                <span>{bead.issueType}</span>
                <strong>P{bead.priority}</strong>
                <span>{bead.status}</span>
              </div>
            </article>
          ))}
        </section>
      ) : (
        <section className="hh-beads-dag-container" aria-label="Mock Flywheel bead DAG">
          <BeadsDagView beads={data.beads} />
        </section>
      )}
    </div>
  );
}
