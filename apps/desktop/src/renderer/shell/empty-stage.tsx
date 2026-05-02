import type { ShellRouteId } from "../stages.ts";

interface StagePanel {
  readonly title: string;
  readonly items: readonly string[];
}

const panelsByStage: Record<ShellRouteId, readonly StagePanel[]> = {
  plan: [
    { title: "Drafts", items: ["Plan intake", "Candidate models", "Critique rounds"] },
    { title: "Artifacts", items: ["Comparative matrix", "Synthesis", "Locked plan"] },
  ],
  bead: [
    { title: "Board", items: ["Ready", "In progress", "Review"] },
    { title: "Graph", items: ["Dependencies", "Unblocks", "Traceability"] },
  ],
  swarm: [
    { title: "Bead board", items: ["Claims", "Priority", "Blocked"] },
    { title: "Agent grid", items: ["Harness", "Account", "Current bead"] },
  ],
  harden: [
    { title: "Review rounds", items: ["UBS", "Fresh eyes", "Convergence"] },
    { title: "Findings", items: ["Triaged", "Fix now", "Deferred"] },
  ],
  diag: [
    { title: "Capabilities", items: ["Tools", "Versions", "Fallbacks"] },
    { title: "Audit", items: ["Actions", "Approvals", "Exports"] },
  ],
};

export function EmptyStage({ stageId }: { readonly stageId: ShellRouteId }) {
  return (
    <section className="hh-empty-grid" aria-label="Stage workspace">
      {panelsByStage[stageId].map((panel) => (
        <article className="hh-empty-panel" key={panel.title}>
          <h2>{panel.title}</h2>
          <div className="hh-empty-panel-list">
            {panel.items.map((item) => (
              <span key={item}>{item}</span>
            ))}
          </div>
        </article>
      ))}
    </section>
  );
}
