package riskledger

// RiskID is the stable string identifier for a §14 risk entry.
// IDs combine the §14 ordinal with a short slug so audit logs and
// CI failure messages stay readable. Renaming an ID is a
// schema-version event — docs/risks.md and CI fixtures key on it.
type RiskID string

const (
	RiskPTYStreamingFidelity         RiskID = "risk.01.pty_streaming_fidelity"
	RiskToolOutputDriftBreaksAdapters RiskID = "risk.02.tool_output_drift"
	RiskHoopoeCacheDivergesFromCanonical RiskID = "risk.03.cache_diverges_canonical"
	RiskFirstInstallBrittle          RiskID = "risk.04.first_install_brittle"
	RiskSubscriptionRateLimitsExhaust RiskID = "risk.05.subscription_rate_limits"
	RiskAgentsCompeteForBuildsTests  RiskID = "risk.06.agents_compete_builds"
	RiskStaleAgentsHoldHostage       RiskID = "risk.07.stale_agents_hostage"
	RiskUnsafeCommandsExposed        RiskID = "risk.08.unsafe_commands_exposed"
	RiskPlanningQualityWeak          RiskID = "risk.09.planning_quality_weak"
	RiskUsersTrustSubjectiveScores   RiskID = "risk.10.subjective_scores_trusted"
	RiskLaptopSleepReliability       RiskID = "risk.11.laptop_sleep_reliability"
	RiskCodexShapedAssumptions       RiskID = "risk.12.codex_shaped_assumptions"
	RiskUpstreamT3CodeDrift          RiskID = "risk.13.upstream_t3code_drift"
	RiskPubSubUnboundedLeaks         RiskID = "risk.14.pubsub_unbounded_leaks"
)

// VerificationKind tells the CI gate which test surface asserts
// the mitigation.
type VerificationKind string

const (
	// VerificationChaos: the chaos / fault-injection suite (hp-2qn)
	// owns the assertion. The fixture injects the failure mode and
	// asserts the mitigation kicks in.
	VerificationChaos VerificationKind = "chaos"

	// VerificationAdapterContract: an adapter contract test
	// (hp-8a8) detects upstream output drift via a golden fixture.
	VerificationAdapterContract VerificationKind = "adapter_contract"

	// VerificationPhaseAcceptance: a phase epic's acceptance
	// criteria assert the mitigation as part of its DOD.
	VerificationPhaseAcceptance VerificationKind = "phase_acceptance"

	// VerificationLintRule: a CI lint rule (e.g., shape-scrub,
	// renderer-isolation) asserts the mitigation by refusing
	// regression patterns at parse time.
	VerificationLintRule VerificationKind = "lint_rule"

	// VerificationProcessReview: a recurring human-driven review
	// (e.g., quarterly upstream-drift review) is the mitigation;
	// the catalog records this so missing reviews are surface
	// evidence rather than silent gaps.
	VerificationProcessReview VerificationKind = "process_review"

	// VerificationUIAssertion: the renderer's component tests
	// assert a UI affordance (e.g., evidence-link beside every
	// quality score, override button present).
	VerificationUIAssertion VerificationKind = "ui_assertion"
)

// Risk is one row in the §14 ledger. The CI gate iterates these,
// resolves each VerificationFixture, and fails loudly when a
// referenced fixture is missing or its assertion has regressed.
type Risk struct {
	// ID is the stable identifier. Audit log + docs/risks.md
	// cross-reference key on this.
	ID RiskID `json:"id"`

	// Number is the §14 ordinal so failure messages can say
	// "risk #6 mitigation regressed" matching the plan reading.
	Number int `json:"number"`

	// Title is the short user-facing risk name.
	Title string `json:"title"`

	// Description is the §14 risk statement (what could go wrong).
	Description string `json:"description"`

	// Mitigation is the §14 strategy (what we do to prevent or
	// detect the failure).
	Mitigation string `json:"mitigation"`

	// VerificationKind classifies the test surface asserting the
	// mitigation.
	VerificationKind VerificationKind `json:"verificationKind"`

	// VerificationFixture is a stable pointer at the test or
	// fixture path. Empty when the verification is process-driven
	// (e.g., quarterly review).
	VerificationFixture string `json:"verificationFixture,omitempty"`

	// OwnerBead is the phase-epic bead that owns implementing the
	// mitigation. CI failure messages route triage to this bead.
	OwnerBead string `json:"ownerBead,omitempty"`

	// SubstrateBeads lists additional beads whose substrate this
	// mitigation depends on (e.g., the chaos suite, the adapter
	// contract substrate). Empty when not applicable.
	SubstrateBeads []string `json:"substrateBeads,omitempty"`

	// DiscoveredIn records when the risk first entered the ledger
	// — "v0" for the original §14 set, or a release version /
	// commit ref for later additions. Lets reviewers see the
	// catalog's growth without re-reading commit history.
	DiscoveredIn string `json:"discoveredIn"`
}

// Catalog returns the §14 source-of-truth list of named risks.
//
// Order matches plan.md §14 reading top-to-bottom; the CI gate
// runs them in this order so failure messages match the plan.
func Catalog() []Risk {
	return []Risk{
		{
			ID:                  RiskPTYStreamingFidelity,
			Number:              1,
			Title:               "PTY streaming fidelity fails",
			Description:         "NTM disconnect breaks pane streaming; tend-swarm cannot read agent output and either flags false-rate-limit or stalls.",
			Mitigation:          "tend-swarm degrades gracefully when NTM is unreachable; Show raw pane Diagnostics fallback works for forensics.",
			VerificationKind:    VerificationChaos,
			VerificationFixture: "apps/daemon/internal/chaos/ntm_disconnect_test.go",
			OwnerBead:           "hp-2qn",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskToolOutputDriftBreaksAdapters,
			Number:              2,
			Title:               "Tool output drift breaks adapters",
			Description:         "An upstream tool (br, bv, ntm, agent-mail) changes its output format; the adapter parses gibberish and the cockpit shows wrong state.",
			Mitigation:          "Adapter contract tests (hp-8a8) load committed golden fixtures; upstream output change → CI fail with diff.",
			VerificationKind:    VerificationAdapterContract,
			VerificationFixture: "packages/fixtures/golden-outputs/",
			OwnerBead:           "hp-8a8",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskHoopoeCacheDivergesFromCanonical,
			Number:              3,
			Title:               "Hoopoe cache diverges from canonical state",
			Description:         "The daemon's read-model cache (Guardrail 4) drifts from canonical tool state; UI shows stale beads / mail / reservations.",
			Mitigation:          "Chaos: corrupt read-model cache → 'reload from tools' restores; reconciliation logged.",
			VerificationKind:    VerificationChaos,
			VerificationFixture: "apps/daemon/internal/chaos/cache_divergence_test.go",
			OwnerBead:           "hp-2qn",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskFirstInstallBrittle,
			Number:              4,
			Title:               "First install is brittle",
			Description:         "ACFS bootstrap on a fresh VPS hits an unforeseen environment edge case; the wizard cannot recover and the user gives up.",
			Mitigation:          "Phase 0 manual wizard run on a real ACFS VPS + Phase 3 ACFS bootstrap test bead automating the path.",
			VerificationKind:    VerificationPhaseAcceptance,
			OwnerBead:           "hp-6us1",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskSubscriptionRateLimitsExhaust,
			Number:              5,
			Title:               "Subscription rate-limits exhaust mid-swarm",
			Description:         "An active swarm's agents collectively exhaust the user's Claude Max / Codex / Gemini / ChatGPT Pro quota; jobs stall mid-flight.",
			Mitigation:          "Rate-limit detection aggregator (hp-v6cq); fixture pairs cover (a) rate-limited agent with healthy CAAM accounts available and (b) without — proves CAAM switchover and casr resume paths work.",
			VerificationKind:    VerificationPhaseAcceptance,
			VerificationFixture: "apps/daemon/internal/ratelimit/classifier_test.go",
			OwnerBead:           "hp-v6cq",
			SubstrateBeads:      []string{"hp-rnn"},
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskAgentsCompeteForBuildsTests,
			Number:              6,
			Title:               "Agents compete for builds / tests",
			Description:         "Five agents simultaneously request the same build/test command; the queue starts five concurrent runs instead of dedup'ing and burns CI quota.",
			Mitigation:          "Build/test queue dedupes by command + cache key (hp-977); fixture: 5 agents request same build → 1 actual run + 4 cache hits.",
			VerificationKind:    VerificationPhaseAcceptance,
			OwnerBead:           "hp-977",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskStaleAgentsHoldHostage,
			Number:              7,
			Title:               "Stale agents hold beads / reservations hostage",
			Description:         "An agent crashes or wedges with a bead claimed and a file reservation held; other agents cannot proceed and the swarm grinds to a halt.",
			Mitigation:          "Stalled-bead detection in tend-swarm; force-release Diagnostics action with audit (hp-6d7); tending evaluation harness asserts the recovery path works.",
			VerificationKind:    VerificationPhaseAcceptance,
			OwnerBead:           "hp-rnn",
			SubstrateBeads:      []string{"hp-6d7"},
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskUnsafeCommandsExposed,
			Number:              8,
			Title:               "Unsafe commands accidentally exposed",
			Description:         "The renderer attempts a privileged operation that should only run from the main process; the preload bridge silently allows it (Guardrail 2 violation).",
			Mitigation:          "Renderer hardening fixtures: any privileged op attempt from renderer → preload rejects + logs; CI gate.",
			VerificationKind:    VerificationLintRule,
			VerificationFixture: "scripts/rendererlint/check-renderer-isolation.ts",
			OwnerBead:           "hp-2qn",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskPlanningQualityWeak,
			Number:              9,
			Title:               "Planning quality is weak",
			Description:         "The planning pipeline produces low-quality plans; users lose trust in Stage 01 and route around it.",
			Mitigation:          "Phase 5 acceptance: 4 candidate models → comparative matrix → best-of-all-worlds synthesis → fresh-eyes critique → 4 refinement rounds → lock.",
			VerificationKind:    VerificationPhaseAcceptance,
			OwnerBead:           "hp-vh7",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskUsersTrustSubjectiveScores,
			Number:              10,
			Title:               "Users trust subjective scores too much",
			Description:         "Quality scores attached to plans / beads / findings appear authoritative; users defer to numbers without reading the underlying evidence.",
			Mitigation:          "UI assertion: every quality score links underlying evidence and exposes an override button (§1.4 inspectability).",
			VerificationKind:    VerificationUIAssertion,
			VerificationFixture: "apps/desktop/src/renderer/components/QualityScore/QualityScore.test.tsx",
			OwnerBead:           "hp-vh7",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskLaptopSleepReliability,
			Number:              11,
			Title:               "Laptop sleep breaks perception of reliability",
			Description:         "MacBook lid closes for 30 minutes; on resume the cockpit shows stale state, missing events, or a corrupted timeline; user concludes Hoopoe is unreliable.",
			Mitigation:          "Phase 2 acceptance: simulated sleep → reconnect → no state corruption + @slo desktop.reconnect.p95 enforced.",
			VerificationKind:    VerificationPhaseAcceptance,
			OwnerBead:           "hp-191",
			SubstrateBeads:      []string{"hp-f99"},
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskCodexShapedAssumptions,
			Number:              12,
			Title:               "Lifted code carries Codex-shaped assumptions",
			Description:         "Files lifted from t3code retain Codex-/T3-specific identifiers (thread/provider/chat) that imply Hoopoe shapes it does not have, e.g. a chat-thread model where Hoopoe has agent-mail threads.",
			Mitigation:          "Phase 1 lint rule (codex-shape-scrub): refuses thread/provider/chat identifiers in non-vendored code; manual scrub checklist in docs/source-provenance.md.",
			VerificationKind:    VerificationLintRule,
			VerificationFixture: "scripts/codexshapescrub/check-codex-shape-scrub.ts",
			OwnerBead:           "hp-zir",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskUpstreamT3CodeDrift,
			Number:              13,
			Title:               "Upstream t3code drift",
			Description:         "github.com/pingdotgg/t3code evolves; vendored helpers diverge from upstream and re-merge cost grows quietly.",
			Mitigation:          "Quarterly review process documented in docs/upstream-drift.md; CHANGELOG monitor; per-quarter audit of the vendored tree against upstream.",
			VerificationKind:    VerificationProcessReview,
			VerificationFixture: "docs/upstream-drift.md",
			OwnerBead:           "hp-zir",
			DiscoveredIn:        "v0",
		},
		{
			ID:                  RiskPubSubUnboundedLeaks,
			Number:              14,
			Title:               "PubSub.unbounded patterns leak",
			Description:         "A new event-fanout path uses an unbounded channel (Anti-pattern #1); a wedged consumer accumulates unbounded backlog and the daemon OOM-kills.",
			Mitigation:          "Chaos: load test with wedged consumer asserts daemon RSS stays bounded; antipattern-compliance CI gate (hp-iswv) refuses unbounded channels in apps/daemon/internal/.",
			VerificationKind:    VerificationChaos,
			VerificationFixture: "apps/daemon/internal/chaos/wedged_consumer_load_test.go",
			OwnerBead:           "hp-q3p",
			SubstrateBeads:      []string{"hp-iswv", "hp-2qn"},
			DiscoveredIn:        "v0",
		},
	}
}

// Lookup returns the catalog entry for id, or false when the id
// is unknown.
func Lookup(id RiskID) (Risk, bool) {
	for _, risk := range Catalog() {
		if risk.ID == id {
			return risk, true
		}
	}
	return Risk{}, false
}

// ByVerificationKind returns the subset of the catalog matching
// the given verification surface — useful for the CI gate to
// dispatch fixtures to the right runner (chaos / adapter contract
// / lint rule / phase acceptance / process review / UI assertion).
func ByVerificationKind(kind VerificationKind) []Risk {
	out := make([]Risk, 0, 4)
	for _, risk := range Catalog() {
		if risk.VerificationKind == kind {
			out = append(out, risk)
		}
	}
	return out
}
