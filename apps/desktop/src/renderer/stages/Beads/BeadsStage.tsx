import {
  CheckCircle2,
  CircleDotDashed,
  ClipboardList,
  ListChecks,
  RotateCcw,
  Wrench,
  Workflow,
} from "lucide-react";
import { useState } from "react";
import { useBeadsStageQuery } from "../../data/stage-data.ts";
import { StateSurface } from "../../state-view/index.ts";
import { BeadsDagView } from "./BeadsDagView.tsx";
import "./BeadsStage.css";

type BeadView = "list" | "dag";

export function BeadsStage({ projectId }: { readonly projectId: string }) {
  const query = useBeadsStageQuery(projectId);
  const [view, setView] = useState<BeadView>("list");

  if (query.isLoading) {
    return (
      <StateSurface
        variant="loading"
        eyebrow="Beads"
        title="Loading beads"
        description="Fetching canonical br state and graph-ready bead summaries."
        details={["br issue list", "Status counts", "DAG-ready dependencies"]}
        testId="beads-stage-loading"
      />
    );
  }

  if (query.isError || !query.data) {
    return (
      <StateSurface
        variant="error"
        eyebrow="Beads"
        title="Bead data unavailable"
        description="Reconnect the daemon or refresh canonical br state before editing the board."
        details={["Renderer cache is not canonical.", "Bead truth must come from br and bv robot output."]}
        actions={[
          {
            label: "Open Diagnostics",
            href: `/${projectId}/diag`,
            icon: <Wrench size={13} strokeWidth={2.1} />,
            variant: "primary",
          },
          {
            label: "Reconnect VPS",
            href: "/first-run",
            icon: <RotateCcw size={13} strokeWidth={2.1} />,
          },
        ]}
        testId="beads-stage-error"
      />
    );
  }

  const { data } = query;
  const hasBeads = data.beads.length > 0;

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
          {hasBeads ? data.beads.map((bead) => (
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
          )) : (
            <StateSurface
              variant="empty"
              density="compact"
              title="No beads yet"
              description="Convert a locked plan into beads or import canonical br state."
              details={["Ready work appears after br has unblocked issues."]}
              actions={[
                {
                  label: "Open Planning",
                  href: `/${projectId}/plan`,
                  icon: <ClipboardList size={13} strokeWidth={2.1} />,
                  variant: "primary",
                },
                {
                  label: "Open Diagnostics",
                  href: `/${projectId}/diag`,
                  icon: <Wrench size={13} strokeWidth={2.1} />,
                },
              ]}
              testId="beads-list-empty"
            />
          )}
        </section>
      ) : (
        <section className="hh-beads-dag-container" aria-label="Mock Flywheel bead DAG">
          {hasBeads ? (
            <BeadsDagView beads={data.beads} />
          ) : (
            <StateSurface
              variant="empty"
              density="compact"
              title="No DAG to render"
              description="DAG view appears after br exposes beads and bv graph intelligence."
              details={["The renderer does not compute graph truth when bv robot data is absent."]}
              actions={[
                {
                  label: "Open Planning",
                  href: `/${projectId}/plan`,
                  icon: <ClipboardList size={13} strokeWidth={2.1} />,
                  variant: "primary",
                },
              ]}
              testId="beads-dag-empty"
            />
          )}
        </section>
      )}
    </div>
  );
}
