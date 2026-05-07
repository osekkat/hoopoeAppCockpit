package e2esmoke

// StepID is the stable string identifier for a §18.2 scenario
// step. Renaming an ID is a schema-version event because evidence
// artifacts under docs/test-evidence/e2e-smoke/ key on it.
type StepID string

// The §18.2 catalog. Sixteen entries; this list is the single
// source of truth and the order matches the plan.md scenario.
const (
	StepInstallSignedDMG          StepID = "e2e_smoke.01.install_signed_dmg"
	StepConnectFreshUbuntuVPS     StepID = "e2e_smoke.02.connect_fresh_vps"
	StepRunACFSBootstrap          StepID = "e2e_smoke.03.run_acfs_bootstrap"
	StepImportFixtureRepo         StepID = "e2e_smoke.04.import_fixture_repo"
	StepCreateOrImportPlan        StepID = "e2e_smoke.05.create_or_import_plan"
	StepGenerateOrLockPlan        StepID = "e2e_smoke.06.generate_or_lock_plan"
	StepConvertPlanToBeads        StepID = "e2e_smoke.07.convert_plan_to_beads"
	StepCurateBeadInUI            StepID = "e2e_smoke.08.curate_bead_in_ui"
	StepLaunchSmokeSwarm          StepID = "e2e_smoke.09.launch_smoke_swarm"
	StepIngestAgentMail           StepID = "e2e_smoke.10.ingest_agent_mail"
	StepVPSCommitAndSync          StepID = "e2e_smoke.11.vps_commit_and_sync"
	StepRunHealthSnapshot         StepID = "e2e_smoke.12.run_health_snapshot"
	StepFreshEyesReview           StepID = "e2e_smoke.13.fresh_eyes_review"
	StepKillRestartReplay         StepID = "e2e_smoke.14.kill_restart_replay"
	StepUpgradeDaemonCompatibility StepID = "e2e_smoke.15.upgrade_daemon"
	StepNoSecretsInLogs           StepID = "e2e_smoke.16.no_secrets_in_logs"
)

// Variant tells the runner which environment the step exercises.
// Each step declares which variants it supports.
type Variant string

const (
	// VariantRealVPS: the step runs against a real research-spike
	// VPS. Required for §18.2 acceptance ("scenario runs end-to-
	// end against a real research-spike VPS").
	VariantRealVPS Variant = "real_vps"

	// VariantMockFlywheel: the step runs against the §13 Mock
	// Flywheel Mode substrate. Used by the nightly CI variant so
	// the gate fires daily without requiring a real VPS.
	VariantMockFlywheel Variant = "mock_flywheel"
)

// Stage maps each step to the cockpit stage it exercises so the
// runner can group evidence and so a regression's failure message
// can surface "stage 02 (Beads) regressed".
type Stage string

const (
	StageOnboarding Stage = "onboarding"
	StagePlanning   Stage = "01_planning"
	StageBeads      Stage = "02_beads"
	StageSwarm      Stage = "03_swarm"
	StageHardening  Stage = "04_hardening"
	StageOperations Stage = "operations"
	StageRelease    Stage = "release"
)

// ScenarioStep is one entry in the §18.2 catalog. The runner
// dispatches by ID and writes evidence under
// docs/test-evidence/e2e-smoke/<run-id>/<id>/.
type ScenarioStep struct {
	// ID is the stable string identifier; evidence and CI logs
	// reference it.
	ID StepID `json:"id"`

	// Number is the §18.2 ordinal (1..16) so failure messages
	// can say "step 9 failed" matching the plan.
	Number int `json:"number"`

	// Title is the short user-facing name.
	Title string `json:"title"`

	// Description is the §18.2 step text the runner asserts.
	Description string `json:"description"`

	// Stage maps the step to its cockpit stage.
	Stage Stage `json:"stage"`

	// SupportsVariants lists which run modes the step supports.
	// All steps must support at least VariantRealVPS; many also
	// support VariantMockFlywheel for the nightly-CI variant.
	SupportsVariants []Variant `json:"supportsVariants"`

	// EvidenceArtifacts lists the per-step artifacts the runner
	// must write under docs/test-evidence/e2e-smoke/<run-id>/<id>/.
	EvidenceArtifacts []string `json:"evidenceArtifacts"`

	// SubstrateBeads lists the implementation-owner beads this
	// step depends on so failure messages can route triage.
	SubstrateBeads []string `json:"substrateBeads,omitempty"`
}

// Catalog returns the §18.2 source-of-truth list of scenario steps.
//
// Order matches plan.md §18.2 reading top-to-bottom; the runner
// executes them in this order so a step-N failure halts before
// step-N+1 (each step depends on prior steps' state).
func Catalog() []ScenarioStep {
	return []ScenarioStep{
		{
			ID:               StepInstallSignedDMG,
			Number:           1,
			Title:            "Install signed Hoopoe DMG on a clean macOS profile",
			Description:      "Mount + install a signed/notarized DMG on a fresh macOS profile; first launch passes Gatekeeper without warnings.",
			Stage:            StageOnboarding,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"dmg-mount.log",
				"first-launch-trace.log",
				"gatekeeper-status.txt",
			},
			SubstrateBeads: []string{"hp-191"},
		},
		{
			ID:               StepConnectFreshUbuntuVPS,
			Number:           2,
			Title:            "Connect to fresh Ubuntu VPS via SSH",
			Description:      "The wizard's connection step pairs with a fresh Ubuntu VPS via SSH; bearer + WS-token issued; tunnel opens.",
			Stage:            StageOnboarding,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"ssh-pair-trace.log",
				"three-token-handshake.json",
				"tunnel-listening.txt",
			},
		},
		{
			ID:               StepRunACFSBootstrap,
			Number:           3,
			Title:            "Run ACFS bootstrap and daemon install",
			Description:      "ACFS bootstrap streams to the wizard; the Hoopoe daemon binary lands on the VPS; /v1/health reports ready.",
			Stage:            StageOnboarding,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"acfs-bootstrap.log",
				"daemon-install-trace.log",
				"daemon-health.json",
			},
		},
		{
			ID:               StepImportFixtureRepo,
			Number:           4,
			Title:            "Import fixture repo with origin remote",
			Description:      "Project registry imports a small fixture repo with an origin remote; readiness gates green; local clone fetched.",
			Stage:            StageOperations,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"project-import.json",
				"readiness-report.json",
				"local-clone-status.txt",
			},
		},
		{
			ID:               StepCreateOrImportPlan,
			Number:           5,
			Title:            "Create or import a plan",
			Description:      "User opens Stage 01 Planning and either creates a fresh plan via the prompt box or imports a fixture .md plan.",
			Stage:            StagePlanning,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"plan-creation-trace.log",
				"plan-artifact-id.json",
			},
			SubstrateBeads: []string{"hp-vh7", "hp-m6am"},
		},
		{
			ID:               StepGenerateOrLockPlan,
			Number:           6,
			Title:            "Generate / refine / lock plan or use fixture locked plan",
			Description:      "Either run the multi-model planning pipeline (4 candidates → matrix → synthesis → critique → 4 refinement rounds → lock) OR load a fixture locked plan.",
			Stage:            StagePlanning,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"refinement-rounds-summary.json",
				"plan-lock.json",
			},
			SubstrateBeads: []string{"hp-vh7"},
		},
		{
			ID:               StepConvertPlanToBeads,
			Number:           7,
			Title:            "Convert plan to beads",
			Description:      "Stage 02 conversion: plan → br beads with traceability.json; quality scores attached; conversion artifact archived.",
			Stage:            StageBeads,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"plan-to-beads-trace.log",
				"traceability.json",
				"bead-set-quality.json",
			},
			SubstrateBeads: []string{"hp-9kt", "hp-ojh"},
		},
		{
			ID:               StepCurateBeadInUI,
			Number:           8,
			Title:            "Curate at least one bead and dependency in UI",
			Description:      "User opens at least one bead in the drawer and edits one dependency; change is persisted via daemon RPC; audit row recorded.",
			Stage:            StageBeads,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"bead-edit-trace.log",
				"dependency-change-audit.json",
			},
			SubstrateBeads: []string{"hp-0ba", "hp-5cr"},
		},
		{
			ID:               StepLaunchSmokeSwarm,
			Number:           9,
			Title:            "Launch a 2-3 agent smoke swarm or mock NTM swarm",
			Description:      "Stage 03 swarm-launch composition picker → NTM spawn (real-vps) or mock NTM spawn (mock-flywheel); 2-3 panes reach Active.",
			Stage:            StageSwarm,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"composition-picker.json",
				"ntm-spawn-trace.log",
				"agent-state-snapshot.json",
			},
			SubstrateBeads: []string{"hp-m9w", "hp-pux"},
		},
		{
			ID:               StepIngestAgentMail,
			Number:           10,
			Title:            "Ingest Agent Mail and reservations",
			Description:      "Activity panel reflects agent-mail messages and file reservations; sequence-cursor advances cleanly; no gap events.",
			Stage:            StageSwarm,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"agent-mail-ingestion.log",
				"reservation-snapshot.json",
				"sequence-cursor-progression.json",
			},
			SubstrateBeads: []string{"hp-v6n", "hp-ay4"},
		},
		{
			ID:               StepVPSCommitAndSync,
			Number:           11,
			Title:            "Create a commit on the VPS and verify origin/local-clone sync",
			Description:      "Agent commits + pushes on VPS; origin updates; desktop local clone fetches; vps_commit_created → vps_push_completed → origin_updated event sequence visible in Activity.",
			Stage:            StageOperations,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"vps-commit.json",
				"push-completed.json",
				"origin-updated.json",
				"local-clone-fetch.txt",
			},
		},
		{
			ID:               StepRunHealthSnapshot,
			Number:           12,
			Title:            "Run health snapshot in isolated worktree",
			Description:      "Snapshot runs in ~/.hoopoe/work/<project-id>/health/<run-id>/ (Guardrail 5); no contamination of agent worktree; KPIs land in Stage 04.",
			Stage:            StageHardening,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"health-worktree-isolation.txt",
				"snapshot-kpis.json",
				"top-bar-pill-update.json",
			},
			SubstrateBeads: []string{"hp-3at", "hp-sgj"},
		},
		{
			ID:               StepFreshEyesReview,
			Number:           13,
			Title:            "Run one fresh-eyes review and resolve one finding into a bead",
			Description:      "A fresh-eyes review round runs (direct-LLM mode); produces ≥1 finding; user disposes one finding as `new_bead` via the lifecycle; linked br bead created.",
			Stage:            StageHardening,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"review-round-artifact.json",
				"finding-disposition-trace.log",
				"created-bead.json",
			},
			SubstrateBeads: []string{"hp-8xm", "hp-k4j", "hp-v9hc"},
		},
		{
			ID:               StepKillRestartReplay,
			Number:           14,
			Title:            "Kill / restart desktop and daemon; verify replay/recovery",
			Description:      "Force-kill desktop; restart; sequence cursor + snapshot reconnect closes the gap with no UI corruption. Then force-kill daemon (systemd Type=notify); restart; jobs resume from persisted registry.",
			Stage:            StageOperations,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"desktop-kill-restart.log",
				"daemon-kill-restart.log",
				"replay-gap-closure.json",
				"job-registry-resume.json",
			},
		},
		{
			ID:               StepUpgradeDaemonCompatibility,
			Number:           15,
			Title:            "Upgrade daemon; verify compatibility checks",
			Description:      "Daemon vN-1 → vN: backup taken, migration runs, /v1/version reports new version, /v1/compatibility passes; desktop recovers without state loss.",
			Stage:            StageRelease,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"daemon-pre-upgrade-version.json",
				"daemon-post-upgrade-version.json",
				"compatibility-report.json",
				"backup-listing.txt",
			},
			SubstrateBeads: []string{"hp-4iz", "hp-o42"},
		},
		{
			ID:               StepNoSecretsInLogs,
			Number:           16,
			Title:            "Confirm no secrets appear in logs or audit artifacts",
			Description:      "Final sweep: scan all artifacts produced during the scenario for bearer tokens, API keys, SSH passphrases, provider credentials, browser cookies, pairing tokens; zero raw-form leaks.",
			Stage:            StageRelease,
			SupportsVariants: []Variant{VariantRealVPS, VariantMockFlywheel},
			EvidenceArtifacts: []string{
				"audit-log-secret-scan.txt",
				"structured-log-secret-scan.txt",
				"plan-job-artifact-secret-scan.txt",
				"crash-report-secret-scan.txt",
				"redaction-fixture-coverage.json",
			},
			SubstrateBeads: []string{"hp-g73"},
		},
	}
}

// Lookup returns the catalog entry for id, or false when the id
// is unknown.
func Lookup(id StepID) (ScenarioStep, bool) {
	for _, step := range Catalog() {
		if step.ID == id {
			return step, true
		}
	}
	return ScenarioStep{}, false
}

// Variant returns the subset of the catalog steps that support
// the given variant. Used by the runner to filter for the nightly
// mock-flywheel run vs the pre-release real-VPS run.
func ByVariant(variant Variant) []ScenarioStep {
	out := make([]ScenarioStep, 0, 16)
	for _, step := range Catalog() {
		for _, v := range step.SupportsVariants {
			if v == variant {
				out = append(out, step)
				break
			}
		}
	}
	return out
}

// ByStage returns the subset of the catalog steps that exercise
// the given cockpit stage. Used by failure messages to surface
// "stage 02 (Beads) regressed".
func ByStage(stage Stage) []ScenarioStep {
	out := make([]ScenarioStep, 0, 4)
	for _, step := range Catalog() {
		if step.Stage == stage {
			out = append(out, step)
		}
	}
	return out
}
