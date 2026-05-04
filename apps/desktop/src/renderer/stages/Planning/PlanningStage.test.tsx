import { expect, test } from "bun:test";
import { renderToStaticMarkup } from "react-dom/server";
import {
  findActivePlan,
  planLockStateLabel,
  planStatusToneClass,
  selectArtifact,
  selectCandidates,
  type PlanArtifact,
  type PlanBundle,
  type PlanHistoryEntry,
  type PlanSummary,
} from "../../data/plan-data.ts";
import { ArtifactRail } from "./ArtifactRail.tsx";
import { ComparativeMatrix } from "./ComparativeMatrix.tsx";
import { HistoryTimeline } from "./HistoryTimeline.tsx";
import { PlanList } from "./PlanList.tsx";

const samplePlans: readonly PlanSummary[] = [
  {
    planId: "plan-001",
    title: "Active demo plan",
    version: 7,
    lockState: "locked",
    lockedAt: "2026-05-03T22:14:00.000Z",
    branch: "main",
    active: true,
    summary: "Locked at v7",
  },
  {
    planId: "plan-002",
    title: "Draft plan",
    version: 2,
    lockState: "draft",
    lockedAt: null,
    branch: "main",
    active: false,
  },
];

const candidateA: PlanArtifact = {
  path: "candidates/a.md",
  kind: "candidate",
  status: "completed",
  label: "Candidate A",
  content: "# A\n\nLine 1\nLine 2\n",
  model: "claude-opus-4-7",
  harness: "claude-code",
  caamAccount: "claude-max-primary",
  latencyMs: 12000,
};

const candidateB: PlanArtifact = {
  path: "candidates/b.md",
  kind: "candidate",
  status: "completed",
  label: "Candidate B",
  content: "# B\n\nLine 1\nLine 2\n",
  model: "gpt-5-pro",
  harness: "codex-cli",
  caamAccount: "chatgpt-pro-primary",
  latencyMs: 18000,
};

const sampleArtifacts: readonly PlanArtifact[] = [
  {
    path: "plan.md",
    kind: "plan",
    status: "completed",
    label: "Locked plan",
    content: "# Plan",
  },
  candidateA,
  candidateB,
  {
    path: "synthesis.md",
    kind: "synthesis",
    status: "running",
    label: "Synthesis",
    content: "# Synthesis",
  },
  {
    path: "comparative-matrix.md",
    kind: "comparative-matrix",
    status: "queued",
    label: "Comparative matrix",
    content: "# Matrix",
  },
];

const sampleHistory: readonly PlanHistoryEntry[] = [
  { ts: "2026-05-01T10:00:00.000Z", kind: "plan_created", actor: "user" },
  {
    ts: "2026-05-01T10:14:00.000Z",
    kind: "candidate_generated",
    actor: "claude-opus-4-7",
    artifact: "candidates/a.md",
    latencyMs: 12000,
  },
  {
    ts: "2026-05-03T22:14:00.000Z",
    kind: "plan_locked",
    actor: "user",
    version: 7,
  },
];

test("findActivePlan returns the plan with active=true; falls back to first", () => {
  expect(findActivePlan(samplePlans)?.planId).toBe("plan-001");
  const noActive = samplePlans.map((plan) => ({ ...plan, active: false }));
  expect(findActivePlan(noActive)?.planId).toBe("plan-001");
  expect(findActivePlan([])).toBeNull();
});

test("selectArtifact prefers the requested path; falls back to plan kind, then first", () => {
  const bundle: PlanBundle = {
    planId: "plan-001",
    title: "x",
    version: 1,
    lockState: "draft",
    lockedAt: null,
    branch: "main",
    active: true,
    artifacts: sampleArtifacts,
    history: sampleHistory,
  };
  expect(selectArtifact(bundle, "synthesis.md")?.kind).toBe("synthesis");
  expect(selectArtifact(bundle, "missing.md")?.kind).toBe("plan");
  const noPlanBundle: PlanBundle = {
    ...bundle,
    artifacts: sampleArtifacts.filter((a) => a.kind !== "plan"),
  };
  expect(selectArtifact(noPlanBundle)?.kind).toBe("candidate");
});

test("selectCandidates filters to candidate-kind artifacts in fixture order", () => {
  const bundle: PlanBundle = {
    planId: "plan-001",
    title: "x",
    version: 1,
    lockState: "draft",
    lockedAt: null,
    branch: "main",
    active: true,
    artifacts: sampleArtifacts,
    history: sampleHistory,
  };
  const candidates = selectCandidates(bundle);
  expect(candidates).toHaveLength(2);
  expect(candidates[0]?.label).toBe("Candidate A");
});

test("plan helpers expose stable status + lock-state strings for CSS bindings", () => {
  expect(planStatusToneClass("running")).toBe("hh-plan-status-running");
  expect(planStatusToneClass("queued")).toBe("hh-plan-status-queued");
  expect(planStatusToneClass("failed")).toBe("hh-plan-status-failed");
  expect(planLockStateLabel("locked")).toBe("LOCKED");
  expect(planLockStateLabel("draft")).toBe("DRAFT");
});

test("PlanList renders all plans with the active row marked aria-current", () => {
  const markup = renderToStaticMarkup(
    <PlanList plans={samplePlans} activePlanId="plan-001" onSelectPlan={() => undefined} />,
  );
  expect(markup).toContain("Active demo plan");
  expect(markup).toContain("Draft plan");
  expect(markup).toContain("hh-plan-list-row-active");
  expect(markup).toContain("v7");
  expect(markup).toContain("LOCKED");
  expect(markup).toContain("DRAFT");
});

test("ArtifactRail renders status pills, the compare CTA when ≥2 candidates, and a history button", () => {
  const markup = renderToStaticMarkup(
    <ArtifactRail
      artifacts={sampleArtifacts}
      selectedPath="plan.md"
      onSelectArtifact={() => undefined}
      onSelectComparativeMatrix={() => undefined}
      onSelectHistory={() => undefined}
      historyCount={9}
    />,
  );

  expect(markup).toContain("Side-by-side compare");
  expect(markup).toContain("2 candidates");
  expect(markup).toContain("Plan history");
  expect(markup).toContain("9 events");
  expect(markup).toContain("hh-plan-status-running");
  expect(markup).toContain("hh-plan-status-queued");
  expect(markup).toContain("hh-plan-status-completed");
  expect(markup).toContain("hh-plan-rail-row-active");
});

test("ArtifactRail hides the compare CTA when fewer than 2 candidates exist", () => {
  const oneCandidate = sampleArtifacts.filter((a) => a.path !== "candidates/b.md");
  const markup = renderToStaticMarkup(
    <ArtifactRail
      artifacts={oneCandidate}
      selectedPath="plan.md"
      onSelectArtifact={() => undefined}
      onSelectComparativeMatrix={() => undefined}
      onSelectHistory={() => undefined}
      historyCount={0}
    />,
  );
  expect(markup).not.toContain("Side-by-side compare");
});

test("ComparativeMatrix renders one column per candidate with model + harness metadata", () => {
  const markup = renderToStaticMarkup(<ComparativeMatrix candidates={[candidateA, candidateB]} />);
  expect(markup).toContain("Candidate A");
  expect(markup).toContain("Candidate B");
  expect(markup).toContain("claude-opus-4-7");
  expect(markup).toContain("gpt-5-pro");
  expect(markup).toContain("Sync scroll");
  expect(markup).toContain("Highlight diff");
  expect(markup).toContain('data-candidate-count="2"');
});

test("ComparativeMatrix renders an empty state when fewer than 2 candidates are passed", () => {
  const markup = renderToStaticMarkup(<ComparativeMatrix candidates={[candidateA]} />);
  expect(markup).toContain("Need at least 2 candidates");
  expect(markup).not.toContain("data-candidate-count");
});

test("HistoryTimeline orders events newest-first and labels well-known kinds", () => {
  const markup = renderToStaticMarkup(<HistoryTimeline history={sampleHistory} />);
  expect(markup).toContain("Plan history");
  expect(markup).toContain("3 events");
  expect(markup).toContain("Plan locked");
  expect(markup).toContain("Candidate generated");
  expect(markup).toContain("Plan created");

  const lockedIdx = markup.indexOf("Plan locked");
  const createdIdx = markup.indexOf("Plan created");
  expect(lockedIdx).toBeGreaterThan(0);
  expect(createdIdx).toBeGreaterThan(lockedIdx);
});

test("HistoryTimeline empty state renders a status hint when no events exist", () => {
  const markup = renderToStaticMarkup(<HistoryTimeline history={[]} />);
  expect(markup).toContain("No plan history yet.");
});
