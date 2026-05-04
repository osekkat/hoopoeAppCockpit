// Package bv is the daemon-side adapter for the bv (beads-viewer) tool's
// `--robot-*` JSON surfaces per plan.md §2.3 BvAdapter + §1.3 (NEVER bare
// bv — bare invocation launches an interactive TUI that blocks automation).
//
// Wraps four robot commands:
//
//   - --robot-triage    Unified triage output (top picks, quick ref, plan,
//                       priorities, insights summary).
//   - --robot-plan      Dependency-respecting parallel execution tracks.
//   - --robot-insights  Graph metrics + bottlenecks + cycles + critical path.
//   - --robot-diff      Changes between two revisions (since-ref aware).
//
// Plus --robot-next as a convenience (single top recommendation; unblocked
// in the bead's "consider" set but not strictly required by the DOD).
//
// All four feed into `/v1/projects/{id}/beads/graph` (the renderer's DAG
// view), the bead-quality dashboard (cycle detection), and tending pre-
// scripts that read --robot-insights to detect graph-health issues.
//
// The adapter keeps the typed surface narrow — fields the daemon + renderer
// actually consume. Unknown / extension fields are preserved as
// json.RawMessage so a bv version bump that adds new fields doesn't break
// the parser.
package bv

import (
	"encoding/json"
	"time"
)

// TriageOutput is the top-level shape of `bv --robot-triage`.
type TriageOutput struct {
	GeneratedAt time.Time `json:"generated_at"`
	DataHash    string    `json:"data_hash"`
	Triage      Triage    `json:"triage"`

	// Status mirrors any "status" field present on some installs.
	Status string `json:"status,omitempty"`

	// Raw preserves the original bytes for forensic / replay use.
	Raw json.RawMessage `json:"-"`
}

// Triage is the inner triage block of TriageOutput.
type Triage struct {
	Meta     TriageMeta     `json:"meta"`
	QuickRef TriageQuickRef `json:"quick_ref"`

	// Recommendations / Insights / Plan summaries are present on
	// some bv versions; left as RawMessage to avoid coupling to
	// internal layouts that change per release.
	Plan            json.RawMessage `json:"plan,omitempty"`
	Insights        json.RawMessage `json:"insights,omitempty"`
	Recommendations json.RawMessage `json:"recommendations,omitempty"`
}

// TriageMeta is the meta block inside Triage.
type TriageMeta struct {
	Version        string `json:"version"`
	GeneratedAt    string `json:"generated_at"`
	Phase2Ready    bool   `json:"phase2_ready"`
	IssueCount     int    `json:"issue_count"`
	ComputeTimeMs  int    `json:"compute_time_ms,omitempty"`
}

// TriageQuickRef is the most-consumed slice — top picks + counts.
type TriageQuickRef struct {
	OpenCount       int        `json:"open_count"`
	ActionableCount int        `json:"actionable_count"`
	BlockedCount    int        `json:"blocked_count"`
	InProgressCount int        `json:"in_progress_count"`
	TopPicks        []TopPick  `json:"top_picks"`
}

// TopPick is one recommended bead from `triage.quick_ref.top_picks`.
type TopPick struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Score    float64  `json:"score"`
	Reasons  []string `json:"reasons"`
	Unblocks int      `json:"unblocks"`
}

// PlanOutput is the top-level shape of `bv --robot-plan`.
type PlanOutput struct {
	GeneratedAt    time.Time       `json:"generated_at"`
	DataHash       string          `json:"data_hash"`
	Plan           Plan            `json:"plan"`
	Status         string          `json:"status,omitempty"`
	AnalysisConfig json.RawMessage `json:"analysis_config,omitempty"`
	UsageHints     json.RawMessage `json:"usage_hints,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// Plan is the inner plan block.
type Plan struct {
	TotalActionable int         `json:"total_actionable"`
	TotalBlocked    int         `json:"total_blocked"`
	Tracks          []PlanTrack `json:"tracks"`
	Summary         PlanSummary `json:"summary"`
}

// PlanTrack is one parallel execution track.
type PlanTrack struct {
	Items []PlanItem `json:"items"`
}

// PlanItem is one bead inside a track.
type PlanItem struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Priority int      `json:"priority"`
	Unblocks []string `json:"unblocks,omitempty"`
}

// PlanSummary is the high-level summary for the whole plan.
type PlanSummary struct {
	HighestImpact  string `json:"highest_impact,omitempty"`
	ImpactReason   string `json:"impact_reason,omitempty"`
	UnblocksCount  int    `json:"unblocks_count"`
}

// InsightsOutput is the top-level shape of `bv --robot-insights`.
//
// The bv insights output uses TitleCase keys (CriticalPath, Bottlenecks,
// Cycles, Stats, Articulation, etc.) because the source tool serializes
// Go structs directly. We mirror that.
type InsightsOutput struct {
	CriticalPath []string        `json:"CriticalPath,omitempty"`
	Bottlenecks  []Bottleneck    `json:"Bottlenecks,omitempty"`
	Cycles       json.RawMessage `json:"Cycles,omitempty"`
	Stats        InsightsStats   `json:"Stats,omitempty"`
	Articulation []string        `json:"Articulation,omitempty"`

	// Authorities / Hubs / KCore / Slack / TopologicalOrder are
	// present on some bv versions; preserved as RawMessage.
	Authorities       json.RawMessage `json:"Authorities,omitempty"`
	Hubs              json.RawMessage `json:"Hubs,omitempty"`
	KCore             json.RawMessage `json:"KCore,omitempty"`
	Slack             json.RawMessage `json:"Slack,omitempty"`
	TopologicalOrder  json.RawMessage `json:"TopologicalOrder,omitempty"`
	GeneratedAt       json.RawMessage `json:"GeneratedAt,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// Bottleneck is one entry in InsightsOutput.Bottlenecks.
type Bottleneck struct {
	ID            string  `json:"ID"`
	Title         string  `json:"Title,omitempty"`
	BlockedCount  int     `json:"BlockedCount,omitempty"`
	BlockedDepth  int     `json:"BlockedDepth,omitempty"`
	Severity      string  `json:"Severity,omitempty"`
	PageRank      float64 `json:"PageRank,omitempty"`
	Betweenness   float64 `json:"Betweenness,omitempty"`
}

// InsightsStats is the metrics summary inside InsightsOutput.Stats.
// Each metric is a map keyed by issue id; values are the metric scores.
type InsightsStats struct {
	PageRank          json.RawMessage `json:"PageRank,omitempty"`
	Betweenness       json.RawMessage `json:"Betweenness,omitempty"`
	TopologicalOrder  json.RawMessage `json:"TopologicalOrder,omitempty"`
}

// DiffOutput is the top-level shape of `bv --robot-diff --diff-since <ref>`.
type DiffOutput struct {
	GeneratedAt      time.Time `json:"generated_at"`
	ResolvedRevision string    `json:"resolved_revision"`
	FromDataHash     string    `json:"from_data_hash"`
	ToDataHash       string    `json:"to_data_hash"`
	Diff             Diff      `json:"diff"`

	Raw json.RawMessage `json:"-"`
}

// Diff is the inner diff block.
type Diff struct {
	FromTimestamp   time.Time      `json:"from_timestamp"`
	ToTimestamp     time.Time      `json:"to_timestamp"`
	FromRevision    string         `json:"from_revision"`
	NewIssues       []DiffIssue    `json:"new_issues"`
	ClosedIssues    []DiffIssue    `json:"closed_issues"`
	RemovedIssues   []DiffIssue    `json:"removed_issues"`
	ReopenedIssues  []DiffIssue    `json:"reopened_issues"`
	ModifiedIssues  []DiffModified `json:"modified_issues"`
	NewCycles       json.RawMessage `json:"new_cycles,omitempty"`
	ResolvedCycles  json.RawMessage `json:"resolved_cycles,omitempty"`
	MetricDeltas    DiffMetrics    `json:"metric_deltas"`
	Summary         DiffSummary    `json:"summary"`
}

// DiffIssue is a full issue snapshot inside a diff section.
// We only model the high-level fields the renderer + tending need.
type DiffIssue struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Status      string          `json:"status"`
	Priority    int             `json:"priority"`
	IssueType   string          `json:"issue_type,omitempty"`
	CreatedAt   time.Time       `json:"created_at,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at,omitempty"`
	ClosedAt    *time.Time      `json:"closed_at,omitempty"`

	// Dependencies + Description are present but typically large;
	// preserved raw.
	Description  string          `json:"description,omitempty"`
	Dependencies json.RawMessage `json:"dependencies,omitempty"`
	SourceRepo   string          `json:"source_repo,omitempty"`
}

// DiffModified records a per-issue change set.
type DiffModified struct {
	IssueID string             `json:"issue_id"`
	Title   string             `json:"title"`
	Changes []DiffFieldChange  `json:"changes"`
}

// DiffFieldChange is one field that changed on an issue.
type DiffFieldChange struct {
	Field    string          `json:"field"`
	OldValue json.RawMessage `json:"old_value"`
	NewValue json.RawMessage `json:"new_value"`
}

// DiffMetrics is the metric deltas summary.
type DiffMetrics struct {
	TotalIssues      int     `json:"total_issues"`
	OpenIssues       int     `json:"open_issues"`
	ClosedIssues     int     `json:"closed_issues"`
	BlockedIssues    int     `json:"blocked_issues"`
	TotalEdges       int     `json:"total_edges"`
	CycleCount       int     `json:"cycle_count"`
	ComponentCount   int     `json:"component_count"`
	AvgPageRank      float64 `json:"avg_pagerank"`
	AvgBetweenness   float64 `json:"avg_betweenness"`
}

// DiffSummary is the human-readable summary of the diff.
type DiffSummary struct {
	TotalChanges      int    `json:"total_changes"`
	IssuesAdded       int    `json:"issues_added"`
	IssuesClosed      int    `json:"issues_closed"`
	IssuesRemoved     int    `json:"issues_removed"`
	IssuesReopened    int    `json:"issues_reopened"`
	IssuesModified    int    `json:"issues_modified"`
	CyclesIntroduced  int    `json:"cycles_introduced"`
	CyclesResolved    int    `json:"cycles_resolved"`
	NetIssueChange    int    `json:"net_issue_change"`
	HealthTrend       string `json:"health_trend"`
}

// NextOutput is the top-level shape of `bv --robot-next`.
type NextOutput struct {
	GeneratedAt time.Time       `json:"generated_at"`
	DataHash    string          `json:"data_hash"`
	Next        json.RawMessage `json:"next,omitempty"`
	Status      string          `json:"status,omitempty"`

	Raw json.RawMessage `json:"-"`
}
