package br

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

type brConformancePack struct {
	Tool            string                          `json:"tool"`
	ToolVersion     string                          `json:"toolVersion"`
	FixturesVersion string                          `json:"fixturesVersion"`
	Commands        map[string]brConformanceCommand `json:"commands"`
	Expected        brConformanceExpected           `json:"expected"`
}

type brConformanceCommand struct {
	Argv       []string        `json:"argv"`
	Exit       int             `json:"exit"`
	StdoutJSON json.RawMessage `json:"stdoutJson,omitempty"`
	StdoutText string          `json:"stdoutText,omitempty"`
	StderrText string          `json:"stderrText,omitempty"`
}

type brConformanceExpected struct {
	List  brListConformanceSummary  `json:"list"`
	Ready brRowsConformanceSummary  `json:"ready"`
	Show  brRowsConformanceSummary  `json:"show"`
	Probe brProbeConformanceSummary `json:"probe"`
}

type brListConformanceSummary struct {
	Total   int                       `json:"total"`
	Limit   int                       `json:"limit"`
	Offset  int                       `json:"offset"`
	HasMore bool                      `json:"hasMore"`
	IDs     []string                  `json:"ids"`
	First   brIssueConformanceSummary `json:"first"`
}

type brRowsConformanceSummary struct {
	Count int                       `json:"count"`
	IDs   []string                  `json:"ids"`
	First brIssueConformanceSummary `json:"first"`
}

type brIssueConformanceSummary struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Status           string   `json:"status"`
	Priority         int      `json:"priority"`
	IssueType        string   `json:"issueType"`
	Labels           []string `json:"labels,omitempty"`
	DependencyCount  int      `json:"dependencyCount,omitempty"`
	DependentCount   int      `json:"dependentCount,omitempty"`
	DependencyTitles []string `json:"dependencyTitles,omitempty"`
}

type brProbeConformanceSummary struct {
	Version      string            `json:"version"`
	Capabilities map[string]string `json:"capabilities"`
}

type brConformanceRunner struct {
	responses map[string]CommandResult
	calls     []string
}

func TestPhase0BRConformanceCaptures(t *testing.T) {
	packs := loadBRConformancePacks(t)
	if len(packs) == 0 {
		t.Fatal("no br conformance capture packs found")
	}

	for _, pack := range packs {
		pack := pack
		t.Run(pack.ToolVersion, func(t *testing.T) {
			if pack.Tool != ToolName {
				t.Fatalf("tool = %q, want %q", pack.Tool, ToolName)
			}
			runner := newBRConformanceRunner(t, pack)
			adapter := New(runner)
			adapter.Now = func() time.Time { return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC) }

			list, err := adapter.List(context.Background(), ListFilter{})
			if err != nil {
				t.Fatalf("List: %v", err)
			}
			assertBRConformanceEqual(t, "list", summarizeBRList(list), pack.Expected.List)

			ready, err := adapter.Ready(context.Background(), ReadyFilter{})
			if err != nil {
				t.Fatalf("Ready: %v", err)
			}
			assertBRConformanceEqual(t, "ready", summarizeBRRows(ready), pack.Expected.Ready)

			showID := firstExpectedID(t, pack.Expected.Show)
			shown, err := adapter.Show(context.Background(), showID)
			if err != nil {
				t.Fatalf("Show: %v", err)
			}
			assertBRConformanceEqual(t, "show", summarizeBRRows(shown), pack.Expected.Show)

			report, err := adapter.Probe(context.Background())
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if err := report.Validate(); err != nil {
				t.Fatalf("probe report failed validation: %v", err)
			}
			assertBRConformanceEqual(t, "probe", summarizeBRProbe(report), pack.Expected.Probe)
		})
	}
}

func loadBRConformancePacks(t *testing.T) []brConformancePack {
	t.Helper()
	capturesDir := filepath.Join(findRepoRoot(t), "packages", "fixtures", "phase0-br", "captures")
	entries, err := os.ReadDir(capturesDir)
	if err != nil {
		t.Fatalf("read br conformance captures: %v", err)
	}
	packs := make([]brConformancePack, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(capturesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var pack brConformancePack
		if err := json.Unmarshal(data, &pack); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		if pack.ToolVersion == "" || pack.FixturesVersion == "" {
			t.Fatalf("%s missing toolVersion/fixturesVersion", path)
		}
		if len(pack.Commands) == 0 {
			t.Fatalf("%s has no command captures", path)
		}
		packs = append(packs, pack)
	}
	sort.Slice(packs, func(i, j int) bool {
		return packs[i].ToolVersion < packs[j].ToolVersion
	})
	return packs
}

func newBRConformanceRunner(t *testing.T, pack brConformancePack) *brConformanceRunner {
	t.Helper()
	runner := &brConformanceRunner{responses: map[string]CommandResult{}}
	for name, command := range pack.Commands {
		if len(command.Argv) == 0 {
			t.Fatalf("%s command %q has empty argv", pack.ToolVersion, name)
		}
		assertBRConformanceArgv(t, command.Argv)
		stdout, err := brConformanceStdout(command)
		if err != nil {
			t.Fatalf("%s command %q: %v", pack.ToolVersion, name, err)
		}
		key := strings.Join(command.Argv, " ")
		if _, exists := runner.responses[key]; exists {
			t.Fatalf("%s command %q duplicates argv %q", pack.ToolVersion, name, key)
		}
		runner.responses[key] = CommandResult{
			ExitCode: command.Exit,
			Stdout:   stdout,
			Stderr:   []byte(command.StderrText),
		}
	}
	return runner
}

func brConformanceStdout(command brConformanceCommand) ([]byte, error) {
	if len(command.StdoutJSON) > 0 {
		if !json.Valid(command.StdoutJSON) {
			return nil, fmt.Errorf("stdoutJson is invalid")
		}
		return append([]byte(nil), command.StdoutJSON...), nil
	}
	return []byte(command.StdoutText), nil
}

func (r *brConformanceRunner) Run(_ context.Context, argv []string) (CommandResult, error) {
	key := strings.Join(argv, " ")
	r.calls = append(r.calls, key)
	if result, ok := r.responses[key]; ok {
		return result, nil
	}
	return CommandResult{
		ExitCode: 127,
		Stderr:   []byte("missing conformance capture for " + key),
	}, nil
}

func assertBRConformanceArgv(t *testing.T, argv []string) {
	t.Helper()
	if argv[0] != ToolName {
		t.Fatalf("capture argv must start with br: %#v", argv)
	}
	for _, part := range argv {
		if part == "sh" || part == "-c" || part == "bash" || strings.Contains(part, "&&") || strings.Contains(part, ";") {
			t.Fatalf("capture argv contains shell token: %#v", argv)
		}
	}
}

func summarizeBRList(list ListResponse) brListConformanceSummary {
	return brListConformanceSummary{
		Total:   list.Total,
		Limit:   list.Limit,
		Offset:  list.Offset,
		HasMore: list.HasMore,
		IDs:     brIssueIDs(list.Issues),
		First:   summarizeBRIssue(firstBRIssue(list.Issues)),
	}
}

func summarizeBRRows(issues []Issue) brRowsConformanceSummary {
	return brRowsConformanceSummary{
		Count: len(issues),
		IDs:   brIssueIDs(issues),
		First: summarizeBRIssue(firstBRIssue(issues)),
	}
}

func summarizeBRIssue(issue Issue) brIssueConformanceSummary {
	return brIssueConformanceSummary{
		ID:               issue.ID,
		Title:            issue.Title,
		Status:           issue.Status,
		Priority:         issue.Priority,
		IssueType:        issue.IssueType,
		Labels:           nonEmptyStrings(issue.Labels),
		DependencyCount:  issue.DependencyCount,
		DependentCount:   issue.DependentCount,
		DependencyTitles: nonEmptyStrings(brDependencyTitles(issue.Dependencies)),
	}
}

func summarizeBRProbe(report *capabilities.ToolReport) brProbeConformanceSummary {
	caps := map[string]string{}
	for _, capID := range []string{
		CapabilityPresent,
		CapabilityIssuesRead,
		CapabilityIssuesUpdate,
		CapabilityReady,
		CapabilityCreate,
		CapabilityClose,
		CapabilityDepAdd,
		CapabilityDepRemove,
		CapabilityDepCycles,
		CapabilitySyncFlushOnly,
		CapabilitySyncFlush,
		CapabilityDoctor,
		CapabilitySchema,
		CapabilityTUI,
	} {
		caps[capID] = string(report.Capabilities[capID].Status)
	}
	return brProbeConformanceSummary{
		Version:      report.Version,
		Capabilities: caps,
	}
}

func brIssueIDs(issues []Issue) []string {
	ids := make([]string, 0, len(issues))
	for _, issue := range issues {
		ids = append(ids, issue.ID)
	}
	return ids
}

func firstBRIssue(issues []Issue) Issue {
	if len(issues) == 0 {
		return Issue{}
	}
	return issues[0]
}

func brDependencyTitles(dependencies []Dependency) []string {
	titles := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.Title != "" {
			titles = append(titles, dependency.Title)
		}
	}
	return titles
}

func nonEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func firstExpectedID(t *testing.T, rows brRowsConformanceSummary) string {
	t.Helper()
	if len(rows.IDs) == 0 {
		t.Fatal("expected rows must include at least one id")
	}
	return rows.IDs[0]
}

func assertBRConformanceEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Fatalf("%s conformance mismatch\nwant:\n%s\ngot:\n%s", label, brConformanceJSON(want), brConformanceJSON(got))
}

func brConformanceJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("<marshal failed: %v>", err)
	}
	return string(data)
}
