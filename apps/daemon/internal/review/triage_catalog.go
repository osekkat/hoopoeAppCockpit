package review

// hp-k4j engine slice: the typed §7.4.3 / §9.3 triage-action
// catalog the renderer dispatches over for each finding-detail
// panel and the daemon dispatches over for each /v1/findings/{id}
// /triage RPC.
//
// Coexists with the existing review-package state machine
// (validateTransition + Ledger.Transition); this file ships the
// USER-FACING action surface keyed by ActionID, while the
// underlying state machine continues to enforce
// FindingStatus / FindingDisposition transitions.

// TriageActionID is the stable identifier for one §7.4.3 triage
// action. Renaming is a schema-version event (per §10.3) because
// the audit log + RPC contract reference these.
type TriageActionID string

// The §7.4.3 source-of-truth triage actions. The renderer's
// finding-detail panel exposes them as buttons; the daemon's
// /v1/findings/{id}/triage handler dispatches by ID.
const (
	TriageActionFixImmediately TriageActionID = "review.triage.fix_immediately"
	TriageActionNewBead        TriageActionID = "review.triage.new_bead"
	TriageActionAttachBlocker  TriageActionID = "review.triage.attach_blocker"
	TriageActionFalsePositive  TriageActionID = "review.triage.false_positive"
	TriageActionNeedsHuman     TriageActionID = "review.triage.needs_human"
)

// TriageSideEffect declares what observable side-effects the
// daemon performs when the action is dispatched. The renderer's
// confirmation dialog uses this list to set user expectations; the
// audit log records each side-effect so reviewers can audit cause
// and effect.
type TriageSideEffect string

const (
	// TriageSideEffectCreatesBead: the daemon calls `br create`
	// to materialize a linked bead from the finding. Used by
	// fix_immediately (auto-prefilled fix bead) and new_bead
	// (general escalation).
	TriageSideEffectCreatesBead TriageSideEffect = "creates_bead"

	// TriageSideEffectLinksToExistingBead: the daemon adds the
	// finding's bead ID to the existing target bead's
	// `dependencies` graph (`br dep add target finding-bead`).
	TriageSideEffectLinksToExistingBead TriageSideEffect = "links_existing_bead"

	// TriageSideEffectPostsAgentMail: a notice fires in the
	// project's Activity panel + the relevant Agent Mail thread
	// (e.g., needs_human escalation pings the user inbox).
	TriageSideEffectPostsAgentMail TriageSideEffect = "posts_agent_mail"

	// TriageSideEffectMarksLedgerOnly: the ledger row's status +
	// disposition are updated; no out-of-package side-effects.
	TriageSideEffectMarksLedgerOnly TriageSideEffect = "marks_ledger_only"

	// TriageSideEffectEscalatesToUserInbox: surfaces the finding
	// in the user's `requires_attention` inbox in the cockpit.
	TriageSideEffectEscalatesToUserInbox TriageSideEffect = "escalates_user_inbox"
)

// RequiredField names a piece of input the renderer must collect
// from the user before dispatching the action. The daemon
// validates presence + non-empty before persisting.
type RequiredField string

const (
	// RequiredFieldReason: free-text reason. False-positive marks
	// require this so future auditors can see WHY the finding was
	// dismissed (audit-trail completeness per §1.4).
	RequiredFieldReason RequiredField = "reason"

	// RequiredFieldTargetBeadID: existing bead ID to link the
	// finding to. attach_blocker requires this.
	RequiredFieldTargetBeadID RequiredField = "target_bead_id"

	// RequiredFieldEscalationContext: short note describing what
	// the human reviewer should look at. needs_human requires
	// this so the user doesn't open the inbox without context.
	RequiredFieldEscalationContext RequiredField = "escalation_context"
)

// TriageAction is one row in the §7.4.3 catalog. The renderer
// renders the finding-detail panel from this; the daemon
// dispatches handlers by ID.
type TriageAction struct {
	// ID is the stable identifier; audit log + RPC reference it.
	ID TriageActionID `json:"id"`

	// Label is the short user-facing button text.
	Label string `json:"label"`

	// Description is the one-paragraph explanation shown in the
	// confirmation dialog.
	Description string `json:"description"`

	// TerminalStatus is the FindingStatus the action lands the
	// finding in once the side-effects complete. The Ledger
	// state machine still validates the transition; this field
	// is the catalog's declaration of intent.
	TerminalStatus FindingStatus `json:"terminalStatus"`

	// Disposition is the FindingDisposition stamp on the
	// transition. Empty for actions that don't terminate the
	// finding (e.g., needs_human routes via fix_now /
	// false_positive AFTER human review).
	Disposition FindingDisposition `json:"disposition,omitempty"`

	// SideEffects are the observable side-effects ordered by
	// when they happen. Audit log records each as a separate
	// row.
	SideEffects []TriageSideEffect `json:"sideEffects"`

	// RequiredFields lists inputs the user must supply before the
	// daemon accepts the dispatch. The daemon validates these.
	RequiredFields []RequiredField `json:"requiredFields,omitempty"`

	// AuditAction is the audit-log action key for the dispatch
	// (Guardrail 10).
	AuditAction string `json:"auditAction"`
}

// TriageCatalog returns the §7.4.3 source-of-truth list of
// triage actions in the order the finding-detail panel renders
// them.
func TriageCatalog() []TriageAction {
	return []TriageAction{
		{
			ID:             TriageActionFixImmediately,
			Label:          "Fix immediately",
			Description:    "Open the bead drawer with an auto-prefilled fix bead. The finding's evidence + suggested fix prepopulate the bead description; user confirms before creation.",
			TerminalStatus: FindingFixNow,
			Disposition:    DispositionFixNow,
			SideEffects: []TriageSideEffect{
				TriageSideEffectCreatesBead,
				TriageSideEffectMarksLedgerOnly,
			},
			AuditAction: "review.triage.fix_immediately",
		},
		{
			ID:             TriageActionNewBead,
			Label:          "New bead",
			Description:    "Create a linked bead via the br adapter; the finding's bead ID stamps onto the new bead's `originating_finding` field for traceability.",
			TerminalStatus: FindingNewBead,
			Disposition:    DispositionNewBead,
			SideEffects: []TriageSideEffect{
				TriageSideEffectCreatesBead,
				TriageSideEffectMarksLedgerOnly,
			},
			AuditAction: "review.triage.new_bead",
		},
		{
			ID:             TriageActionAttachBlocker,
			Label:          "Attach as blocker",
			Description:    "Link the finding to an existing bead as a blocker via `br dep add`. The finding's terminal status is `new_bead` (the linked bead is the new bead); the existing bead gains a dependency on the finding.",
			TerminalStatus: FindingNewBead,
			Disposition:    DispositionNewBead,
			SideEffects: []TriageSideEffect{
				TriageSideEffectLinksToExistingBead,
				TriageSideEffectMarksLedgerOnly,
			},
			RequiredFields: []RequiredField{RequiredFieldTargetBeadID},
			AuditAction:    "review.triage.attach_blocker",
		},
		{
			ID:             TriageActionFalsePositive,
			Label:          "False positive",
			Description:    "Mark the finding as a false positive. Requires a free-text reason for audit-trail completeness (§1.4); future scans for the same fingerprint will surface the prior false-positive disposition.",
			TerminalStatus: FindingFalsePositive,
			Disposition:    DispositionFalsePositive,
			SideEffects: []TriageSideEffect{
				TriageSideEffectMarksLedgerOnly,
			},
			RequiredFields: []RequiredField{RequiredFieldReason},
			AuditAction:    "review.triage.false_positive",
		},
		{
			ID:             TriageActionNeedsHuman,
			Label:          "Needs human",
			Description:    "Escalate the finding to the user's `requires_attention` inbox. Surfaces in the Activity panel; tend-swarm will not retry until the user dispositions to fix_now / new_bead / false_positive.",
			TerminalStatus: FindingNeedsHuman,
			Disposition:    DispositionNeedsHuman,
			SideEffects: []TriageSideEffect{
				TriageSideEffectPostsAgentMail,
				TriageSideEffectEscalatesToUserInbox,
			},
			RequiredFields: []RequiredField{RequiredFieldEscalationContext},
			AuditAction:    "review.triage.needs_human",
		},
	}
}

// LookupTriageAction returns the catalog entry for the given ID,
// or false on miss. Use this in handler dispatch to refuse
// unknown ids before reaching side-effecting code.
func LookupTriageAction(id TriageActionID) (TriageAction, bool) {
	for _, action := range TriageCatalog() {
		if action.ID == id {
			return action, true
		}
	}
	return TriageAction{}, false
}

// ActionsForCurrentStatus returns the subset of the triage
// catalog applicable to a finding currently at the given
// FindingStatus. This narrows the renderer's button list to only
// actions whose underlying state-machine transition is valid.
//
// The state machine (validateTransition) remains the authority on
// what's allowed; this helper pre-filters to give the user a
// clean palette and avoid dead-end clicks.
func ActionsForCurrentStatus(status FindingStatus) []TriageAction {
	out := make([]TriageAction, 0, 5)
	for _, action := range TriageCatalog() {
		if isTransitionAllowed(status, action.TerminalStatus) {
			out = append(out, action)
		}
	}
	return out
}

// isTransitionAllowed encodes the §9.3 lifecycle map at the
// triage layer. The Ledger state machine repeats this check via
// validateTransition; the duplication is deliberate — the renderer
// must filter without round-tripping the daemon to test each
// candidate action.
func isTransitionAllowed(from, to FindingStatus) bool {
	switch from {
	case FindingNew, FindingTriaged:
		switch to {
		case FindingFixNow, FindingNewBead, FindingFalsePositive, FindingNeedsHuman:
			return true
		}
	case FindingNeedsHuman:
		// After human review, escalation lands at fix_now /
		// new_bead / false_positive only.
		switch to {
		case FindingFixNow, FindingNewBead, FindingFalsePositive:
			return true
		}
	}
	return false
}

// IsTerminalStatus returns true when the finding is in a
// disposition-terminal state (closed by the system or by a
// disposition stamp). The renderer hides triage buttons in these
// states.
func IsTerminalStatus(status FindingStatus) bool {
	switch status {
	case FindingFixNow, FindingNewBead, FindingFalsePositive, FindingClosed:
		return true
	}
	return false
}
