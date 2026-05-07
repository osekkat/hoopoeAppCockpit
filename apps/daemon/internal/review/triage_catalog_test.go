package review

import (
	"strings"
	"testing"
)

func TestTriageCatalogContainsAllFiveSection743Actions(t *testing.T) {
	t.Parallel()
	want := []TriageActionID{
		TriageActionFixImmediately,
		TriageActionNewBead,
		TriageActionAttachBlocker,
		TriageActionFalsePositive,
		TriageActionNeedsHuman,
	}
	got := TriageCatalog()
	if len(got) != len(want) {
		t.Fatalf("triage catalog length = %d, want %d (the §7.4.3 triage actions)", len(got), len(want))
	}
	for i, action := range got {
		if action.ID != want[i] {
			t.Errorf("catalog[%d].ID = %q, want %q (order must match the §7.4.3 reading)", i, action.ID, want[i])
		}
	}
}

func TestEveryTriageActionHasNonEmptyLabelDescriptionAuditAction(t *testing.T) {
	t.Parallel()
	for _, action := range TriageCatalog() {
		if action.Label == "" {
			t.Errorf("%s: Label is empty", action.ID)
		}
		if action.Description == "" {
			t.Errorf("%s: Description is empty", action.ID)
		}
		if action.AuditAction == "" {
			t.Errorf("%s: AuditAction is empty (Guardrail 10)", action.ID)
		}
	}
}

func TestEveryTriageActionIDIsReviewNamespaced(t *testing.T) {
	t.Parallel()
	for _, action := range TriageCatalog() {
		if !strings.HasPrefix(string(action.ID), "review.triage.") {
			t.Errorf("%s: ID is missing the `review.triage.` namespace prefix", action.ID)
		}
	}
}

func TestEveryTriageActionDeclaresAtLeastOneSideEffect(t *testing.T) {
	t.Parallel()
	for _, action := range TriageCatalog() {
		if len(action.SideEffects) == 0 {
			t.Errorf("%s: SideEffects is empty (every action has at least 1 audit-visible side effect)", action.ID)
		}
	}
}

func TestFixImmediatelyAndNewBeadCreateBeads(t *testing.T) {
	t.Parallel()
	for _, id := range []TriageActionID{TriageActionFixImmediately, TriageActionNewBead} {
		action, ok := LookupTriageAction(id)
		if !ok {
			t.Fatalf("%s missing", id)
		}
		if !containsSideEffect(action.SideEffects, TriageSideEffectCreatesBead) {
			t.Errorf("%s: must declare creates_bead side effect", action.ID)
		}
	}
}

func TestAttachBlockerLinksToExistingBead(t *testing.T) {
	t.Parallel()
	action, ok := LookupTriageAction(TriageActionAttachBlocker)
	if !ok {
		t.Fatal("attach_blocker missing")
	}
	if !containsSideEffect(action.SideEffects, TriageSideEffectLinksToExistingBead) {
		t.Errorf("attach_blocker must declare links_existing_bead side effect")
	}
	if !containsRequiredField(action.RequiredFields, RequiredFieldTargetBeadID) {
		t.Errorf("attach_blocker must require target_bead_id input")
	}
}

func TestFalsePositiveRequiresReason(t *testing.T) {
	t.Parallel()
	action, ok := LookupTriageAction(TriageActionFalsePositive)
	if !ok {
		t.Fatal("false_positive missing")
	}
	if !containsRequiredField(action.RequiredFields, RequiredFieldReason) {
		t.Errorf("false_positive must require reason input (audit-trail completeness per §1.4)")
	}
	if action.TerminalStatus != FindingFalsePositive {
		t.Errorf("TerminalStatus = %s, want false_positive", action.TerminalStatus)
	}
	if action.Disposition != DispositionFalsePositive {
		t.Errorf("Disposition = %s, want false_positive", action.Disposition)
	}
}

func TestNeedsHumanEscalatesToUserInboxAndPostsMail(t *testing.T) {
	t.Parallel()
	action, ok := LookupTriageAction(TriageActionNeedsHuman)
	if !ok {
		t.Fatal("needs_human missing")
	}
	if !containsSideEffect(action.SideEffects, TriageSideEffectEscalatesToUserInbox) {
		t.Errorf("needs_human must escalate to user inbox")
	}
	if !containsSideEffect(action.SideEffects, TriageSideEffectPostsAgentMail) {
		t.Errorf("needs_human must post Agent Mail notice")
	}
	if !containsRequiredField(action.RequiredFields, RequiredFieldEscalationContext) {
		t.Errorf("needs_human must require escalation_context input")
	}
}

func TestActionsForCurrentStatusOnNewReturnsZero(t *testing.T) {
	t.Parallel()
	// `new` findings must transition to `triaged` first
	// (validateTransition rule). No catalog entry terminates at
	// FindingTriaged, so the renderer shows zero triage buttons
	// while the daemon promotes new → triaged.
	got := ActionsForCurrentStatus(FindingNew)
	if len(got) != 0 {
		t.Errorf("ActionsForCurrentStatus(new) returned %d actions; want 0 (new must auto-triage before any action)", len(got))
	}
}

func TestActionsForCurrentStatusOnTriagedReturnsAllFiveCatalogEntries(t *testing.T) {
	t.Parallel()
	// All 5 catalog entries are valid from `triaged`: fix_now,
	// new_bead, attach_blocker (shares new_bead terminal),
	// false_positive, needs_human — the four allowed forward
	// targets per validateTransition.
	got := ActionsForCurrentStatus(FindingTriaged)
	if len(got) != len(TriageCatalog()) {
		t.Errorf("ActionsForCurrentStatus(triaged) returned %d, want %d", len(got), len(TriageCatalog()))
	}
}

func TestActionsForCurrentStatusOnNeedsHumanReturnsZero(t *testing.T) {
	t.Parallel()
	// Per validateTransition, needs_human findings can only
	// transition to closed. No catalog entry terminates at
	// closed (closing a finding is not a triage action — it's a
	// separate `close` RPC), so the renderer shows zero triage
	// buttons. The user resolves the finding via close, not
	// triage.
	got := ActionsForCurrentStatus(FindingNeedsHuman)
	if len(got) != 0 {
		t.Errorf("ActionsForCurrentStatus(needs_human) returned %d actions; want 0 (only `close` is allowed, not a triage action)", len(got))
	}
}

func TestIsTransitionAllowedMatchesValidateTransition(t *testing.T) {
	t.Parallel()
	// The catalog's isTransitionAllowed must exactly mirror
	// validateTransition's accept/reject decision for every
	// (from, to) FindingStatus pair. Side-condition fields
	// (disposition / beadID / reason) are populated with
	// non-empty placeholders so validateTransition's content
	// checks pass; only the structural transition rule is
	// compared.
	allStatuses := []FindingStatus{
		FindingNew, FindingTriaged, FindingFixNow, FindingNewBead,
		FindingFalsePositive, FindingNeedsHuman, FindingClosed,
	}
	for _, from := range allStatuses {
		for _, to := range allStatuses {
			catalogAllows := isTransitionAllowed(from, to)
			validateAllows := validateTransition(from, to, "", "dummy-bead", "dummy-reason") == nil
			if catalogAllows != validateAllows {
				t.Errorf("transition (%s → %s): catalog=%v validateTransition=%v (must agree)",
					from, to, catalogAllows, validateAllows)
			}
		}
	}
}

func TestActionsForCurrentStatusOnTerminalStatesIsEmpty(t *testing.T) {
	t.Parallel()
	for _, terminal := range []FindingStatus{
		FindingFixNow, FindingNewBead, FindingFalsePositive, FindingClosed,
	} {
		got := ActionsForCurrentStatus(terminal)
		if len(got) != 0 {
			t.Errorf("ActionsForCurrentStatus(%s) returned %d actions; terminal states must offer none", terminal, len(got))
		}
	}
}

func TestIsTerminalStatusFlagsTerminalStates(t *testing.T) {
	t.Parallel()
	terminals := []FindingStatus{FindingFixNow, FindingNewBead, FindingFalsePositive, FindingClosed}
	for _, s := range terminals {
		if !IsTerminalStatus(s) {
			t.Errorf("IsTerminalStatus(%s) = false, want true", s)
		}
	}
}

func TestIsTerminalStatusRejectsActiveStates(t *testing.T) {
	t.Parallel()
	active := []FindingStatus{FindingNew, FindingTriaged, FindingNeedsHuman}
	for _, s := range active {
		if IsTerminalStatus(s) {
			t.Errorf("IsTerminalStatus(%s) = true, want false", s)
		}
	}
}

func TestLookupTriageActionReturnsOkOnHitFalseOnMiss(t *testing.T) {
	t.Parallel()
	if _, ok := LookupTriageAction(TriageActionFixImmediately); !ok {
		t.Errorf("Lookup must return true for a known ID")
	}
	if _, ok := LookupTriageAction(TriageActionID("review.triage.does_not_exist")); ok {
		t.Errorf("Lookup must return false for unknown ID")
	}
}

func TestTriageActionIDsAreUnique(t *testing.T) {
	t.Parallel()
	seen := make(map[TriageActionID]bool, 5)
	for _, action := range TriageCatalog() {
		if seen[action.ID] {
			t.Errorf("duplicate triage action ID: %s", action.ID)
		}
		seen[action.ID] = true
	}
}

func TestTerminalStatusesMatchDispositions(t *testing.T) {
	t.Parallel()
	// Sanity check: when an action declares a Disposition, its
	// TerminalStatus should match the disposition's intent.
	for _, action := range TriageCatalog() {
		if action.Disposition == "" {
			continue
		}
		switch action.Disposition {
		case DispositionFixNow:
			if action.TerminalStatus != FindingFixNow {
				t.Errorf("%s: Disposition=fix_now but TerminalStatus=%s", action.ID, action.TerminalStatus)
			}
		case DispositionNewBead:
			if action.TerminalStatus != FindingNewBead {
				t.Errorf("%s: Disposition=new_bead but TerminalStatus=%s", action.ID, action.TerminalStatus)
			}
		case DispositionFalsePositive:
			if action.TerminalStatus != FindingFalsePositive {
				t.Errorf("%s: Disposition=false_positive but TerminalStatus=%s", action.ID, action.TerminalStatus)
			}
		case DispositionNeedsHuman:
			if action.TerminalStatus != FindingNeedsHuman {
				t.Errorf("%s: Disposition=needs_human but TerminalStatus=%s", action.ID, action.TerminalStatus)
			}
		}
	}
}

func TestTriageCatalogIsImmutableAcrossCalls(t *testing.T) {
	t.Parallel()
	a := TriageCatalog()
	b := TriageCatalog()
	if len(a) != len(b) {
		t.Fatalf("triage catalog length differs across calls")
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Errorf("triage[%d] differs across calls", i)
		}
	}
}

func containsSideEffect(haystack []TriageSideEffect, needle TriageSideEffect) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func containsRequiredField(haystack []RequiredField, needle RequiredField) bool {
	for _, f := range haystack {
		if f == needle {
			return true
		}
	}
	return false
}
