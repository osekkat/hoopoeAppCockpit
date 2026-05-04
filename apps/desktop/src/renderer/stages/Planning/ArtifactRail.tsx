import {
  Columns3,
  FileText,
  GitCompare,
  History,
  Layers,
  Lightbulb,
  ListTree,
  RefreshCcw,
  Sparkles,
  Sprout,
} from "lucide-react";
import {
  planStatusToneClass,
  type PlanArtifact,
  type PlanArtifactKind,
} from "../../data/plan-data.ts";

const ARTIFACT_KIND_ICON: Record<
  PlanArtifactKind,
  React.ComponentType<{ readonly size?: number; readonly strokeWidth?: number }>
> = {
  plan: Layers,
  "rough-idea": Sprout,
  candidate: Sparkles,
  "comparative-matrix": Columns3,
  synthesis: GitCompare,
  "fresh-eyes-critique": Lightbulb,
  "refinement-round": RefreshCcw,
  "unresolved-decisions": ListTree,
};

const SECTION_ORDER: { readonly title: string; readonly kinds: readonly PlanArtifactKind[] }[] = [
  { title: "Source", kinds: ["plan", "rough-idea"] },
  { title: "Candidates", kinds: ["candidate"] },
  { title: "Comparison", kinds: ["comparative-matrix", "synthesis", "fresh-eyes-critique"] },
  { title: "Refinement", kinds: ["refinement-round", "unresolved-decisions"] },
];

interface ArtifactRailProps {
  readonly artifacts: readonly PlanArtifact[];
  readonly selectedPath: string | null;
  readonly comparativeMatrixActive?: boolean;
  readonly historyActive?: boolean;
  readonly onSelectArtifact: (path: string) => void;
  readonly onSelectComparativeMatrix?: () => void;
  readonly onSelectHistory?: () => void;
  readonly historyCount: number;
}

export function ArtifactRail({
  artifacts,
  selectedPath,
  comparativeMatrixActive,
  historyActive,
  onSelectArtifact,
  onSelectComparativeMatrix,
  onSelectHistory,
  historyCount,
}: ArtifactRailProps) {
  const byKind = new Map<PlanArtifactKind, PlanArtifact[]>();
  for (const artifact of artifacts) {
    const list = byKind.get(artifact.kind) ?? [];
    list.push(artifact);
    byKind.set(artifact.kind, list);
  }

  const candidateCount = byKind.get("candidate")?.length ?? 0;

  return (
    <aside className="hh-plan-rail" aria-label="Plan artifact rail">
      <header className="hh-plan-rail-header">
        <span className="hh-plan-rail-kicker">ARTIFACTS</span>
      </header>

      {onSelectComparativeMatrix && candidateCount >= 2 ? (
        <button
          type="button"
          className={`hh-plan-rail-special${
            comparativeMatrixActive ? " hh-plan-rail-special-active" : ""
          }`}
          onClick={onSelectComparativeMatrix}
          data-testid="artifact-rail-compare"
        >
          <Columns3 size={13} strokeWidth={2.1} />
          <span className="hh-plan-rail-special-label">Side-by-side compare</span>
          <span className="hh-plan-rail-special-meta">{candidateCount} candidates</span>
        </button>
      ) : null}

      {SECTION_ORDER.map((section) => {
        const items = section.kinds.flatMap((kind) => byKind.get(kind) ?? []);
        if (items.length === 0) return null;
        return (
          <section key={section.title} className="hh-plan-rail-section">
            <h3 className="hh-plan-rail-section-title">{section.title}</h3>
            <ul className="hh-plan-rail-list">
              {items.map((artifact) => {
                const Icon = ARTIFACT_KIND_ICON[artifact.kind] ?? FileText;
                const isSelected =
                  artifact.path === selectedPath && !comparativeMatrixActive && !historyActive;
                return (
                  <li key={artifact.path}>
                    <button
                      type="button"
                      className={`hh-plan-rail-row${
                        isSelected ? " hh-plan-rail-row-active" : ""
                      }`}
                      onClick={() => onSelectArtifact(artifact.path)}
                      aria-current={isSelected ? "true" : undefined}
                      data-testid={`artifact-rail-row-${artifact.path}`}
                    >
                      <span className="hh-plan-rail-row-icon" aria-hidden="true">
                        <Icon size={12} strokeWidth={2.1} />
                      </span>
                      <span className="hh-plan-rail-row-label">{artifact.label}</span>
                      <span
                        className={`hh-plan-rail-status ${planStatusToneClass(artifact.status)}`}
                        aria-label={`Status: ${artifact.status}`}
                      >
                        {artifact.status}
                      </span>
                    </button>
                  </li>
                );
              })}
            </ul>
          </section>
        );
      })}

      {onSelectHistory ? (
        <button
          type="button"
          className={`hh-plan-rail-special${
            historyActive ? " hh-plan-rail-special-active" : ""
          }`}
          onClick={onSelectHistory}
          data-testid="artifact-rail-history"
        >
          <History size={13} strokeWidth={2.1} />
          <span className="hh-plan-rail-special-label">Plan history</span>
          <span className="hh-plan-rail-special-meta">{historyCount} events</span>
        </button>
      ) : null}
    </aside>
  );
}
