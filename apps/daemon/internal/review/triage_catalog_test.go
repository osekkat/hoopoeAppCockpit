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

func TestActionsForCurrentStatusOnNewReturnsFourTerminalActions(t *testing.T) {
	t.Parallel()
	got := ActionsForCurrentStatus(FindingNew)
	wantTerminals := map[FindingStatus]bool{
		FindingFixNow:        false,
		FindingNewBead:       false,
		FindingFalsePositive: false,
		FindingNeedsHuman:    false,
	}
	for _, action := range got {
		wantTerminals[action.TerminalStatus] = true
	}
	for status, found := range wantTerminals {
		if !found {
			t.Errorf("ActionsForCurrentStatus(new) missing action terminating at %s", status)
		}
	}
	// attach_blocker shares terminal status with new_bead so
	// the count of returned actions should be 5 (every entry).
	if len(got) != len(TriageCatalog()) {
		t.Errorf("ActionsForCurrentStatus(new) returned %d actions; expected all %d (every action is valid from `new`)",
			len(got), len(TriageCatalog()))
	}
}

func TestActionsForCurrentStatusOnTriagedReturnsFourTerminals(t *testing.T) {
	t.Parallel()
	// `triaged` has the same forward set as `new`.
	got := ActionsForCurrentStatus(FindingTriaged)
	if len(got) != len(TriageCatalog()) {
		t.Errorf("ActionsForCurrentStatus(triaged) returned %d, want %d", len(got), len(TriageCatalog()))
	}
}

func TestActionsForCurrentStatusOnNeedsHumanReturnsThreeFollowups(t *testing.T) {
	t.Parallel()
	got := ActionsForCurrentStatus(FindingNeedsHuman)
	// After human review, escalation can land at fix_now /
	// new_bead / false_positive only — needs_human itself is no
	// longer a forward target.
	allowedTerminals := map[FindingStatus]bool{
		FindingFixNow:        true,
		FindingNewBead:       true,
		FindingFalsePositive: true,
	}
	for _, action := range got {
		if !allowedTerminals[action.TerminalStatus] {
			t.Errorf("ActionsForCurrentStatus(needs_human) returned action with disallowed TerminalStatus: %s", action.TerminalStatus)
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
