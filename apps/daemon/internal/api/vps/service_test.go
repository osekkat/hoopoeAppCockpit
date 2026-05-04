package vps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	gitadapter "github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/adapters/git"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestWorkingTreeStatusMapsPorcelainEntries(t *testing.T) {
	t.Parallel()
	service, fake := newTestService(t)
	got, err := service.WorkingTreeStatus(context.Background(), "proj_1")
	if err != nil {
		t.Fatalf("WorkingTreeStatus: %v", err)
	}
	if got.ProjectID != "proj_1" || got.HeadSHA != fake.head || got.Branch != "main" {
		t.Fatalf("status metadata = %+v", got)
	}
	if got.DirtyCounts.Files != 4 || got.DirtyCounts.Staged != 2 || got.DirtyCounts.Unstaged != 2 || got.DirtyCounts.Untracked != 1 || got.DirtyCounts.Renamed != 1 {
		t.Fatalf("dirty counts = %+v", got.DirtyCounts)
	}
	if !got.Files[2].Untracked || got.Files[3].OldPath != "old.go" || !got.Files[3].Renamed {
		t.Fatalf("files not mapped correctly: %+v", got.Files)
	}
}

func TestDiffPaginationAndCache(t *testing.T) {
	t.Parallel()
	service, fake := newTestService(t)
	first, err := service.Diff(context.Background(), "proj_1", DiffKindStaged, DiffPage{StartLine: 2, Limit: 2})
	if err != nil {
		t.Fatalf("Diff first: %v", err)
	}
	if first.Cached {
		t.Fatalf("first diff should not be cached")
	}
	if first.TotalLines != 4 || first.Diff != "line2\nline3\n" || !first.HasMore {
		t.Fatalf("first diff pagination = %+v", first)
	}
	second, err := service.Diff(context.Background(), "proj_1", DiffKindStaged, DiffPage{StartLine: 1, Limit: 10})
	if err != nil {
		t.Fatalf("Diff second: %v", err)
	}
	if !second.Cached {
		t.Fatalf("second diff should be served from cache")
	}
	if fake.stagedCalls != 1 {
		t.Fatalf("staged diff calls = %d, want 1", fake.stagedCalls)
	}
}

func TestUnpushedCommitsUsesBranchFromStatus(t *testing.T) {
	t.Parallel()
	service, fake := newTestService(t)
	got, err := service.UnpushedCommits(context.Background(), "proj_1")
	if err != nil {
		t.Fatalf("UnpushedCommits: %v", err)
	}
	if got.FromRef != "origin/main" || got.ToRef != "main" {
		t.Fatalf("refs = %s..%s", got.FromRef, got.ToRef)
	}
	if len(got.Commits) != 2 || got.Commits[0].SHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("commits = %+v", got.Commits)
	}
	if fake.unpushedBranch != "main" {
		t.Fatalf("branch passed to git = %q", fake.unpushedBranch)
	}
}

func TestHTTPMountServesOpenFiles(t *testing.T) {
	t.Parallel()
	service, _ := newTestService(t)
	router := chi.NewRouter()
	router.Route("/v1/projects/{projectId}", func(r chi.Router) {
		MountGitRoutes(r, Config{
			Projects:         service.projects,
			GitClientFactory: service.newGit,
			Cache:            service.cache,
			Now:              service.now,
		})
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/proj_1/git/open-files", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got OpenFilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Files) != 4 {
		t.Fatalf("open files = %+v", got.Files)
	}
}

func TestMissingVPSClonePathReturnsProblem(t *testing.T) {
	t.Parallel()
	resolver := fakeProjects{project: schemas.Project{Id: "proj_1", Repo: schemas.ProjectRepoRef{Branch: "main", Origin: "origin"}}}
	service := NewService(Config{Projects: resolver})
	_, err := service.WorkingTreeStatus(context.Background(), "proj_1")
	if err == nil {
		t.Fatal("expected missing VPS clone path error")
	}
	status, code, _ := mapProjectError(err)
	if status != http.StatusUnprocessableEntity || code != "project.vps_clone_missing" {
		t.Fatalf("mapped error = %d %s", status, code)
	}
}

func newTestService(t *testing.T) (*Service, *fakeGit) {
	t.Helper()
	repoPath := t.TempDir()
	for _, path := range []string{"modified.go", "staged.go", "new.txt", "new.go"} {
		full := filepath.Join(repoPath, path)
		if err := os.WriteFile(full, []byte(path), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repoPath, ".git", "index"), []byte("index"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}
	fake := &fakeGit{
		head: "0123456789abcdef0123456789abcdef01234567",
		status: &gitadapter.Status{
			Branch:   "main",
			Upstream: "origin/main",
			AheadBy:  2,
			BehindBy: 1,
			Entries: []gitadapter.StatusEntry{
				{XY: " M", Path: "modified.go"},
				{XY: "M ", Path: "staged.go"},
				{XY: "??", Path: "new.txt"},
				{XY: "R ", OldPath: "old.go", Path: "new.go"},
			},
		},
		stagedDiff:   []byte("line1\nline2\nline3\nline4\n"),
		unstagedDiff: []byte("diff --git a/modified.go b/modified.go\n"),
	}
	project := schemas.Project{
		Id:   "proj_1",
		Slug: "proj-1",
		Name: "Project 1",
		Repo: schemas.ProjectRepoRef{
			Branch:       "main",
			Origin:       "https://example.invalid/repo.git",
			VpsClonePath: &repoPath,
		},
		LifecycleState: schemas.ProjectLifecycleStateImported,
		SchemaVersion:  1,
		VpsId:          "vps_local",
	}
	service := NewService(Config{
		Projects: fakeProjects{project: project},
		GitClientFactory: func(string) GitClient {
			return fake
		},
		Cache: NewDiffCache(time.Minute),
		Now: func() time.Time {
			return time.Date(2026, 5, 4, 1, 0, 0, 0, time.UTC)
		},
	})
	return service, fake
}

type fakeProjects struct {
	project schemas.Project
}

func (p fakeProjects) Project(_ context.Context, id string) (schemas.Project, error) {
	if id != p.project.Id {
		return schemas.Project{}, os.ErrNotExist
	}
	return p.project, nil
}

type fakeGit struct {
	status         *gitadapter.Status
	head           string
	stagedDiff     []byte
	unstagedDiff   []byte
	stagedCalls    int
	unstagedCalls  int
	unpushedBranch string
}

func (g *fakeGit) Status(context.Context) (*gitadapter.Status, error) {
	return g.status, nil
}

func (g *fakeGit) DiffStaged(context.Context) ([]byte, error) {
	g.stagedCalls++
	return g.stagedDiff, nil
}

func (g *fakeGit) DiffUnstaged(context.Context) ([]byte, error) {
	g.unstagedCalls++
	return g.unstagedDiff, nil
}

func (g *fakeGit) UnpushedCommits(_ context.Context, branch string) (*gitadapter.CommitDelta, error) {
	g.unpushedBranch = branch
	return &gitadapter.CommitDelta{
		From: "origin/" + branch,
		To:   branch,
		Commits: []gitadapter.Commit{{
			SHA:      "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ShortSHA: "aaaaaaa",
		}, {
			SHA:      "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ShortSHA: "bbbbbbb",
		}},
	}, nil
}

func (g *fakeGit) RevParse(context.Context, string) (string, error) {
	return g.head, nil
}
