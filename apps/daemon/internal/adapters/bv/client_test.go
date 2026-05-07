// client_test.go — exercises the bv adapter against a fake executor +
// real fixtures from packages/fixtures.
//
// Coverage:
//   - Bare invocation refused (Guardrail 1).
//   - Triage / Plan / Insights / Diff / Next round-trip the real bv
//     output captured into testdata/.
//   - Golden output cases from packages/fixtures/golden-outputs/bv:
//     missing-tool, malformed-json, timeout, unsupported-version,
//     high-volume — each produces the expected CapabilityReport status.
//   - Probe summarises the per-capability status.
package bv

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeExecutor returns canned bytes/errors per command argv, enabling
// hermetic tests without bv installed.
type fakeExecutor struct {
	Stdouts map[string][]byte // key: argv joined by space
	Stderrs map[string][]byte
	Exits   map[string]int
	Errors  map[string]error
	Calls   []string // recorded argv per call (for assertions)
}

func newFakeExecutor() *fakeExecutor {
	return &fakeExecutor{
		Stdouts: map[string][]byte{},
		Stderrs: map[string][]byte{},
		Exits:   map[string]int{},
		Errors:  map[string]error{},
	}
}

func (f *fakeExecutor) Run(_ context.Context, args []string) ([]byte, []byte, int, error) {
	key := strings.Join(args, " ")
	f.Calls = append(f.Calls, key)
	if err := f.Errors[key]; err != nil {
		return nil, f.Stderrs[key], f.Exits[key], err
	}
	return f.Stdouts[key], f.Stderrs[key], f.Exits[key], nil
}

// loadFixture reads a fixture file from the repo's packages/fixtures
// tree. Skips the test if the fixture isn't found (e.g., the fixture
// corpus hasn't been pinned for this scenario yet).
func loadFixture(t *testing.T, relativePath string) []byte {
	t.Helper()
	// The test runs from the package dir; walk up to find the repo
	// root by looking for .beads/ or packages/.
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "packages", "fixtures", relativePath)
		if data, err := os.ReadFile(candidate); err == nil {
			return data
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skipf("fixture not found: packages/fixtures/%s", relativePath)
	return nil
}

func TestClientRefusesBareInvocation(t *testing.T) {
	t.Parallel()
	c := NewWithExecutor(newFakeExecutor())
	_, err := c.run(context.Background(), []string{}) // no robot flag
	if !errors.Is(err, ErrBareInvocationRefused) {
		t.Fatalf("expected ErrBareInvocationRefused, got %v", err)
	}
	_, err = c.run(context.Background(), []string{"--version"})
	if !errors.Is(err, ErrBareInvocationRefused) {
		t.Fatalf("expected ErrBareInvocationRefused for non-robot flag, got %v", err)
	}
}

func TestHasRobotFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"--robot-triage"}, true},
		{[]string{"--robot-plan"}, true},
		{[]string{"--recipe", "actionable", "--robot-plan"}, true},
		{[]string{"--version"}, false},
		{[]string{}, false},
		{[]string{"-r", "actionable"}, false}, // no robot flag
	}
	for _, c := range cases {
		got := hasRobotFlag(c.args)
		if got != c.want {
			t.Errorf("hasRobotFlag(%v) = %v, want %v", c.args, got, c.want)
		}
	}
}

func TestTriageDecodesRealOutput(t *testing.T) {
	t.Parallel()
	// Use a representative scenario fixture (raw bv stdout for triage).
	data := loadFixture(t, "scenarios/healthy-hour/bv-triage.json")

	fake := newFakeExecutor()
	fake.Stdouts["--robot-triage"] = data

	c := NewWithExecutor(fake)
	out, err := c.Triage(context.Background())
	if err != nil {
		t.Fatalf("Triage: %v", err)
	}
	if out.Triage.Meta.IssueCount == 0 {
		t.Fatalf("expected non-zero issue count from real fixture")
	}
	if len(out.Triage.QuickRef.TopPicks) == 0 {
		t.Fatalf("expected non-empty top picks from real fixture")
	}
	if out.Triage.QuickRef.TopPicks[0].ID == "" {
		t.Fatalf("top pick missing id")
	}
	// Raw bytes preserved.
	if len(out.Raw) == 0 {
		t.Fatalf("Raw bytes should be preserved")
	}
}

func TestTriageGoldenNormal(t *testing.T) {
	t.Parallel()
	data := loadFixture(t, "golden-outputs/bv/normal.json")

	// The golden-outputs format wraps the bv stdout in {meta, argv,
	// exit, durationMs, stdoutJson}. Unwrap to get just the stdout.
	var wrap struct {
		Argv       []string        `json:"argv"`
		Exit       int             `json:"exit"`
		StdoutJson json.RawMessage `json:"stdoutJson"`
	}
	if err := json.Unmarshal(data, &wrap); err != nil {
		t.Fatalf("decode golden wrapper: %v", err)
	}
	if wrap.Exit != 0 {
		t.Fatalf("golden 'normal' should have exit 0, got %d", wrap.Exit)
	}

	fake := newFakeExecutor()
	fake.Stdouts["--robot-triage"] = []byte(wrap.StdoutJson)
	c := NewWithExecutor(fake)

	out, err := c.Triage(context.Background())
	if err != nil {
		t.Fatalf("Triage on golden 'normal': %v", err)
	}
	if out.Triage.Meta.IssueCount == 0 {
		t.Fatalf("golden 'normal' should have non-zero issues")
	}
}

func TestTriageReturnsErrorOnNonZeroExit(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["--robot-triage"] = []byte("")
	fake.Stderrs["--robot-triage"] = []byte("bv: project not found at .beads/")
	fake.Exits["--robot-triage"] = 2

	c := NewWithExecutor(fake)
	_, err := c.Triage(context.Background())
	if err == nil {
		t.Fatalf("expected error on non-zero exit")
	}
	if !strings.Contains(err.Error(), "exited 2") {
		t.Fatalf("error should mention exit code, got %v", err)
	}
	if !strings.Contains(err.Error(), "project not found") {
		t.Fatalf("error should include stderr, got %v", err)
	}
}

func TestTriageReturnsErrorOnEmptyStdout(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["--robot-triage"] = []byte("")
	c := NewWithExecutor(fake)
	_, err := c.Triage(context.Background())
	if err == nil {
		t.Fatalf("expected error on empty stdout")
	}
	if !strings.Contains(err.Error(), "empty stdout") {
		t.Fatalf("error should mention empty stdout, got %v", err)
	}
}

func TestTriageReturnsErrorOnMalformedJSON(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["--robot-triage"] = []byte("{not json at all")
	c := NewWithExecutor(fake)
	_, err := c.Triage(context.Background())
	if err == nil {
		t.Fatalf("expected error on malformed JSON")
	}
	if !strings.Contains(err.Error(), "decode triage") {
		t.Fatalf("error should mention decode failure, got %v", err)
	}
}

func TestPlanWithRecipePassesArgsThrough(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	planJSON := []byte(`{
		"generated_at": "2026-05-04T00:00:00Z",
		"data_hash": "abc",
		"plan": {
			"total_actionable": 1,
			"total_blocked": 0,
			"tracks": [{"items": [{"id": "hp-x", "title": "T", "status": "open", "priority": 0}]}],
			"summary": {"highest_impact": "hp-x", "impact_reason": "test", "unblocks_count": 0}
		}
	}`)
	fake.Stdouts["--recipe actionable --robot-plan"] = planJSON

	c := NewWithExecutor(fake)
	out, err := c.Plan(context.Background(), "actionable")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if out.Plan.TotalActionable != 1 {
		t.Fatalf("expected total_actionable 1, got %d", out.Plan.TotalActionable)
	}
	if out.Plan.Summary.HighestImpact != "hp-x" {
		t.Fatalf("expected highest_impact hp-x, got %q", out.Plan.Summary.HighestImpact)
	}
}

func TestDiffRequiresSinceRef(t *testing.T) {
	t.Parallel()
	c := NewWithExecutor(newFakeExecutor())
	_, err := c.Diff(context.Background(), "")
	if err == nil {
		t.Fatalf("expected error on empty since-ref")
	}
}

func TestDiffPassesSinceRef(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	diffJSON := []byte(`{
		"generated_at": "2026-05-04T00:00:00Z",
		"resolved_revision": "abc",
		"from_data_hash": "h1",
		"to_data_hash": "h2",
		"diff": {
			"from_timestamp": "2026-05-03T00:00:00Z",
			"to_timestamp": "2026-05-04T00:00:00Z",
			"from_revision": "HEAD~5",
			"new_issues": null,
			"closed_issues": null,
			"removed_issues": null,
			"reopened_issues": null,
			"modified_issues": [],
			"metric_deltas": {
				"total_issues": 0, "open_issues": 0, "closed_issues": 0,
				"blocked_issues": 0, "total_edges": 0, "cycle_count": 0,
				"component_count": 0, "avg_pagerank": 0, "avg_betweenness": 0
			},
			"summary": {
				"total_changes": 0, "issues_added": 0, "issues_closed": 0,
				"issues_removed": 0, "issues_reopened": 0, "issues_modified": 0,
				"cycles_introduced": 0, "cycles_resolved": 0, "net_issue_change": 0,
				"health_trend": "stable"
			}
		}
	}`)
	fake.Stdouts["--robot-diff --diff-since HEAD~5"] = diffJSON
	c := NewWithExecutor(fake)
	out, err := c.Diff(context.Background(), "HEAD~5")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if out.Diff.Summary.HealthTrend != "stable" {
		t.Fatalf("expected health_trend stable, got %q", out.Diff.Summary.HealthTrend)
	}
}

func TestNextDecodesMinimalShape(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Stdouts["--robot-next"] = []byte(`{
		"generated_at": "2026-05-04T00:00:00Z",
		"data_hash": "abc",
		"next": {"id": "hp-x", "title": "T"},
		"status": "ok"
	}`)
	c := NewWithExecutor(fake)
	out, err := c.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	if out.Status != "ok" {
		t.Fatalf("expected status ok, got %q", out.Status)
	}
}

func TestProbeReportsOkWhenAllCommandsSucceed(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	// Minimum-viable JSON for each command — enough to decode without errors.
	fake.Stdouts["--robot-triage"] = []byte(`{"generated_at":"2026-05-04T00:00:00Z","data_hash":"x","triage":{"meta":{"version":"1","generated_at":"x","phase2_ready":true,"issue_count":1},"quick_ref":{"open_count":1,"actionable_count":0,"blocked_count":0,"in_progress_count":0,"top_picks":[]}}}`)
	fake.Stdouts["--robot-plan"] = []byte(`{"generated_at":"2026-05-04T00:00:00Z","data_hash":"x","plan":{"total_actionable":0,"total_blocked":0,"tracks":[],"summary":{"unblocks_count":0}}}`)
	fake.Stdouts["--robot-insights"] = []byte(`{}`)
	fake.Stdouts["--robot-diff --diff-since HEAD"] = []byte(`{
		"generated_at":"2026-05-04T00:00:00Z","resolved_revision":"x","from_data_hash":"a","to_data_hash":"b",
		"diff":{"from_timestamp":"2026-05-04T00:00:00Z","to_timestamp":"2026-05-04T00:00:00Z","from_revision":"HEAD","new_issues":null,"closed_issues":null,"removed_issues":null,"reopened_issues":null,"modified_issues":[],"metric_deltas":{"total_issues":0,"open_issues":0,"closed_issues":0,"blocked_issues":0,"total_edges":0,"cycle_count":0,"component_count":0,"avg_pagerank":0,"avg_betweenness":0},"summary":{"total_changes":0,"issues_added":0,"issues_closed":0,"issues_removed":0,"issues_reopened":0,"issues_modified":0,"cycles_introduced":0,"cycles_resolved":0,"net_issue_change":0,"health_trend":"stable"}}
	}`)
	fake.Stdouts["--robot-next"] = []byte(`{"generated_at":"2026-05-04T00:00:00Z","data_hash":"x","status":"ok"}`)

	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, func() time.Time {
		return time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	})

	if res.Tool != "bv" {
		t.Fatalf("expected tool bv, got %q", res.Tool)
	}
	if res.Source != "CLI" {
		t.Fatalf("expected source CLI, got %q", res.Source)
	}
	for _, id := range AllCapabilityIDs() {
		report, ok := res.Reports[id]
		if !ok {
			t.Fatalf("missing report for %s", id)
			continue
		}
		if report.Status != StatusOK {
			t.Fatalf("expected %s status ok, got %q (notes: %s)",
				id, report.Status, report.Notes)
		}
	}
}

func TestProbeReportsDegradedOnNonZeroExit(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Exits["--robot-triage"] = 1
	fake.Stderrs["--robot-triage"] = []byte("bv: parse error")

	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, nil)
	report := res.Reports[CapTriage]
	if report.Status != StatusDegraded {
		t.Fatalf("expected degraded, got %q", report.Status)
	}
	if !strings.Contains(report.Notes, "parse error") || !strings.Contains(report.Notes, "triage") {
		t.Fatalf("notes should mention the error, got %q", report.Notes)
	}
}

func TestProbeReportsMissingWhenBinaryAbsent(t *testing.T) {
	t.Parallel()
	fake := newFakeExecutor()
	fake.Errors["--robot-triage"] = errors.New(`exec: "bv": executable file not found in $PATH`)

	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, nil)
	report := res.Reports[CapTriage]
	if report.Status != StatusMissing {
		t.Fatalf("expected missing, got %q", report.Status)
	}
}

// TestProbeOnMissingToolGoldenFixtureMarksAllCapabilitiesMissing loads
// packages/fixtures/golden-outputs/bv/missing-tool.json and pins the
// adapter contract from plan.md §18.3 by driving the captured exit-127 +
// "bv: command not found" pair through Probe via the existing fake
// executor — distinct from TestProbeReportsMissingWhenBinaryAbsent above
// which exercises the *err-set* path of fakeExecutor (os/exec ENOENT
// surfaces). This test exercises the *exit-only* path: stderr says
// "command not found" and exit=127, so isMissingBinary catches the
// "command not found" substring inside Client.run's wrapped error
// (capabilities.go:191) and every probed capability lands at StatusMissing.
func TestProbeOnMissingToolGoldenFixtureMarksAllCapabilitiesMissing(t *testing.T) {
	t.Parallel()
	fixture := loadBVGoldenFixture(t, "missing-tool.json")
	if fixture.Meta.State != "missing-tool" {
		t.Fatalf("fixture state = %q, want missing-tool", fixture.Meta.State)
	}
	if cap, ok := fixture.Capabilities["bv._present"]; !ok || cap.Status != "missing" {
		t.Fatalf("fixture must declare bv._present=missing, got %+v", fixture.Capabilities)
	}
	if fixture.Exit != 127 {
		t.Fatalf("fixture exit = %d, want 127", fixture.Exit)
	}

	fake := newFakeExecutor()
	for _, robotFlag := range []string{
		"--robot-triage",
		"--robot-plan",
		"--robot-insights",
		"--robot-next",
	} {
		fake.Exits[robotFlag] = fixture.Exit
		fake.Stderrs[robotFlag] = []byte(fixture.StderrText)
	}
	fake.Exits["--robot-diff --diff-since HEAD"] = fixture.Exit
	fake.Stderrs["--robot-diff --diff-since HEAD"] = []byte(fixture.StderrText)

	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, func() time.Time { return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC) })

	for _, capID := range []string{CapTriage, CapPlan, CapInsights, CapDiff, CapNext} {
		report, ok := res.Reports[capID]
		if !ok {
			t.Fatalf("%s missing from probe report", capID)
		}
		if report.Status != StatusMissing {
			t.Fatalf("%s status = %q, want missing (fixture state=missing-tool)", capID, report.Status)
		}
		if !strings.Contains(report.Notes, "command not found") {
			t.Fatalf("%s notes = %q; want fixture stderr surfaced", capID, report.Notes)
		}
	}
}

type bvGoldenFixture struct {
	Meta struct {
		Adapter string `json:"adapter"`
		State   string `json:"state"`
	} `json:"meta"`
	Exit         int    `json:"exit"`
	StdoutText   string `json:"stdoutText"`
	StderrText   string `json:"stderrText"`
	Truncated    bool   `json:"truncated"`
	Capabilities map[string]struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	} `json:"capabilities"`
}

// TestProbeOnMalformedJSONGoldenFixtureDegradesAllCapabilities loads
// packages/fixtures/golden-outputs/bv/malformed-json.json and pins the
// contract from plan.md §18.3 for the malformed-json state: exit-0 with a
// truncated/non-JSON stdout produces StatusDegraded on every probed
// capability — neither StatusOK (parser saw no error path) nor
// StatusMissing (binary present and exited cleanly).
//
// Specifically exercises the *parser* failure surface: Client.run returns
// the raw stdout because exit=0, then each typed parser (Triage, Plan,
// Insights, Diff, Next) hits json.Unmarshal on the truncated bytes and
// returns a wrapped "decode X" error. probeOne reads StatusDegraded with
// the parser error trace surfaced via the "%s probe error: %s" format.
func TestProbeOnMalformedJSONGoldenFixtureDegradesAllCapabilities(t *testing.T) {
	t.Parallel()
	fixture := loadBVGoldenFixture(t, "malformed-json.json")
	if fixture.Meta.State != "malformed-json" {
		t.Fatalf("fixture state = %q, want malformed-json", fixture.Meta.State)
	}
	if cap, ok := fixture.Capabilities["bv._parse"]; !ok || cap.Status != "degraded" {
		t.Fatalf("fixture must declare bv._parse=degraded, got %+v", fixture.Capabilities)
	}
	if fixture.StdoutText == "" || fixture.Exit != 0 {
		t.Fatalf("fixture stdoutText/exit = %q/%d; want non-empty/0", fixture.StdoutText, fixture.Exit)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Probe panicked on malformed JSON fixture: %v", r)
		}
	}()

	fake := newFakeExecutor()
	for _, robotFlag := range []string{
		"--robot-triage",
		"--robot-plan",
		"--robot-insights",
		"--robot-next",
		"--robot-diff --diff-since HEAD",
	} {
		fake.Stdouts[robotFlag] = []byte(fixture.StdoutText)
	}

	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, func() time.Time { return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC) })

	for _, capID := range []string{CapTriage, CapPlan, CapInsights, CapDiff, CapNext} {
		report, ok := res.Reports[capID]
		if !ok {
			t.Fatalf("%s missing from probe report", capID)
		}
		if report.Status != StatusDegraded {
			t.Fatalf("%s status = %q, want degraded (fixture state=malformed-json)", capID, report.Status)
		}
		if !strings.Contains(strings.ToLower(report.Notes), "decode") {
			t.Fatalf("%s notes = %q; want parser decode-error wrapping", capID, report.Notes)
		}
	}

	// Also exercise the typed parsers directly so a regression that drops
	// the json.Unmarshal error surface — e.g., "best-effort" empty parse
	// returning a zero value and nil — fails this test rather than
	// silently passing.
	directFake := newFakeExecutor()
	directFake.Stdouts["--robot-triage"] = []byte(fixture.StdoutText)
	dc := NewWithExecutor(directFake)
	if _, err := dc.Triage(context.Background()); err == nil {
		t.Fatalf("Triage accepted truncated JSON without error")
	}
}

// TestProbeOnTimeoutGoldenFixtureDegradesAllCapabilities loads
// packages/fixtures/golden-outputs/bv/timeout.json and pins the contract
// from plan.md §18.3 for the timeout state: exit-124 + "timeout: sending
// signal TERM" stderr (the GNU coreutils `timeout` envelope's standard
// signal trace) must produce StatusDegraded on every probed capability,
// not StatusMissing — the binary exists but the call exceeded
// ENVELOPE_TIMEOUT_S.
//
// "Do not retry without backoff" from the fixture notes is a daemon-level
// contract for the recovery action; this test only pins the surface that
// classification correctly distinguishes envelope timeout from missing
// binary.
func TestProbeOnTimeoutGoldenFixtureDegradesAllCapabilities(t *testing.T) {
	t.Parallel()
	fixture := loadBVGoldenFixture(t, "timeout.json")
	if fixture.Meta.State != "timeout" {
		t.Fatalf("fixture state = %q, want timeout", fixture.Meta.State)
	}
	if cap, ok := fixture.Capabilities["bv._timeout"]; !ok || cap.Status != "degraded" {
		t.Fatalf("fixture must declare bv._timeout=degraded, got %+v", fixture.Capabilities)
	}
	if fixture.Exit != 124 {
		t.Fatalf("fixture exit = %d, want 124 (GNU timeout)", fixture.Exit)
	}

	fake := newFakeExecutor()
	for _, robotFlag := range []string{
		"--robot-triage",
		"--robot-plan",
		"--robot-insights",
		"--robot-next",
		"--robot-diff --diff-since HEAD",
	} {
		fake.Exits[robotFlag] = fixture.Exit
		fake.Stderrs[robotFlag] = []byte(fixture.StderrText)
	}

	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, func() time.Time { return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC) })

	for _, capID := range []string{CapTriage, CapPlan, CapInsights, CapDiff, CapNext} {
		report, ok := res.Reports[capID]
		if !ok {
			t.Fatalf("%s missing from probe report", capID)
		}
		if report.Status != StatusDegraded {
			t.Fatalf("%s status = %q, want degraded (fixture state=timeout)", capID, report.Status)
		}
		if !strings.Contains(report.Notes, "exited 124") {
			t.Fatalf("%s notes = %q; want exit-124 wrapping preserved", capID, report.Notes)
		}
		if !strings.Contains(report.Notes, "TERM") {
			t.Fatalf("%s notes = %q; want fixture stderr signal trace surfaced", capID, report.Notes)
		}
	}
}

// TestProbeOnHighVolumeGoldenFixtureDegradesAllCapabilities loads
// packages/fixtures/golden-outputs/bv/high-volume.json and pins the
// adapter contract from plan.md §18.3 for the "high-volume" state.
//
// Fixture mimics ENVELOPE_MAX_BYTES truncation: stdoutBytes=1048576
// (1MiB), truncated=true, synthetic bv._highVolume=degraded with the
// note "stdout exceeded ENVELOPE_MAX_BYTES; pagination required".
//
// The bv adapter does not enforce its own output cap (no MaxBytes in
// client.go) — the truncation contract documents that *upstream* (the
// envelope harness running bv) enforces a cap, and when the resulting
// stdout is a truncated/non-parseable JSON envelope, the typed parsers
// (Triage, Plan, Insights, Diff, Next) must surface StatusDegraded
// rather than crashing or accepting a zero value as success.
//
// Pinned here:
//
//  1. Fixture self-consistency (state, exit=0, truncated=true,
//     _highVolume=degraded).
//  2. The fixture's stdoutText placeholder does NOT parse as valid
//     JSON (sanity guard against drift).
//  3. Probe drives through the truncated envelope: every probed
//     capability degrades with parser-error notes, distinct from the
//     timeout (exit=124) and missing-tool (exit=127) error classes.
func TestProbeOnHighVolumeGoldenFixtureDegradesAllCapabilities(t *testing.T) {
	t.Parallel()
	fixture := loadBVGoldenFixture(t, "high-volume.json")
	if fixture.Meta.State != "high-volume" {
		t.Fatalf("fixture state = %q, want high-volume", fixture.Meta.State)
	}
	if !fixture.Truncated {
		t.Fatalf("fixture truncated = false, want true (envelope-truncation is the high-volume contract)")
	}
	if fixture.Exit != 0 {
		t.Fatalf("fixture exit = %d, want 0 (high-volume is envelope-size, not failure)", fixture.Exit)
	}
	cap, ok := fixture.Capabilities["bv._highVolume"]
	if !ok || cap.Status != "degraded" {
		t.Fatalf("fixture must declare bv._highVolume=degraded, got %+v", fixture.Capabilities)
	}

	// Sanity: the fixture's stdoutText placeholder must NOT be valid JSON.
	var parsed any
	if err := json.Unmarshal([]byte(fixture.StdoutText), &parsed); err == nil {
		t.Fatalf("fixture stdoutText parses as valid JSON; the high-volume contract requires a non-parseable truncated sample")
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Probe panicked on high-volume fixture: %v", r)
		}
	}()

	fake := newFakeExecutor()
	for _, robotFlag := range []string{
		"--robot-triage",
		"--robot-plan",
		"--robot-insights",
		"--robot-next",
		"--robot-diff --diff-since HEAD",
	} {
		fake.Stdouts[robotFlag] = []byte(fixture.StdoutText)
	}

	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, func() time.Time { return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC) })

	for _, capID := range []string{CapTriage, CapPlan, CapInsights, CapDiff, CapNext} {
		report, ok := res.Reports[capID]
		if !ok {
			t.Fatalf("%s missing from probe report", capID)
		}
		if report.Status != StatusDegraded {
			t.Fatalf("%s status = %q, want degraded (fixture state=high-volume; truncated envelope must not parse as success)", capID, report.Status)
		}
		if !strings.Contains(strings.ToLower(report.Notes), "decode") {
			t.Fatalf("%s notes = %q; want parser decode-error wrapping for truncated envelope", capID, report.Notes)
		}
	}
}

// TestProbeOnUnsupportedVersionGoldenFixturePinsCorpusContract loads
// packages/fixtures/golden-outputs/bv/unsupported-version.json and pins
// the bv adapter contract from plan.md §18.3 for the
// "unsupported-version" state.
//
// Today probeVersion (capabilities.go:158) records the observed bv
// version into ProbeResult.Version on a best-effort basis but no
// capability is gated on a minimum version. The fixture documents the
// downstream contract that future version-gating logic must honor:
// report `bv._minVersion` as `missing` when the observed version is
// below the integration-contract minimum.
//
// Pinned here:
//
//  1. Fixture self-consistency (state, exit=0, stdoutText "bv 0.0.1",
//     _minVersion=missing).
//  2. The synthetic `bv._minVersion` capability is *not* a real adapter
//     capability constant — fixture-corpus marker only. AllCapabilityIDs
//     must not contain it; if it ever does, this test fails so the
//     contract change is intentional.
//  3. Probe drives through the unsupported version: `--version` returns
//     "bv 0.0.1\n" and probeVersion captures "0.0.1" verbatim into
//     ProbeResult.Version. All real capabilities (Triage, Plan, Insights,
//     Diff, Next) still report StatusOK because no version gate exists
//     yet — a future commit that introduces version gating must promote
//     _minVersion to a real capability and downgrade gated capabilities
//     here intentionally.
func TestProbeOnUnsupportedVersionGoldenFixturePinsCorpusContract(t *testing.T) {
	t.Parallel()
	fixture := loadBVGoldenFixture(t, "unsupported-version.json")
	if fixture.Meta.State != "unsupported-version" {
		t.Fatalf("fixture state = %q, want unsupported-version", fixture.Meta.State)
	}
	if fixture.Exit != 0 {
		t.Fatalf("fixture exit = %d, want 0 (binary executed and printed version)", fixture.Exit)
	}
	if !strings.Contains(fixture.StdoutText, "0.0.1") {
		t.Fatalf("fixture stdoutText = %q, want '0.0.1' marker", fixture.StdoutText)
	}
	versionCap, ok := fixture.Capabilities["bv._minVersion"]
	if !ok || versionCap.Status != "missing" {
		t.Fatalf("fixture must declare bv._minVersion=missing, got %+v", fixture.Capabilities)
	}

	for _, id := range AllCapabilityIDs() {
		if id == "bv._minVersion" {
			t.Fatalf("bv._minVersion is now a real adapter capability — update the fixture/test contract intentionally")
		}
	}

	fake := newFakeExecutor()
	fake.Stdouts["--version"] = []byte(fixture.StdoutText)
	// Minimum-viable JSON for each capability so probeOne reaches StatusOK —
	// reused from TestProbeReportsOkWhenAllCommandsSucceed.
	fake.Stdouts["--robot-triage"] = []byte(`{"generated_at":"2026-05-04T00:00:00Z","data_hash":"x","triage":{"meta":{"version":"1","generated_at":"x","phase2_ready":true,"issue_count":1},"quick_ref":{"open_count":1,"actionable_count":0,"blocked_count":0,"in_progress_count":0,"top_picks":[]}}}`)
	fake.Stdouts["--robot-plan"] = []byte(`{"generated_at":"2026-05-04T00:00:00Z","data_hash":"x","plan":{"total_actionable":0,"total_blocked":0,"tracks":[],"summary":{"unblocks_count":0}}}`)
	fake.Stdouts["--robot-insights"] = []byte(`{}`)
	fake.Stdouts["--robot-diff --diff-since HEAD"] = []byte(`{"generated_at":"2026-05-04T00:00:00Z","resolved_revision":"x","from_data_hash":"a","to_data_hash":"b","diff":{"from_timestamp":"2026-05-04T00:00:00Z","to_timestamp":"2026-05-04T00:00:00Z","from_revision":"HEAD","new_issues":null,"closed_issues":null,"removed_issues":null,"reopened_issues":null,"modified_issues":[],"metric_deltas":{"total_issues":0,"open_issues":0,"closed_issues":0,"blocked_issues":0,"total_edges":0,"cycle_count":0,"component_count":0,"avg_pagerank":0,"avg_betweenness":0},"summary":{"total_changes":0,"issues_added":0,"issues_closed":0,"issues_removed":0,"issues_reopened":0,"issues_modified":0,"cycles_introduced":0,"cycles_resolved":0,"net_issue_change":0,"health_trend":"stable"}}}`)
	fake.Stdouts["--robot-next"] = []byte(`{"generated_at":"2026-05-04T00:00:00Z","data_hash":"x","status":"ok"}`)

	c := NewWithExecutor(fake)
	res := Probe(context.Background(), c, func() time.Time { return time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC) })

	if res.Tool != "bv" {
		t.Fatalf("ProbeResult.Tool = %q, want bv", res.Tool)
	}
	if !strings.Contains(res.Version, "0.0.1") {
		t.Fatalf("ProbeResult.Version = %q, want it to capture the unsupported '0.0.1' verbatim", res.Version)
	}
	for _, capID := range AllCapabilityIDs() {
		report, ok := res.Reports[capID]
		if !ok {
			t.Fatalf("%s missing from probe report", capID)
		}
		if report.Status != StatusOK {
			t.Fatalf("%s = %q, want ok (no version gating today; future version-gating must promote _minVersion). notes=%q", capID, report.Status, report.Notes)
		}
	}
}

func loadBVGoldenFixture(t *testing.T, name string) bvGoldenFixture {
	t.Helper()
	data := loadFixture(t, filepath.Join("golden-outputs", "bv", name))
	var fixture bvGoldenFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture %s: %v", name, err)
	}
	if fixture.Meta.Adapter != "bv" {
		t.Fatalf("fixture %s adapter = %q, want bv", name, fixture.Meta.Adapter)
	}
	return fixture
}
