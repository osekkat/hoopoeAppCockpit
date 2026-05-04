package bv

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
)

const bvConformanceToolName = "bv"

type bvConformanceManifest struct {
	FixturesVersion string `json:"fixturesVersion"`
	Tool            struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"tool"`
	Captures map[string]bvConformanceCapture `json:"captures"`
}

type bvConformanceCapture struct {
	Argv []string `json:"argv"`
	Exit int      `json:"exit"`
}

type bvConformancePack struct {
	Tool            string
	ToolVersion     string
	FixturesVersion string
	Commands        map[string]bvConformanceCommand
}

type bvConformanceCommand struct {
	Argv   []string
	Exit   int
	Stdout []byte
	Stderr []byte
}

type bvConformanceRunner struct {
	responses map[string]bvConformanceCommand
	calls     []string
}

type bvConformanceExpected struct {
	Triage   bvTriageConformanceSummary   `json:"triage"`
	Plan     bvPlanConformanceSummary     `json:"plan"`
	Insights bvInsightsConformanceSummary `json:"insights"`
	Next     bvNextConformanceSummary     `json:"next"`
	Probe    bvProbeConformanceSummary    `json:"probe"`
}

type bvTriageConformanceSummary struct {
	DataHash        string                        `json:"dataHash"`
	MetaVersion     string                        `json:"metaVersion"`
	Phase2Ready     bool                          `json:"phase2Ready"`
	IssueCount      int                           `json:"issueCount"`
	OpenCount       int                           `json:"openCount"`
	ActionableCount int                           `json:"actionableCount"`
	BlockedCount    int                           `json:"blockedCount"`
	InProgressCount int                           `json:"inProgressCount"`
	TopPicks        []bvTopPickConformanceSummary `json:"topPicks"`
}

type bvTopPickConformanceSummary struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Score       float64 `json:"score"`
	ReasonCount int     `json:"reasonCount"`
	Unblocks    int     `json:"unblocks"`
}

type bvPlanConformanceSummary struct {
	DataHash        string                         `json:"dataHash"`
	TotalActionable int                            `json:"totalActionable"`
	TotalBlocked    int                            `json:"totalBlocked"`
	TrackIDs        []string                       `json:"trackIds"`
	Items           []bvPlanItemConformanceSummary `json:"items"`
	HighestImpact   string                         `json:"highestImpact"`
	ImpactReason    string                         `json:"impactReason"`
	UnblocksCount   int                            `json:"unblocksCount"`
}

type bvPlanItemConformanceSummary struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Priority int      `json:"priority"`
	Unblocks []string `json:"unblocks,omitempty"`
}

type bvInsightsConformanceSummary struct {
	DataHash           string                         `json:"dataHash"`
	NodeCount          int                            `json:"nodeCount"`
	EdgeCount          int                            `json:"edgeCount"`
	BottleneckCount    int                            `json:"bottleneckCount"`
	FirstBottleneck    bvBottleneckConformanceSummary `json:"firstBottleneck"`
	TopologicalFirstID string                         `json:"topologicalFirstId"`
}

type bvBottleneckConformanceSummary struct {
	ID    string  `json:"id"`
	Value float64 `json:"value"`
}

type bvNextConformanceSummary struct {
	DataHash     string  `json:"dataHash"`
	Version      string  `json:"version"`
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Score        float64 `json:"score"`
	ReasonCount  int     `json:"reasonCount"`
	Unblocks     int     `json:"unblocks"`
	ClaimCommand string  `json:"claimCommand"`
	ShowCommand  string  `json:"showCommand"`
}

type bvProbeConformanceSummary struct {
	Version      string            `json:"version"`
	Capabilities map[string]string `json:"capabilities"`
}

func TestPhase0BVConformanceCaptures(t *testing.T) {
	t.Parallel()

	pack := loadBVConformancePack(t)
	if pack.Tool != bvConformanceToolName {
		t.Fatalf("tool = %q, want %q", pack.Tool, bvConformanceToolName)
	}
	if pack.ToolVersion != "v0.16.0" || pack.FixturesVersion != "phase0-bv" {
		t.Fatalf("unexpected bv fixture version: tool=%q fixtures=%q", pack.ToolVersion, pack.FixturesVersion)
	}

	runner := newBVConformanceRunner(t, pack)
	adapter := NewWithExecutor(runner)

	triage, err := adapter.Triage(context.Background())
	if err != nil {
		t.Fatalf("Triage: %v", err)
	}
	plan, err := adapter.Plan(context.Background(), "")
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	insights, err := adapter.Insights(context.Background())
	if err != nil {
		t.Fatalf("Insights: %v", err)
	}
	next, err := adapter.Next(context.Background())
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	probe := Probe(context.Background(), adapter, nil)

	got := bvConformanceExpected{
		Triage:   summarizeBVTriage(triage),
		Plan:     summarizeBVPlan(plan),
		Insights: summarizeBVInsights(t, insights),
		Next:     summarizeBVNext(next),
		Probe:    summarizeBVProbe(probe),
	}
	assertBVConformanceEqual(t, got, expectedBVConformance())
	assertBVConformanceCalls(t, runner.calls)
}

func loadBVConformancePack(t *testing.T) bvConformancePack {
	t.Helper()

	root := findBVRepoRoot(t)
	packDir := filepath.Join(root, "packages", "fixtures", "phase0-bv")
	data, err := os.ReadFile(filepath.Join(packDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read bv manifest: %v", err)
	}
	var manifest bvConformanceManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse bv manifest: %v", err)
	}
	if manifest.Tool.Name == "" || manifest.Tool.Version == "" || manifest.FixturesVersion == "" {
		t.Fatalf("bv manifest missing tool/version metadata")
	}

	commands := map[string]bvConformanceCommand{}
	for name, capture := range manifest.Captures {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		assertBVConformanceArgv(t, name, capture.Argv)
		path := filepath.Join(packDir, "captures", name)
		stdout, stderr := readBVConformanceStdout(t, path, capture.Exit)
		commands[name] = bvConformanceCommand{
			Argv:   append([]string(nil), capture.Argv...),
			Exit:   capture.Exit,
			Stdout: stdout,
			Stderr: stderr,
		}
	}

	return bvConformancePack{
		Tool:            manifest.Tool.Name,
		ToolVersion:     manifest.Tool.Version,
		FixturesVersion: manifest.FixturesVersion,
		Commands:        commands,
	}
}

func readBVConformanceStdout(t *testing.T, path string, exit int) ([]byte, []byte) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if exit == 0 {
		if !json.Valid(data) {
			t.Fatalf("%s is not valid JSON", path)
		}
		return data, nil
	}

	var envelope struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("parse unsupported capture %s: %v", path, err)
	}
	return []byte(envelope.Stdout), []byte(envelope.Stderr)
}

func newBVConformanceRunner(t *testing.T, pack bvConformancePack) *bvConformanceRunner {
	t.Helper()

	runner := &bvConformanceRunner{responses: map[string]bvConformanceCommand{}}
	runner.responses["--version"] = bvConformanceCommand{
		Argv:   []string{bvConformanceToolName, "--version"},
		Exit:   0,
		Stdout: []byte("bv " + pack.ToolVersion + "\n"),
	}
	for name, command := range pack.Commands {
		if len(command.Argv) < 2 {
			t.Fatalf("%s command %q has empty bv args", pack.ToolVersion, name)
		}
		key := strings.Join(command.Argv[1:], " ")
		if _, exists := runner.responses[key]; exists {
			t.Fatalf("%s command %q duplicates argv %q", pack.ToolVersion, name, key)
		}
		runner.responses[key] = command
	}
	return runner
}

func (r *bvConformanceRunner) Run(_ context.Context, args []string) ([]byte, []byte, int, error) {
	key := strings.Join(args, " ")
	r.calls = append(r.calls, key)
	if command, ok := r.responses[key]; ok {
		return command.Stdout, command.Stderr, command.Exit, nil
	}
	return nil, []byte("missing bv conformance capture for " + key), 127, nil
}

func assertBVConformanceArgv(t *testing.T, name string, argv []string) {
	t.Helper()
	if len(argv) == 0 || argv[0] != bvConformanceToolName {
		t.Fatalf("%s capture argv must start with bv: %#v", name, argv)
	}
	for _, part := range argv {
		if part == "sh" || part == "-c" || part == "bash" || strings.Contains(part, "&&") || strings.Contains(part, ";") {
			t.Fatalf("%s capture argv contains shell token: %#v", name, argv)
		}
	}
}

func summarizeBVTriage(out *TriageOutput) bvTriageConformanceSummary {
	topPicks := make([]bvTopPickConformanceSummary, 0, len(out.Triage.QuickRef.TopPicks))
	for _, pick := range out.Triage.QuickRef.TopPicks {
		topPicks = append(topPicks, bvTopPickConformanceSummary{
			ID:          pick.ID,
			Title:       pick.Title,
			Score:       pick.Score,
			ReasonCount: len(pick.Reasons),
			Unblocks:    pick.Unblocks,
		})
	}
	return bvTriageConformanceSummary{
		DataHash:        out.DataHash,
		MetaVersion:     out.Triage.Meta.Version,
		Phase2Ready:     out.Triage.Meta.Phase2Ready,
		IssueCount:      out.Triage.Meta.IssueCount,
		OpenCount:       out.Triage.QuickRef.OpenCount,
		ActionableCount: out.Triage.QuickRef.ActionableCount,
		BlockedCount:    out.Triage.QuickRef.BlockedCount,
		InProgressCount: out.Triage.QuickRef.InProgressCount,
		TopPicks:        topPicks,
	}
}

func summarizeBVPlan(out *PlanOutput) bvPlanConformanceSummary {
	trackIDs := make([]string, 0, len(out.Plan.Tracks))
	items := []bvPlanItemConformanceSummary{}
	for _, track := range out.Plan.Tracks {
		trackIDs = append(trackIDs, track.TrackID)
		for _, item := range track.Items {
			items = append(items, bvPlanItemConformanceSummary{
				ID:       item.ID,
				Title:    item.Title,
				Status:   item.Status,
				Priority: item.Priority,
				Unblocks: nonEmptyBVStrings(item.Unblocks),
			})
		}
	}
	return bvPlanConformanceSummary{
		DataHash:        out.DataHash,
		TotalActionable: out.Plan.TotalActionable,
		TotalBlocked:    out.Plan.TotalBlocked,
		TrackIDs:        trackIDs,
		Items:           items,
		HighestImpact:   out.Plan.Summary.HighestImpact,
		ImpactReason:    out.Plan.Summary.ImpactReason,
		UnblocksCount:   out.Plan.Summary.UnblocksCount,
	}
}

func summarizeBVInsights(t *testing.T, out *InsightsOutput) bvInsightsConformanceSummary {
	t.Helper()

	topologicalOrder := []string{}
	if len(out.Stats.TopologicalOrder) > 0 {
		if err := json.Unmarshal(out.Stats.TopologicalOrder, &topologicalOrder); err != nil {
			t.Fatalf("decode insights Stats.TopologicalOrder: %v", err)
		}
	}
	first := bvBottleneckConformanceSummary{}
	if len(out.Bottlenecks) > 0 {
		first = bvBottleneckConformanceSummary{
			ID:    out.Bottlenecks[0].ID,
			Value: out.Bottlenecks[0].Value,
		}
	}
	return bvInsightsConformanceSummary{
		DataHash:           insightsDataHash(t, out.Raw),
		NodeCount:          out.Stats.NodeCount,
		EdgeCount:          out.Stats.EdgeCount,
		BottleneckCount:    len(out.Bottlenecks),
		FirstBottleneck:    first,
		TopologicalFirstID: firstBVString(topologicalOrder),
	}
}

func summarizeBVNext(out *NextOutput) bvNextConformanceSummary {
	return bvNextConformanceSummary{
		DataHash:     out.DataHash,
		Version:      out.Version,
		ID:           out.ID,
		Title:        out.Title,
		Score:        out.Score,
		ReasonCount:  len(out.Reasons),
		Unblocks:     out.Unblocks,
		ClaimCommand: out.ClaimCommand,
		ShowCommand:  out.ShowCommand,
	}
}

func summarizeBVProbe(probe ProbeResult) bvProbeConformanceSummary {
	caps := map[string]string{}
	for _, capID := range AllCapabilityIDs() {
		caps[capID] = string(probe.Reports[capID].Status)
	}
	return bvProbeConformanceSummary{
		Version:      probe.Version,
		Capabilities: caps,
	}
}

func insightsDataHash(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var payload struct {
		DataHash string `json:"data_hash"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode insights data_hash: %v", err)
	}
	return payload.DataHash
}

func expectedBVConformance() bvConformanceExpected {
	return bvConformanceExpected{
		Triage: bvTriageConformanceSummary{
			DataHash:        "028088e2dfc5abcf",
			MetaVersion:     "1.0.0",
			Phase2Ready:     true,
			IssueCount:      167,
			OpenCount:       4,
			ActionableCount: 2,
			BlockedCount:    2,
			InProgressCount: 0,
			TopPicks: []bvTopPickConformanceSummary{
				{
					ID:          "nexusaudio-9ga",
					Title:       "R3: Neural Encoder Path",
					Score:       0.12621051184973409,
					ReasonCount: 2,
					Unblocks:    0,
				},
			},
		},
		Plan: bvPlanConformanceSummary{
			DataHash:        "028088e2dfc5abcf",
			TotalActionable: 2,
			TotalBlocked:    2,
			TrackIDs:        []string{"track-A", "track-B"},
			Items: []bvPlanItemConformanceSummary{
				{
					ID:       "nexusaudio-9ga",
					Title:    "R3: Neural Encoder Path",
					Status:   "open",
					Priority: 4,
				},
				{
					ID:       "nexusaudio-9ga.1",
					Title:    "Joint neural encoder training/adaptation",
					Status:   "open",
					Priority: 4,
					Unblocks: []string{"nexusaudio-9ga.2", "nexusaudio-9ga.3"},
				},
			},
			HighestImpact: "nexusaudio-9ga.1",
			ImpactReason:  "Unblocks multiple tasks",
			UnblocksCount: 2,
		},
		Insights: bvInsightsConformanceSummary{
			DataHash:        "028088e2dfc5abcf",
			NodeCount:       167,
			EdgeCount:       216,
			BottleneckCount: 50,
			FirstBottleneck: bvBottleneckConformanceSummary{
				ID:    "nexusaudio-670u",
				Value: 112.49999999999999,
			},
			TopologicalFirstID: "nexusaudio-3dl.6",
		},
		Next: bvNextConformanceSummary{
			DataHash:     "028088e2dfc5abcf",
			Version:      "v0.16.0",
			ID:           "nexusaudio-9ga",
			Title:        "R3: Neural Encoder Path",
			Score:        0.12621051665343383,
			ReasonCount:  2,
			Unblocks:     0,
			ClaimCommand: "br update nexusaudio-9ga --status=in_progress",
			ShowCommand:  "br show nexusaudio-9ga",
		},
		Probe: bvProbeConformanceSummary{
			Version: "v0.16.0",
			Capabilities: map[string]string{
				CapTriage:   string(StatusOK),
				CapPlan:     string(StatusOK),
				CapInsights: string(StatusOK),
				CapDiff:     string(StatusDegraded),
				CapNext:     string(StatusOK),
			},
		},
	}
}

func assertBVConformanceCalls(t *testing.T, calls []string) {
	t.Helper()
	got := append([]string(nil), calls...)
	sort.Strings(got)
	want := []string{
		"--robot-diff --diff-since HEAD",
		"--robot-insights",
		"--robot-insights",
		"--robot-next",
		"--robot-next",
		"--robot-plan",
		"--robot-plan",
		"--robot-triage",
		"--robot-triage",
		"--version",
	}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bv conformance calls mismatch\nwant:\n%s\ngot:\n%s", bvConformanceJSON(want), bvConformanceJSON(got))
	}
}

func assertBVConformanceEqual(t *testing.T, got, want bvConformanceExpected) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Fatalf("bv conformance mismatch\nwant:\n%s\ngot:\n%s", bvConformanceJSON(want), bvConformanceJSON(got))
}

func nonEmptyBVStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
}

func firstBVString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func findBVRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "packages", "fixtures", "phase0-bv")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("could not find repo root containing packages/fixtures/phase0-bv")
	return ""
}

func bvConformanceJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("<marshal failed: %v>", err)
	}
	return string(data)
}
