import {
  Boxes,
  ClipboardList,
  GitBranch,
  PlayCircle,
  ShieldCheck,
  Stethoscope,
  Wrench,
} from "lucide-react";
import type { ReactNode } from "react";
import type { ShellRouteId } from "../stages.ts";
import { StateSurface, type StateSurfaceAction } from "../state-view/index.ts";
import { useShellUiStore } from "../store.ts";

interface StagePanel {
  readonly title: string;
  readonly items: readonly string[];
}

interface EmptyStageCopy {
  readonly title: string;
  readonly description: string;
  readonly icon: ReactNode;
  readonly actions?: readonly StateSurfaceAction[];
  readonly details: readonly string[];
}

const copyByStage: Record<ShellRouteId, EmptyStageCopy> = {
  plan: {
    title: "No planning workspace selected",
    description: "Start with a plain-language goal or import an existing plan artifact.",
    details: ["Candidate models, synthesis, critique, and refinement rounds appear here."],
    icon: <Boxes size={18} strokeWidth={2.1} />,
    actions: [
      {
        label: "Add project",
        href: "/",
        icon: <GitBranch size={13} strokeWidth={2.1} />,
        variant: "primary",
      },
    ],
  },
  bead: {
    title: "No bead workspace selected",
    description: "Lock a plan before converting work into canonical br beads.",
    details: ["The board and DAG stay read from br and bv robot surfaces."],
    icon: <Boxes size={18} strokeWidth={2.1} />,
    actions: [
      {
        label: "Open Planning",
        href: "plan",
        icon: <ClipboardList size={13} strokeWidth={2.1} />,
        variant: "primary",
      },
    ],
  },
  swarm: {
    title: "No active swarm",
    description: "Launch agents after the ready frontier, build queue, and rate-limit warnings are clear.",
    details: ["The default dashboard shows bead and agent state; raw panes stay in Diagnostics."],
    icon: <Boxes size={18} strokeWidth={2.1} />,
    actions: [
      {
        label: "Open Beads",
        href: "bead",
        icon: <PlayCircle size={13} strokeWidth={2.1} />,
        variant: "primary",
      },
    ],
  },
  harden: {
    title: "No hardening session",
    description: "Run review rounds after implementation beads converge.",
    details: ["UBS, findings, health metrics, and convergence status collect here."],
    icon: <ShieldCheck size={18} strokeWidth={2.1} />,
    actions: [
      {
        label: "Open Swarm",
        href: "swarm",
        icon: <PlayCircle size={13} strokeWidth={2.1} />,
        variant: "primary",
      },
    ],
  },
  diag: {
    title: "Diagnostics ready",
    description: "Inspect capabilities, repairs, audit entries, and raw panes from here.",
    details: ["Raw panes require an explicit audited toggle and are never the default swarm UI."],
    icon: <Stethoscope size={18} strokeWidth={2.1} />,
    actions: [
      {
        label: "Reconnect VPS",
        href: "/first-run",
        icon: <Wrench size={13} strokeWidth={2.1} />,
        variant: "primary",
        testId: "diagnostics-reconnect-wizard",
      },
    ],
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
  const openOnboardingTour = useShellUiStore((state) => state.openOnboardingTour);
  const onboardingTourCompletedAt = useShellUiStore((state) => state.onboardingTourCompletedAt);
  const onboardingTourSkippedAt = useShellUiStore((state) => state.onboardingTourSkippedAt);
  const onboardingTourVisited = onboardingTourCompletedAt !== null || onboardingTourSkippedAt !== null;

  return (
    <div className="hh-empty-stage">
      <StateSurface
        variant="empty"
        eyebrow="Workspace"
        icon={copyByStage[stageId].icon}
        title={copyByStage[stageId].title}
        description={copyByStage[stageId].description}
        details={copyByStage[stageId].details}
        {...(copyByStage[stageId].actions ? { actions: copyByStage[stageId].actions } : {})}
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
        <div className="hh-empty-actions">
          <button
            className="hh-text-button"
            data-testid="diagnostics-onboarding-tour"
            onClick={() => openOnboardingTour()}
            type="button"
          >
            {onboardingTourVisited ? "Resume onboarding tour" : "Start onboarding tour"}
          </button>
        </div>
      ) : null}
    </div>
  );
}
