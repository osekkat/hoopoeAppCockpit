// Package flip owns the deterministic transition contract from implementation
// swarm mode into hardening mode.
//
// The package is intentionally adapter-light. Callers pass canonical snapshots
// and tool decisions in, then flip returns audit-ready state transitions,
// snapshot manifests, tending job patches, UI events, and reversibility
// decisions. NTM launch, renderer routing, approval persistence, and scheduler
// mutation stay in their owning packages.
package flip

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/projects/gates"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	SchemaVersion              = 1
	DefaultDrainGrace          = 10 * time.Minute
	DefaultReversibilityWindow = 24 * time.Hour

	ActionHardeningFlipInitiated = "hardening_flip_initiated"
	ActionDrainWaiting           = "hardening_flip_drain_waiting"
	ActionDrainNeedsResolution   = "hardening_flip_drain_needs_resolution"
	ActionHardeningFlipCompleted = "hardening_flip_completed"
	ActionReturnRequested        = "hardening_return_to_implementation_requested"

	SnapshotFileName = "implementation-state.json"
)

var (
	ErrInvalidInput      = errors.New("flip: invalid input")
	ErrGateBlocked       = errors.New("flip: hardening gate blocked")
	ErrDrainUnresolved   = errors.New("flip: drain has unresolved in-flight beads")
	ErrInvalidResolution = errors.New("flip: invalid drain resolution")
	ErrSnapshotMismatch  = errors.New("flip: snapshot digest mismatch")
)

type State string

const (
	StateRefused                 State = "refused"
	StateDraining                State = "draining"
	StateAwaitingDrainResolution State = "awaiting_drain_resolution"
	StateCompleted               State = "completed"
)

type DrainState string

const (
	DrainComplete           DrainState = "complete"
	DrainInProgress         DrainState = "in_progress"
	DrainAwaitingResolution DrainState = "awaiting_resolution"
)

type BeadStatus string

const (
	BeadInProgress BeadStatus = "in_progress"
	BeadCompleted  BeadStatus = "completed"
	BeadClosed     BeadStatus = "closed"
	BeadDeferred   BeadStatus = "deferred"
)

type DrainResolution string

const (
	ResolutionCarryToHardening DrainResolution = "carry_to_hardening"
	ResolutionDefer            DrainResolution = "defer"
)

type TendingPatchAction string

const (
	PatchPause    TendingPatchAction = "pause"
	PatchResume   TendingPatchAction = "resume"
	PatchCreate   TendingPatchAction = "create"
	PatchRetune   TendingPatchAction = "retune"
	PatchNoChange TendingPatchAction = "no_change"
)

type Request struct {
	FlipID                string                 `json:"flipId,omitempty"`
	ProjectID             string                 `json:"projectId"`
	ImplementationSwarmID string                 `json:"implementationSwarmId"`
	Actor                 schemas.Actor          `json:"actor"`
	GateSnapshot          gates.Snapshot         `json:"gateSnapshot"`
	Override              *OverrideApproval      `json:"override,omitempty"`
	Drain                 DrainInput             `json:"drain"`
	Snapshot              ImplementationSnapshot `json:"snapshot"`
	SnapshotRoot          string                 `json:"snapshotRoot,omitempty"`
	Hardening             HardeningLaunchSpec    `json:"hardening"`
	TendingJobs           []TendingJob           `json:"tendingJobs,omitempty"`
	TendingPlan           TendingSwitchPlan      `json:"tendingPlan,omitempty"`
	Now                   time.Time              `json:"now,omitempty"`
	ReversibilityWindow   time.Duration          `json:"reversibilityWindow,omitempty"`
	Metadata              map[string]string      `json:"metadata,omitempty"`
}

type Plan struct {
	SchemaVersion         int                 `json:"schemaVersion"`
	FlipID                string              `json:"flipId"`
	ProjectID             string              `json:"projectId"`
	ImplementationSwarmID string              `json:"implementationSwarmId"`
	HardeningSwarmID      string              `json:"hardeningSwarmId,omitempty"`
	State                 State               `json:"state"`
	StartedAt             time.Time           `json:"startedAt"`
	CompletedAt           *time.Time          `json:"completedAt,omitempty"`
	ReversibleUntil       *time.Time          `json:"reversibleUntil,omitempty"`
	Gate                  GateDecision        `json:"gate"`
	Drain                 DrainReport         `json:"drain"`
	Snapshot              *SnapshotManifest   `json:"snapshot,omitempty"`
	Hardening             HardeningLaunchSpec `json:"hardening"`
	Tending               TendingSwitchResult `json:"tending"`
	UI                    *UIFlipEvent        `json:"ui,omitempty"`
	Problem               *schemas.Problem    `json:"problem,omitempty"`
	Audit                 []AuditEvent        `json:"audit"`
	Metadata              map[string]string   `json:"metadata,omitempty"`
}

type OverrideApproval struct {
	ApprovalID string        `json:"approvalId"`
	Approved   bool          `json:"approved"`
	Reason     string        `json:"reason,omitempty"`
	Actor      schemas.Actor `json:"actor"`
	DecidedAt  time.Time     `json:"decidedAt,omitempty"`
}

type GateDecision struct {
	Satisfied        bool                     `json:"satisfied"`
	OverrideAccepted bool                     `json:"overrideAccepted"`
	ApprovalID       string                   `json:"approvalId,omitempty"`
	MissingCheckIDs  []string                 `json:"missingCheckIds,omitempty"`
	Readiness        schemas.ProjectReadiness `json:"readiness"`
	Problem          *schemas.Problem         `json:"problem,omitempty"`
}

type DrainInput struct {
	GraceStartedAt time.Time                  `json:"graceStartedAt,omitempty"`
	Now            time.Time                  `json:"now,omitempty"`
	GraceWindow    time.Duration              `json:"graceWindow,omitempty"`
	InFlight       []InFlightBead             `json:"inFlight,omitempty"`
	Resolutions    map[string]DrainResolution `json:"resolutions,omitempty"`
}

type InFlightBead struct {
	BeadID         string     `json:"beadId"`
	AgentID        string     `json:"agentId,omitempty"`
	Status         BeadStatus `json:"status"`
	StartedAt      time.Time  `json:"startedAt,omitempty"`
	LastActivityAt time.Time  `json:"lastActivityAt,omitempty"`
}

type DrainChoice struct {
	BeadID       string    `json:"beadId"`
	AgentID      string    `json:"agentId,omitempty"`
	Choices      []string  `json:"choices"`
	GraceExpired bool      `json:"graceExpired"`
	Deadline     time.Time `json:"deadline"`
}

type DrainResolutionRecord struct {
	BeadID     string          `json:"beadId"`
	AgentID    string          `json:"agentId,omitempty"`
	Resolution DrainResolution `json:"resolution"`
}

type DrainReport struct {
	State           DrainState              `json:"state"`
	InFlightCount   int                     `json:"inFlightCount"`
	CompletedCount  int                     `json:"completedCount"`
	UnresolvedCount int                     `json:"unresolvedCount"`
	CarryCount      int                     `json:"carryCount"`
	DeferCount      int                     `json:"deferCount"`
	GraceStartedAt  time.Time               `json:"graceStartedAt"`
	GraceEndsAt     time.Time               `json:"graceEndsAt"`
	GraceRemaining  time.Duration           `json:"graceRemaining"`
	RequiredChoices []DrainChoice           `json:"requiredChoices,omitempty"`
	Resolved        []DrainResolutionRecord `json:"resolved,omitempty"`
}

type ImplementationSnapshot struct {
	SchemaVersion         int                         `json:"schemaVersion"`
	FlipID                string                      `json:"flipId"`
	ProjectID             string                      `json:"projectId"`
	ImplementationSwarmID string                      `json:"implementationSwarmId"`
	CapturedAt            time.Time                   `json:"capturedAt"`
	Beads                 map[string]BeadSnapshot     `json:"beads"`
	Agents                map[string]AgentSnapshot    `json:"agents"`
	MailThreadIndexHashes map[string]string           `json:"mailThreadIndexHashes,omitempty"`
	BuildQueue            BuildQueueSnapshot          `json:"buildQueue"`
	PushStatus            map[string]BranchPushStatus `json:"pushStatus,omitempty"`
	AuditRange            AuditSequenceRange          `json:"auditRange"`
	Metadata              map[string]string           `json:"metadata,omitempty"`
}

type BeadSnapshot struct {
	Status          string    `json:"status"`
	AssignedAgentID string    `json:"assignedAgentId,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt,omitempty"`
}

type AgentSnapshot struct {
	LastClaimedBeadID string    `json:"lastClaimedBeadId,omitempty"`
	FinalStatus       string    `json:"finalStatus"`
	LastActivityAt    time.Time `json:"lastActivityAt,omitempty"`
}

type BuildQueueSnapshot struct {
	Running int      `json:"running"`
	Queued  int      `json:"queued"`
	Blocked bool     `json:"blocked"`
	JobIDs  []string `json:"jobIds,omitempty"`
}

type BranchPushStatus struct {
	Branch        string    `json:"branch"`
	LastPushedSHA string    `json:"lastPushedSha"`
	PushedAt      time.Time `json:"pushedAt,omitempty"`
	Clean         bool      `json:"clean"`
}

type AuditSequenceRange struct {
	FirstSequence int64 `json:"firstSequence"`
	LastSequence  int64 `json:"lastSequence"`
}

type SnapshotManifest struct {
	SchemaVersion int       `json:"schemaVersion"`
	FlipID        string    `json:"flipId"`
	ProjectID     string    `json:"projectId"`
	ArtifactID    string    `json:"artifactId"`
	Path          string    `json:"path"`
	SHA256        string    `json:"sha256"`
	SizeBytes     int       `json:"sizeBytes"`
	FieldCount    int       `json:"fieldCount"`
	WrittenAt     time.Time `json:"writtenAt"`
}

type HardeningLaunchSpec struct {
	SwarmID                  string            `json:"swarmId,omitempty"`
	ReviewTopicID            string            `json:"reviewTopicId,omitempty"`
	KickoffPromptTemplateID  string            `json:"kickoffPromptTemplateId,omitempty"`
	KickoffPromptVersion     string            `json:"kickoffPromptVersion,omitempty"`
	Composition              []ReviewAgentSpec `json:"composition,omitempty"`
	Skills                   []string          `json:"skills,omitempty"`
	Round0AutoStart          bool              `json:"round0AutoStart"`
	FindingLedgerInitialized bool              `json:"findingLedgerInitialized"`
	Metadata                 map[string]string `json:"metadata,omitempty"`
}

type ReviewAgentSpec struct {
	Role    string `json:"role"`
	Harness string `json:"harness"`
	Count   int    `json:"count"`
}

type TendingJob struct {
	ID      string        `json:"id"`
	Paused  bool          `json:"paused"`
	Cadence time.Duration `json:"cadence,omitempty"`
	Mode    string        `json:"mode,omitempty"`
}

type TendingSwitchPlan struct {
	Pause    []string                 `json:"pause,omitempty"`
	Activate []TendingJob             `json:"activate,omitempty"`
	Retune   map[string]time.Duration `json:"retune,omitempty"`
}

type TendingSwitchResult struct {
	IdempotencyKey string            `json:"idempotencyKey"`
	Jobs           []TendingJob      `json:"jobs,omitempty"`
	Patches        []TendingJobPatch `json:"patches,omitempty"`
	PausedCount    int               `json:"pausedCount"`
	ActivatedCount int               `json:"activatedCount"`
	RetunedCount   int               `json:"retunedCount"`
	Warnings       []string          `json:"warnings,omitempty"`
}

type TendingJobPatch struct {
	JobID       string             `json:"jobId"`
	Action      TendingPatchAction `json:"action"`
	FromPaused  bool               `json:"fromPaused,omitempty"`
	ToPaused    bool               `json:"toPaused,omitempty"`
	FromCadence time.Duration      `json:"fromCadence,omitempty"`
	ToCadence   time.Duration      `json:"toCadence,omitempty"`
}

type UIFlipEvent struct {
	SchemaVersion         int       `json:"schemaVersion"`
	EventID               string    `json:"eventId"`
	Type                  string    `json:"type"`
	ProjectID             string    `json:"projectId"`
	FlipID                string    `json:"flipId"`
	ImplementationSwarmID string    `json:"implementationSwarmId"`
	HardeningSwarmID      string    `json:"hardeningSwarmId"`
	Route                 string    `json:"route"`
	ActiveStage           string    `json:"activeStage"`
	SwarmMode             string    `json:"swarmMode"`
	SwarmStatePill        string    `json:"swarmStatePill"`
	BeadBoardFilters      []string  `json:"beadBoardFilters"`
	ConvergenceMounted    bool      `json:"convergenceMounted"`
	At                    time.Time `json:"at"`
}

type UIState struct {
	ProjectID          string   `json:"projectId"`
	ActiveStage        string   `json:"activeStage"`
	Route              string   `json:"route"`
	SwarmMode          string   `json:"swarmMode"`
	SwarmStatePill     string   `json:"swarmStatePill"`
	BeadBoardFilters   []string `json:"beadBoardFilters"`
	ConvergenceMounted bool     `json:"convergenceMounted"`
	AppliedEventID     string   `json:"appliedEventId"`
}

type AuditEvent struct {
	SchemaVersion         int               `json:"schemaVersion"`
	EventID               string            `json:"eventId"`
	Action                string            `json:"action"`
	Result                string            `json:"result"`
	ProjectID             string            `json:"projectId"`
	FlipID                string            `json:"flipId"`
	ImplementationSwarmID string            `json:"implementationSwarmId"`
	HardeningSwarmID      string            `json:"hardeningSwarmId,omitempty"`
	Actor                 schemas.Actor     `json:"actor"`
	CorrelationID         string            `json:"correlationId"`
	At                    time.Time         `json:"at"`
	Data                  map[string]string `json:"data,omitempty"`
}

type ReturnDecision struct {
	Allowed                   bool                 `json:"allowed"`
	RequiresApproval          bool                 `json:"requiresApproval"`
	Reason                    string               `json:"reason"`
	ReversibleUntil           time.Time            `json:"reversibleUntil"`
	CarryBackBeads            []string             `json:"carryBackBeads,omitempty"`
	RestoreImplementationJobs bool                 `json:"restoreImplementationJobs"`
	HaltHardeningSwarm        bool                 `json:"haltHardeningSwarm"`
	ApprovalAction            *schemas.CommandSpec `json:"approvalAction,omitempty"`
}

func BuildPlan(req Request) (Plan, error) {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(req.GateSnapshot.ProjectID)
	}
	if projectID == "" {
		return Plan{}, fmt.Errorf("%w: projectId is required", ErrInvalidInput)
	}
	implementationSwarmID := strings.TrimSpace(req.ImplementationSwarmID)
	if implementationSwarmID == "" {
		return Plan{}, fmt.Errorf("%w: implementationSwarmId is required", ErrInvalidInput)
	}
	if !req.Actor.Kind.Valid() {
		return Plan{}, fmt.Errorf("%w: actor.kind is required", ErrInvalidInput)
	}
	flipID := strings.TrimSpace(req.FlipID)
	if flipID == "" {
		flipID = generatedFlipID(projectID, implementationSwarmID, now)
	}
	plan := Plan{
		SchemaVersion:         SchemaVersion,
		FlipID:                flipID,
		ProjectID:             projectID,
		ImplementationSwarmID: implementationSwarmID,
		State:                 StateDraining,
		StartedAt:             now,
		Audit: []AuditEvent{auditEvent(ActionHardeningFlipInitiated, "started", req.Actor, projectID, flipID, implementationSwarmID, "", now, map[string]string{
			"drainGrace": normalizeDuration(req.Drain.GraceWindow, DefaultDrainGrace).String(),
		})},
		Metadata: cloneStringMap(req.Metadata),
	}

	gate, err := EvaluateHardeningGate(req.GateSnapshot, projectID, flipID, req.Override, now)
	if err != nil {
		return Plan{}, err
	}
	plan.Gate = gate
	if !gate.Satisfied {
		plan.State = StateRefused
		plan.Problem = gate.Problem
		plan.Audit[0].Result = "rejected"
		plan.Audit[0].Data["missingCheckIds"] = strings.Join(gate.MissingCheckIDs, ",")
		return plan, nil
	}

	drain, err := EvaluateDrain(req.Drain, now)
	if err != nil {
		return Plan{}, err
	}
	plan.Drain = drain
	switch drain.State {
	case DrainInProgress:
		plan.State = StateDraining
		plan.Audit = append(plan.Audit, auditEvent(ActionDrainWaiting, "waiting", req.Actor, projectID, flipID, implementationSwarmID, "", now, map[string]string{
			"inFlight": fmt.Sprintf("%d", drain.InFlightCount),
		}))
		return plan, nil
	case DrainAwaitingResolution:
		plan.State = StateAwaitingDrainResolution
		plan.Audit = append(plan.Audit, auditEvent(ActionDrainNeedsResolution, "waiting", req.Actor, projectID, flipID, implementationSwarmID, "", now, map[string]string{
			"unresolved": fmt.Sprintf("%d", drain.UnresolvedCount),
		}))
		return plan, nil
	case DrainComplete:
	default:
		return Plan{}, fmt.Errorf("%w: unknown drain state %q", ErrInvalidInput, drain.State)
	}

	snapshot := req.Snapshot
	snapshot.SchemaVersion = SchemaVersion
	snapshot.ProjectID = projectID
	snapshot.FlipID = flipID
	snapshot.ImplementationSwarmID = implementationSwarmID
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = now
	}
	var manifest SnapshotManifest
	if strings.TrimSpace(req.SnapshotRoot) != "" {
		manifest, err = WriteImplementationSnapshot(req.SnapshotRoot, snapshot, now)
		if err != nil {
			return Plan{}, err
		}
	} else {
		manifest, err = ManifestForSnapshot(snapshot, "", now)
		if err != nil {
			return Plan{}, err
		}
	}
	plan.Snapshot = &manifest

	hardening := NormalizeHardeningSpec(projectID, flipID, req.Hardening)
	plan.Hardening = hardening
	plan.HardeningSwarmID = hardening.SwarmID

	tendingPlan := req.TendingPlan
	if tendingPlan.Empty() {
		tendingPlan = DefaultTendingSwitchPlan()
	}
	tending, err := SwitchTendingJobs(req.TendingJobs, tendingPlan)
	if err != nil {
		return Plan{}, err
	}
	plan.Tending = tending

	ui := BuildUIFlipEvent(projectID, flipID, implementationSwarmID, hardening.SwarmID, now)
	plan.UI = &ui
	completedAt := now
	plan.CompletedAt = &completedAt
	window := normalizeDuration(req.ReversibilityWindow, DefaultReversibilityWindow)
	reversibleUntil := now.Add(window).UTC()
	plan.ReversibleUntil = &reversibleUntil
	plan.State = StateCompleted
	plan.Audit = append(plan.Audit, auditEvent(ActionHardeningFlipCompleted, "success", req.Actor, projectID, flipID, implementationSwarmID, hardening.SwarmID, now, map[string]string{
		"snapshotArtifactId": manifest.ArtifactID,
		"carryCount":         fmt.Sprintf("%d", drain.CarryCount),
		"deferCount":         fmt.Sprintf("%d", drain.DeferCount),
		"tendingPatches":     fmt.Sprintf("%d", len(tending.Patches)),
	}))
	return plan, nil
}

func EvaluateHardeningGate(snapshot gates.Snapshot, projectID, flipID string, override *OverrideApproval, now time.Time) (GateDecision, error) {
	snapshot.ProjectID = strings.TrimSpace(projectID)
	if snapshot.ProjectID == "" {
		return GateDecision{}, fmt.Errorf("%w: projectId is required", ErrInvalidInput)
	}
	if snapshot.CheckedAt.IsZero() {
		snapshot.CheckedAt = now
	}
	readiness, err := gates.Evaluate(snapshot, schemas.ProjectGateHardeningReady)
	if err != nil {
		return GateDecision{}, err
	}
	if len(readiness.Gates) != 1 {
		return GateDecision{}, fmt.Errorf("%w: expected one hardening gate, got %d", ErrInvalidInput, len(readiness.Gates))
	}
	gate := readiness.Gates[0]
	decision := GateDecision{Satisfied: gate.Satisfied, Readiness: readiness}
	if gate.Satisfied {
		return decision, nil
	}
	decision.MissingCheckIDs = missingGateCheckIDs(gate)
	if override != nil && override.Approved {
		if strings.TrimSpace(override.ApprovalID) == "" {
			return GateDecision{}, fmt.Errorf("%w: approved override requires approvalId", ErrInvalidInput)
		}
		if !override.Actor.Kind.Valid() {
			return GateDecision{}, fmt.Errorf("%w: approved override requires actor.kind", ErrInvalidInput)
		}
		decision.Satisfied = true
		decision.OverrideAccepted = true
		decision.ApprovalID = strings.TrimSpace(override.ApprovalID)
		return decision, nil
	}
	decision.Problem = gateBlockedProblem(projectID, flipID, decision.MissingCheckIDs)
	return decision, nil
}

func EvaluateDrain(input DrainInput, fallbackNow time.Time) (DrainReport, error) {
	now := input.Now
	if now.IsZero() {
		now = fallbackNow
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	grace := normalizeDuration(input.GraceWindow, DefaultDrainGrace)
	started := input.GraceStartedAt
	if started.IsZero() {
		started = now
	}
	started = started.UTC()
	ends := started.Add(grace).UTC()
	remaining := ends.Sub(now)
	if remaining < 0 {
		remaining = 0
	}
	report := DrainReport{
		State:          DrainComplete,
		GraceStartedAt: started,
		GraceEndsAt:    ends,
		GraceRemaining: remaining,
	}
	for _, bead := range input.InFlight {
		normalized, active, err := normalizeInFlightBead(bead)
		if err != nil {
			return DrainReport{}, err
		}
		if !active {
			report.CompletedCount++
			continue
		}
		report.InFlightCount++
		resolution, resolved := input.Resolutions[normalized.BeadID]
		if resolved {
			switch resolution {
			case ResolutionCarryToHardening:
				report.CarryCount++
			case ResolutionDefer:
				report.DeferCount++
			default:
				return DrainReport{}, fmt.Errorf("%w: %s for bead %s", ErrInvalidResolution, resolution, normalized.BeadID)
			}
			report.Resolved = append(report.Resolved, DrainResolutionRecord{
				BeadID:     normalized.BeadID,
				AgentID:    normalized.AgentID,
				Resolution: resolution,
			})
			continue
		}
		report.UnresolvedCount++
		if now.Before(ends) {
			report.State = DrainInProgress
			continue
		}
		report.State = DrainAwaitingResolution
		report.RequiredChoices = append(report.RequiredChoices, DrainChoice{
			BeadID:       normalized.BeadID,
			AgentID:      normalized.AgentID,
			Choices:      []string{string(ResolutionCarryToHardening), string(ResolutionDefer)},
			GraceExpired: true,
			Deadline:     ends,
		})
	}
	sort.Slice(report.Resolved, func(i, j int) bool { return report.Resolved[i].BeadID < report.Resolved[j].BeadID })
	sort.Slice(report.RequiredChoices, func(i, j int) bool { return report.RequiredChoices[i].BeadID < report.RequiredChoices[j].BeadID })
	if report.UnresolvedCount == 0 {
		report.State = DrainComplete
	}
	return report, nil
}

func ManifestForSnapshot(snapshot ImplementationSnapshot, path string, writtenAt time.Time) (SnapshotManifest, error) {
	normalized := normalizeSnapshot(snapshot)
	data, err := marshalSnapshot(normalized)
	if err != nil {
		return SnapshotManifest{}, err
	}
	sum := sha256.Sum256(data)
	artifactID := fmt.Sprintf("hardening-snapshot:%s:%s", normalized.FlipID, hex.EncodeToString(sum[:8]))
	if writtenAt.IsZero() {
		writtenAt = normalized.CapturedAt
	}
	if writtenAt.IsZero() {
		writtenAt = time.Now()
	}
	return SnapshotManifest{
		SchemaVersion: SchemaVersion,
		FlipID:        normalized.FlipID,
		ProjectID:     normalized.ProjectID,
		ArtifactID:    artifactID,
		Path:          path,
		SHA256:        hex.EncodeToString(sum[:]),
		SizeBytes:     len(data),
		FieldCount:    snapshotFieldCount(normalized),
		WrittenAt:     writtenAt.UTC(),
	}, nil
}

func WriteImplementationSnapshot(root string, snapshot ImplementationSnapshot, writtenAt time.Time) (SnapshotManifest, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return SnapshotManifest{}, fmt.Errorf("%w: snapshot root is required", ErrInvalidInput)
	}
	normalized := normalizeSnapshot(snapshot)
	if normalized.ProjectID == "" || normalized.FlipID == "" {
		return SnapshotManifest{}, fmt.Errorf("%w: snapshot projectId and flipId are required", ErrInvalidInput)
	}
	data, err := marshalSnapshot(normalized)
	if err != nil {
		return SnapshotManifest{}, err
	}
	dir := filepath.Join(root, normalized.ProjectID, "hardening-snapshots", normalized.FlipID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return SnapshotManifest{}, err
	}
	path := filepath.Join(dir, SnapshotFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return SnapshotManifest{}, err
	}
	return ManifestForSnapshot(normalized, path, writtenAt)
}

func ReadImplementationSnapshot(path string) (ImplementationSnapshot, SnapshotManifest, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return ImplementationSnapshot{}, SnapshotManifest{}, fmt.Errorf("%w: snapshot path is required", ErrInvalidInput)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ImplementationSnapshot{}, SnapshotManifest{}, err
	}
	var snapshot ImplementationSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return ImplementationSnapshot{}, SnapshotManifest{}, err
	}
	manifest, err := ManifestForSnapshot(snapshot, path, time.Time{})
	if err != nil {
		return ImplementationSnapshot{}, SnapshotManifest{}, err
	}
	return normalizeSnapshot(snapshot), manifest, nil
}

func VerifySnapshot(path, expectedSHA256 string) (SnapshotManifest, error) {
	_, manifest, err := ReadImplementationSnapshot(path)
	if err != nil {
		return SnapshotManifest{}, err
	}
	if !strings.EqualFold(manifest.SHA256, strings.TrimSpace(expectedSHA256)) {
		return manifest, ErrSnapshotMismatch
	}
	return manifest, nil
}

func NormalizeHardeningSpec(projectID, flipID string, spec HardeningLaunchSpec) HardeningLaunchSpec {
	projectID = sanitizeID(projectID)
	flipID = sanitizeID(flipID)
	if strings.TrimSpace(spec.SwarmID) == "" {
		spec.SwarmID = "hardening-" + flipID
	}
	if strings.TrimSpace(spec.ReviewTopicID) == "" {
		spec.ReviewTopicID = "review-" + projectID + "-" + flipID
	}
	if strings.TrimSpace(spec.KickoffPromptTemplateID) == "" {
		spec.KickoffPromptTemplateID = "swarm-kickoff-hardening"
	}
	if strings.TrimSpace(spec.KickoffPromptVersion) == "" {
		spec.KickoffPromptVersion = "1"
	}
	if len(spec.Composition) == 0 {
		spec.Composition = []ReviewAgentSpec{
			{Role: "fresh_eyes", Harness: "claude-code", Count: 1},
			{Role: "cross_agent_reviewer", Harness: "codex-cli", Count: 2},
		}
	}
	if len(spec.Skills) == 0 {
		spec.Skills = []string{"vibing-with-ntm", "ntm", "ubs"}
	}
	spec.Skills = uniqueSortedStrings(spec.Skills)
	spec.Round0AutoStart = true
	spec.FindingLedgerInitialized = true
	return spec
}

func DefaultTendingSwitchPlan() TendingSwitchPlan {
	return TendingSwitchPlan{
		Pause: []string{"drift-check"},
		Activate: []TendingJob{
			{ID: "review-round-driver", Paused: false, Cadence: time.Minute, Mode: "hardening"},
			{ID: "finding-triage-helper", Paused: false, Cadence: 2 * time.Minute, Mode: "hardening"},
			{ID: "convergence-watcher", Paused: false, Cadence: 2 * time.Minute, Mode: "hardening"},
		},
		Retune: map[string]time.Duration{"tend-swarm": 2 * time.Minute},
	}
}

func (p TendingSwitchPlan) Empty() bool {
	return len(p.Pause) == 0 && len(p.Activate) == 0 && len(p.Retune) == 0
}

func SwitchTendingJobs(current []TendingJob, plan TendingSwitchPlan) (TendingSwitchResult, error) {
	if plan.Empty() {
		plan = DefaultTendingSwitchPlan()
	}
	jobs := map[string]TendingJob{}
	for _, job := range current {
		id := strings.TrimSpace(job.ID)
		if id == "" {
			return TendingSwitchResult{}, fmt.Errorf("%w: tending job id is required", ErrInvalidInput)
		}
		job.ID = id
		if _, exists := jobs[id]; exists {
			return TendingSwitchResult{}, fmt.Errorf("%w: duplicate tending job %s", ErrInvalidInput, id)
		}
		jobs[id] = job
	}
	result := TendingSwitchResult{}
	for _, id := range uniqueSortedStrings(plan.Pause) {
		job, ok := jobs[id]
		if !ok {
			result.Warnings = append(result.Warnings, "pause target not found: "+id)
			continue
		}
		if !job.Paused {
			result.Patches = append(result.Patches, TendingJobPatch{JobID: id, Action: PatchPause, FromPaused: false, ToPaused: true})
			result.PausedCount++
			job.Paused = true
			jobs[id] = job
		}
	}
	for _, job := range sortedActivateJobs(plan.Activate) {
		id := strings.TrimSpace(job.ID)
		if id == "" {
			return TendingSwitchResult{}, fmt.Errorf("%w: activate job id is required", ErrInvalidInput)
		}
		job.ID = id
		existing, ok := jobs[id]
		if !ok {
			result.Patches = append(result.Patches, TendingJobPatch{JobID: id, Action: PatchCreate, ToPaused: job.Paused, ToCadence: job.Cadence})
			result.ActivatedCount++
			jobs[id] = job
			continue
		}
		if existing.Paused && !job.Paused {
			result.Patches = append(result.Patches, TendingJobPatch{JobID: id, Action: PatchResume, FromPaused: true, ToPaused: false})
			result.ActivatedCount++
			existing.Paused = false
		}
		if job.Cadence > 0 && existing.Cadence != job.Cadence {
			result.Patches = append(result.Patches, TendingJobPatch{JobID: id, Action: PatchRetune, FromCadence: existing.Cadence, ToCadence: job.Cadence})
			result.RetunedCount++
			existing.Cadence = job.Cadence
		}
		if job.Mode != "" {
			existing.Mode = job.Mode
		}
		jobs[id] = existing
	}
	for _, id := range sortedRetuneKeys(plan.Retune) {
		cadence := plan.Retune[id]
		if cadence <= 0 {
			return TendingSwitchResult{}, fmt.Errorf("%w: retune cadence for %s must be positive", ErrInvalidInput, id)
		}
		job, ok := jobs[id]
		if !ok {
			result.Warnings = append(result.Warnings, "retune target not found: "+id)
			continue
		}
		if job.Cadence != cadence {
			result.Patches = append(result.Patches, TendingJobPatch{JobID: id, Action: PatchRetune, FromCadence: job.Cadence, ToCadence: cadence})
			result.RetunedCount++
			job.Cadence = cadence
			jobs[id] = job
		}
	}
	result.Jobs = sortedTendingJobs(jobs)
	result.IdempotencyKey = tendingIdempotencyKey(result.Jobs)
	sort.Strings(result.Warnings)
	return result, nil
}

func BuildUIFlipEvent(projectID, flipID, implementationSwarmID, hardeningSwarmID string, at time.Time) UIFlipEvent {
	if at.IsZero() {
		at = time.Now()
	}
	projectID = strings.TrimSpace(projectID)
	flipID = strings.TrimSpace(flipID)
	return UIFlipEvent{
		SchemaVersion:         SchemaVersion,
		EventID:               sanitizeID("ui:" + flipID + ":hardening_flip_completed"),
		Type:                  ActionHardeningFlipCompleted,
		ProjectID:             projectID,
		FlipID:                flipID,
		ImplementationSwarmID: strings.TrimSpace(implementationSwarmID),
		HardeningSwarmID:      strings.TrimSpace(hardeningSwarmID),
		Route:                 "/projects/" + projectID + "/hardening",
		ActiveStage:           "04",
		SwarmMode:             "hardening",
		SwarmStatePill:        "Hardening (round 0)",
		BeadBoardFilters:      []string{"in_review", "needs_human"},
		ConvergenceMounted:    true,
		At:                    at.UTC(),
	}
}

func CoalesceUIFlipEvents(events []UIFlipEvent) UIState {
	var latest *UIFlipEvent
	for i := range events {
		event := events[i]
		if event.Type != ActionHardeningFlipCompleted {
			continue
		}
		if latest == nil || event.At.After(latest.At) || (event.At.Equal(latest.At) && event.EventID > latest.EventID) {
			latest = &event
		}
	}
	if latest == nil {
		return UIState{}
	}
	return UIState{
		ProjectID:          latest.ProjectID,
		ActiveStage:        latest.ActiveStage,
		Route:              latest.Route,
		SwarmMode:          latest.SwarmMode,
		SwarmStatePill:     latest.SwarmStatePill,
		BeadBoardFilters:   append([]string(nil), latest.BeadBoardFilters...),
		ConvergenceMounted: latest.ConvergenceMounted,
		AppliedEventID:     latest.EventID,
	}
}

func EvaluateReturn(plan Plan, now time.Time, hardeningCreatedBeads []string) ReturnDecision {
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	var reversibleUntil time.Time
	if plan.ReversibleUntil != nil {
		reversibleUntil = plan.ReversibleUntil.UTC()
	}
	decision := ReturnDecision{
		Allowed:                   true,
		ReversibleUntil:           reversibleUntil,
		CarryBackBeads:            uniqueSortedStrings(hardeningCreatedBeads),
		RestoreImplementationJobs: true,
		HaltHardeningSwarm:        true,
	}
	if reversibleUntil.IsZero() || now.After(reversibleUntil) {
		decision.Allowed = false
		decision.RequiresApproval = true
		decision.Reason = "reversibility window expired; approval required"
		kind := "swarm.return_to_implementation"
		target := map[string]any{
			"projectId":        plan.ProjectID,
			"flipId":           plan.FlipID,
			"hardeningSwarmId": plan.HardeningSwarmID,
		}
		idempotencyKey := "return:" + plan.FlipID
		decision.ApprovalAction = &schemas.CommandSpec{
			Kind:           kind,
			Target:         target,
			IdempotencyKey: &idempotencyKey,
		}
		return decision
	}
	decision.Reason = "within reversibility window"
	return decision
}

func normalizeInFlightBead(bead InFlightBead) (InFlightBead, bool, error) {
	bead.BeadID = strings.TrimSpace(bead.BeadID)
	bead.AgentID = strings.TrimSpace(bead.AgentID)
	if bead.BeadID == "" {
		return InFlightBead{}, false, fmt.Errorf("%w: in-flight bead id is required", ErrInvalidInput)
	}
	if bead.Status == "" {
		bead.Status = BeadInProgress
	}
	switch bead.Status {
	case BeadInProgress:
		return bead, true, nil
	case BeadCompleted, BeadClosed, BeadDeferred:
		return bead, false, nil
	default:
		return InFlightBead{}, false, fmt.Errorf("%w: unsupported bead status %q", ErrInvalidInput, bead.Status)
	}
}

func normalizeSnapshot(snapshot ImplementationSnapshot) ImplementationSnapshot {
	snapshot.SchemaVersion = SchemaVersion
	snapshot.ProjectID = strings.TrimSpace(snapshot.ProjectID)
	snapshot.FlipID = strings.TrimSpace(snapshot.FlipID)
	snapshot.ImplementationSwarmID = strings.TrimSpace(snapshot.ImplementationSwarmID)
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = time.Now().UTC()
	} else {
		snapshot.CapturedAt = snapshot.CapturedAt.UTC()
	}
	if snapshot.Beads == nil {
		snapshot.Beads = map[string]BeadSnapshot{}
	}
	if snapshot.Agents == nil {
		snapshot.Agents = map[string]AgentSnapshot{}
	}
	if snapshot.MailThreadIndexHashes == nil {
		snapshot.MailThreadIndexHashes = map[string]string{}
	}
	if snapshot.PushStatus == nil {
		snapshot.PushStatus = map[string]BranchPushStatus{}
	}
	if snapshot.Metadata == nil {
		snapshot.Metadata = map[string]string{}
	}
	snapshot.BuildQueue.JobIDs = uniqueSortedStrings(snapshot.BuildQueue.JobIDs)
	return snapshot
}

func marshalSnapshot(snapshot ImplementationSnapshot) ([]byte, error) {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func snapshotFieldCount(snapshot ImplementationSnapshot) int {
	return len(snapshot.Beads) + len(snapshot.Agents) + len(snapshot.MailThreadIndexHashes) + len(snapshot.PushStatus) + len(snapshot.BuildQueue.JobIDs) + 2
}

func missingGateCheckIDs(gate schemas.ProjectReadinessGate) []string {
	var missing []string
	for _, check := range gate.Checks {
		if !check.Ok {
			missing = append(missing, check.Id)
		}
	}
	sort.Strings(missing)
	return missing
}

func gateBlockedProblem(projectID, flipID string, missing []string) *schemas.Problem {
	detail := "hardening_ready gate is blocked"
	if len(missing) > 0 {
		detail += ": " + strings.Join(missing, ", ")
	}
	instance := "urn:hoopoe:incident:" + sanitizeID(flipID)
	correlationID := sanitizeID(flipID)
	retryable := true
	capability := string(schemas.ProjectGateHardeningReady)
	return &schemas.Problem{
		Type:          "urn:hoopoe:swarms/hardening-gate-blocked",
		Code:          "project.gate_blocked",
		Title:         "Hardening gate blocked",
		Status:        422,
		Detail:        &detail,
		Instance:      &instance,
		CorrelationId: &correlationID,
		Retryable:     &retryable,
		Capability:    &capability,
	}
}

func auditEvent(action, result string, actor schemas.Actor, projectID, flipID, implementationSwarmID, hardeningSwarmID string, at time.Time, data map[string]string) AuditEvent {
	if at.IsZero() {
		at = time.Now()
	}
	return AuditEvent{
		SchemaVersion:         SchemaVersion,
		EventID:               sanitizeID(flipID + ":" + action),
		Action:                action,
		Result:                result,
		ProjectID:             projectID,
		FlipID:                flipID,
		ImplementationSwarmID: implementationSwarmID,
		HardeningSwarmID:      hardeningSwarmID,
		Actor:                 actor,
		CorrelationID:         flipID,
		At:                    at.UTC(),
		Data:                  cloneStringMap(data),
	}
}

func generatedFlipID(projectID, swarmID string, at time.Time) string {
	sum := sha256.Sum256([]byte(projectID + "\x00" + swarmID + "\x00" + at.UTC().Format(time.RFC3339Nano)))
	return "flip-" + hex.EncodeToString(sum[:6])
}

func normalizeDuration(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

func sortedActivateJobs(jobs []TendingJob) []TendingJob {
	out := append([]TendingJob(nil), jobs...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sortedRetuneKeys(retune map[string]time.Duration) []string {
	keys := make([]string, 0, len(retune))
	for key := range retune {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedTendingJobs(jobs map[string]TendingJob) []TendingJob {
	ids := make([]string, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]TendingJob, 0, len(ids))
	for _, id := range ids {
		out = append(out, jobs[id])
	}
	return out
}

func tendingIdempotencyKey(jobs []TendingJob) string {
	data, err := json.Marshal(jobs)
	if err != nil {
		return "tending-switch:unhashable"
	}
	sum := sha256.Sum256(data)
	return "tending-switch:" + hex.EncodeToString(sum[:8])
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sanitizeID(value string) string {
	var builder strings.Builder
	lastDash := false
	for _, r := range strings.TrimSpace(value) {
		allowed := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.'
		if allowed {
			builder.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}
