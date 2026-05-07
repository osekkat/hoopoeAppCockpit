package tendingeval

// FixtureID is the stable identifier for a §8.8 fixture. Renaming
// is a schema-version event because evidence under
// docs/test-evidence/tending-eval/ keys on it.
type FixtureID string

// The §8.8 catalog. Twelve entries; this list is the single source
// of truth and the order matches the plan.md table.
const (
	FixtureHealthyHour          FixtureID = "tending_eval.01.healthy_hour"
	FixtureIdleButNotStuck      FixtureID = "tending_eval.02.idle_but_not_stuck"
	FixtureGenuinelyWedgedPane  FixtureID = "tending_eval.03.genuinely_wedged_pane"
	FixtureRateLimitedWithCAAM  FixtureID = "tending_eval.04.rate_limited_with_caam"
	FixtureRateLimitedNoCAAM    FixtureID = "tending_eval.05.rate_limited_no_caam"
	FixtureStaleReservation     FixtureID = "tending_eval.06.stale_reservation"
	FixtureCommitBurst          FixtureID = "tending_eval.07.commit_burst"
	FixtureBudgetBreach         FixtureID = "tending_eval.08.budget_breach"
	FixtureSkillDrift           FixtureID = "tending_eval.09.skill_drift"
	FixtureMissingTool          FixtureID = "tending_eval.10.missing_tool"
	FixturePostconditionFailure FixtureID = "tending_eval.11.postcondition_failure"
	FixtureActionArbitration    FixtureID = "tending_eval.12.action_arbitration"
)

// HealthClass tells the harness whether the fixture represents a
// healthy state (no LLM wake expected, panel quiet) or one of the
// unhealthy scenarios where the tending agent must act.
type HealthClass string

const (
	// HealthClassHealthy: deterministic pre-script returns
	// {wakeAgent: false}; agent never wakes; panel silent except
	// system heartbeats.
	HealthClassHealthy HealthClass = "healthy"

	// HealthClassDegraded: a signal source is unavailable but the
	// system is still functional — the agent may wake but with
	// reduced confidence.
	HealthClassDegraded HealthClass = "degraded"

	// HealthClassUnhealthy: the tending agent must wake and
	// produce an ActionPlan; the harness asserts the plan reaches
	// the executor and postconditions verify.
	HealthClassUnhealthy HealthClass = "unhealthy"

	// HealthClassEdge: an arbitration / contention scenario where
	// multiple agents compete and the tending layer must resolve
	// without livelock.
	HealthClassEdge HealthClass = "edge"
)

// ExpectedWakeBehavior is the §8.6 invariant the harness asserts:
// healthy hours produce wakeAgent:false on every pre-script tick
// (zero LLM cost); unhealthy scenarios produce wakeAgent:true at
// least once.
type ExpectedWakeBehavior string

const (
	WakeNeverFires   ExpectedWakeBehavior = "never_fires"
	WakeAtLeastOnce  ExpectedWakeBehavior = "at_least_once"
	WakeOptional     ExpectedWakeBehavior = "optional"
)

// Fixture is one §8.8 row. The harness replays the fixture against
// the Mock Flywheel substrate, then asserts every dimension the
// catalog declares.
type Fixture struct {
	// ID is the stable identifier; evidence + audit log key on it.
	ID FixtureID `json:"id"`

	// Number is the §8.8 ordinal (1..12) so failure messages
	// reference the plan reading.
	Number int `json:"number"`

	// Title is the short user-facing fixture name.
	Title string `json:"title"`

	// Description is the §8.8 scenario text — what the fixture
	// simulates and what the harness must assert.
	Description string `json:"description"`

	// HealthClass tells the harness which family the fixture
	// belongs to (healthy / degraded / unhealthy / edge).
	HealthClass HealthClass `json:"healthClass"`

	// ExpectedWake declares the §8.6 wake invariant the harness
	// asserts.
	ExpectedWake ExpectedWakeBehavior `json:"expectedWake"`

	// ExpectedActionKinds lists the typed §8.3.1 ActionPlan
	// action kinds the harness expects to see produced (or empty
	// when no action is expected — healthy / degraded / arbitration
	// scenarios may produce zero plans).
	ExpectedActionKinds []string `json:"expectedActionKinds,omitempty"`

	// ExpectedApprovals declares whether the fixture should
	// surface an approval request (e.g., destructive recovery
	// actions go through the unified approvals queue).
	ExpectedApprovals bool `json:"expectedApprovals"`

	// MaxTokenBudget is the upper bound on cost per replay run.
	// Healthy-hour fixtures cap at very low values so a regression
	// that wakes the agent unnecessarily fails the gate.
	MaxTokenBudget int `json:"maxTokenBudget"`

	// MaxActivityEntries is the upper bound on Activity panel
	// entries the fixture should produce (system heartbeats and
	// audit-only entries are excluded).
	MaxActivityEntries int `json:"maxActivityEntries"`

	// SubstrateBeads lists the implementation beads this fixture
	// depends on; failure messages route triage there.
	SubstrateBeads []string `json:"substrateBeads,omitempty"`
}

// Catalog returns the §8.8 source-of-truth list of fixtures.
//
// Order matches plan.md §8.8 reading top-to-bottom; the harness
// runs them in this order so failure messages match the plan.
func Catalog() []Fixture {
	return []Fixture{
		{
			ID:                  FixtureHealthyHour,
			Number:              1,
			Title:               "Healthy hour",
			Description:         "60 minutes of mocked canonical state with no detections; all 7 default tending jobs run on schedule; pre-script returns wakeAgent:false on every tick (§8.6 invariant).",
			HealthClass:         HealthClassHealthy,
			ExpectedWake:        WakeNeverFires,
			ExpectedActionKinds: nil,
			ExpectedApprovals:   false,
			MaxTokenBudget:      0,
			MaxActivityEntries:  1,
			SubstrateBeads:      []string{"hp-fb0", "hp-ilrr"},
		},
		{
			ID:                  FixtureIdleButNotStuck,
			Number:              2,
			Title:               "Idle but not stuck",
			Description:         "Agent has been silent for an extended period but is mid-judgment-class work (planning, deep reasoning); the long-no-output heuristic alone must NOT escalate; wakeAgent stays false; no force-release fires.",
			HealthClass:         HealthClassHealthy,
			ExpectedWake:        WakeNeverFires,
			ExpectedActionKinds: nil,
			ExpectedApprovals:   false,
			MaxTokenBudget:      0,
			MaxActivityEntries:  0,
			SubstrateBeads:      []string{"hp-v6cq"},
		},
		{
			ID:                  FixtureGenuinelyWedgedPane,
			Number:              3,
			Title:               "Genuinely wedged pane",
			Description:         "Pane has produced no bytes for > N seconds AND the agent's last audit entry was a model call AND the bead update is stale; multiple signals confirm wedge; tend-swarm wakes; ActionPlan proposes agent.kill_wedged_process; approval requested.",
			HealthClass:         HealthClassUnhealthy,
			ExpectedWake:        WakeAtLeastOnce,
			ExpectedActionKinds: []string{"agent.kill_wedged_process"},
			ExpectedApprovals:   true,
			MaxTokenBudget:      8000,
			MaxActivityEntries:  3,
			SubstrateBeads:      []string{"hp-fb0", "hp-209"},
		},
		{
			ID:                  FixtureRateLimitedWithCAAM,
			Number:              4,
			Title:               "Rate-limited with CAAM",
			Description:         "caut + CLI status both confirm rate-limit; CAAM has a healthy account available; ActionPlan proposes caam.switch_account; postcondition verifies the new account's caut reads fresh quota; aggregator re-classifies to none within 30s.",
			HealthClass:         HealthClassUnhealthy,
			ExpectedWake:        WakeAtLeastOnce,
			ExpectedActionKinds: []string{"caam.switch_account"},
			ExpectedApprovals:   false,
			MaxTokenBudget:      6000,
			MaxActivityEntries:  3,
			SubstrateBeads:      []string{"hp-v6cq", "hp-fb0"},
		},
		{
			ID:                  FixtureRateLimitedNoCAAM,
			Number:              5,
			Title:               "Rate-limited, no CAAM (degraded mode)",
			Description:         "caut + CLI status confirm rate-limit; CAAM has no healthy accounts left; aggregator falls back to casr.resume_session with BlockedBy=caam.healthy_account; if that fails, swarm.pause_agent fires; user sees an Activity-panel warning instead of silent halt.",
			HealthClass:         HealthClassDegraded,
			ExpectedWake:        WakeAtLeastOnce,
			ExpectedActionKinds: []string{"casr.resume_session", "swarm.pause_agent"},
			ExpectedApprovals:   true,
			MaxTokenBudget:      6000,
			MaxActivityEntries:  4,
			SubstrateBeads:      []string{"hp-v6cq"},
		},
		{
			ID:                  FixtureStaleReservation,
			Number:              6,
			Title:               "Stale reservation",
			Description:         "An agent crashed with a file reservation held; tend-swarm detects the stale reservation; force-release ActionPlan with audited reason; Agent Mail notice posted to the original reserver; reservation released; other agents proceed.",
			HealthClass:         HealthClassUnhealthy,
			ExpectedWake:        WakeAtLeastOnce,
			ExpectedActionKinds: []string{"agent_mail.force_release_reservation"},
			ExpectedApprovals:   true,
			MaxTokenBudget:      4000,
			MaxActivityEntries:  3,
			SubstrateBeads:      []string{"hp-6d7", "hp-fb0"},
		},
		{
			ID:                  FixtureCommitBurst,
			Number:              7,
			Title:               "Commit burst",
			Description:         "Agent commits 10 times in rapid succession without pushing; push-stale-commits ticks; deterministic pre-script pushes the bead's branch; no agent wake; audit row per push.",
			HealthClass:         HealthClassHealthy,
			ExpectedWake:        WakeNeverFires,
			ExpectedActionKinds: []string{"git.push"},
			ExpectedApprovals:   false,
			MaxTokenBudget:      0,
			MaxActivityEntries:  2,
			SubstrateBeads:      []string{"hp-fb0"},
		},
		{
			ID:                  FixtureBudgetBreach,
			Number:              8,
			Title:               "Budget breach",
			Description:         "Subscription quota crosses the configured budget threshold; watch-safety-thresholds ticks; ActionPlan proposes swarm.pause_agent on lowest-priority bead; approval requested; user gets Activity-panel warning with budget snapshot.",
			HealthClass:         HealthClassUnhealthy,
			ExpectedWake:        WakeAtLeastOnce,
			ExpectedActionKinds: []string{"swarm.pause_agent"},
			ExpectedApprovals:   true,
			MaxTokenBudget:      6000,
			MaxActivityEntries:  3,
			SubstrateBeads:      []string{"hp-fb0", "hp-v6cq"},
		},
		{
			ID:                  FixtureSkillDrift,
			Number:              9,
			Title:               "Skill drift",
			Description:         "jsm reports a skill SHA-256 mismatch (vibing-with-ntm or ntm); drift-check ticks; ActionPlan proposes diagnostics.verify_skills (re-pin to known-good); user approval; verifies postcondition (skill SHA matches lock file).",
			HealthClass:         HealthClassUnhealthy,
			ExpectedWake:        WakeOptional,
			ExpectedActionKinds: []string{"diagnostics.verify_skills"},
			ExpectedApprovals:   true,
			MaxTokenBudget:      4000,
			MaxActivityEntries:  3,
			SubstrateBeads:      []string{"hp-6d7", "hp-fb0"},
		},
		{
			ID:                  FixtureMissingTool,
			Number:              10,
			Title:               "Missing tool",
			Description:         "A required tool (rch / br / ntm / agent-mail) is unreachable; capability registry flips to degraded; tend-swarm pre-script bypasses model wake; surfaces a degraded-capability warning; no ActionPlan fires until tool returns.",
			HealthClass:         HealthClassDegraded,
			ExpectedWake:        WakeNeverFires,
			ExpectedActionKinds: nil,
			ExpectedApprovals:   false,
			MaxTokenBudget:      0,
			MaxActivityEntries:  2,
			SubstrateBeads:      []string{"hp-fb0"},
		},
		{
			ID:                  FixturePostconditionFailure,
			Number:              11,
			Title:               "Postcondition failure",
			Description:         "ActionPlan executes but the postcondition (verified against canonical state) fails; the daemon raises a follow-up detection; subsequent tick re-attempts with a different action or escalates to user; audit row records both attempt and follow-up.",
			HealthClass:         HealthClassUnhealthy,
			ExpectedWake:        WakeAtLeastOnce,
			ExpectedActionKinds: []string{"agent.send_marching_orders"},
			ExpectedApprovals:   false,
			MaxTokenBudget:      8000,
			MaxActivityEntries:  4,
			SubstrateBeads:      []string{"hp-209"},
		},
		{
			ID:                  FixtureActionArbitration,
			Number:              12,
			Title:               "Action arbitration",
			Description:         "Two tending jobs concurrently propose conflicting ActionPlans on the same agent (e.g., tend-swarm wants caam.switch_account, watch-safety-thresholds wants swarm.pause_agent). The §8.3.1 executor resolves by priority; only one ActionPlan executes; audit records both proposals + the arbitration decision; no livelock.",
			HealthClass:         HealthClassEdge,
			ExpectedWake:        WakeAtLeastOnce,
			ExpectedActionKinds: []string{"caam.switch_account", "swarm.pause_agent"},
			ExpectedApprovals:   true,
			MaxTokenBudget:      8000,
			MaxActivityEntries:  4,
			SubstrateBeads:      []string{"hp-209", "hp-fb0"},
		},
	}
}

// Lookup returns the catalog entry for id, or false when the id
// is unknown.
func Lookup(id FixtureID) (Fixture, bool) {
	for _, fixture := range Catalog() {
		if fixture.ID == id {
			return fixture, true
		}
	}
	return Fixture{}, false
}

// ByHealthClass returns the subset of the catalog matching the
// given health class — useful for the harness to dispatch healthy
// fixtures (which assert §8.6 zero-cost invariants) separately
// from unhealthy fixtures (which assert action paths).
func ByHealthClass(class HealthClass) []Fixture {
	out := make([]Fixture, 0, 4)
	for _, fixture := range Catalog() {
		if fixture.HealthClass == class {
			out = append(out, fixture)
		}
	}
	return out
}
