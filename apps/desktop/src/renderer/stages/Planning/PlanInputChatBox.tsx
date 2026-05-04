// hp-z5r — Plan-input chat box (§7.1 empty-state for a project with no plans).
//
// One textarea for the user's description + three control rows:
//   1. Primary model picker (radio).
//   2. Competing-candidate picker (checkboxes, max 3).
//   3. "Let Hoopoe choose" toggle + clarify/first-shot toggle.
//
// "Generate plans" submit projects the draft into a PlanGenerationRequest
// via compileDraft() and fires the optional onGenerate callback. The
// renderer doesn't actually run the planning pipeline yet — that's
// Phase 5 wire-up (hp-vh7); this commit ships the load-bearing input
// shell so downstream agents can wire onGenerate to the real RPC.

import { useMemo, useState } from "react";
import { CheckCircle2, FileText, MessageCircleQuestion, Send, Sparkles } from "lucide-react";
import {
  EMPTY_PLAN_INPUT_DRAFT,
  MAX_COMPETING_CANDIDATES,
  PLAN_MODELS,
  compileDraft,
  emptyPlanModelAvailability,
  findPlanModel,
  type CompileDraftResult,
  type ModelKickoffMode,
  type PlanGenerationRequest,
  type PlanInputDraft,
  type PlanInputValidationIssue,
  type PlanModelAvailability,
  type PlanModelSelection,
  type PlanProviderId,
} from "./plan-input-models.ts";

export interface PlanInputChatBoxProps {
  /** Per-provider availability; consumed from CAAM via daemon RPC in
   *  production. Tests + Storybook pass a fixed map. */
  readonly availability?: PlanModelAvailability;
  /** True when the project has a populated repository — enables the
   *  "existing-codebase context bundle" sub-mode (§7.1). */
  readonly projectHasCodebase?: boolean;
  /** Optional initial draft (resume from a prior session). */
  readonly initialDraft?: PlanInputDraft;
  /** Called on Generate-plans click with the validated request shape. */
  readonly onGenerate?: (request: PlanGenerationRequest) => void;
  /** Called when the form fails validation. Provides the issue list so
   *  containers can surface a banner instead of just the inline errors. */
  readonly onValidationFailure?: (issues: readonly PlanInputValidationIssue[]) => void;
}

export function PlanInputChatBox({
  availability = emptyPlanModelAvailability(),
  initialDraft = EMPTY_PLAN_INPUT_DRAFT,
  onGenerate,
  onValidationFailure,
  projectHasCodebase = false,
}: PlanInputChatBoxProps) {
  const [draft, setDraft] = useState<PlanInputDraft>(initialDraft);
  const [submitIssues, setSubmitIssues] = useState<readonly PlanInputValidationIssue[]>([]);

  const anyAvailable = useMemo(
    () => Object.values(availability).some((v) => v),
    [availability],
  );

  function update(patch: Partial<PlanInputDraft>): void {
    setDraft((prev) => ({ ...prev, ...patch }));
    if (submitIssues.length > 0) setSubmitIssues([]);
  }

  function updateSelection(patch: Partial<PlanModelSelection>): void {
    setDraft((prev) => ({ ...prev, selection: { ...prev.selection, ...patch } }));
    if (submitIssues.length > 0) setSubmitIssues([]);
  }

  function togglePrimary(id: PlanProviderId): void {
    if (draft.hoopoeChoosesModels) update({ hoopoeChoosesModels: false });
    updateSelection({ primary: id });
    // Drop the new primary from competition if it was there.
    if (draft.selection.competition.includes(id)) {
      updateSelection({ competition: draft.selection.competition.filter((c) => c !== id) });
    }
  }

  function toggleCompetitor(id: PlanProviderId): void {
    if (draft.hoopoeChoosesModels) update({ hoopoeChoosesModels: false });
    if (id === draft.selection.primary) return; // primary can't be competition
    const current = draft.selection.competition;
    if (current.includes(id)) {
      updateSelection({ competition: current.filter((c) => c !== id) });
      return;
    }
    if (current.length >= MAX_COMPETING_CANDIDATES) {
      // Replace the oldest candidate (FIFO) when at cap.
      updateSelection({ competition: [...current.slice(1), id] });
      return;
    }
    updateSelection({ competition: [...current, id] });
  }

  function handleSubmit(event: React.FormEvent): void {
    event.preventDefault();
    const result: CompileDraftResult = compileDraft({
      draft,
      availability,
      projectHasCodebase,
    });
    if (!result.ok) {
      setSubmitIssues(result.issues);
      onValidationFailure?.(result.issues);
      return;
    }
    setSubmitIssues([]);
    onGenerate?.(result.request);
  }

  return (
    <section
      aria-labelledby="hh-plan-input-title"
      className="hh-plan-input"
      data-testid="plan-input"
    >
      <header className="hh-plan-input-header">
        <span className="hh-stage-kicker">PLAN INPUT</span>
        <h2 id="hh-plan-input-title">Describe what you'd like to plan</h2>
        <p>
          One textarea — Hoopoe runs your description through several models in
          parallel + synthesizes a plan you can refine and lock.
        </p>
      </header>

      {!anyAvailable ? (
        <div
          className="hh-plan-input-empty"
          data-testid="plan-input-no-subscription"
          role="alert"
        >
          <strong>No qualifying subscription configured</strong>
          <p>
            Hoopoe needs at least one of: ChatGPT Pro (Oracle), Claude Max,
            GPT Pro, Gemini Ultra. Open Settings → Subscriptions to configure
            one.
          </p>
        </div>
      ) : null}

      <form
        className="hh-plan-input-form"
        data-testid="plan-input-form"
        onSubmit={handleSubmit}
      >
        <textarea
          aria-label="Plan description"
          className="hh-plan-input-textarea"
          data-testid="plan-input-textarea"
          onChange={(event) => update({ description: event.target.value })}
          placeholder="Describe what you want to build. Be specific — Hoopoe will optionally ask clarifying questions before drafting."
          rows={6}
          spellCheck={true}
          value={draft.description}
        />

        <fieldset
          aria-describedby="hh-plan-input-primary-hint"
          className="hh-plan-input-fieldset"
          data-testid="plan-input-primary"
          disabled={draft.hoopoeChoosesModels}
        >
          <legend>Primary model</legend>
          <p className="hh-plan-input-hint" id="hh-plan-input-primary-hint">
            Runs synthesis + refinement rounds.
          </p>
          <div className="hh-plan-input-radios">
            {PLAN_MODELS.map((model) => {
              const available = availability[model.id];
              const selected = draft.selection.primary === model.id;
              return (
                <label
                  className="hh-plan-input-radio"
                  data-available={available}
                  data-selected={selected}
                  data-testid={`plan-input-primary-${model.id}`}
                  key={model.id}
                >
                  <input
                    checked={selected}
                    disabled={!available || draft.hoopoeChoosesModels}
                    name="hh-plan-input-primary"
                    onChange={() => togglePrimary(model.id)}
                    type="radio"
                    value={model.id}
                  />
                  <span>
                    <strong>{model.label}</strong>
                    {!available ? (
                      <em className="hh-plan-input-unavailable"> · subscription not configured</em>
                    ) : null}
                    <p>{model.description}</p>
                  </span>
                </label>
              );
            })}
          </div>
        </fieldset>

        <fieldset
          aria-describedby="hh-plan-input-competition-hint"
          className="hh-plan-input-fieldset"
          data-testid="plan-input-competition"
          disabled={draft.hoopoeChoosesModels}
        >
          <legend>Competing candidates (up to {MAX_COMPETING_CANDIDATES})</legend>
          <p className="hh-plan-input-hint" id="hh-plan-input-competition-hint">
            Each candidate drafts a plan in parallel; the primary model picks
            the synthesis from across the candidates.
          </p>
          <div className="hh-plan-input-checks">
            {PLAN_MODELS.map((model) => {
              const isPrimary = model.id === draft.selection.primary;
              const checked = draft.selection.competition.includes(model.id);
              const available = availability[model.id];
              return (
                <label
                  className="hh-plan-input-check"
                  data-available={available}
                  data-checked={checked}
                  data-testid={`plan-input-competitor-${model.id}`}
                  key={model.id}
                >
                  <input
                    checked={checked}
                    disabled={!available || isPrimary || draft.hoopoeChoosesModels}
                    onChange={() => toggleCompetitor(model.id)}
                    type="checkbox"
                  />
                  <span>
                    <strong>{model.label}</strong>
                    {isPrimary ? (
                      <em className="hh-plan-input-unavailable"> · primary</em>
                    ) : !available ? (
                      <em className="hh-plan-input-unavailable"> · subscription not configured</em>
                    ) : null}
                  </span>
                </label>
              );
            })}
          </div>
        </fieldset>

        <div className="hh-plan-input-toggles">
          <label
            className="hh-plan-input-toggle"
            data-testid="plan-input-hoopoe-choose"
          >
            <input
              checked={draft.hoopoeChoosesModels}
              onChange={(event) => update({ hoopoeChoosesModels: event.target.checked })}
              type="checkbox"
            />
            <span>
              <Sparkles size={14} strokeWidth={2.1} aria-hidden="true" />
              <strong>Let Hoopoe choose models for me</strong>
              <p>Applies the agent-flywheel.com default ratio (Oracle + Claude + Gemini + Grok).</p>
            </span>
          </label>

          <fieldset
            aria-describedby="hh-plan-input-kickoff-hint"
            className="hh-plan-input-fieldset hh-plan-input-kickoff"
            data-testid="plan-input-kickoff"
          >
            <legend>Kickoff mode</legend>
            <p className="hh-plan-input-hint" id="hh-plan-input-kickoff-hint">
              Default: clarify. Switch to "first shot" if you've already
              described the project in detail.
            </p>
            <div className="hh-plan-input-kickoff-options">
              {(["clarify", "first_shot"] as const).map((mode) => (
                <label
                  className="hh-plan-input-kickoff-option"
                  data-selected={draft.kickoffMode === mode}
                  data-testid={`plan-input-kickoff-${mode}`}
                  key={mode}
                >
                  <input
                    checked={draft.kickoffMode === mode}
                    name="hh-plan-input-kickoff"
                    onChange={() => update({ kickoffMode: mode satisfies ModelKickoffMode })}
                    type="radio"
                    value={mode}
                  />
                  <span>
                    {mode === "clarify" ? (
                      <MessageCircleQuestion size={14} strokeWidth={2.1} aria-hidden="true" />
                    ) : (
                      <Send size={14} strokeWidth={2.1} aria-hidden="true" />
                    )}
                    <strong>
                      {mode === "clarify" ? "Ask clarifying questions" : "Take a first shot"}
                    </strong>
                  </span>
                </label>
              ))}
            </div>
          </fieldset>
        </div>

        {projectHasCodebase ? (
          <p className="hh-plan-input-codebase" data-testid="plan-input-codebase">
            <FileText size={14} strokeWidth={2.1} aria-hidden="true" />
            Hoopoe will attach an existing-codebase context bundle (README,
            AGENTS.md, manifests, beads, hotspots).
          </p>
        ) : null}

        {submitIssues.length > 0 ? (
          <ul className="hh-plan-input-issues" data-testid="plan-input-issues" role="alert">
            {submitIssues.map((issue) => (
              <li data-testid={`plan-input-issue-${issue.code}`} key={`${issue.code}-${issue.message}`}>
                {issue.message}
              </li>
            ))}
          </ul>
        ) : null}

        <button
          className="hh-plan-input-submit"
          data-testid="plan-input-submit"
          disabled={!anyAvailable}
          type="submit"
        >
          <CheckCircle2 size={14} strokeWidth={2.1} aria-hidden="true" />
          Generate plans
        </button>
      </form>

      <PrimarySummary draft={draft} availability={availability} />
    </section>
  );
}

interface PrimarySummaryProps {
  readonly draft: PlanInputDraft;
  readonly availability: PlanModelAvailability;
}

/** Footer summary so the user sees what 'Generate plans' will actually
 *  invoke — especially valuable when 'Let Hoopoe choose' is on. */
function PrimarySummary({ availability, draft }: PrimarySummaryProps) {
  // Compute the effective selection (Hoopoe-chosen if toggled, else
  // the user's manual picks). Don't error on no-subscription here —
  // the empty-state banner handles that.
  let primary: PlanProviderId | null = null;
  let competition: readonly PlanProviderId[] = [];
  if (draft.hoopoeChoosesModels) {
    try {
      const lineup = compileDraft({
        draft: { ...draft, description: "summary-noop" },
        availability,
        projectHasCodebase: false,
      });
      if (lineup.ok) {
        primary = lineup.request.primary;
        competition = lineup.request.competition;
      }
    } catch {
      /* swallow; covered by anyAvailable */
    }
  } else {
    primary = draft.selection.primary;
    competition = draft.selection.competition;
  }
  if (!primary) return null;
  const primaryModel = findPlanModel(primary);
  return (
    <footer className="hh-plan-input-summary" data-testid="plan-input-summary">
      <span>
        <strong>Primary:</strong> {primaryModel.label}
      </span>
      <span>
        <strong>Competing:</strong>{" "}
        {competition.length === 0
          ? "none"
          : competition.map((id) => findPlanModel(id).label).join(", ")}
      </span>
      <span>
        <strong>Kickoff:</strong>{" "}
        {draft.kickoffMode === "clarify" ? "Ask clarifying questions" : "Take a first shot"}
      </span>
    </footer>
  );
}
