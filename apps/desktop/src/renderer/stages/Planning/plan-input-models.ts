// hp-z5r — Model picker config + availability logic for the plan-input chat box.
//
// Plan.md §7.1 spec:
//   - Primary model (synthesis + refinement). Default: ChatGPT Pro via
//     Oracle browser mode.
//   - Up to 3 competing-candidate models (in addition to primary).
//     Default: Claude Opus, Gemini 3 Pro, Grok Heavy/GPT-5.4.
//   - "Let Hoopoe choose" applies the agent-flywheel.com default ratio.
//
// Subscription availability comes from CAAM via a daemon RPC; until the
// renderer wires `caam.subscriptions.list`, the UI accepts an injected
// `availability` map so Storybook + tests can drive the rendering.

/** Stable provider ids — match plan.md §7.1 + the SubscriptionPill in
 *  hp-4ya so the cross-references line up. */
export const PLAN_PROVIDER_IDS = [
  "chatgpt_pro_browser",
  "claude_max",
  "gpt_pro",
  "gemini_ultra",
  "grok_heavy",
] as const;
export type PlanProviderId = (typeof PLAN_PROVIDER_IDS)[number];

export type ModelKickoffMode = "clarify" | "first_shot";

export interface PlanModelOption {
  readonly id: PlanProviderId;
  /** Short label for the picker. */
  readonly label: string;
  /** Longer description shown in tooltips / picker. */
  readonly description: string;
  /** Which Hoopoe-supported subscription / harness this model uses.
   *  Drives the empty-state guidance ("Configure ChatGPT Pro to enable
   *  Oracle browser mode" etc.). */
  readonly harness:
    | "oracle_browser"
    | "claude_code"
    | "codex_cli"
    | "gemini_cli"
    | "grok_browser";
}

/** All five models the chat box can pick. Order is the canonical one
 *  used by the picker UI. */
export const PLAN_MODELS: readonly PlanModelOption[] = [
  {
    id: "chatgpt_pro_browser",
    label: "ChatGPT Pro (Oracle)",
    description:
      "Browser-mode harness via the Oracle CLI. Default primary model — used for synthesis + refinement.",
    harness: "oracle_browser",
  },
  {
    id: "claude_max",
    label: "Claude Opus (Claude Max)",
    description: "Claude Code CLI; strongest planner candidate alongside Oracle.",
    harness: "claude_code",
  },
  {
    id: "gpt_pro",
    label: "GPT-5.4 (Codex CLI)",
    description: "Codex CLI; routes through GPT Pro.",
    harness: "codex_cli",
  },
  {
    id: "gemini_ultra",
    label: "Gemini 3 Pro (Deep Think)",
    description: "Gemini CLI; runs Deep Think mode for the planning pipeline.",
    harness: "gemini_cli",
  },
  {
    id: "grok_heavy",
    label: "Grok Heavy",
    description: "Browser-mode harness; experimental, fourth competing candidate slot.",
    harness: "grok_browser",
  },
] as const;

/** agent-flywheel.com canonical default lineup: ChatGPT Pro primary +
 *  Claude / Gemini / Grok as competition. */
export const PLAN_DEFAULT_PRIMARY: PlanProviderId = "chatgpt_pro_browser";
export const PLAN_DEFAULT_COMPETITION: readonly PlanProviderId[] = [
  "claude_max",
  "gemini_ultra",
  "grok_heavy",
];
export const MAX_COMPETING_CANDIDATES = 3;

/** Per-provider availability snapshot. Keyed by PlanProviderId. */
export type PlanModelAvailability = Readonly<Record<PlanProviderId, boolean>>;

/** Default availability — every model unavailable until CAAM reports
 *  back. Tests + Storybook override. */
export function emptyPlanModelAvailability(): PlanModelAvailability {
  return Object.fromEntries(PLAN_PROVIDER_IDS.map((id) => [id, false])) as PlanModelAvailability;
}

export interface PlanModelSelection {
  readonly primary: PlanProviderId;
  readonly competition: readonly PlanProviderId[];
}

export interface PlanInputDraft {
  readonly description: string;
  readonly selection: PlanModelSelection;
  readonly hoopoeChoosesModels: boolean;
  readonly kickoffMode: ModelKickoffMode;
}

export const EMPTY_PLAN_INPUT_DRAFT: PlanInputDraft = {
  description: "",
  selection: {
    primary: PLAN_DEFAULT_PRIMARY,
    competition: PLAN_DEFAULT_COMPETITION,
  },
  hoopoeChoosesModels: true,
  kickoffMode: "clarify",
};

// ── Pure logic helpers (testable) ────────────────────────────────────────

/** Compute the "Hoopoe choose for me" lineup, filtered by availability.
 *  When the canonical defaults aren't all available, falls back to
 *  whichever subset IS available — preserving primary-first ordering.
 *  Always returns a primary; throws if no model is available at all
 *  (caller renders the no-subscription empty state in that case). */
export function selectHoopoeDefaults(availability: PlanModelAvailability): PlanModelSelection {
  const available = PLAN_PROVIDER_IDS.filter((id) => availability[id]);
  if (available.length === 0) {
    throw new PlanInputError(
      "no_subscription",
      "No qualifying subscriptions configured — open Settings to add one.",
    );
  }
  const primary = availability[PLAN_DEFAULT_PRIMARY] ? PLAN_DEFAULT_PRIMARY : (available[0] as PlanProviderId);
  const competition: PlanProviderId[] = [];
  for (const id of [...PLAN_DEFAULT_COMPETITION, ...PLAN_PROVIDER_IDS]) {
    if (id === primary) continue;
    if (!availability[id]) continue;
    if (competition.includes(id)) continue;
    competition.push(id);
    if (competition.length >= MAX_COMPETING_CANDIDATES) break;
  }
  return { primary, competition };
}

/** Refuse selections that violate the bead's constraints:
 *   - primary must be available
 *   - competition entries must be available + must not include primary
 *   - competition size must not exceed MAX_COMPETING_CANDIDATES
 *  Returns a list of diagnostic codes; empty array == valid. */
export function validatePlanModelSelection(
  selection: PlanModelSelection,
  availability: PlanModelAvailability,
): readonly PlanInputValidationIssue[] {
  const issues: PlanInputValidationIssue[] = [];
  if (!availability[selection.primary]) {
    issues.push({ code: "primary_unavailable", message: `Primary model ${selection.primary} is not available.` });
  }
  if (selection.competition.length > MAX_COMPETING_CANDIDATES) {
    issues.push({
      code: "too_many_candidates",
      message: `At most ${MAX_COMPETING_CANDIDATES} competing candidates allowed (got ${selection.competition.length}).`,
    });
  }
  const seen = new Set<string>();
  for (const id of selection.competition) {
    if (id === selection.primary) {
      issues.push({ code: "candidate_is_primary", message: `Competing candidate ${id} is also the primary.` });
      continue;
    }
    if (seen.has(id)) {
      issues.push({ code: "duplicate_candidate", message: `Competing candidate ${id} listed twice.` });
      continue;
    }
    if (!availability[id]) {
      issues.push({ code: "candidate_unavailable", message: `Competing candidate ${id} is not available.` });
    }
    seen.add(id);
  }
  return issues;
}

/** Compose a PlanInputDraft into the wire-shape the planning pipeline
 *  expects (mirrors §7.1 'Generate plans' click). */
export interface PlanGenerationRequest {
  readonly description: string;
  readonly primary: PlanProviderId;
  readonly competition: readonly PlanProviderId[];
  readonly kickoffMode: ModelKickoffMode;
  readonly attachExistingCodebase: boolean;
}

export interface CompileDraftInput {
  readonly draft: PlanInputDraft;
  readonly availability: PlanModelAvailability;
  /** When true, populate `attachExistingCodebase` per §7.1 sub-mode. */
  readonly projectHasCodebase: boolean;
}

export type CompileDraftResult =
  | { readonly ok: true; readonly request: PlanGenerationRequest }
  | { readonly ok: false; readonly issues: readonly PlanInputValidationIssue[] };

/** Validate + project the draft into a PlanGenerationRequest. When
 *  hoopoeChoosesModels is true, the selection is overridden by
 *  selectHoopoeDefaults(availability). */
export function compileDraft(input: CompileDraftInput): CompileDraftResult {
  if (input.draft.description.trim().length === 0) {
    return {
      ok: false,
      issues: [{ code: "empty_description", message: "Describe what you'd like to build before generating." }],
    };
  }
  let selection: PlanModelSelection;
  if (input.draft.hoopoeChoosesModels) {
    try {
      selection = selectHoopoeDefaults(input.availability);
    } catch (err) {
      return {
        ok: false,
        issues: [{ code: "no_subscription", message: (err as Error).message }],
      };
    }
  } else {
    selection = input.draft.selection;
  }
  const issues = validatePlanModelSelection(selection, input.availability);
  if (issues.length > 0) return { ok: false, issues };
  return {
    ok: true,
    request: {
      description: input.draft.description.trim(),
      primary: selection.primary,
      competition: selection.competition,
      kickoffMode: input.draft.kickoffMode,
      attachExistingCodebase: input.projectHasCodebase,
    },
  };
}

export interface PlanInputValidationIssue {
  readonly code:
    | "empty_description"
    | "primary_unavailable"
    | "candidate_unavailable"
    | "candidate_is_primary"
    | "duplicate_candidate"
    | "too_many_candidates"
    | "no_subscription";
  readonly message: string;
}

export class PlanInputError extends Error {
  override readonly name = "PlanInputError";
  readonly code: string;
  constructor(code: string, message: string) {
    super(`plan-input (${code}): ${message}`);
    this.code = code;
  }
}

/** Look up a model option by id; useful for rendering labels. */
export function findPlanModel(id: PlanProviderId): PlanModelOption {
  const found = PLAN_MODELS.find((m) => m.id === id);
  if (!found) throw new PlanInputError("unknown_model", `Unknown plan model: ${id}`);
  return found;
}
