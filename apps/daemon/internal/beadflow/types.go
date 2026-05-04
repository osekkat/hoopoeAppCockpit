// Package beadflow owns deterministic Phase 6 bead-conversion primitives.
//
// It deliberately avoids invoking br, bv, or agent CLIs directly. Callers pass
// canonical data from the br and bv adapters, and beadflow builds traceability,
// quality, and implementation-evidence artifacts from those typed inputs.
package beadflow

import "time"

const (
	TraceabilitySchemaVersion = 1
	QualitySchemaVersion      = 1
	EvidenceSchemaVersion     = 1
)

type CoverageStatus string

const (
	CoverageCovered CoverageStatus = "covered"
	CoveragePartial CoverageStatus = "partial"
	CoverageMissing CoverageStatus = "missing"
	CoverageOrphan  CoverageStatus = "orphan"
)

type PlanSection struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	RequiredTests   []string `json:"requiredTests,omitempty"`
	AcceptanceItems []string `json:"acceptanceItems,omitempty"`
}

type Bead struct {
	ID                  string   `json:"id"`
	Title               string   `json:"title"`
	Description         string   `json:"description,omitempty"`
	Status              string   `json:"status,omitempty"`
	Priority            int      `json:"priority,omitempty"`
	PlanSections        []string `json:"planSections,omitempty"`
	TestObligations     []string `json:"testObligations,omitempty"`
	AcceptanceCriteria  []string `json:"acceptanceCriteria,omitempty"`
	DependsOn           []string `json:"dependsOn,omitempty"`
	EstimatedMinutes    int      `json:"estimatedMinutes,omitempty"`
	ImplementationNotes []string `json:"implementationNotes,omitempty"`
}

type TraceabilityMap struct {
	SchemaVersion     int              `json:"schemaVersion"`
	PlanID            string           `json:"planId"`
	GeneratedAt       time.Time        `json:"generatedAt"`
	Sections          []SectionTrace   `json:"sections"`
	Beads             []BeadTrace      `json:"beads"`
	UnmappedSections  []SectionTrace   `json:"unmappedSections,omitempty"`
	OrphanBeads       []BeadTrace      `json:"orphanBeads,omitempty"`
	OversizedBeads    []BeadTrace      `json:"oversizedBeads,omitempty"`
	DuplicateGroups   []DuplicateBeads `json:"duplicateGroups,omitempty"`
	MissingTestBeads  []BeadTrace      `json:"missingTestBeads,omitempty"`
	PlanCoverageScore int              `json:"planCoverageScore"`
}

type SectionTrace struct {
	SectionID       string         `json:"sectionId"`
	Title           string         `json:"title"`
	Status          CoverageStatus `json:"status"`
	BeadIDs         []string       `json:"beadIds,omitempty"`
	RequiredTests   []string       `json:"requiredTests,omitempty"`
	MissingTests    []string       `json:"missingTests,omitempty"`
	AcceptanceItems []string       `json:"acceptanceItems,omitempty"`
}

type BeadTrace struct {
	BeadID              string         `json:"beadId"`
	Title               string         `json:"title"`
	Status              CoverageStatus `json:"status"`
	PlanSectionIDs      []string       `json:"planSectionIds,omitempty"`
	TestObligations     []string       `json:"testObligations,omitempty"`
	AcceptanceCriteria  []string       `json:"acceptanceCriteria,omitempty"`
	DependsOn           []string       `json:"dependsOn,omitempty"`
	EstimatedMinutes    int            `json:"estimatedMinutes,omitempty"`
	DescriptionWords    int            `json:"descriptionWords"`
	ImplementationNotes []string       `json:"implementationNotes,omitempty"`
}

type DuplicateBeads struct {
	Reason string   `json:"reason"`
	Beads  []string `json:"beads"`
}

type GraphHealth struct {
	Cycles         [][]string `json:"cycles,omitempty"`
	ReadyCount     int        `json:"readyCount"`
	BlockedCount   int        `json:"blockedCount"`
	ParallelTracks []Track    `json:"parallelTracks,omitempty"`
}

type Track struct {
	ID      string   `json:"id"`
	BeadIDs []string `json:"beadIds"`
}

type QualityReport struct {
	SchemaVersion int                `json:"schemaVersion"`
	PlanID        string             `json:"planId"`
	GeneratedAt   time.Time          `json:"generatedAt"`
	OverallScore  int                `json:"overallScore"`
	Dimensions    []QualityDimension `json:"dimensions"`
	Delta         *QualityDelta      `json:"delta,omitempty"`
	Recommended   string             `json:"recommendedPolishRound"`
	Findings      []string           `json:"findings,omitempty"`
}

type QualityDimension struct {
	Name     string   `json:"name"`
	Score    int      `json:"score"`
	Findings []string `json:"findings,omitempty"`
}

type QualityDelta struct {
	PreviousScore int `json:"previousScore"`
	CurrentScore  int `json:"currentScore"`
	Delta         int `json:"delta"`
}

type EvidenceKind string

const (
	EvidenceBeadClaimed   EvidenceKind = "bead_claimed"
	EvidenceCommitPushed  EvidenceKind = "commit_pushed"
	EvidenceTestRun       EvidenceKind = "test_run"
	EvidenceHealthDelta   EvidenceKind = "health_delta"
	EvidenceReviewFinding EvidenceKind = "review_finding"
	EvidenceLanding       EvidenceKind = "landing"
)

type EvidenceEntry struct {
	SchemaVersion    int          `json:"schemaVersion"`
	Kind             EvidenceKind `json:"kind"`
	PlanID           string       `json:"planId"`
	BeadID           string       `json:"beadId"`
	PlanSectionID    string       `json:"planSectionId,omitempty"`
	Branch           string       `json:"branch,omitempty"`
	Worktree         string       `json:"worktree,omitempty"`
	Commits          []string     `json:"commits,omitempty"`
	FilesTouched     []string     `json:"filesTouched,omitempty"`
	TestRuns         []string     `json:"testRuns,omitempty"`
	HealthDeltas     []string     `json:"healthDeltas,omitempty"`
	ReviewFindings   []string     `json:"reviewFindings,omitempty"`
	LandingQueueItem string       `json:"landingQueueItem,omitempty"`
	Actor            string       `json:"actor,omitempty"`
	Time             time.Time    `json:"time"`
}
