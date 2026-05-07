package exportbundle

import "time"

// RetentionDomain identifies one §10.3 retention class. Each
// domain has its own TTL or compaction strategy.
type RetentionDomain string

const (
	RetentionAuditLog               RetentionDomain = "audit_log"
	RetentionTerminalLogs           RetentionDomain = "terminal_logs"
	RetentionBuildLogs              RetentionDomain = "build_logs"
	RetentionBootstrapLogs          RetentionDomain = "bootstrap_logs"
	RetentionModelRawArtifacts      RetentionDomain = "model_raw_artifacts"
	RetentionHealthSnapshots        RetentionDomain = "health_snapshots"
	RetentionEventReplayLog         RetentionDomain = "event_replay_log"
	RetentionSkillInstalls          RetentionDomain = "skill_installs"
)

// CompactionStrategy describes how the retention pruner reduces
// a domain's footprint. Different domains use different mixes:
// pure TTL (drop after N days), keep-last-N (snapshots),
// rolling-snapshot (compact older entries into a smaller summary
// representation).
type CompactionStrategy string

const (
	// CompactionDropAfterTTL: entries older than the TTL are
	// deleted entirely. Used for logs.
	CompactionDropAfterTTL CompactionStrategy = "drop_after_ttl"

	// CompactionKeepLastN: keep the N most recent entries. Used
	// for snapshots where the user wants recent detail and a
	// rolling history.
	CompactionKeepLastN CompactionStrategy = "keep_last_n"

	// CompactionRollingSnapshot: compact older entries into a
	// summary representation rather than dropping. Used for the
	// event-replay log where reconnect needs at least some
	// history but full per-event detail isn't needed beyond a
	// recency window.
	CompactionRollingSnapshot CompactionStrategy = "rolling_snapshot"

	// CompactionIndefinite: never auto-drop. The user must
	// explicitly export-and-prune. Used for the audit log per
	// §10.3 ("indefinite unless user exports + prunes").
	CompactionIndefinite CompactionStrategy = "indefinite"

	// CompactionPinned: not subject to TTL — pinned via lock
	// file. Used for skill installs (`.hoopoe/skills.lock.json`).
	CompactionPinned CompactionStrategy = "pinned"
)

// RetentionPolicy is the §10.3 row for one domain.
type RetentionPolicy struct {
	Domain      RetentionDomain    `json:"domain"`
	Description string             `json:"description"`
	Strategy    CompactionStrategy `json:"strategy"`

	// DefaultTTL is the default retention window for strategies
	// that use one (CompactionDropAfterTTL primarily). Zero for
	// strategies that don't apply a TTL.
	DefaultTTL time.Duration `json:"defaultTtl"`

	// DefaultKeepN is the default keep-N count for
	// CompactionKeepLastN. Zero for strategies that don't apply.
	DefaultKeepN int `json:"defaultKeepN"`

	// PerProjectConfigurable indicates whether the user can
	// override the default in project settings.
	PerProjectConfigurable bool `json:"perProjectConfigurable"`
}

// RetentionCatalog returns the §10.3 source-of-truth list of
// retention policies. The pruner iterates these to schedule its
// per-domain compaction passes.
func RetentionCatalog() []RetentionPolicy {
	return []RetentionPolicy{
		{
			Domain:                 RetentionAuditLog,
			Description:            "Audit log entries (Guardrail 10). Indefinite unless user exports + prunes; export-and-drop is the only compaction path.",
			Strategy:               CompactionIndefinite,
			PerProjectConfigurable: false,
		},
		{
			Domain:                 RetentionTerminalLogs,
			Description:            "Terminal pane scrollback rollups. 30-day default TTL, configurable per project.",
			Strategy:               CompactionDropAfterTTL,
			DefaultTTL:             30 * 24 * time.Hour,
			PerProjectConfigurable: true,
		},
		{
			Domain:                 RetentionBuildLogs,
			Description:            "Build/test execution logs from rch + language-native runners. 30-day default TTL.",
			Strategy:               CompactionDropAfterTTL,
			DefaultTTL:             30 * 24 * time.Hour,
			PerProjectConfigurable: true,
		},
		{
			Domain:                 RetentionBootstrapLogs,
			Description:            "ACFS bootstrap + daemon-install streamed logs. 30-day default TTL.",
			Strategy:               CompactionDropAfterTTL,
			DefaultTTL:             30 * 24 * time.Hour,
			PerProjectConfigurable: true,
		},
		{
			Domain:                 RetentionModelRawArtifacts,
			Description:            "Raw plan-job model outputs (pre-redaction). 30-day private retention default; redacted versions move to audit log.",
			Strategy:               CompactionDropAfterTTL,
			DefaultTTL:             30 * 24 * time.Hour,
			PerProjectConfigurable: true,
		},
		{
			Domain:                 RetentionHealthSnapshots,
			Description:            "Per-project health snapshots. Keep last N + compacted trend history (older snapshots collapse into trend rollups).",
			Strategy:               CompactionKeepLastN,
			DefaultKeepN:           60,
			PerProjectConfigurable: true,
		},
		{
			Domain:                 RetentionEventReplayLog,
			Description:            "WS event replay log. Rolling-snapshot: keep enough recent events to cover reconnects, then compact into snapshots.",
			Strategy:               CompactionRollingSnapshot,
			DefaultTTL:             7 * 24 * time.Hour,
			PerProjectConfigurable: false,
		},
		{
			Domain:                 RetentionSkillInstalls,
			Description:            "Installed skills pinned via .hoopoe/skills.lock.json. Not subject to TTL — managed by jsm/jfp install flow.",
			Strategy:               CompactionPinned,
			PerProjectConfigurable: false,
		},
	}
}

// LookupPolicy returns the policy for the given domain, or false
// when the domain is unknown.
func LookupPolicy(domain RetentionDomain) (RetentionPolicy, bool) {
	for _, policy := range RetentionCatalog() {
		if policy.Domain == domain {
			return policy, true
		}
	}
	return RetentionPolicy{}, false
}
