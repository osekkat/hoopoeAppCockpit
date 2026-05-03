package capabilities

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// ProbeFunc returns a fresh ToolReport for one tool. Adapters register a
// ProbeFunc per tool; the Registry calls them on Probe() to refresh state.
// Probes that error report the tool as missing so adapter outages don't
// cascade into a failed /v1/capabilities response.
type ProbeFunc func() (*ToolReport, error)

// Registry composes ToolReports from registered probes (or static fixture
// snapshots in mock-flywheel mode) into a CapabilityRegistry. Safe for
// concurrent reads; Probe() takes a write lock during refresh.
type Registry struct {
	mu sync.RWMutex

	daemonAPIVersion string
	fixturesVersion  string

	probes  map[ToolID]ProbeFunc
	reports map[ToolID]*ToolReport

	// features maps featureId → requirement. Renderer hooks consume this via
	// /v1/capabilities/features (deferred — currently exposed only through
	// the in-process Determine() helper for tending jobs).
	features map[string]*FeatureCapabilityRequirement

	now func() time.Time
}

// New constructs an empty Registry. daemonAPIVersion is required and must be
// set at daemon boot from the build's version-injection ldflags. The Registry
// starts with no probes and no reports; adapters register probes on init.
func New(daemonAPIVersion string) *Registry {
	if daemonAPIVersion == "" {
		// Defensive — boot code should always supply a version. We refuse
		// silently here (panic in tests; production is wired via main.go).
		panic("capabilities.New: daemonAPIVersion must be non-empty")
	}
	return &Registry{
		daemonAPIVersion: daemonAPIVersion,
		probes:           make(map[ToolID]ProbeFunc),
		reports:          make(map[ToolID]*ToolReport),
		features:         make(map[string]*FeatureCapabilityRequirement),
		now:              time.Now,
	}
}

// SetClock overrides the time source for deterministic tests. Production code
// should leave the default.
func (r *Registry) SetClock(now func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = now
}

// SetFixturesVersion records the dominant fixture tag for the registry-level
// envelope. Per-tool fixturesVersion can diverge (e.g., a tool only available
// in the latest corpus); this value is the registry-wide reference for the
// /v1/capabilities snapshot.
func (r *Registry) SetFixturesVersion(tag string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fixturesVersion = tag
}

// RegisterProbe attaches a ProbeFunc for a tool. Subsequent Probe() calls
// invoke it. Replacing an existing probe is allowed (mock-flywheel mode swaps
// real probes for fixture probes at boot).
func (r *Registry) RegisterProbe(tool ToolID, probe ProbeFunc) error {
	if !tool.Valid() {
		return fmt.Errorf("capabilities: cannot register probe for invalid tool id %q", tool)
	}
	if probe == nil {
		return fmt.Errorf("capabilities: cannot register nil probe for %s", tool)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probes[tool] = probe
	return nil
}

// SetReport directly inserts a ToolReport, bypassing probe execution. Used by
// fixture-backed boots where the daemon never invokes a real CLI.
func (r *Registry) SetReport(report *ToolReport) error {
	if err := report.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reports[report.Tool] = report
	return nil
}

// RegisterFeature records a UI feature or tending job's capability
// requirements. Determine() consults this map to compute degraded-mode
// behavior.
func (r *Registry) RegisterFeature(req *FeatureCapabilityRequirement) error {
	if err := req.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.features[req.FeatureID] = req
	return nil
}

// Probe runs every registered ProbeFunc and refreshes the in-memory report
// table. Probes that error are recorded as a single capability `__probe__`
// with status=missing so the failure surfaces in /v1/capabilities rather
// than disappearing.
func (r *Registry) Probe() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.now().UTC().Format(time.RFC3339)
	for tool, probe := range r.probes {
		report, err := probe()
		if err != nil {
			r.reports[tool] = &ToolReport{
				Tool:          tool,
				Version:       "",
				Source:        "probe-error",
				LastCheckedAt: now,
				Capabilities: map[string]Capability{
					"__probe__": {
						Status: StatusMissing,
						Notes:  fmt.Sprintf("probe error: %v", err),
					},
				},
			}
			continue
		}
		if report == nil {
			r.reports[tool] = &ToolReport{
				Tool:          tool,
				Version:       "",
				Source:        "probe-nil",
				LastCheckedAt: now,
				Capabilities: map[string]Capability{
					"__probe__": {
						Status: StatusMissing,
						Notes:  "probe returned nil report",
					},
				},
			}
			continue
		}
		// If the adapter forgot to stamp lastCheckedAt, fill it.
		if report.LastCheckedAt == "" {
			report.LastCheckedAt = now
		}
		// Normalize the tool field — adapters could otherwise mismatch their
		// registered key. Registry key wins.
		report.Tool = tool
		r.reports[tool] = report
	}
}

// Snapshot returns a deep-enough copy of the current registry state for
// safe serialization. Callers may mutate the returned struct freely.
func (r *Registry) Snapshot() *CapabilityRegistry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := &CapabilityRegistry{
		SchemaVersion:    SchemaVersion,
		SnapshotAt:       r.now().UTC().Format(time.RFC3339),
		DaemonAPIVersion: r.daemonAPIVersion,
		FixturesVersion:  r.fixturesVersion,
		Tools:            make(map[ToolID]*ToolReport, len(r.reports)),
	}
	for tool, report := range r.reports {
		out.Tools[tool] = cloneReport(report)
	}
	return out
}

// LookupCapability finds a single capability by tool + capId. Returns
// (Capability{}, false) if the tool isn't registered or the capId is unknown.
func (r *Registry) LookupCapability(tool ToolID, capID string) (Capability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	report, ok := r.reports[tool]
	if !ok {
		return Capability{}, false
	}
	cap, ok := report.Capabilities[capID]
	return cap, ok
}

// LookupCapabilityStatus parses a fully-qualified capability reference
// (e.g., 'br.issues.read') and returns the current status. Adapters that
// need to gate their own behavior on capability availability use this
// rather than reaching into Snapshot().
func (r *Registry) LookupCapabilityStatus(capRef string) (CapabilityStatus, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.lookupCapabilityStatus(capRef)
}

// FeatureDecision is the runtime answer for whether a feature should run, run
// degraded, or block, based on capability availability.
type FeatureDecision struct {
	FeatureID         string
	Render            FeatureRender
	MissingRequired   []string
	MissingOptional   []string
	BlockedByPolicy   []string
	DegradedReasons   []string
	ContractAction    IfMissingRequired
	ActivityBehavior  ActivityBehavior
	OptionalAction    IfMissingOptional
}

// Determine evaluates a registered FeatureCapabilityRequirement against
// current state and returns a FeatureDecision. Feature requirement IDs use a
// dotted namespace per plan.md §7 / §8.2 (e.g., 'stage.swarm.launch',
// 'tending.push-stale-commits'). Capability IDs in required/optional are
// fully-qualified `<tool>.<capId>` (e.g., 'git.push').
//
// The decision precedence is:
//  1. Any required capability with status='blocked-by-policy' → BlockedByPolicy
//     render + ContractAction reflects ifMissingRequired.
//  2. Any required capability missing/untested → Unavailable render +
//     ContractAction = ifMissingRequired.
//  3. Any required capability degraded → Degraded render.
//  4. Otherwise → Available; if optional capabilities are missing/degraded,
//     OptionalAction reflects ifMissingOptional.
func (r *Registry) Determine(featureID string) (FeatureDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	req, ok := r.features[featureID]
	if !ok {
		return FeatureDecision{}, fmt.Errorf("capabilities: feature %q not registered", featureID)
	}

	dec := FeatureDecision{
		FeatureID:        featureID,
		Render:           RenderAvailable,
		ContractAction:   req.DegradedMode.IfMissingRequired,
		OptionalAction:   req.DegradedMode.IfMissingOptional,
		ActivityBehavior: req.DegradedMode.ActivityBehavior,
	}

	for _, capRef := range req.CapabilitiesRequired {
		status, ok := r.lookupCapabilityStatus(capRef)
		switch {
		case !ok || status == StatusMissing || status == StatusUntested:
			dec.MissingRequired = append(dec.MissingRequired, capRef)
			if dec.Render != RenderBlockedByPolicy {
				dec.Render = RenderUnavailable
			}
		case status == StatusBlockedByPolicy:
			dec.BlockedByPolicy = append(dec.BlockedByPolicy, capRef)
			dec.Render = RenderBlockedByPolicy
		case status == StatusDegraded:
			dec.DegradedReasons = append(dec.DegradedReasons, capRef)
			if dec.Render == RenderAvailable {
				dec.Render = RenderDegraded
			}
		}
	}

	for _, capRef := range req.CapabilitiesOptional {
		status, ok := r.lookupCapabilityStatus(capRef)
		switch {
		case !ok || status == StatusMissing || status == StatusUntested:
			dec.MissingOptional = append(dec.MissingOptional, capRef)
		case status == StatusDegraded:
			dec.DegradedReasons = append(dec.DegradedReasons, capRef)
			if dec.Render == RenderAvailable {
				dec.Render = RenderDegraded
			}
		}
	}

	sort.Strings(dec.MissingRequired)
	sort.Strings(dec.MissingOptional)
	sort.Strings(dec.BlockedByPolicy)
	sort.Strings(dec.DegradedReasons)
	return dec, nil
}

// lookupCapabilityStatus parses a fully-qualified `<tool>.<capId>` reference
// (e.g., 'git.status.read') and returns the current status. The capability
// is keyed by the full reference inside the ToolReport.Capabilities map, so
// only the tool prefix is needed to find the right report — the lookup key
// is the unmodified ref. Unknown tool or capId returns (StatusMissing,
// false).
func (r *Registry) lookupCapabilityStatus(capRef string) (CapabilityStatus, bool) {
	tool, ok := toolFromCapRef(capRef)
	if !ok {
		return StatusMissing, false
	}
	report, ok := r.reports[tool]
	if !ok {
		return StatusMissing, false
	}
	cap, ok := report.Capabilities[capRef]
	if !ok {
		return StatusMissing, false
	}
	return cap.Status, true
}

// toolFromCapRef extracts the ToolID prefix from a fully-qualified capability
// reference. The leading segment up to the first dot is the tool name (which
// itself never contains a dot — `agent_mail` and `health_<lang>` both use
// underscores). The full ref is the capability key inside the tool's
// Capabilities map; this function does NOT strip the prefix.
func toolFromCapRef(ref string) (ToolID, bool) {
	dot := -1
	for i := 0; i < len(ref); i++ {
		if ref[i] == '.' {
			dot = i
			break
		}
	}
	if dot <= 0 || dot == len(ref)-1 {
		return "", false
	}
	tool := ToolID(ref[:dot])
	if !tool.Valid() {
		return "", false
	}
	return tool, true
}

// cloneReport copies a ToolReport so callers can mutate freely without
// affecting registry state.
func cloneReport(in *ToolReport) *ToolReport {
	out := &ToolReport{
		Tool:            in.Tool,
		Version:         in.Version,
		Source:          in.Source,
		LastCheckedAt:   in.LastCheckedAt,
		FixturesVersion: in.FixturesVersion,
		Capabilities:    make(map[string]Capability, len(in.Capabilities)),
	}
	for capID, cap := range in.Capabilities {
		out.Capabilities[capID] = cap
	}
	return out
}
