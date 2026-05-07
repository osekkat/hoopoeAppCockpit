package releasesmoke

// CheckID is the stable string identifier for a §18.4 smoke gate
// entry. Renaming an ID is a schema-version event because evidence
// artifacts under docs/test-evidence/release-<v>/ key on it.
type CheckID string

// The §18.4 catalog. Ten entries; this list is the single source
// of truth and the order matches the plan.md table.
const (
	CheckSignedAppLaunches            CheckID = "release.signed_app_launches"
	CheckAutoUpdateMockServer         CheckID = "release.auto_update_mock_server"
	CheckSettingsKeybindingsMigrate   CheckID = "release.settings_keybindings_migrate"
	CheckDaemonUpgradeBacksUpDB       CheckID = "release.daemon_upgrade_backs_up_db"
	CheckLocalCloneClearAndRebuild    CheckID = "release.local_clone_clear_and_rebuild"
	CheckEventReplayAfterSleep        CheckID = "release.event_replay_after_sleep"
	CheckProcessManagerCancelNoOrphan CheckID = "release.process_manager_cancel_no_orphan"
	CheckHealthWorktreeCleanup        CheckID = "release.health_worktree_cleanup"
	CheckAuditRedactionAllSecrets     CheckID = "release.audit_redaction_all_secrets"
	CheckExportRestoreRoundTrip       CheckID = "release.export_restore_round_trip"
)

// Severity is the impact class when the check fails. The CI gate
// fails the release on any single failure regardless of severity;
// the field exists so post-release triage can prioritize fixes.
type Severity string

const (
	// SeverityBlocking: any failure blocks release. Default.
	SeverityBlocking Severity = "blocking"

	// SeverityRegression: the check has historically been flaky;
	// failure should be investigated but might not block on its
	// own. Reserved for future use; today every §18.4 check is
	// blocking.
	SeverityRegression Severity = "regression"
)

// SLO is the optional service-level-objective target a check
// asserts against (per plan.md §10.5). Empty when the check is
// pass/fail without a numeric target.
type SLO struct {
	// Metric is the metric ID in the form `domain.action.statistic`.
	// e.g., `events.replay.10min.p95`.
	Metric string `json:"metric,omitempty"`

	// MaxValue is the upper bound the check enforces.
	MaxValue float64 `json:"maxValue,omitempty"`

	// Unit is the value's unit (`seconds`, `milliseconds`,
	// `count`). Empty when MaxValue is unitless.
	Unit string `json:"unit,omitempty"`
}

// SmokeCheck is one entry in the §18.4 catalog. The runner
// dispatches by ID and writes evidence under
// docs/test-evidence/release-<version>/<id>/.
type SmokeCheck struct {
	// ID is the stable string identifier; evidence and CI logs
	// reference it.
	ID CheckID `json:"id"`

	// Number is the §18.4 ordinal (1..10) so failure messages
	// can say "check 6 failed" matching the plan.
	Number int `json:"number"`

	// Title is the short user-facing name shown in CI logs and
	// failure messages.
	Title string `json:"title"`

	// Description is the one-paragraph spec the check enforces.
	Description string `json:"description"`

	// Severity is the impact class. All §18.4 entries are
	// SeverityBlocking today.
	Severity Severity `json:"severity"`

	// SLO is the optional numeric target the check enforces.
	// Zero-value when the check is pass/fail.
	SLO SLO `json:"slo"`

	// EvidenceArtifacts lists the per-check artifacts the runner
	// must write under docs/test-evidence/release-<version>/<id>/.
	// CI evidence retention follows §10.3 (released-version
	// evidence kept indefinitely; pre-release runs follow the
	// retention defaults).
	EvidenceArtifacts []string `json:"evidenceArtifacts"`

	// SubstrateBeads lists the implementation-owner beads this
	// check depends on so failure messages can point at the right
	// remediation owner.
	SubstrateBeads []string `json:"substrateBeads,omitempty"`
}

// Catalog returns the §18.4 source-of-truth list of smoke checks.
//
// Order matches plan.md §18.4 reading top-to-bottom; CI runs them
// in this order so failure-message numbering matches the plan.
func Catalog() []SmokeCheck {
	return []SmokeCheck{
		{
			ID:          CheckSignedAppLaunches,
			Number:      1,
			Title:       "Signed app launches on arm64 + x64 macOS",
			Description: "Signed/notarized DMG opens on both arm64 and x64; app runs end-to-end without signing or notarization warnings.",
			Severity:    SeverityBlocking,
			EvidenceArtifacts: []string{
				"app-launch-arm64.log",
				"app-launch-x64.log",
				"codesign-verify.txt",
				"notarization-status.txt",
			},
			SubstrateBeads: []string{"hp-191"},
		},
		{
			ID:          CheckAutoUpdateMockServer,
			Number:      2,
			Title:       "Auto-update mock server stable↔beta",
			Description: "Auto-update mock server upgrades stable → beta and downgrades beta → stable; rollback works without state loss.",
			Severity:    SeverityBlocking,
			EvidenceArtifacts: []string{
				"upgrade-stable-to-beta.log",
				"rollback-beta-to-stable.log",
				"updater-feed.yml",
			},
			SubstrateBeads: []string{"hp-191"},
		},
		{
			ID:          CheckSettingsKeybindingsMigrate,
			Number:      3,
			Title:       "Settings + keybindings survive migration",
			Description: "Start app on N-1 release, upgrade to N; settings, keybindings, and custom commands all preserved across the migration.",
			Severity:    SeverityBlocking,
			EvidenceArtifacts: []string{
				"settings-before.json",
				"settings-after.json",
				"keybindings-before.json",
				"keybindings-after.json",
				"migration-log.txt",
			},
		},
		{
			ID:          CheckDaemonUpgradeBacksUpDB,
			Number:      4,
			Title:       "Daemon upgrade backs up config + DB",
			Description: "Daemon vN-1 → vN: config and DB backed up before migration; /v1/version reports new version; /v1/compatibility checks pass.",
			Severity:    SeverityBlocking,
			EvidenceArtifacts: []string{
				"daemon-pre-upgrade-version.json",
				"daemon-post-upgrade-version.json",
				"db-backup-listing.txt",
				"compatibility-report.json",
			},
			SubstrateBeads: []string{"hp-4iz", "hp-o42"},
		},
		{
			ID:          CheckLocalCloneClearAndRebuild,
			Number:      5,
			Title:       "Local clone cache clears + rebuilds",
			Description: "Diagnostics 'Clear local clone' deletes the desktop sync mirror; next access re-clones from origin; no orphan files remain.",
			Severity:    SeverityBlocking,
			EvidenceArtifacts: []string{
				"clone-pre-clear.txt",
				"clone-post-clear.txt",
				"clone-post-rebuild.txt",
				"orphan-scan.json",
			},
			SubstrateBeads: []string{"hp-6d7"},
		},
		{
			ID:          CheckEventReplayAfterSleep,
			Number:      6,
			Title:       "Event replay after simulated 10-min sleep",
			Description: "Desktop sleeps 10 min; resumes; sequence-cursor reconnect closes the gap; no UI corruption.",
			Severity:    SeverityBlocking,
			SLO: SLO{
				Metric:   "events.replay.10min.p95",
				MaxValue: 5.0,
				Unit:     "seconds",
			},
			EvidenceArtifacts: []string{
				"sleep-resume-trace.log",
				"sequence-cursor-progression.json",
				"replay-latency-samples.csv",
			},
		},
		{
			ID:          CheckProcessManagerCancelNoOrphan,
			Number:      7,
			Title:       "Process manager cancels long job without orphans",
			Description: "Long-running job → cancel → SIGTERM → SIGKILL escalation completes within the timeout; no orphan child process group remains.",
			Severity:    SeverityBlocking,
			SLO: SLO{
				Metric: "job.cancellation.no-orphans",
			},
			EvidenceArtifacts: []string{
				"cancel-escalation-trace.log",
				"process-tree-pre.txt",
				"process-tree-post.txt",
				"orphan-pid-scan.txt",
			},
		},
		{
			ID:          CheckHealthWorktreeCleanup,
			Number:      8,
			Title:       "Health worktree cleanup",
			Description: "After 100 health snapshots in a fixture project, no stale worktrees remain beyond the retention window.",
			Severity:    SeverityBlocking,
			EvidenceArtifacts: []string{
				"worktree-listing-after-100-runs.txt",
				"retention-window.json",
				"disk-pressure-summary.json",
			},
		},
		{
			ID:          CheckAuditRedactionAllSecrets,
			Number:      9,
			Title:       "Audit redaction catches every secret class",
			Description: "Bearer tokens, model API keys, SSH passphrases, provider credentials, browser cookies, and pairing tokens all redacted across audit log + structured-log evidence + crash reports + plan-job artifacts.",
			Severity:    SeverityBlocking,
			EvidenceArtifacts: []string{
				"redaction-fixture-results.json",
				"audit-log-scan.txt",
				"structured-log-scan.txt",
				"crash-report-scan.txt",
				"plan-job-artifact-scan.txt",
			},
			SubstrateBeads: []string{"hp-g73"},
		},
		{
			ID:          CheckExportRestoreRoundTrip,
			Number:      10,
			Title:       "Project export/restore round-trip",
			Description: "Export bundle includes plan + beads traceability + findings + landing history + artifact hashes; restore produces a byte-identical artifact set; no secrets in export.",
			Severity:    SeverityBlocking,
			EvidenceArtifacts: []string{
				"export-bundle-manifest.json",
				"restore-artifact-hashes.txt",
				"round-trip-diff.txt",
				"export-secret-scan.txt",
			},
			SubstrateBeads: []string{"hp-o42"},
		},
	}
}

// Lookup returns the catalog entry for id, or false when the id
// is unknown. Use this in runner dispatch to refuse unknown ids
// before they reach side-effecting code.
func Lookup(id CheckID) (SmokeCheck, bool) {
	for _, check := range Catalog() {
		if check.ID == id {
			return check, true
		}
	}
	return SmokeCheck{}, false
}
