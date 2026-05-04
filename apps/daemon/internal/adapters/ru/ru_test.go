package ru

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

func TestArgvBuildersCoverAdoptedSurfacesAndAvoidShell(t *testing.T) {
	syncDryRun, err := SyncArgv(SyncOptions{DryRun: true})
	if err != nil {
		t.Fatalf("sync dry-run argv: %v", err)
	}
	syncRun, err := SyncArgv(SyncOptions{Parallel: 4, PullOnly: true})
	if err != nil {
		t.Fatalf("sync argv: %v", err)
	}
	tests := []struct {
		name string
		got  []string
		want []string
	}{
		{name: "schema", got: SchemaArgv(), want: []string{"ru", "--schema"}},
		{name: "version", got: VersionArgv(), want: []string{"ru", "--version"}},
		{name: "robot docs", got: RobotDocsArgv(), want: []string{"ru", "robot-docs"}},
		{name: "status", got: StatusArgv(), want: []string{"ru", "status", "--no-fetch", "--json"}},
		{name: "list", got: ListArgv(), want: []string{"ru", "list", "--json"}},
		{name: "list paths", got: ListPathsArgv(), want: []string{"ru", "list", "--paths"}},
		{name: "sync dry-run", got: syncDryRun, want: []string{"ru", "sync", "--dry-run", "--json", "--non-interactive"}},
		{name: "sync run", got: syncRun, want: []string{"ru", "sync", "--json", "--non-interactive", "--parallel", "4", "--pull-only"}},
		{name: "prune dry-run", got: PruneDryRunArgv(), want: []string{"ru", "prune", "--dry-run"}},
		{name: "prune archive", got: PruneArchiveArgv(), want: []string{"ru", "prune", "--archive"}},
	}
	for _, tt := range tests {
		if !reflect.DeepEqual(tt.got, tt.want) {
			t.Fatalf("%s argv = %#v, want %#v", tt.name, tt.got, tt.want)
		}
		assertNoShellTokens(t, tt.got)
		if err := validateAdoptedArgv(tt.got); err != nil {
			t.Fatalf("%s validate: %v", tt.name, err)
		}
	}
}

func TestArgvGuardsRejectPromptingOrUnadoptedCommands(t *testing.T) {
	if _, err := SyncArgv(SyncOptions{CloneOnly: true, PullOnly: true}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("sync mutually exclusive err = %v, want ErrInvalidRequest", err)
	}
	for _, argv := range [][]string{
		{"ru", "sync", "--json"},
		{"ru", "status", "--json"},
		{"ru", "prune", "--delete"},
		{"ru", "review"},
		{"sh", "-c", "ru status"},
	} {
		if err := validateAdoptedArgv(argv); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("validateAdoptedArgv(%v) err = %v, want ErrInvalidRequest", argv, err)
		}
	}
}

func TestInitParsesSchemaFixtureAndPinsCommands(t *testing.T) {
	fixture := loadRUFixture(t, "normal.json")
	runner := &fakeRunner{responses: map[string]CommandResult{
		"ru --schema": {Stdout: fixture.StdoutJSON},
	}}
	adapter := New(runner)
	if err := adapter.Init(context.Background()); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if adapter.Schema == nil || adapter.Schema.SchemaVersion != "1.0.0" {
		t.Fatalf("schema = %+v", adapter.Schema)
	}
	for _, command := range []string{"status", "list", "sync"} {
		if !adapter.Schema.HasCommand(command) {
			t.Fatalf("schema missing command %s", command)
		}
	}
}

func TestAdapterMethodsParseRuSurfaces(t *testing.T) {
	runner := &fakeRunner{responses: map[string]CommandResult{
		"ru status --no-fetch --json": {Stdout: []byte(envelope("status", `{"total":2,"repos":[{"repo":"osekkat/hoopoe","path":"/data/projects/hoopoe","status":"current","branch":"main","ahead":0,"behind":0,"dirty":false,"mismatch":false},{"repo":"osekkat/other","path":"/data/projects/other","status":"behind","branch":"main","ahead":0,"behind":2,"dirty":true,"mismatch":false}]}`))},
		"ru list --json":              {Stdout: []byte(envelope("list", `{"total":1,"repos":[{"repo":"osekkat/hoopoe","url":"git@example.com:osekkat/hoopoe.git","branch":"main","path":"/data/projects/hoopoe"}]}`))},
		"ru list --paths":             {Stdout: []byte("/data/projects/hoopoe\n/data/projects/other\n")},
		"ru sync --dry-run --json --non-interactive": {Stdout: []byte(
			`{"repo":"osekkat/hoopoe","status":"up-to-date","duration_ms":7}` + "\n" +
				`{"repo":"osekkat/other","status":"failed","detail":"remote rejected"}` + "\n",
		)},
		"ru prune --dry-run": {Stdout: []byte("would archive /data/projects/orphan-one\nno-op /data/projects/orphan-two\n")},
		"ru prune --archive": {Stdout: []byte("archived /data/projects/orphan-one\n")},
		"ru robot-docs":      {Stdout: []byte("# ru robot docs\n")},
	}}
	adapter := New(runner)

	status, err := adapter.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Total != 2 || status.Repos[1].Behind != 2 || !status.Repos[1].Dirty {
		t.Fatalf("status = %+v", status)
	}

	list, err := adapter.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Total != 1 || list.Repos[0].Path != "/data/projects/hoopoe" {
		t.Fatalf("list = %+v", list)
	}

	paths, err := adapter.ListPaths(context.Background())
	if err != nil {
		t.Fatalf("ListPaths: %v", err)
	}
	if !reflect.DeepEqual(paths, []string{"/data/projects/hoopoe", "/data/projects/other"}) {
		t.Fatalf("paths = %#v", paths)
	}

	sync, err := adapter.SyncDryRun(context.Background())
	if err != nil {
		t.Fatalf("SyncDryRun: %v", err)
	}
	if sync.Summary.Total != 2 || sync.Summary.Failed != 1 || sync.Summary.Skipped != 1 {
		t.Fatalf("sync summary = %+v", sync.Summary)
	}

	prune, err := adapter.PruneDryRun(context.Background())
	if err != nil {
		t.Fatalf("PruneDryRun: %v", err)
	}
	if len(prune.Candidates) != 2 || prune.Candidates[0].Path != "/data/projects/orphan-one" {
		t.Fatalf("prune = %+v", prune)
	}
	if _, err := adapter.PruneArchive(context.Background()); err != nil {
		t.Fatalf("PruneArchive: %v", err)
	}
	if docs, err := adapter.RobotDocs(context.Background()); err != nil || !strings.Contains(docs, "robot docs") {
		t.Fatalf("RobotDocs docs=%q err=%v", docs, err)
	}
}

func TestParseListPathsRejectsRelativePaths(t *testing.T) {
	_, err := ParseListPaths([]byte("/data/projects/ok\nrelative/repo\n"))
	if !errors.Is(err, ErrCommandContract) {
		t.Fatalf("ParseListPaths err = %v, want ErrCommandContract", err)
	}
}

func TestParseSyncAcceptsEnvelopeAndNDJSON(t *testing.T) {
	enveloped, err := ParseSync([]byte(envelope("sync", `{"config":{"dry_run":true},"summary":{"total":1,"cloned":1},"repos":[{"repo":"one","status":"cloned"}]}`)))
	if err != nil {
		t.Fatalf("ParseSync envelope: %v", err)
	}
	if !enveloped.Config.DryRun || enveloped.Summary.Cloned != 1 || enveloped.Repos[0].Repo != "one" {
		t.Fatalf("enveloped sync = %+v", enveloped)
	}

	ndjson, err := ParseSync([]byte(`{"repo":"one","status":"pulled"}` + "\n" + `{"repo":"two","status":"dirty"}` + "\n"))
	if err != nil {
		t.Fatalf("ParseSync NDJSON: %v", err)
	}
	if ndjson.Summary.Total != 2 || ndjson.Summary.Pulled != 1 || ndjson.Summary.Skipped != 1 {
		t.Fatalf("ndjson summary = %+v", ndjson.Summary)
	}

	empty, err := ParseSync([]byte(`{"summary":{"total":0},"repos":[]}`))
	if err != nil {
		t.Fatalf("ParseSync empty direct document: %v", err)
	}
	if empty.Summary.Total != 0 || len(empty.Repos) != 0 {
		t.Fatalf("empty sync = %+v", empty)
	}
}

func TestExitCodeMeaningDocumentsObservedVocabulary(t *testing.T) {
	tests := map[int]string{
		0:   "ok",
		124: "timeout",
		127: "missing-tool",
		2:   "ru-error",
	}
	for exit, want := range tests {
		if got := ExitCodeMeaning(exit); got != want {
			t.Fatalf("ExitCodeMeaning(%d) = %q, want %q", exit, got, want)
		}
	}
}

func TestProbeReportsCapabilitiesAndPolicyBlocks(t *testing.T) {
	fixture := loadRUFixture(t, "normal.json")
	runner := &fakeRunner{responses: map[string]CommandResult{
		"ru --version":                {Stdout: []byte("ru version 1.3.1\n")},
		"ru --schema":                 {Stdout: fixture.StdoutJSON},
		"ru status --no-fetch --json": {Stdout: []byte(envelope("status", `{"total":0,"repos":[]}`))},
		"ru list --paths":             {Stdout: []byte("/data/projects/hoopoe\n")},
		"ru sync --dry-run --json --non-interactive": {Stdout: []byte(`{"repo":"hoopoe","status":"up-to-date"}` + "\n")},
		"ru prune --dry-run":                         {Stdout: []byte("no orphans\n")},
		"ru robot-docs":                              {Stdout: []byte("# docs\n")},
	}}
	adapter := New(runner)
	adapter.Now = fixedNow
	report, err := adapter.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if report.Tool != capabilities.ToolRU || report.Version != "1.3.1" {
		t.Fatalf("report identity = %+v", report)
	}
	for _, id := range []string{CapabilityPresent, CapabilitySchema, CapabilityStatusRead, CapabilityListPaths, CapabilitySyncDryRun, CapabilityPruneDryRun, CapabilityRobotDocs} {
		if report.Capabilities[id].Status != capabilities.StatusOK {
			t.Fatalf("%s = %+v, want ok", id, report.Capabilities[id])
		}
	}
	if report.Capabilities[CapabilityPruneArchive].Status != capabilities.StatusBlockedByPolicy {
		t.Fatalf("prune archive = %+v", report.Capabilities[CapabilityPruneArchive])
	}
	if report.Capabilities[CapabilityReview].Status != capabilities.StatusBlockedByPolicy {
		t.Fatalf("review = %+v", report.Capabilities[CapabilityReview])
	}
}

func TestProbeReportsMissingMalformedTimeoutHighVolumeAndUnsupportedVersion(t *testing.T) {
	missing := New(&fakeRunner{err: ErrMissingBinary})
	missing.Now = fixedNow
	report, err := missing.Probe(context.Background())
	if err != nil {
		t.Fatalf("missing probe: %v", err)
	}
	if report.Capabilities[CapabilityPresent].Status != capabilities.StatusMissing {
		t.Fatalf("missing present = %+v", report.Capabilities[CapabilityPresent])
	}

	malformed := New(&fakeRunner{responses: map[string]CommandResult{
		"ru --version":                {Stdout: []byte("ru version 1.3.1\n")},
		"ru --schema":                 {Stdout: []byte("{not json")},
		"ru status --no-fetch --json": {Stdout: []byte(envelope("status", `{"total":0,"repos":[]}`))},
		"ru list --paths":             {Stdout: []byte("/data/projects/hoopoe\n")},
		"ru sync --dry-run --json --non-interactive": {Stdout: []byte(`{"repo":"hoopoe","status":"up-to-date"}` + "\n")},
		"ru prune --dry-run":                         {Stdout: []byte("none\n")},
		"ru robot-docs":                              {Stdout: []byte("# docs\n")},
	}})
	report, err = malformed.Probe(context.Background())
	if err != nil {
		t.Fatalf("malformed probe: %v", err)
	}
	if report.Capabilities[CapabilitySchema].Status != capabilities.StatusDegraded {
		t.Fatalf("malformed schema = %+v", report.Capabilities[CapabilitySchema])
	}

	timeout := New(&fakeRunner{responses: map[string]CommandResult{
		"ru --version":                {Stdout: []byte("ru version 1.3.1\n")},
		"ru --schema":                 {Stdout: loadRUFixture(t, "normal.json").StdoutJSON},
		"ru status --no-fetch --json": {Stdout: []byte(envelope("status", `{"total":0,"repos":[]}`))},
		"ru list --paths":             {Stdout: []byte("/data/projects/hoopoe\n")},
		"ru sync --dry-run --json --non-interactive": {ExitCode: 124, Stderr: []byte("timeout")},
		"ru prune --dry-run":                         {Stdout: []byte("none\n")},
		"ru robot-docs":                              {Stdout: []byte("# docs\n")},
	}})
	report, err = timeout.Probe(context.Background())
	if err != nil {
		t.Fatalf("timeout probe: %v", err)
	}
	if report.Capabilities[CapabilitySyncDryRun].Status != capabilities.StatusDegraded {
		t.Fatalf("timeout sync = %+v", report.Capabilities[CapabilitySyncDryRun])
	}

	highVolume := New(&fakeRunner{responses: map[string]CommandResult{
		"ru --version":                {Stdout: []byte("ru version 1.3.1\n")},
		"ru --schema":                 {Stdout: []byte(envelope("robot-docs", `{"schema_version":"1.0.0","content":{"commands":{}}}`) + strings.Repeat(" ", 128))},
		"ru status --no-fetch --json": {Stdout: []byte(envelope("status", `{"total":0,"repos":[]}`))},
		"ru list --paths":             {Stdout: []byte("/data/projects/hoopoe\n")},
		"ru sync --dry-run --json --non-interactive": {Stdout: []byte(`{"repo":"hoopoe","status":"up-to-date"}` + "\n")},
		"ru prune --dry-run":                         {Stdout: []byte("none\n")},
		"ru robot-docs":                              {Stdout: []byte("# docs\n")},
	}})
	highVolume.MaxStdoutBytes = 64
	report, err = highVolume.Probe(context.Background())
	if err != nil {
		t.Fatalf("high-volume probe: %v", err)
	}
	if report.Capabilities[CapabilitySchema].Status != capabilities.StatusDegraded {
		t.Fatalf("high-volume schema = %+v", report.Capabilities[CapabilitySchema])
	}

	unsupported := New(&fakeRunner{responses: map[string]CommandResult{
		"ru --version":                {Stdout: []byte("ru version 1.1.9\n")},
		"ru --schema":                 {Stdout: loadRUFixture(t, "normal.json").StdoutJSON},
		"ru status --no-fetch --json": {Stdout: []byte(envelope("status", `{"total":0,"repos":[]}`))},
		"ru list --paths":             {Stdout: []byte("/data/projects/hoopoe\n")},
		"ru sync --dry-run --json --non-interactive": {Stdout: []byte(`{"repo":"hoopoe","status":"up-to-date"}` + "\n")},
		"ru prune --dry-run":                         {Stdout: []byte("none\n")},
		"ru robot-docs":                              {Stdout: []byte("# docs\n")},
	}})
	report, err = unsupported.Probe(context.Background())
	if err != nil {
		t.Fatalf("unsupported probe: %v", err)
	}
	if report.Capabilities[CapabilityPresent].Status != capabilities.StatusDegraded {
		t.Fatalf("unsupported present = %+v", report.Capabilities[CapabilityPresent])
	}
}

type ruFixture struct {
	Argv       []string        `json:"argv"`
	Exit       int             `json:"exit"`
	StdoutJSON json.RawMessage `json:"stdoutJson"`
}

func loadRUFixture(t *testing.T, name string) ruFixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(findRepoRoot(t), "packages", "fixtures", "golden-outputs", "ru", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture ruFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if fixture.Exit != 0 {
		t.Fatalf("fixture %s exit = %d", name, fixture.Exit)
	}
	return fixture
}

func envelope(command, data string) string {
	return `{"generated_at":"2026-05-04T00:00:00Z","version":"1.3.1","output_format":"json","command":"` + command + `","data":` + data + `}`
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
	key := strings.Join(argv, " ")
	result, ok := r.responses[key]
	if !ok {
		return CommandResult{ExitCode: 127, Stderr: []byte("unexpected argv: " + key)}, nil
	}
	return result, nil
}

func fixedNow() time.Time {
	return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
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
