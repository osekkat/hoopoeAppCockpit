package br

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/capabilities"
)

func TestArgvBuildersCoverRequiredCommandsAndAvoidShell(t *testing.T) {
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "list",
			got:  ListArgv(ListFilter{Statuses: []string{"open"}, Limit: 250}),
			want: []string{"br", "list", "--json", "--status", "open", "--limit", "250"},
		},
		{
			name: "ready",
			got:  ReadyArgv(ReadyFilter{Limit: 5, Unassigned: true}),
			want: []string{"br", "ready", "--json", "--limit", "5", "--unassigned"},
		},
		{
			name: "blocked",
			got:  BlockedArgv(BlockedFilter{Limit: 3, Detailed: true}),
			want: []string{"br", "blocked", "--json", "--limit", "3", "--detailed"},
		},
		{
			name: "dep cycles",
			got:  DepCyclesArgv(CommonOptions{}),
			want: []string{"br", "dep", "cycles", "--json"},
		},
		{
			name: "sync flush only",
			got:  SyncFlushOnlyArgv(CommonOptions{Actor: "FuchsiaBear"}),
			want: []string{"br", "sync", "--flush-only", "--json", "--actor", "FuchsiaBear"},
		},
		{
			name: "doctor",
			got:  DoctorArgv(CommonOptions{}),
			want: []string{"br", "doctor", "--json"},
		},
		{
			name: "schema",
			got:  SchemaArgv("issue", CommonOptions{}),
			want: []string{"br", "schema", "issue", "--json"},
		},
	}

	show, err := ShowArgv("hp-1")
	if err != nil {
		t.Fatalf("show argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"show", show, []string{"br", "show", "hp-1", "--json"}})

	search, err := SearchArgv("audit", ListFilter{Limit: 10})
	if err != nil {
		t.Fatalf("search argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"search", search, []string{"br", "search", "audit", "--json", "--limit", "10"}})

	depTree, err := DepTreeArgv(DepTreeRequest{IssueID: "hp-1", Direction: "up", MaxDepth: 2})
	if err != nil {
		t.Fatalf("dep tree argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"dep tree", depTree, []string{"br", "dep", "tree", "hp-1", "--json", "--direction", "up", "--max-depth", "2"}})

	depList, err := DepListArgv(DepListRequest{IssueID: "hp-1", Direction: "both", DepType: "blocks"})
	if err != nil {
		t.Fatalf("dep list argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"dep list", depList, []string{"br", "dep", "list", "hp-1", "--json", "--direction", "both", "--type", "blocks"}})

	create, err := CreateArgv(CreateRequest{Title: "new bead", IssueType: "task", Priority: "P0", Labels: []string{"phase6", "adapter"}})
	if err != nil {
		t.Fatalf("create argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"create", create, []string{"br", "create", "--title", "new bead", "--json", "--type", "task", "--priority", "P0", "--labels", "phase6,adapter"}})

	update, err := UpdateArgv(UpdateRequest{IDs: []string{"hp-1"}, Status: "in_progress", Claim: true})
	if err != nil {
		t.Fatalf("update argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"update", update, []string{"br", "update", "hp-1", "--json", "--status", "in_progress", "--claim"}})

	closeArgv, err := CloseArgv(CloseRequest{IDs: []string{"hp-1"}, Reason: "done"})
	if err != nil {
		t.Fatalf("close argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"close", closeArgv, []string{"br", "close", "hp-1", "--json", "--reason", "done"}})

	reopen, err := ReopenArgv(ReopenRequest{IDs: []string{"hp-1"}, Reason: "needs follow-up"})
	if err != nil {
		t.Fatalf("reopen argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"reopen", reopen, []string{"br", "reopen", "hp-1", "--json", "--reason", "needs follow-up"}})

	depAdd, err := DepAddArgv(DepRequest{IssueID: "hp-1", DependsOn: "hp-2", DepType: "blocks"})
	if err != nil {
		t.Fatalf("dep add argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"dep add", depAdd, []string{"br", "dep", "add", "hp-1", "hp-2", "--json", "--type", "blocks"}})

	depRemove, err := DepRemoveArgv(DepRequest{IssueID: "hp-1", DependsOn: "hp-2"})
	if err != nil {
		t.Fatalf("dep remove argv: %v", err)
	}
	tests = append(tests, struct {
		name string
		got  []string
		want []string
	}{"dep remove", depRemove, []string{"br", "dep", "remove", "hp-1", "hp-2", "--json"}})

	for _, tt := range tests {
		if !reflect.DeepEqual(tt.got, tt.want) {
			t.Fatalf("%s argv = %#v, want %#v", tt.name, tt.got, tt.want)
		}
		assertNoShellTokens(t, tt.got)
	}
}

func TestArgvBuildersRejectAmbiguousMutations(t *testing.T) {
	if _, err := CreateArgv(CreateRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CreateArgv err = %v, want ErrInvalidRequest", err)
	}
	if _, err := UpdateArgv(UpdateRequest{IDs: []string{"hp-1"}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("UpdateArgv err = %v, want ErrInvalidRequest", err)
	}
	if _, err := CloseArgv(CloseRequest{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("CloseArgv err = %v, want ErrInvalidRequest", err)
	}
	if _, err := DepAddArgv(DepRequest{IssueID: "hp-1", DependsOn: "hp-1"}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("DepAddArgv err = %v, want ErrInvalidRequest", err)
	}
}

func TestParseListResponseUsesPhase0FixtureAndRequiresPaginationContract(t *testing.T) {
	var fixture struct {
		StdoutJSON   json.RawMessage                    `json:"stdoutJson"`
		Capabilities map[string]capabilities.Capability `json:"capabilities"`
	}
	mustReadFixture(t, "packages/fixtures/golden-outputs/br/normal.json", &fixture)

	response, err := ParseListResponse(fixture.StdoutJSON)
	if err != nil {
		t.Fatalf("ParseListResponse: %v", err)
	}
	if response.Total != 204 || response.Limit != 250 || response.Offset != 0 || response.HasMore {
		t.Fatalf("pagination = total:%d limit:%d offset:%d hasMore:%v", response.Total, response.Limit, response.Offset, response.HasMore)
	}
	if len(response.Issues) == 0 || response.Issues[0].ID == "" || response.Issues[0].DependencyCount == 0 {
		t.Fatalf("fixture issues did not decode expected fields: first=%+v", response.Issues[0])
	}
	if fixture.Capabilities[CapabilityIssuesRead].Status != capabilities.StatusOK {
		t.Fatalf("fixture br.issues.read = %+v", fixture.Capabilities[CapabilityIssuesRead])
	}
	if fixture.Capabilities[CapabilitySyncFlushOnly].Status != capabilities.StatusOK {
		t.Fatalf("fixture br.sync.flush_only = %+v", fixture.Capabilities[CapabilitySyncFlushOnly])
	}

	_, err = ParseListResponse([]byte(`{"issues":[]}`))
	if !errors.Is(err, ErrListContract) {
		t.Fatalf("missing pagination err = %v, want ErrListContract", err)
	}
	_, err = ParseListResponse([]byte(`{"issues":[],"total":-1,"limit":0,"offset":0,"has_more":false}`))
	if !errors.Is(err, ErrListContract) {
		t.Fatalf("negative pagination err = %v, want ErrListContract", err)
	}
}

func TestAdapterReadMethodsParseJSONSubcommands(t *testing.T) {
	runner := &fakeRunner{responses: map[string]CommandResult{
		"br list --json --limit 2":                 {Stdout: []byte(`{"issues":[{"id":"hp-1","title":"one","status":"open","priority":0}],"total":1,"limit":2,"offset":0,"has_more":false}`)},
		"br ready --json --limit 1":                {Stdout: []byte(`[{"id":"hp-2","title":"ready","status":"open","priority":1}]`)},
		"br blocked --json --limit 1":              {Stdout: []byte(`[{"id":"hp-3","title":"blocked","status":"open","priority":2}]`)},
		"br search audit --json --limit 1":         {Stdout: []byte(`{"issues":[{"id":"hp-4","title":"audit","status":"open","priority":0}],"total":1,"limit":1,"offset":0,"has_more":false}`)},
		"br show hp-1 --json":                      {Stdout: []byte(`[{"id":"hp-1","title":"one","status":"open","priority":0}]`)},
		"br dep cycles --json":                     {Stdout: []byte(`{"cycles":[],"count":0}`)},
		"br dep list hp-1 --json --direction both": {Stdout: []byte(`[{"issue_id":"hp-1","depends_on_id":"hp-2","type":"blocks"}]`)},
		"br dep tree hp-1 --json --max-depth 3":    {Stdout: []byte(`[{"id":"hp-1","children":[]}]`)},
	}}
	adapter := New(runner)

	list, err := adapter.List(context.Background(), ListFilter{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if list.Total != 1 || list.Issues[0].ID != "hp-1" {
		t.Fatalf("list = %+v", list)
	}

	ready, err := adapter.Ready(context.Background(), ReadyFilter{Limit: 1})
	if err != nil {
		t.Fatalf("ready: %v", err)
	}
	if ready[0].ID != "hp-2" {
		t.Fatalf("ready = %+v", ready)
	}

	blocked, err := adapter.Blocked(context.Background(), BlockedFilter{Limit: 1})
	if err != nil {
		t.Fatalf("blocked: %v", err)
	}
	if blocked[0].ID != "hp-3" {
		t.Fatalf("blocked = %+v", blocked)
	}

	search, err := adapter.Search(context.Background(), "audit", ListFilter{Limit: 1})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if search.Issues[0].ID != "hp-4" {
		t.Fatalf("search = %+v", search)
	}

	show, err := adapter.Show(context.Background(), "hp-1")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if show[0].Title != "one" {
		t.Fatalf("show = %+v", show)
	}

	cycles, err := adapter.DepCycles(context.Background(), CommonOptions{})
	if err != nil {
		t.Fatalf("dep cycles: %v", err)
	}
	if err := EnsureNoCycles(cycles); err != nil {
		t.Fatalf("EnsureNoCycles: %v", err)
	}

	if _, err := adapter.DepList(context.Background(), DepListRequest{IssueID: "hp-1", Direction: "both"}); err != nil {
		t.Fatalf("dep list: %v", err)
	}
	if _, err := adapter.DepTree(context.Background(), DepTreeRequest{IssueID: "hp-1", MaxDepth: 3}); err != nil {
		t.Fatalf("dep tree: %v", err)
	}
}

func TestAdapterMutationMethodsWrapJSONSubcommands(t *testing.T) {
	runner := &fakeRunner{responses: map[string]CommandResult{
		"br create --title new --json --type task":      {Stdout: []byte(`{"id":"hp-new"}`)},
		"br update hp-new --json --status in_progress":  {Stdout: []byte(`{"id":"hp-new","status":"in_progress"}`)},
		"br close hp-new --json --reason done":          {Stdout: []byte(`{"id":"hp-new","status":"closed"}`)},
		"br reopen hp-new --json --reason more":         {Stdout: []byte(`{"id":"hp-new","status":"open"}`)},
		"br dep add hp-new hp-old --json --type blocks": {Stdout: []byte(`{"ok":true}`)},
		"br dep remove hp-new hp-old --json":            {Stdout: []byte(`{"ok":true}`)},
		"br sync --flush-only --json":                   {Stdout: []byte(`{"ok":true}`)},
		"br doctor --json":                              {Stdout: []byte(`{"ok":true}`)},
		"br schema issue --json":                        {Stdout: []byte(`{"type":"object"}`)},
	}}
	adapter := New(runner)

	if _, err := adapter.Create(context.Background(), CreateRequest{Title: "new", IssueType: "task"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := adapter.Update(context.Background(), UpdateRequest{IDs: []string{"hp-new"}, Status: "in_progress"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := adapter.Close(context.Background(), CloseRequest{IDs: []string{"hp-new"}, Reason: "done"}); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := adapter.Reopen(context.Background(), ReopenRequest{IDs: []string{"hp-new"}, Reason: "more"}); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if _, err := adapter.DepAdd(context.Background(), DepRequest{IssueID: "hp-new", DependsOn: "hp-old", DepType: "blocks"}); err != nil {
		t.Fatalf("dep add: %v", err)
	}
	if _, err := adapter.DepRemove(context.Background(), DepRequest{IssueID: "hp-new", DependsOn: "hp-old"}); err != nil {
		t.Fatalf("dep remove: %v", err)
	}
	if _, err := adapter.SyncFlushOnly(context.Background(), CommonOptions{}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if _, err := adapter.Doctor(context.Background(), CommonOptions{}); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if _, err := adapter.Schema(context.Background(), "issue", CommonOptions{}); err != nil {
		t.Fatalf("schema: %v", err)
	}
}

func TestReadModelLoadsIssuesJSONLAndQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "issues.jsonl")
	data := strings.Join([]string{
		`{"id":"hp-1","title":"one","status":"open","priority":0,"owner":"a@example.com","assignee":"FuchsiaBear"}`,
		``,
		`{"id":"hp-2","title":"two","status":"closed","priority":2,"owner":"b@example.com"}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	model, err := LoadReadModel(path)
	if err != nil {
		t.Fatalf("LoadReadModel: %v", err)
	}
	if len(model.Issues) != 2 || model.ByID["hp-1"].Title != "one" {
		t.Fatalf("model = %+v", model)
	}
	open := model.Query(ReadModelQuery{Status: "open", Assignee: "FuchsiaBear", UseMinMax: true, MinPrio: 0, MaxPrio: 1})
	if len(open) != 1 || open[0].ID != "hp-1" {
		t.Fatalf("open query = %+v", open)
	}
	owner := model.Query(ReadModelQuery{Owner: "b@example.com"})
	if len(owner) != 1 || owner[0].ID != "hp-2" {
		t.Fatalf("owner query = %+v", owner)
	}
}

func TestReadModelReportsMalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "issues.jsonl")
	if err := os.WriteFile(path, []byte("{\"id\":\"hp-1\"}\nnot-json\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, err := LoadReadModel(path)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("LoadReadModel err = %v, want line 2", err)
	}
}

func TestSyncAfterWritePlanKeepsGitSeparateAndAudited(t *testing.T) {
	plan, err := SyncAfterWritePlan("[hp-dz8] sync beads", CommonOptions{Actor: "FuchsiaBear"})
	if err != nil {
		t.Fatalf("SyncAfterWritePlan: %v", err)
	}
	if len(plan.Steps) != 3 {
		t.Fatalf("steps = %+v", plan.Steps)
	}
	if !reflect.DeepEqual(plan.Steps[0].Argv, []string{"br", "sync", "--flush-only", "--json", "--actor", "FuchsiaBear"}) {
		t.Fatalf("sync step = %+v", plan.Steps[0])
	}
	if !reflect.DeepEqual(plan.Steps[1].Argv, []string{"git", "add", ".beads/"}) {
		t.Fatalf("git add step = %+v", plan.Steps[1])
	}
	if !reflect.DeepEqual(plan.Steps[2].Argv, []string{"git", "commit", "-m", "[hp-dz8] sync beads"}) {
		t.Fatalf("git commit step = %+v", plan.Steps[2])
	}
	for _, step := range plan.Steps {
		if step.AuditAction == "" || !step.Mutating {
			t.Fatalf("step lacks audit/mutation metadata: %+v", step)
		}
	}
	if _, err := SyncAfterWritePlan("", CommonOptions{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("empty commit message err = %v, want ErrInvalidRequest", err)
	}
}

func TestDependencyCycleDetectionIsEnforced(t *testing.T) {
	report, err := ParseCycleReport([]byte(`{"cycles":[["hp-1","hp-2","hp-1"]],"count":1}`))
	if err != nil {
		t.Fatalf("ParseCycleReport: %v", err)
	}
	err = EnsureNoCycles(report)
	if !errors.Is(err, ErrDependencyCycle) {
		t.Fatalf("EnsureNoCycles err = %v, want ErrDependencyCycle", err)
	}
	var cycleErr DependencyCycleError
	if !errors.As(err, &cycleErr) || len(cycleErr.Cycles) != 1 {
		t.Fatalf("cycle error = %#v", err)
	}

	arrayReport, err := ParseCycleReport([]byte(`[["hp-3","hp-4","hp-3"]]`))
	if err != nil {
		t.Fatalf("ParseCycleReport array: %v", err)
	}
	if arrayReport.Count != 1 {
		t.Fatalf("array report = %+v", arrayReport)
	}
}

func TestProbeReportsCapabilitiesForRegistry(t *testing.T) {
	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"br version --short":       {Stdout: []byte("1.2.3\n")},
		"br list --json --limit 1": {Stdout: []byte(`{"issues":[],"total":0,"limit":1,"offset":0,"has_more":false}`)},
		"br dep cycles --json":     {Stdout: []byte(`{"cycles":[],"count":0}`)},
	}})
	adapter.Now = fixedNow

	registry := capabilities.New("test-api")
	if err := registry.RegisterProbe(capabilities.ToolBR, func() (*capabilities.ToolReport, error) {
		return adapter.Probe(context.Background())
	}); err != nil {
		t.Fatal(err)
	}
	registry.Probe()

	if got, ok := registry.LookupCapability(capabilities.ToolBR, CapabilityIssuesRead); !ok || got.Status != capabilities.StatusOK {
		t.Fatalf("br.issues.read = %+v, %v", got, ok)
	}
	if got, ok := registry.LookupCapability(capabilities.ToolBR, CapabilityIssuesUpdate); !ok || got.Status != capabilities.StatusOK {
		t.Fatalf("br.issues.update = %+v, %v", got, ok)
	}
	if got, ok := registry.LookupCapability(capabilities.ToolBR, CapabilityDepAdd); !ok || got.Status != capabilities.StatusOK {
		t.Fatalf("br.dep.add = %+v, %v", got, ok)
	}
	if got, ok := registry.LookupCapability(capabilities.ToolBR, CapabilitySyncFlushOnly); !ok || got.Status != capabilities.StatusOK {
		t.Fatalf("br.sync.flush_only = %+v, %v", got, ok)
	}
	if got, ok := registry.LookupCapability(capabilities.ToolBR, CapabilitySyncFlush); !ok || got.Status != capabilities.StatusOK {
		t.Fatalf("br.sync.flush = %+v, %v", got, ok)
	}
	if got, ok := registry.LookupCapability(capabilities.ToolBR, CapabilityTUI); !ok || got.Status != capabilities.StatusBlockedByPolicy {
		t.Fatalf("br.tui = %+v, %v", got, ok)
	}
}

// TestProbeOnMissingToolGoldenFixtureMarksAllCapabilitiesMissing loads
// packages/fixtures/golden-outputs/br/missing-tool.json and pins the
// adapter contract from plan.md §18.3: when the version probe exits with
// the captured 127 / "command not found" pair, statusForError takes the
// ExitCode==127 branch (br.go:1131) — distinct from the existing test that
// drives the runner-level "executable file not found" string-match path —
// and every declared capability lands at StatusMissing, with TUI
// downgraded to BlockedByPolicy as today.
func TestProbeOnMissingToolGoldenFixtureMarksAllCapabilitiesMissing(t *testing.T) {
	fixture := loadBRGoldenFixture(t, "missing-tool.json")
	if fixture.Meta.State != "missing-tool" {
		t.Fatalf("fixture state = %q, want missing-tool", fixture.Meta.State)
	}
	if cap, ok := fixture.Capabilities["br._present"]; !ok || cap.Status != "missing" {
		t.Fatalf("fixture must declare br._present=missing, got %+v", fixture.Capabilities)
	}
	if fixture.Exit != 127 {
		t.Fatalf("fixture exit = %d, want 127", fixture.Exit)
	}

	adapter := New(&fakeRunner{responses: map[string]CommandResult{
		"br version --short": {
			ExitCode: fixture.Exit,
			Stderr:   []byte(fixture.StderrText),
		},
	}})
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if report.Tool != capabilities.ToolBR {
		t.Fatalf("tool = %s", report.Tool)
	}
	if got := report.Capabilities[CapabilityPresent].Status; got != capabilities.StatusMissing {
		t.Fatalf("br._present = %s, want missing", got)
	}
	for _, capID := range []string{
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
	} {
		got := report.Capabilities[capID]
		if got.Status != capabilities.StatusMissing {
			t.Fatalf("%s = %+v, want missing", capID, got)
		}
		if !strings.Contains(got.Notes, "exited 127") {
			t.Fatalf("%s notes = %q; want exit-127 wrapping preserved", capID, got.Notes)
		}
	}
	tui := report.Capabilities[CapabilityTUI]
	if tui.Status != capabilities.StatusMissing {
		t.Fatalf("br.tui = %+v, want missing (TUI inherits the missing state when binary absent — see Probe condition at br.go:837)", tui)
	}
}

type brGoldenFixture struct {
	Meta struct {
		Adapter string `json:"adapter"`
		State   string `json:"state"`
	} `json:"meta"`
	Exit         int    `json:"exit"`
	StdoutText   string `json:"stdoutText"`
	StderrText   string `json:"stderrText"`
	Capabilities map[string]struct {
		Status string `json:"status"`
		Notes  string `json:"notes"`
	} `json:"capabilities"`
}

func loadBRGoldenFixture(t *testing.T, name string) brGoldenFixture {
	t.Helper()
	rel := filepath.Join("packages", "fixtures", "golden-outputs", "br", name)
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	var fixture brGoldenFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("parse fixture %s: %v", rel, err)
	}
	if fixture.Meta.Adapter != "br" {
		t.Fatalf("fixture %s adapter = %q, want br", rel, fixture.Meta.Adapter)
	}
	return fixture
}

func TestProbeReportsMissingMalformedTimeoutHighVolumeAndCycles(t *testing.T) {
	missing := New(&fakeRunner{err: errors.New("exec: \"br\": executable file not found")})
	missing.Now = fixedNow
	report, err := missing.Probe(context.Background())
	if err != nil {
		t.Fatalf("missing probe: %v", err)
	}
	if report.Capabilities[CapabilityPresent].Status != capabilities.StatusMissing {
		t.Fatalf("missing present = %+v", report.Capabilities[CapabilityPresent])
	}

	malformed := New(&fakeRunner{responses: map[string]CommandResult{
		"br version --short":       {Stdout: []byte("1.2.3\n")},
		"br list --json --limit 1": {Stdout: []byte(`{"issues":[]`)},
	}})
	malformed.Now = fixedNow
	report, err = malformed.Probe(context.Background())
	if err != nil {
		t.Fatalf("malformed probe: %v", err)
	}
	if report.Capabilities[CapabilityIssuesRead].Status != capabilities.StatusDegraded {
		t.Fatalf("malformed issues.read = %+v", report.Capabilities[CapabilityIssuesRead])
	}

	timeout := New(&fakeRunner{responses: map[string]CommandResult{
		"br version --short":       {Stdout: []byte("1.2.3\n")},
		"br list --json --limit 1": {ExitCode: 124, Stderr: []byte("timeout: sending signal TERM")},
	}})
	timeout.Now = fixedNow
	report, err = timeout.Probe(context.Background())
	if err != nil {
		t.Fatalf("timeout probe: %v", err)
	}
	if report.Capabilities[CapabilityIssuesRead].Status != capabilities.StatusDegraded {
		t.Fatalf("timeout issues.read = %+v", report.Capabilities[CapabilityIssuesRead])
	}

	highVolume := New(&fakeRunner{responses: map[string]CommandResult{
		"br version --short":       {Stdout: []byte("1.2.3\n")},
		"br list --json --limit 1": {Stdout: []byte(`{"issues":[],"total":0,"limit":1,"offset":0,"has_more":false}` + strings.Repeat(" ", 64))},
	}})
	highVolume.MaxStdoutBytes = 32
	highVolume.Now = fixedNow
	report, err = highVolume.Probe(context.Background())
	if err != nil {
		t.Fatalf("high-volume probe: %v", err)
	}
	if report.Capabilities[CapabilityIssuesRead].Status != capabilities.StatusDegraded {
		t.Fatalf("high-volume issues.read = %+v", report.Capabilities[CapabilityIssuesRead])
	}

	withCycles := New(&fakeRunner{responses: map[string]CommandResult{
		"br version --short":       {Stdout: []byte("1.2.3\n")},
		"br list --json --limit 1": {Stdout: []byte(`{"issues":[],"total":0,"limit":1,"offset":0,"has_more":false}`)},
		"br dep cycles --json":     {Stdout: []byte(`{"cycles":[["hp-1","hp-2","hp-1"]],"count":1}`)},
	}})
	withCycles.Now = fixedNow
	report, err = withCycles.Probe(context.Background())
	if err != nil {
		t.Fatalf("cycle probe: %v", err)
	}
	if report.Capabilities[CapabilityDepCycles].Status != capabilities.StatusDegraded {
		t.Fatalf("cycle dep.cycles = %+v", report.Capabilities[CapabilityDepCycles])
	}
}

func assertNoShellTokens(t *testing.T, argv []string) {
	t.Helper()
	for _, part := range argv {
		if part == "sh" || part == "-c" || part == "bash" {
			t.Fatalf("argv used shell token: %#v", argv)
		}
	}
}

type fakeRunner struct {
	responses map[string]CommandResult
	err       error
}

func (r *fakeRunner) Run(_ context.Context, argv []string) (CommandResult, error) {
	if r.err != nil {
		return CommandResult{}, r.err
	}
	key := join(argv)
	result, ok := r.responses[key]
	if !ok {
		return CommandResult{ExitCode: 127, Stderr: []byte("unexpected argv: " + key)}, nil
	}
	return result, nil
}

func join(parts []string) string {
	return strings.Join(parts, " ")
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
}

func mustReadFixture(t *testing.T, rel string, target any) {
	t.Helper()
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("parse fixture %s: %v", rel, err)
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatalf("could not find repo root from %s", dir)
		}
		dir = next
	}
}
