package git

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
)

type gitConformancePack struct {
	Tool            string                           `json:"tool"`
	ToolVersion     string                           `json:"toolVersion"`
	FixturesVersion string                           `json:"fixturesVersion"`
	Source          string                           `json:"source"`
	RepoPath        string                           `json:"repoPath"`
	Commands        map[string]gitConformanceCommand `json:"commands"`
	Expected        gitConformanceExpected           `json:"expected"`
}

type gitConformanceCommand struct {
	Argv       []string `json:"argv"`
	Exit       int      `json:"exit"`
	StdoutText string   `json:"stdoutText"`
	StderrText string   `json:"stderrText,omitempty"`
}

type gitConformanceExpected struct {
	Status       gitStatusConformanceSummary `json:"status"`
	Log          gitLogConformanceSummary    `json:"log"`
	DiffUnstaged string                      `json:"diffUnstaged"`
	DiffStaged   string                      `json:"diffStaged"`
	Show         gitShowConformanceSummary   `json:"show"`
	Remotes      []gitRemoteConformance      `json:"remotes"`
	Push         gitPushConformanceSummary   `json:"push"`
	Probe        gitProbeConformanceSummary  `json:"probe"`
}

type gitStatusConformanceSummary struct {
	Branch   string                      `json:"branch"`
	Upstream string                      `json:"upstream"`
	AheadBy  int                         `json:"aheadBy"`
	BehindBy int                         `json:"behindBy"`
	Detached bool                        `json:"detached"`
	Clean    bool                        `json:"clean"`
	Entries  []gitStatusEntryConformance `json:"entries"`
}

type gitStatusEntryConformance struct {
	XY      string `json:"xy"`
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
}

type gitLogConformanceSummary struct {
	Limit   int                    `json:"limit"`
	Count   int                    `json:"count"`
	Commits []gitCommitConformance `json:"commits"`
}

type gitCommitConformance struct {
	SHA            string   `json:"sha"`
	ShortSHA       string   `json:"shortSha"`
	AuthorName     string   `json:"authorName"`
	AuthorEmail    string   `json:"authorEmail"`
	AuthoredAt     string   `json:"authoredAt"`
	CommitterName  string   `json:"committerName"`
	CommitterEmail string   `json:"committerEmail"`
	CommittedAt    string   `json:"committedAt"`
	Subject        string   `json:"subject"`
	Body           string   `json:"body"`
	ParentSHAs     []string `json:"parentShas"`
}

type gitShowConformanceSummary struct {
	SHA  string `json:"sha"`
	Text string `json:"text"`
}

type gitRemoteConformance struct {
	Name     string `json:"name"`
	FetchURL string `json:"fetchUrl"`
	PushURL  string `json:"pushUrl"`
}

type gitPushConformanceSummary struct {
	OK          bool                      `json:"ok"`
	Forced      bool                      `json:"forced"`
	RemoteRef   string                    `json:"remoteRef"`
	OldSHA      string                    `json:"oldSha"`
	NewSHA      string                    `json:"newSha"`
	Summary     string                    `json:"summary"`
	UpdatedRefs []gitPushedRefConformance `json:"updatedRefs"`
}

type gitPushedRefConformance struct {
	Status      string `json:"status"`
	Summary     string `json:"summary"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Reason      string `json:"reason,omitempty"`
}

type gitProbeConformanceSummary struct {
	Version      string            `json:"version"`
	Capabilities map[string]string `json:"capabilities"`
}

type gitConformanceRunner struct {
	repoPath  string
	responses map[string]gitConformanceCommand
	calls     []string
}

func TestPhase0GitConformanceCaptures(t *testing.T) {
	packs := loadGitConformancePacks(t)
	if len(packs) == 0 {
		t.Fatal("no git conformance capture packs found")
	}

	for _, pack := range packs {
		pack := pack
		t.Run(pack.ToolVersion, func(t *testing.T) {
			if pack.Tool != "git" {
				t.Fatalf("tool = %q, want git", pack.Tool)
			}
			if pack.RepoPath == "" {
				t.Fatal("conformance pack missing repoPath")
			}
			runner := newGitConformanceRunner(t, pack)
			client := NewWithExecutor(pack.RepoPath, runner)
			ctx := context.Background()

			status, err := client.Status(ctx)
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			assertGitConformanceEqual(t, "status", summarizeGitStatus(status), pack.Expected.Status)

			diffUnstaged, err := client.DiffUnstaged(ctx)
			if err != nil {
				t.Fatalf("DiffUnstaged: %v", err)
			}
			assertGitConformanceEqual(t, "diffUnstaged", string(diffUnstaged), pack.Expected.DiffUnstaged)

			diffStaged, err := client.DiffStaged(ctx)
			if err != nil {
				t.Fatalf("DiffStaged: %v", err)
			}
			assertGitConformanceEqual(t, "diffStaged", string(diffStaged), pack.Expected.DiffStaged)

			commits, err := client.Log(ctx, LogOpts{Limit: pack.Expected.Log.Limit})
			if err != nil {
				t.Fatalf("Log: %v", err)
			}
			assertGitConformanceEqual(t, "log", summarizeGitLog(commits, pack.Expected.Log.Limit), pack.Expected.Log)

			show, err := client.Show(ctx, pack.Expected.Show.SHA)
			if err != nil {
				t.Fatalf("Show: %v", err)
			}
			assertGitConformanceEqual(t, "show", gitShowConformanceSummary{
				SHA:  pack.Expected.Show.SHA,
				Text: string(show),
			}, pack.Expected.Show)

			remotes, err := client.Remotes(ctx)
			if err != nil {
				t.Fatalf("Remotes: %v", err)
			}
			assertGitConformanceEqual(t, "remotes", summarizeGitRemotes(remotes), pack.Expected.Remotes)

			push, err := client.Push(ctx, PushOpts{Remote: "origin", Branch: "main"})
			if err != nil {
				t.Fatalf("Push: %v", err)
			}
			assertGitConformanceEqual(t, "push", summarizeGitPush(push), pack.Expected.Push)

			probe := Probe(ctx, client, func() time.Time {
				return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
			})
			assertGitConformanceEqual(t, "probe", summarizeGitProbe(probe), pack.Expected.Probe)
		})
	}
}

func loadGitConformancePacks(t *testing.T) []gitConformancePack {
	t.Helper()
	capturesDir := filepath.Join(findGitConformanceRepoRoot(t), "packages", "fixtures", "phase0-git", "captures")
	entries, err := os.ReadDir(capturesDir)
	if err != nil {
		t.Fatalf("read git conformance captures: %v", err)
	}
	packs := make([]gitConformancePack, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(capturesDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var pack gitConformancePack
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

func newGitConformanceRunner(t *testing.T, pack gitConformancePack) *gitConformanceRunner {
	t.Helper()
	runner := &gitConformanceRunner{
		repoPath:  pack.RepoPath,
		responses: map[string]gitConformanceCommand{},
	}
	for name, command := range pack.Commands {
		if len(command.Argv) == 0 {
			t.Fatalf("%s command %q has empty argv", pack.ToolVersion, name)
		}
		key := gitConformanceKey(t, command.Argv)
		if _, exists := runner.responses[key]; exists {
			t.Fatalf("%s command %q duplicates argv %q", pack.ToolVersion, name, key)
		}
		runner.responses[key] = command
	}
	return runner
}

func (r *gitConformanceRunner) Run(_ context.Context, repoPath string, args []string) ([]byte, []byte, int, error) {
	if repoPath != r.repoPath {
		return nil, []byte("unexpected repo path " + repoPath), 1, nil
	}
	key := strings.Join(args, " ")
	r.calls = append(r.calls, key)
	command, ok := r.responses[key]
	if !ok {
		return nil, []byte("missing conformance capture for " + key), 127, nil
	}
	return []byte(command.StdoutText), []byte(command.StderrText), command.Exit, nil
}

func gitConformanceKey(t *testing.T, argv []string) string {
	t.Helper()
	if argv[0] != "git" {
		t.Fatalf("capture argv must start with git: %#v", argv)
	}
	for _, part := range argv {
		if part == "sh" || part == "-c" || part == "bash" || strings.Contains(part, "&&") || strings.Contains(part, ";") {
			t.Fatalf("capture argv contains shell token: %#v", argv)
		}
		if part == "-C" {
			t.Fatalf("capture argv must not include -C; repoPath is captured separately: %#v", argv)
		}
	}
	return strings.Join(argv[1:], " ")
}

func summarizeGitStatus(status *Status) gitStatusConformanceSummary {
	entries := make([]gitStatusEntryConformance, 0, len(status.Entries))
	for _, entry := range status.Entries {
		entries = append(entries, gitStatusEntryConformance{
			XY:      entry.XY,
			Path:    entry.Path,
			OldPath: entry.OldPath,
		})
	}
	return gitStatusConformanceSummary{
		Branch:   status.Branch,
		Upstream: status.Upstream,
		AheadBy:  status.AheadBy,
		BehindBy: status.BehindBy,
		Detached: status.Detached,
		Clean:    status.Clean,
		Entries:  entries,
	}
}

func summarizeGitLog(commits []Commit, limit int) gitLogConformanceSummary {
	out := gitLogConformanceSummary{
		Limit: limit,
		Count: len(commits),
	}
	for _, commit := range commits {
		out.Commits = append(out.Commits, gitCommitConformance{
			SHA:            commit.SHA,
			ShortSHA:       commit.ShortSHA,
			AuthorName:     commit.AuthorName,
			AuthorEmail:    commit.AuthorEmail,
			AuthoredAt:     commit.AuthoredAt.UTC().Format(time.RFC3339),
			CommitterName:  commit.CommitterName,
			CommitterEmail: commit.CommitterEmail,
			CommittedAt:    commit.CommittedAt.UTC().Format(time.RFC3339),
			Subject:        commit.Subject,
			Body:           commit.Body,
			ParentSHAs:     nonNilStrings(commit.ParentSHAs),
		})
	}
	return out
}

func summarizeGitRemotes(remotes []Remote) []gitRemoteConformance {
	out := make([]gitRemoteConformance, 0, len(remotes))
	for _, remote := range remotes {
		out = append(out, gitRemoteConformance{
			Name:     remote.Name,
			FetchURL: remote.FetchURL,
			PushURL:  remote.PushURL,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func summarizeGitPush(push *PushResult) gitPushConformanceSummary {
	out := gitPushConformanceSummary{
		OK:        push.OK,
		Forced:    push.Forced,
		RemoteRef: push.RemoteRef,
		OldSHA:    push.OldSHA,
		NewSHA:    push.NewSHA,
		Summary:   push.Summary,
	}
	for _, update := range push.UpdatedRefs {
		out.UpdatedRefs = append(out.UpdatedRefs, gitPushedRefConformance{
			Status:      update.Status,
			Summary:     update.Summary,
			Source:      update.Source,
			Destination: update.Destination,
			Reason:      update.Reason,
		})
	}
	return out
}

func summarizeGitProbe(probe ProbeResult) gitProbeConformanceSummary {
	caps := map[string]string{}
	for _, capID := range AllCapabilityIDs() {
		caps[capID] = string(probe.Reports[capID].Status)
	}
	return gitProbeConformanceSummary{
		Version:      probe.Version,
		Capabilities: caps,
	}
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func assertGitConformanceEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Fatalf("%s conformance mismatch\nwant:\n%s\ngot:\n%s", label, gitConformanceJSON(want), gitConformanceJSON(got))
}

func gitConformanceJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("<marshal failed: %v>", err)
	}
	return string(data)
}

func findGitConformanceRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "apps", "daemon", "go.mod")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %s", dir)
		}
		dir = parent
	}
}
