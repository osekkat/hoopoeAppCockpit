package capabilities

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const fixedDaemonAPI = "0.1.0"

func fixedClock(stamp string) func() time.Time {
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

func newTestRegistry(t *testing.T, snapshotAt string) *Registry {
	t.Helper()
	r := New(fixedDaemonAPI)
	r.SetClock(fixedClock(snapshotAt))
	r.SetFixturesVersion("phase0-test")
	return r
}

func okReport(tool ToolID, capID string) *ToolReport {
	return &ToolReport{
		Tool:    tool,
		Version: "1.0.0",
		Source:  "fixture",
		Capabilities: map[string]Capability{
			capID: {Status: StatusOK},
		},
		LastCheckedAt:   "2026-05-02T23:29:34Z",
		FixturesVersion: "phase0-test",
	}
}

func TestToolIDValidation(t *testing.T) {
	for _, id := range KnownClosedTools {
		if !id.Valid() {
			t.Errorf("known closed tool %s rejected by Valid()", id)
		}
	}
	healthIDs := []ToolID{"health_ts", "health_py", "health_rs", "health_go", "health_generic"}
	for _, id := range healthIDs {
		if !id.Valid() {
			t.Errorf("health tool %s rejected by Valid()", id)
		}
	}
	bad := []ToolID{"", "health_", "kubelet", "ntm.serve", "notreal"}
	for _, id := range bad {
		if id.Valid() {
			t.Errorf("invalid tool %q accepted by Valid()", id)
		}
	}
}

func TestCapabilityStatusValid(t *testing.T) {
	good := []CapabilityStatus{StatusOK, StatusDegraded, StatusMissing, StatusBlockedByPolicy, StatusUntested}
	for _, s := range good {
		if !s.Valid() {
			t.Errorf("status %q rejected", s)
		}
	}
	if CapabilityStatus("nope").Valid() {
		t.Errorf("unknown status accepted")
	}
}

func TestRegistrySetReportAndSnapshot(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.SetReport(okReport(ToolGit, "git.status.read")); err != nil {
		t.Fatal(err)
	}
	if err := r.SetReport(okReport(ToolBR, "br.issues.read")); err != nil {
		t.Fatal(err)
	}

	snap := r.Snapshot()
	if snap.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion=%d", snap.SchemaVersion)
	}
	if snap.DaemonAPIVersion != fixedDaemonAPI {
		t.Errorf("daemonAPIVersion=%q", snap.DaemonAPIVersion)
	}
	if snap.SnapshotAt != "2026-05-02T23:29:34Z" {
		t.Errorf("snapshotAt=%q", snap.SnapshotAt)
	}
	if snap.FixturesVersion != "phase0-test" {
		t.Errorf("fixturesVersion=%q", snap.FixturesVersion)
	}
	if got := snap.Tools[ToolGit]; got == nil || got.Capabilities["git.status.read"].Status != StatusOK {
		t.Errorf("git report not present in snapshot: %+v", got)
	}
	if got := snap.Tools[ToolBR]; got == nil || got.Capabilities["br.issues.read"].Status != StatusOK {
		t.Errorf("br report not present in snapshot: %+v", got)
	}
}

func TestSetReportRejectsInvalidToolID(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	bad := &ToolReport{
		Tool:          "kubelet",
		Source:        "fixture",
		LastCheckedAt: "2026-05-02T23:29:34Z",
		Capabilities:  map[string]Capability{"x": {Status: StatusOK}},
	}
	if err := r.SetReport(bad); err == nil {
		t.Fatal("expected validation error for invalid tool id")
	}
}

func TestSetReportRejectsInvalidStatus(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	bad := &ToolReport{
		Tool:          ToolGit,
		Source:        "fixture",
		LastCheckedAt: "2026-05-02T23:29:34Z",
		Capabilities:  map[string]Capability{"git.status.read": {Status: "bogus"}},
	}
	if err := r.SetReport(bad); err == nil {
		t.Fatal("expected validation error for invalid status")
	}
}

func TestProbeNormalAdapter(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	probeRan := false
	if err := r.RegisterProbe(ToolNTM, func() (*ToolReport, error) {
		probeRan = true
		return &ToolReport{
			Tool:    ToolNTM,
			Version: "1.2.0",
			Source:  "ntm serve",
			Capabilities: map[string]Capability{
				"ntm.sessions.list":  {Status: StatusOK},
				"ntm.panes.stream":   {Status: StatusOK, Transport: "websocket"},
				"ntm.approvals.list": {Status: StatusMissing, Fallback: "none"},
			},
			LastCheckedAt:   "2026-05-02T23:29:34Z",
			FixturesVersion: "live",
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	r.Probe()
	if !probeRan {
		t.Errorf("probe not invoked")
	}
	snap := r.Snapshot()
	got := snap.Tools[ToolNTM]
	if got == nil {
		t.Fatal("ntm not present after probe")
	}
	if got.Capabilities["ntm.approvals.list"].Status != StatusMissing {
		t.Errorf("expected approvals=missing, got %+v", got.Capabilities["ntm.approvals.list"])
	}
	// Transport carried through.
	if got.Capabilities["ntm.panes.stream"].Transport != "websocket" {
		t.Errorf("transport not carried")
	}
}

func TestProbeMissingTool(t *testing.T) {
	// Adapter contract: a tool that isn't installed is reported as a
	// ToolReport with all required capabilities marked missing — not as a
	// probe error.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.RegisterProbe(ToolJSM, func() (*ToolReport, error) {
		return &ToolReport{
			Tool:    ToolJSM,
			Version: "",
			Source:  "CLI",
			Capabilities: map[string]Capability{
				"jsm.skill.install": {Status: StatusMissing, Notes: "binary not found in PATH"},
				"jsm.skill.verify":  {Status: StatusMissing},
			},
			LastCheckedAt: "2026-05-02T23:29:34Z",
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	r.Probe()
	snap := r.Snapshot()
	got := snap.Tools[ToolJSM]
	if got == nil {
		t.Fatal("jsm tool report missing")
	}
	if got.Capabilities["jsm.skill.install"].Status != StatusMissing {
		t.Errorf("expected missing, got %s", got.Capabilities["jsm.skill.install"].Status)
	}
}

func TestProbeUnsupportedVersion(t *testing.T) {
	// Adapter parses output successfully but the binary version doesn't
	// satisfy the capability — the capability MUST report degraded or
	// missing per §2.8 ("a fixture that parses but cannot satisfy the
	// capability must not mark the feature as available").
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.RegisterProbe(ToolBR, func() (*ToolReport, error) {
		return &ToolReport{
			Tool:    ToolBR,
			Version: "0.0.1-pre", // older than minimum
			Source:  "CLI",
			Capabilities: map[string]Capability{
				"br.issues.read": {
					Status:   StatusDegraded,
					Notes:    "br 0.0.1 lacks --json on `show`; falling back to text parse",
					Fallback: "text-parse",
				},
				"br.issues.update": {
					Status: StatusMissing,
					Notes:  "br 0.0.1 lacks `update` subcommand",
				},
				"br.dep.add": {Status: StatusOK},
			},
			LastCheckedAt: "2026-05-02T23:29:34Z",
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	r.Probe()
	if status, _ := r.LookupCapabilityStatus("br.issues.read"); status != StatusDegraded {
		t.Errorf("expected br.issues.read=degraded, got %s", status)
	}
	if status, _ := r.LookupCapabilityStatus("br.issues.update"); status != StatusMissing {
		t.Errorf("expected br.issues.update=missing, got %s", status)
	}
	if status, _ := r.LookupCapabilityStatus("br.dep.add"); status != StatusOK {
		t.Errorf("expected br.dep.add=ok, got %s", status)
	}
}

func TestProbeMalformedOutput(t *testing.T) {
	// Adapter returns an error (e.g., JSON parse failure). Registry must
	// surface this as a probe failure ToolReport with status=missing and
	// a __probe__ notes field — not propagate the error to the HTTP layer.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.RegisterProbe(ToolBV, func() (*ToolReport, error) {
		return nil, errors.New("malformed JSON: unexpected token at pos 42")
	}); err != nil {
		t.Fatal(err)
	}
	r.Probe()
	snap := r.Snapshot()
	got := snap.Tools[ToolBV]
	if got == nil {
		t.Fatal("expected a placeholder ToolReport for failing probe")
	}
	if got.Source != "probe-error" {
		t.Errorf("expected source=probe-error, got %s", got.Source)
	}
	probe, ok := got.Capabilities["__probe__"]
	if !ok || probe.Status != StatusMissing {
		t.Errorf("expected __probe__:missing, got %+v", got.Capabilities)
	}
}

func TestProbeTimeout(t *testing.T) {
	// Adapter returns a timeout error; same path as malformed but a
	// distinct notes field. The contract is identical: registry never
	// fails open.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.RegisterProbe(ToolNTM, func() (*ToolReport, error) {
		return nil, errors.New("context deadline exceeded after 5s")
	}); err != nil {
		t.Fatal(err)
	}
	r.Probe()
	snap := r.Snapshot()
	if snap.Tools[ToolNTM].Capabilities["__probe__"].Status != StatusMissing {
		t.Errorf("timeout did not produce missing status")
	}
}

func TestProbeHighVolumeOutput(t *testing.T) {
	// Adapter handles a tool that emits 1000 capability entries (e.g.,
	// per-bead status capabilities). Registry must accept all of them
	// without truncation.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.RegisterProbe(ToolBV, func() (*ToolReport, error) {
		caps := make(map[string]Capability, 1000)
		for i := 0; i < 1000; i++ {
			caps[capabilityIDForBead(i)] = Capability{Status: StatusOK}
		}
		return &ToolReport{
			Tool:          ToolBV,
			Version:       "1.0.0",
			Source:        "CLI",
			Capabilities:  caps,
			LastCheckedAt: "2026-05-02T23:29:34Z",
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	r.Probe()
	snap := r.Snapshot()
	got := snap.Tools[ToolBV]
	if got == nil {
		t.Fatal("bv missing")
	}
	if len(got.Capabilities) != 1000 {
		t.Errorf("high-volume output truncated: got %d", len(got.Capabilities))
	}
}

func capabilityIDForBead(i int) string {
	return "bv.bead." + itoa(i)
}

func TestProbeDoesNotBlockSnapshotWhileProbeFuncIsSlow(t *testing.T) {
	// hp-10kh: Probe used to hold the registry write lock for the full
	// duration of every CLI probe (Snapshot, LookupCapabilityStatus,
	// HandleCapabilities all blocked). The fix snapshots the probe set
	// under RLock, runs the probes outside the lock, and merges results
	// under the write lock briefly. Snapshot must therefore return
	// promptly even while a slow probe is in flight.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")

	// Pre-populate so Snapshot has a previous result to return during
	// the sweep.
	if err := r.SetReport(&ToolReport{
		Tool:   ToolNTM,
		Source: "test",
		Capabilities: map[string]Capability{
			"ntm.sessions.list": {Status: StatusOK},
		},
		LastCheckedAt: "2026-05-02T23:29:34Z",
	}); err != nil {
		t.Fatal(err)
	}

	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	if err := r.RegisterProbe(ToolBV, func() (*ToolReport, error) {
		close(probeStarted)
		<-releaseProbe
		return &ToolReport{
			Tool:          ToolBV,
			Version:       "1.0.0",
			Capabilities:  map[string]Capability{"bv.list.read": {Status: StatusOK}},
			LastCheckedAt: "2026-05-02T23:29:34Z",
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	// Start Probe in a goroutine; it will block inside the probe func.
	probeDone := make(chan struct{})
	go func() {
		r.Probe()
		close(probeDone)
	}()

	// Wait for the probe to be in flight.
	select {
	case <-probeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("probe never started")
	}

	// Snapshot + LookupCapabilityStatus must complete promptly while
	// the probe is still blocked. 250ms is generous; before the fix
	// these calls would block for the full probe duration.
	deadline := time.After(250 * time.Millisecond)
	snapDone := make(chan struct{})
	go func() {
		snap := r.Snapshot()
		if snap == nil {
			t.Error("nil snapshot")
		}
		// Pre-populated NTM report must still be visible while BV is
		// being probed.
		if snap.Tools[ToolNTM] == nil {
			t.Error("ntm missing from snapshot during in-flight probe")
		}
		_, _ = r.LookupCapabilityStatus("ntm.sessions.list")
		close(snapDone)
	}()
	select {
	case <-snapDone:
	case <-deadline:
		t.Fatal("Snapshot/LookupCapabilityStatus blocked while probe held the registry lock (regression of hp-10kh)")
	}

	close(releaseProbe)
	<-probeDone

	// After the sweep finishes, the new BV report is merged in.
	got := r.Snapshot().Tools[ToolBV]
	if got == nil || got.Capabilities["bv.list.read"].Status != StatusOK {
		t.Fatalf("bv report not merged after probe finished: %+v", got)
	}
}

func TestProbeMergesInsteadOfReplacingPreservesConcurrentlyRegisteredTool(t *testing.T) {
	// hp-10kh: the new Probe snapshots the probe set, drops the lock,
	// runs probes, then merges. A tool registered between snapshot and
	// merge must NOT be dropped by the merge. (Regression guard for the
	// "replace map" mistake — we explicitly merge instead.)
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	probeStarted := make(chan struct{})
	releaseProbe := make(chan struct{})
	if err := r.RegisterProbe(ToolBV, func() (*ToolReport, error) {
		close(probeStarted)
		<-releaseProbe
		return &ToolReport{
			Tool:          ToolBV,
			Capabilities:  map[string]Capability{"bv.list.read": {Status: StatusOK}},
			LastCheckedAt: "2026-05-02T23:29:34Z",
		}, nil
	}); err != nil {
		t.Fatal(err)
	}

	probeDone := make(chan struct{})
	go func() {
		r.Probe()
		close(probeDone)
	}()
	<-probeStarted

	// Inject a second tool's report while the first probe is mid-flight.
	if err := r.SetReport(&ToolReport{
		Tool:          ToolNTM,
		Source:        "test",
		Capabilities:  map[string]Capability{"ntm.sessions.list": {Status: StatusOK}},
		LastCheckedAt: "2026-05-02T23:29:34Z",
	}); err != nil {
		t.Fatal(err)
	}

	close(releaseProbe)
	<-probeDone

	// Both tools must survive the merge.
	snap := r.Snapshot()
	if snap.Tools[ToolBV] == nil {
		t.Fatal("bv dropped after merge")
	}
	if snap.Tools[ToolNTM] == nil {
		t.Fatal("ntm dropped — Probe replaced reports map instead of merging")
	}
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	out := ""
	for i > 0 {
		out = string(digits[i%10]) + out
		i /= 10
	}
	return out
}

func TestDetermineFeatureBlockedByPolicy(t *testing.T) {
	// A feature requires git.push but git.push is blocked-by-policy in the
	// snapshot (e.g., desktop in read-only mirror mode). Determine() must
	// return RenderBlockedByPolicy regardless of other capability status.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.SetReport(&ToolReport{
		Tool:    ToolGit,
		Version: "2.40.0",
		Source:  "CLI",
		Capabilities: map[string]Capability{
			"git.status.read": {Status: StatusOK},
			"git.push":        {Status: StatusBlockedByPolicy, Notes: "snapshot script never pushes"},
		},
		LastCheckedAt: "2026-05-02T23:29:34Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterFeature(&FeatureCapabilityRequirement{
		FeatureID:            "swarm.bead.push-branch",
		CapabilitiesRequired: []string{"git.status.read", "git.push"},
		DegradedMode: DegradedModeContract{
			IfMissingRequired: BlockJob,
			IfMissingOptional: ContinueWithWarning,
			ActivityBehavior:  ActivityPanelWarning,
		},
	}); err != nil {
		t.Fatal(err)
	}
	dec, err := r.Determine("swarm.bead.push-branch")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Render != RenderBlockedByPolicy {
		t.Errorf("expected blocked-by-policy, got %s", dec.Render)
	}
	if len(dec.BlockedByPolicy) != 1 || dec.BlockedByPolicy[0] != "git.push" {
		t.Errorf("expected BlockedByPolicy=[git.push], got %v", dec.BlockedByPolicy)
	}
	if dec.ContractAction != BlockJob {
		t.Errorf("expected contract action=block_job, got %s", dec.ContractAction)
	}
}

func TestDetermineFeatureMissingRequired(t *testing.T) {
	// Feature requires ntm.swarm.halt; it is missing. Render=unavailable.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.SetReport(&ToolReport{
		Tool:    ToolNTM,
		Version: "1.0.0",
		Source:  "CLI",
		Capabilities: map[string]Capability{
			"ntm.sessions.list": {Status: StatusOK},
			"ntm.swarm.halt":    {Status: StatusMissing, Notes: "tool too old"},
		},
		LastCheckedAt: "2026-05-02T23:29:34Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterFeature(&FeatureCapabilityRequirement{
		FeatureID:            "tending.watch-safety-thresholds",
		CapabilitiesRequired: []string{"ntm.sessions.list", "ntm.swarm.halt"},
		DegradedMode: DegradedModeContract{
			IfMissingRequired: EmitDiagnostic,
			IfMissingOptional: ContinueWithWarning,
			ActivityBehavior:  ActivityDiagnosticsOnly,
		},
	}); err != nil {
		t.Fatal(err)
	}
	dec, err := r.Determine("tending.watch-safety-thresholds")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Render != RenderUnavailable {
		t.Errorf("expected unavailable, got %s", dec.Render)
	}
	if len(dec.MissingRequired) != 1 || dec.MissingRequired[0] != "ntm.swarm.halt" {
		t.Errorf("expected MissingRequired=[ntm.swarm.halt], got %v", dec.MissingRequired)
	}
	if dec.ContractAction != EmitDiagnostic {
		t.Errorf("expected contract action=emit_diagnostic, got %s", dec.ContractAction)
	}
}

func TestDetermineFeatureUntestedTreatedAsMissing(t *testing.T) {
	// 'untested' must land in MissingRequired buckets (renderer treats it
	// as unavailable); it is NOT silently upgraded to ok.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.SetReport(&ToolReport{
		Tool:    ToolDCG,
		Version: "1.0.0",
		Source:  "CLI",
		Capabilities: map[string]Capability{
			"dcg.verdicts.subscribe": {Status: StatusUntested, Notes: "format pinned via --help"},
		},
		LastCheckedAt: "2026-05-02T23:29:34Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterFeature(&FeatureCapabilityRequirement{
		FeatureID:            "approvals.dcg.subscribe",
		CapabilitiesRequired: []string{"dcg.verdicts.subscribe"},
		DegradedMode: DegradedModeContract{
			IfMissingRequired: EmitDiagnostic,
			IfMissingOptional: ContinueWithWarning,
			ActivityBehavior:  ActivityDiagnosticsOnly,
		},
	}); err != nil {
		t.Fatal(err)
	}
	dec, err := r.Determine("approvals.dcg.subscribe")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Render != RenderUnavailable {
		t.Errorf("untested treated wrongly: render=%s", dec.Render)
	}
	if len(dec.MissingRequired) == 0 {
		t.Errorf("untested should populate MissingRequired")
	}
}

func TestDetermineFeatureDegraded(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.SetReport(&ToolReport{
		Tool:    ToolBR,
		Version: "0.5.0",
		Source:  "CLI",
		Capabilities: map[string]Capability{
			"br.issues.read": {Status: StatusDegraded, Fallback: "text-parse"},
		},
		LastCheckedAt: "2026-05-02T23:29:34Z",
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterFeature(&FeatureCapabilityRequirement{
		FeatureID:            "bead.kanban.refresh",
		CapabilitiesRequired: []string{"br.issues.read"},
		DegradedMode: DegradedModeContract{
			IfMissingRequired: RunReadOnly,
			IfMissingOptional: ContinueWithWarning,
			ActivityBehavior:  ActivityPanelWarning,
		},
	}); err != nil {
		t.Fatal(err)
	}
	dec, err := r.Determine("bead.kanban.refresh")
	if err != nil {
		t.Fatal(err)
	}
	if dec.Render != RenderDegraded {
		t.Errorf("expected degraded, got %s", dec.Render)
	}
}

func TestDetermineUnknownFeatureErrors(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if _, err := r.Determine("unknown.feature"); err == nil {
		t.Error("expected error for unknown feature")
	}
}

func TestHandleCapabilitiesGET(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.SetReport(okReport(ToolGit, "git.status.read")); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil)
	rr := httptest.NewRecorder()
	r.HandleCapabilities(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("content-type=%q", got)
	}
	var snap CapabilityRegistry
	if err := json.Unmarshal(rr.Body.Bytes(), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion=%d", snap.SchemaVersion)
	}
	if snap.Tools[ToolGit] == nil {
		t.Errorf("git missing in serialized payload")
	}
}

func TestHandleCapabilitiesRejectsPOST(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities", nil)
	rr := httptest.NewRecorder()
	r.HandleCapabilities(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rr.Code)
	}
}

func TestHandleCapabilitiesRejectsPOSTReturnsProblemJSON(t *testing.T) {
	// hp-49tc: error responses must be RFC 7807 problem+json (matches the
	// rest of the daemon), not text/plain http.Error output.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	req := httptest.NewRequest(http.MethodPost, "/v1/capabilities", nil)
	rr := httptest.NewRecorder()
	r.HandleCapabilities(rr, req)
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("content-type = %q, want problem+json", got)
	}
	var problem schemas.Problem
	if err := json.Unmarshal(rr.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "capabilities.method_not_allowed" {
		t.Fatalf("problem.Code = %q, want capabilities.method_not_allowed", problem.Code)
	}
	if problem.Status != http.StatusMethodNotAllowed {
		t.Fatalf("problem.Status = %d, want %d", problem.Status, http.StatusMethodNotAllowed)
	}
	if rr.Header().Get("Allow") != "GET" {
		t.Fatalf("Allow header = %q, want GET", rr.Header().Get("Allow"))
	}
}

func TestHandleCompatibilityRejectsPOSTReturnsProblemJSON(t *testing.T) {
	// hp-49tc: same shape contract for /v1/compatibility.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	composer := StaticCompatibilityComposer{MinDesktopVersion: "0.1.0"}
	handler := r.HandleCompatibility(composer)
	req := httptest.NewRequest(http.MethodPost, "/v1/compatibility", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("content-type = %q, want problem+json", got)
	}
	var problem schemas.Problem
	if err := json.Unmarshal(rr.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "compatibility.method_not_allowed" {
		t.Fatalf("problem.Code = %q, want compatibility.method_not_allowed", problem.Code)
	}
}

type nilReturningComposer struct{}

func (nilReturningComposer) Compose(*CapabilityRegistry) *CompatibilityReport { return nil }

func TestHandleCompatibilityNilComposerResultReturnsProblemJSON(t *testing.T) {
	// hp-49tc: composer returning nil is a misconfiguration; the response
	// must be problem+json (was plain-text http.Error). Note: this is the
	// COMPOSE-returning-nil path, distinct from the boot-time panic when
	// HandleCompatibility itself is constructed with a nil CompatibilityComposer.
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	handler := r.HandleCompatibility(nilReturningComposer{})
	req := httptest.NewRequest(http.MethodGet, "/v1/compatibility", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusInternalServerError, rr.Body.String())
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/problem+json") {
		t.Fatalf("content-type = %q, want problem+json", got)
	}
	var problem schemas.Problem
	if err := json.Unmarshal(rr.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if problem.Code != "compatibility.composer_returned_nil" {
		t.Fatalf("problem.Code = %q, want compatibility.composer_returned_nil", problem.Code)
	}
}

func TestEncodeCapabilityJSONEncodingFailureReturnsError(t *testing.T) {
	// hp-49tc: writeCapabilityJSON now buffers the encoded body before
	// WriteHeader. The fix relies on encodeCapabilityJSON surfacing
	// encoding errors instead of writing partial bytes to the response —
	// pin that contract here so a future refactor doesn't regress to
	// "encode mid-stream after WriteHeader committed."
	if _, err := encodeCapabilityJSON(map[string]any{"bad": func() {}}); err == nil {
		t.Fatal("encodeCapabilityJSON returned nil error for func payload; encoding-failure detection is broken")
	}
}

func TestHandleCompatibility(t *testing.T) {
	r := newTestRegistry(t, "2026-05-02T23:29:34Z")
	if err := r.SetReport(okReport(ToolGit, "git.status.read")); err != nil {
		t.Fatal(err)
	}
	composer := StaticCompatibilityComposer{
		MinDesktopVersion: "0.1.0",
		EventSchemaVersions: map[string]int{
			"project": 1,
			"swarm":   1,
		},
		Migration: MigrationState{
			SchemaVersion: 1,
			AppliedAt:     "2026-05-02T23:00:00Z",
			Pending:       []string{},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/compatibility", nil)
	rr := httptest.NewRecorder()
	r.HandleCompatibility(composer)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var report CompatibilityReport
	if err := json.Unmarshal(rr.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion=%d", report.SchemaVersion)
	}
	if report.DaemonAPIVersion != fixedDaemonAPI {
		t.Errorf("daemonAPIVersion=%q", report.DaemonAPIVersion)
	}
	if report.MinDesktopVersion != "0.1.0" {
		t.Errorf("minDesktopVersion=%q", report.MinDesktopVersion)
	}
	if report.Capabilities == nil || report.Capabilities.Tools[ToolGit] == nil {
		t.Errorf("compatibility report missing capabilities snapshot")
	}
	if report.MigrationState.SchemaVersion != 1 {
		t.Errorf("migrationState.schemaVersion=%d", report.MigrationState.SchemaVersion)
	}
	if report.EventSchemaVersions["project"] != 1 {
		t.Errorf("eventSchemaVersions.project=%d", report.EventSchemaVersions["project"])
	}
}

