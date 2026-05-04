import { Boxes, ShieldCheck, Stethoscope } from "lucide-react";
import type { ReactNode } from "react";
import type { ShellRouteId } from "../stages.ts";
import { StateSurface } from "../state-view/index.ts";

interface StagePanel {
  readonly title: string;
  readonly items: readonly string[];
}

interface EmptyStageCopy {
  readonly title: string;
  readonly description: string;
  readonly icon: ReactNode;
}

const copyByStage: Record<ShellRouteId, EmptyStageCopy> = {
  plan: {
    title: "No planning workspace selected",
    description: "Create or import a project, then start with a plan intake.",
    icon: <Boxes size={18} strokeWidth={2.1} />,
  },
  bead: {
    title: "No bead workspace selected",
    description: "Lock a plan before converting work into canonical br beads.",
    icon: <Boxes size={18} strokeWidth={2.1} />,
  },
  swarm: {
    title: "No active swarm",
    description: "Launch agents after the ready frontier is curated.",
    icon: <Boxes size={18} strokeWidth={2.1} />,
  },
  harden: {
    title: "No hardening session",
    description: "Run review rounds after implementation beads converge.",
    icon: <ShieldCheck size={18} strokeWidth={2.1} />,
  },
  diag: {
    title: "Diagnostics ready",
    description: "Inspect capabilities, repairs, audit entries, and raw panes from here.",
    icon: <Stethoscope size={18} strokeWidth={2.1} />,
  },
};

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
    <div className="hh-empty-stage">
      <StateSurface
        variant="empty"
        eyebrow="Workspace"
        icon={copyByStage[stageId].icon}
        title={copyByStage[stageId].title}
        description={copyByStage[stageId].description}
        testId={`empty-stage-${stageId}`}
      />
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
      {stageId === "diag" ? (
        <a
          className="hh-wizard-secondary"
          data-testid="diagnostics-reconnect-wizard"
          href="/first-run"
        >
          Reconnect VPS
        </a>
      ) : null}
    </div>
  );
}
