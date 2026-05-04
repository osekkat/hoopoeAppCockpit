import { AlertCircle, FileText, Lock } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import {
  findActivePlan,
  planLockStateLabel,
  selectArtifact,
  selectCandidates,
  usePlanStageQuery,
} from "../../data/plan-data.ts";
import { ArtifactRail } from "./ArtifactRail.tsx";
import { ComparativeMatrix } from "./ComparativeMatrix.tsx";
import { HistoryTimeline } from "./HistoryTimeline.tsx";
import { PlanEditor } from "./PlanEditor.tsx";
import { PlanList } from "./PlanList.tsx";
import "./PlanningStage.css";

type CenterView =
  | { readonly kind: "artifact"; readonly path: string }
  | { readonly kind: "compare" }
  | { readonly kind: "history" };

export function PlanningStage({ projectId }: { readonly projectId: string }) {
  const query = usePlanStageQuery(projectId);
  const [activePlanId, setActivePlanId] = useState<string | null>(null);
  const [centerView, setCenterView] = useState<CenterView>({
    kind: "artifact",
    path: "plan.md",
  });
  const [editedContent, setEditedContent] = useState<Record<string, string>>({});

  const data = query.data;

  useEffect(() => {
    if (!data) return;
    if (activePlanId && data.bundles[activePlanId]) return;
    const fallback = findActivePlan(data.plans);
    if (fallback) setActivePlanId(fallback.planId);
  }, [data, activePlanId]);

  const activePlan = useMemo(() => {
    if (!data || !activePlanId) return null;
    return data.plans.find((p) => p.planId === activePlanId) ?? null;
  }, [data, activePlanId]);

  const activeBundle = useMemo(() => {
    if (!data || !activePlanId) return undefined;
    return data.bundles[activePlanId];
  }, [data, activePlanId]);

  const selectedArtifact = useMemo(() => {
    if (centerView.kind !== "artifact") return null;
    return selectArtifact(activeBundle, centerView.path);
  }, [activeBundle, centerView]);

  const candidates = useMemo(() => selectCandidates(activeBundle), [activeBundle]);

  if (query.isLoading) {
    return (
      <div className="hh-live-stage hh-live-stage-loading">Loading Mock Flywheel plans...</div>
    );
  }

  if (query.isError || !data) {
    return (
      <div className="hh-live-stage hh-live-stage-error" role="status">
        <AlertCircle size={18} strokeWidth={2.1} />
        <span>Daemon plan data is not available for this project yet.</span>
      </div>
    );
  }

  if (!activePlan || !activeBundle) {
    return (
      <div className="hh-live-stage hh-plan-stage-empty" role="status">
        <FileText size={18} strokeWidth={2.1} />
        <span>No plans yet. Start by capturing a rough idea.</span>
      </div>
    );
  }

  const isLocked = activePlan.lockState === "locked";
  const editorContent = selectedArtifact
    ? editedContent[selectedArtifact.path] ?? selectedArtifact.content
    : "";

  function handleSelectPlan(planId: string) {
    setActivePlanId(planId);
    setCenterView({ kind: "artifact", path: "plan.md" });
    setEditedContent({});
  }

  function handleSelectArtifact(path: string) {
    setCenterView({ kind: "artifact", path });
  }

  function handleSelectCompare() {
    setCenterView({ kind: "compare" });
  }

  function handleSelectHistory() {
    setCenterView({ kind: "history" });
  }

  function handleSave(next: string) {
    if (!selectedArtifact) return;
    setEditedContent((prev) => ({ ...prev, [selectedArtifact.path]: next }));
  }

  return (
    <div className="hh-live-stage hh-plan-stage" data-testid="planning-stage">
      <section className="hh-fixture-strip" aria-label="Mock Flywheel source">
        <span>{data.source.scenarioId}</span>
        <strong>{data.source.fixturesVersion}</strong>
        <span>{data.source.transport}</span>
        <span>{data.plans.length} plans</span>
      </section>

      <header className="hh-plan-stage-banner">
        <div className="hh-plan-stage-banner-main">
          <span className="hh-plan-stage-banner-kicker">ACTIVE PLAN</span>
          <h2 className="hh-plan-stage-banner-title">{activePlan.title}</h2>
        </div>
        <div className="hh-plan-stage-banner-meta">
          <span className={`hh-plan-stage-state hh-plan-state-${activePlan.lockState}`}>
            {isLocked ? <Lock size={11} strokeWidth={2.1} /> : null}
            <span>{planLockStateLabel(activePlan.lockState)}</span>
          </span>
          <span className="hh-plan-stage-version">v{activePlan.version}</span>
          <span className="hh-plan-stage-branch">branch: {activePlan.branch}</span>
        </div>
      </header>

      <div className="hh-plan-stage-grid">
        <div className="hh-plan-stage-left">
          <PlanList
            plans={data.plans}
            activePlanId={activePlan.planId}
            onSelectPlan={handleSelectPlan}
          />
        </div>

        <div className="hh-plan-stage-center">
          {centerView.kind === "compare" ? (
            <ComparativeMatrix candidates={candidates} />
          ) : centerView.kind === "history" ? (
            <HistoryTimeline history={activeBundle.history} />
          ) : selectedArtifact ? (
            <PlanEditor
              key={`${activePlan.planId}:${selectedArtifact.path}`}
              artifactPath={selectedArtifact.path}
              initialContent={editorContent}
              readOnly={isLocked}
              readOnlyReason={
                isLocked ? `Locked at v${activePlan.version} — edits create a new version` : undefined
              }
              onSave={handleSave}
            />
          ) : (
            <div className="hh-plan-stage-empty" role="status">
              <FileText size={18} strokeWidth={2.1} />
              <span>Select an artifact to view it.</span>
            </div>
          )}
        </div>

        <div className="hh-plan-stage-right">
          <ArtifactRail
            artifacts={activeBundle.artifacts}
            selectedPath={centerView.kind === "artifact" ? centerView.path : null}
            comparativeMatrixActive={centerView.kind === "compare"}
            historyActive={centerView.kind === "history"}
            onSelectArtifact={handleSelectArtifact}
            onSelectComparativeMatrix={handleSelectCompare}
            onSelectHistory={handleSelectHistory}
            historyCount={activeBundle.history.length}
          />
        </div>
      </div>
    </div>
  );
}
