package ubs

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestArgvBuildersCoverRoundZeroAndHotspots(t *testing.T) {
	t.Parallel()
	first, err := FirstPassArgv(ScanRequest{
		ProjectDir: "/data/projects/hoopoe",
		Languages:  []string{"golang", "rust", "golang"},
		Categories: []string{"resource-lifecycle"},
		Jobs:       4,
		Hotspots:   []string{"ignored.go"},
	})
	if err != nil {
		t.Fatalf("FirstPassArgv: %v", err)
	}
	wantFirst := []string{
		"ubs",
		"--format=sarif",
		"--ci",
		"--non-interactive",
		"--no-auto-update",
		"--jobs=4",
		"--only=golang,rust",
		"--category=resource-lifecycle",
		"/data/projects/hoopoe",
	}
	if !reflect.DeepEqual(first, wantFirst) {
		t.Fatalf("first argv = %#v, want %#v", first, wantFirst)
	}
	assertNoShell(t, first)

	hotspots, err := HotspotArgv(ScanRequest{
		ProjectDir: "/data/projects/hoopoe",
		Hotspots: []string{
			"/data/projects/hoopoe/apps/daemon/main.go",
			"apps/daemon/internal/api/server.go",
			"apps/daemon/main.go",
		},
	})
	if err != nil {
		t.Fatalf("HotspotArgv: %v", err)
	}
	wantHotspots := []string{
		"ubs",
		"--format=sarif",
		"--ci",
		"--non-interactive",
		"--no-auto-update",
		"--files=apps/daemon/internal/api/server.go,apps/daemon/main.go",
		"/data/projects/hoopoe",
	}
	if !reflect.DeepEqual(hotspots, wantHotspots) {
		t.Fatalf("hotspot argv = %#v, want %#v", hotspots, wantHotspots)
	}
	assertNoShell(t, hotspots)
}

func TestArgvGuardsRejectUnsafeInputs(t *testing.T) {
	t.Parallel()
	tests := []ScanRequest{
		{},
		{ProjectDir: "/repo", Hotspots: []string{"../outside.go"}},
		{ProjectDir: "/repo", Hotspots: []string{"--format=text"}},
		{ProjectDir: "/repo", Hotspots: []string{"a,b.go"}},
		{ProjectDir: "/repo", Languages: []string{"go,python"}},
		{ProjectDir: "/repo", Categories: []string{"bad category"}},
	}
	for _, req := range tests {
		if _, err := ScanArgv(req); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("ScanArgv(%+v) err = %v, want ErrInvalidRequest", req, err)
		}
	}
	if _, err := HotspotArgv(ScanRequest{ProjectDir: "/repo"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("HotspotArgv without paths err = %v, want ErrInvalidRequest", err)
	}
}

func TestScanParsesSARIFIntoSourceStampedFindings(t *testing.T) {
	t.Parallel()
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"ubs --format=sarif --ci --non-interactive --no-auto-update /data/projects/hoopoe": {
			Stdout: []byte("UBS Meta-Runner v5.2.42\n" + sampleSARIF()),
		},
	}})
	adapter.Now = fixedNow
	got, err := adapter.FirstPass(context.Background(), ScanRequest{ProjectDir: "/data/projects/hoopoe"})
	if err != nil {
		t.Fatalf("FirstPass: %v", err)
	}
	if got.Round != RoundFirstPass || got.CheckedAt != fixedNow().UTC() {
		t.Fatalf("metadata = %+v", got)
	}
	if got.Summary.Critical != 1 || got.Summary.Warning != 1 || got.Summary.Info != 1 || got.Summary.Files != 2 {
		t.Fatalf("summary = %+v", got.Summary)
	}
	if len(got.Findings) != 3 {
		t.Fatalf("findings = %+v", got.Findings)
	}
	first := got.Findings[0]
	if first.Source != SourceUBS || !reflect.DeepEqual(first.Sources, []string{SourceUBS}) {
		t.Fatalf("source stamping = %+v", first)
	}
	if first.FilePath != "apps/daemon/internal/api/server.go" || first.LineRange.StartLine != 10 || first.LineRange.EndLine != 12 {
		t.Fatalf("location = %+v", first)
	}
	if first.Severity != SeverityWarning || first.Category != "go" || first.RuleID != "go.err-shadow" {
		t.Fatalf("classification = %+v", first)
	}
	if first.CodeContext != "err := call()" || first.FindingID == "" || first.Time != fixedNow().UTC() {
		t.Fatalf("context/id/time = %+v", first)
	}
}

func TestHotspotScanUsesScopedFiles(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{responses: map[string]CommandResult{
		"ubs --format=sarif --ci --non-interactive --no-auto-update --files=apps/daemon/internal/api/server.go /data/projects/hoopoe": {
			Stdout: []byte(sampleSARIF()),
		},
	}}
	adapter := New(runner)
	adapter.Now = fixedNow
	got, err := adapter.Hotspots(context.Background(), ScanRequest{
		ProjectDir: "/data/projects/hoopoe",
		Hotspots:   []string{"apps/daemon/internal/api/server.go"},
	})
	if err != nil {
		t.Fatalf("Hotspots: %v", err)
	}
	if got.Round != RoundHotspotTarget {
		t.Fatalf("round = %q", got.Round)
	}
	if len(runner.calls) != 1 || !strings.Contains(strings.Join(runner.calls[0], " "), "--files=apps/daemon/internal/api/server.go") {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
}

func TestMergeFindingsDedupesByFileLineRuleAndMergesSources(t *testing.T) {
	t.Parallel()
	base := Finding{
		FindingID: "agent_1",
		Source:    "skill:deadlock-finder",
		Sources:   []string{"skill:deadlock-finder"},
		FilePath:  "apps/daemon/internal/api/server.go",
		LineRange: LineRange{StartLine: 10},
		RuleID:    "go.err-shadow",
		Message:   "agent finding",
	}
	ubsFinding := Finding{
		FindingID: "ubs_1",
		Source:    SourceUBS,
		Sources:   []string{SourceUBS},
		FilePath:  "apps/daemon/internal/api/server.go",
		LineRange: LineRange{StartLine: 10},
		RuleID:    "go.err-shadow",
		Message:   "ubs finding",
	}
	merged := MergeFindings([]Finding{base}, []Finding{ubsFinding})
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	wantSources := []string{"skill:deadlock-finder", SourceUBS}
	if !reflect.DeepEqual(merged[0].Sources, wantSources) {
		t.Fatalf("sources = %#v, want %#v", merged[0].Sources, wantSources)
	}
	if merged[0].FindingID != "agent_1" {
		t.Fatalf("finding id should preserve existing ledger id, got %q", merged[0].FindingID)
	}
}

func TestProbeReportsScanCapability(t *testing.T) {
	t.Parallel()
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"ubs --version": {Stdout: []byte("UBS Meta-Runner v5.2.42 (git abc123)\n")},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if report.Tool != capabilities.ToolUBS || report.Version != "5.2.42" || report.Source != "cli" {
		t.Fatalf("identity = %+v", report)
	}
	if got := report.Capabilities[CapabilityScan]; got.Status != capabilities.StatusOK || got.Transport != "stdio" {
		t.Fatalf("ubs.scan = %+v", got)
	}
}

func TestProbeClassifiesMissingAndMalformedVersion(t *testing.T) {
	t.Parallel()
	missing := New(&fakeRunner{err: ErrMissingBinary})
	missing.Now = fixedNow
	report, err := missing.Probe(context.Background())
	if err != nil {
		t.Fatalf("missing Probe: %v", err)
	}
	if report.Capabilities[CapabilityScan].Status != capabilities.StatusMissing {
		t.Fatalf("missing ubs.scan = %+v", report.Capabilities[CapabilityScan])
	}

	malformed := New(&fakeRunner{responses: map[string]CommandResult{
		"ubs --version": {Stdout: []byte("not a version\n")},
	}})
	malformed.Now = fixedNow
	report, err = malformed.Probe(context.Background())
	if err != nil {
		t.Fatalf("malformed Probe: %v", err)
	}
	if report.Capabilities[CapabilityScan].Status != capabilities.StatusDegraded {
		t.Fatalf("malformed ubs.scan = %+v", report.Capabilities[CapabilityScan])
	}
}

func TestParseSARIFRejectsMalformedDocument(t *testing.T) {
	t.Parallel()
	_, _, err := ParseSARIF([]byte(`{"runs":[]}`), "/repo", fixedNow())
	if !errors.Is(err, ErrCommandContract) {
		t.Fatalf("ParseSARIF err = %v, want ErrCommandContract", err)
	}
}

func assertNoShell(t *testing.T, argv []string) {
	t.Helper()
	if len(argv) == 0 || argv[0] != ToolName {
		t.Fatalf("argv must invoke %s directly: %#v", ToolName, argv)
	}
	for _, arg := range argv {
		if arg == "sh" || arg == "-c" || strings.Contains(arg, "&&") || strings.Contains(arg, ";") {
			t.Fatalf("argv contains shell token: %#v", argv)
		}
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 1, 50, 0, 0, time.UTC)
}

type fakeRunner struct {
	responses map[string]CommandResult
	err       error
	calls     [][]string
}

func (r *fakeRunner) Run(_ context.Context, argv []string) (CommandResult, error) {
	r.calls = append(r.calls, append([]string(nil), argv...))
	if r.err != nil {
		return CommandResult{ExitCode: -1}, r.err
	}
	key := strings.Join(argv, " ")
	if result, ok := r.responses[key]; ok {
		return result, nil
	}
	return CommandResult{ExitCode: 127, Stderr: []byte("missing fake response for " + key)}, nil
}

func sampleSARIF() string {
	return `{
  "version": "2.1.0",
  "runs": [
    {
      "tool": {"driver": {"name": "ast-grep"}},
      "results": [
        {
          "level": "warning",
          "ruleId": "go.err-shadow",
          "message": {"text": "Potential err shadowing."},
          "locations": [{
            "physicalLocation": {
              "artifactLocation": {"uri": "/data/projects/hoopoe/apps/daemon/internal/api/server.go"},
              "region": {
                "startLine": 10,
                "endLine": 12,
                "startColumn": 3,
                "endColumn": 18,
                "snippet": {"text": "err := call()"}
              }
            }
          }]
        },
        {
          "level": "error",
          "ruleId": "go.resource-leak",
          "message": {"text": "Potential resource leak."},
          "locations": [{
            "physicalLocation": {
              "artifactLocation": {"uri": "/data/projects/hoopoe/apps/daemon/internal/api/server.go"},
              "region": {"startLine": 30, "snippet": {"text": "open()"}}
            }
          }]
        },
        {
          "level": "note",
          "ruleId": "js.no-console",
          "message": {"text": "Debug log."},
          "locations": [{
            "physicalLocation": {
              "artifactLocation": {"uri": "/data/projects/hoopoe/apps/desktop/src/main.ts"},
              "region": {"startLine": 5, "snippet": {"text": "console.log(value)"}}
            }
          }]
        }
      ]
    }
  ]
}`
}
