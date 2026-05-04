package projects

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
	_ "modernc.org/sqlite"
)

type fakeRepo struct {
	isGit  bool
	origin string
	branch string
}

type fakeRunner struct {
	repos    map[string]fakeRepo
	ruStdout string
	commands [][]string
}

func (r *fakeRunner) Run(_ context.Context, dir string, argv []string) (CommandResult, error) {
	r.commands = append(r.commands, append([]string(nil), argv...))
	if len(argv) == 0 {
		return CommandResult{ExitCode: 2}, nil
	}
	switch {
	case reflect.DeepEqual(argv, []string{"git", "rev-parse", "--is-inside-work-tree"}):
		repo := r.repos[cleanPath(dir)]
		if !repo.isGit {
			return CommandResult{ExitCode: 1, Stdout: []byte("false\n")}, nil
		}
		return CommandResult{Stdout: []byte("true\n")}, nil
	case reflect.DeepEqual(argv, []string{"git", "remote", "get-url", "origin"}):
		repo := r.repos[cleanPath(dir)]
		if repo.origin == "" {
			return CommandResult{ExitCode: 2}, nil
		}
		return CommandResult{Stdout: []byte(repo.origin + "\n")}, nil
	case reflect.DeepEqual(argv, []string{"git", "branch", "--show-current"}):
		repo := r.repos[cleanPath(dir)]
		return CommandResult{Stdout: []byte(repo.branch + "\n")}, nil
	case reflect.DeepEqual(argv, []string{"br", "init"}):
		if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
			return CommandResult{}, err
		}
		return CommandResult{}, nil
	case reflect.DeepEqual(argv, RUListPathsArgv()):
		return CommandResult{Stdout: []byte(r.ruStdout)}, nil
	default:
		return CommandResult{ExitCode: 127, Stderr: []byte("unexpected command")}, nil
	}
}

func TestServiceImportInitializesHoopoeBeadsAndRegistry(t *testing.T) {
	root := testProjectRoot(t, true)
	runner := &fakeRunner{repos: map[string]fakeRepo{
		cleanPath(root): {isGit: true, origin: "https://example.invalid/hoopoe.git", branch: "main"},
	}}
	service := testService(t, runner)

	project, err := service.Import(context.Background(), testImportRequest(root, "proj_test", "Hoopoe Test", "idem-one"))
	if err != nil {
		t.Fatalf("import project: %v", err)
	}
	if project.Id != "proj_test" || project.Name != "Hoopoe Test" {
		t.Fatalf("unexpected project: %+v", project)
	}
	if project.Repo.Origin != "https://example.invalid/hoopoe.git" || project.Repo.Branch != "main" {
		t.Fatalf("unexpected repo ref: %+v", project.Repo)
	}
	if project.AgentsManifestPresent == nil || !*project.AgentsManifestPresent {
		t.Fatalf("agents manifest flag = %v, want true", project.AgentsManifestPresent)
	}
	if project.HoopoeInitialized == nil || !*project.HoopoeInitialized {
		t.Fatalf("hoopoe initialized flag = %v, want true", project.HoopoeInitialized)
	}
	if project.ToolDetectionDone == nil || !*project.ToolDetectionDone {
		t.Fatalf("tool detection flag = %v, want true", project.ToolDetectionDone)
	}
	for _, path := range []string{
		filepath.Join(root, ".hoopoe", "project.json"),
		filepath.Join(root, ".hoopoe", "plans"),
		filepath.Join(root, ".hoopoe", "skills.lock.json"),
		filepath.Join(root, ".hoopoe", "model-context-policy.json"),
		filepath.Join(root, ".beads"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected initialized path %s: %v", path, err)
		}
	}
	projectJSON, err := ReadProjectJSON(root)
	if err != nil {
		t.Fatalf("read project.json: %v", err)
	}
	if projectJSON.Project.ID != project.Id || projectJSON.Project.Slug != "hoopoe-test" {
		t.Fatalf("project.json = %+v, want imported project metadata", projectJSON.Project)
	}
	listed, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(listed) != 1 || listed[0].Id != project.Id {
		t.Fatalf("listed projects = %+v, want imported project", listed)
	}
	readiness, err := service.Readiness(context.Background(), project.Id)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if len(readiness.Gates) != 1 || !readiness.Gates[0].Satisfied {
		t.Fatalf("readiness = %+v, want satisfied imported gate", readiness)
	}
	if readiness.Gates[0].BlockingCount == nil || *readiness.Gates[0].BlockingCount != 0 {
		t.Fatalf("blocking count = %v, want 0", readiness.Gates[0].BlockingCount)
	}

	replayed, err := service.Import(context.Background(), testImportRequest(root, "proj_test", "Hoopoe Test", "idem-one"))
	if err != nil {
		t.Fatalf("replay idempotent import: %v", err)
	}
	if replayed.Id != project.Id {
		t.Fatalf("replayed project id = %s, want %s", replayed.Id, project.Id)
	}

	_, err = service.Import(context.Background(), testImportRequest(root, "proj_test", "Different Name", "idem-one"))
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("reused idempotency key err = %v, want ErrIdempotencyConflict", err)
	}
}

func TestServiceReadinessReportsMissingAgentAndManifest(t *testing.T) {
	root := testProjectRoot(t, false)
	runner := &fakeRunner{repos: map[string]fakeRepo{
		cleanPath(root): {isGit: true, origin: "https://example.invalid/docs.git", branch: "main"},
	}}
	service := testService(t, runner)

	project, err := service.Import(context.Background(), testImportRequest(root, "proj_docs", "Docs Only", "idem-docs"))
	if err != nil {
		t.Fatalf("import docs project: %v", err)
	}
	readiness, err := service.Readiness(context.Background(), project.Id)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if len(readiness.Gates) != 1 || readiness.Gates[0].Satisfied {
		t.Fatalf("readiness = %+v, want unsatisfied imported gate", readiness)
	}
	missing := map[string]bool{}
	for _, check := range readiness.Gates[0].Checks {
		if !check.Ok {
			missing[check.Id] = true
		}
	}
	for _, id := range []string{"agents.md", "tools.detected"} {
		if !missing[id] {
			t.Fatalf("missing checks = %+v, want %s", missing, id)
		}
	}
}

func TestServiceImportRejectsMissingOrigin(t *testing.T) {
	root := testProjectRoot(t, true)
	runner := &fakeRunner{repos: map[string]fakeRepo{
		cleanPath(root): {isGit: true, branch: "main"},
	}}
	service := testService(t, runner)

	_, err := service.Import(context.Background(), testImportRequest(root, "proj_missing_origin", "", "idem-missing-origin"))
	if !errors.Is(err, ErrMissingOrigin) {
		t.Fatalf("import err = %v, want ErrMissingOrigin", err)
	}
}

func TestInitializeHoopoeDirPreservesExistingProjectJSON(t *testing.T) {
	root := testProjectRoot(t, true)
	runner := &fakeRunner{repos: map[string]fakeRepo{
		cleanPath(root): {isGit: true, origin: "https://example.invalid/hoopoe.git", branch: "main"},
	}}

	first, err := InitializeHoopoeDir(root, InitializeOptions{
		ID:     "proj_first",
		Name:   "First Project",
		Runner: runner,
	})
	if err != nil {
		t.Fatalf("first init: %v", err)
	}
	second, err := InitializeHoopoeDir(root, InitializeOptions{
		ID:     "proj_second",
		Name:   "Second Project",
		Runner: runner,
	})
	if err != nil {
		t.Fatalf("second init: %v", err)
	}
	if first.Metadata.ID != "proj_first" || second.Metadata.ID != "proj_first" {
		t.Fatalf("metadata ids = first:%s second:%s, want preserved first id", first.Metadata.ID, second.Metadata.ID)
	}
}

func TestListRUPathsParsesAbsolutePaths(t *testing.T) {
	runner := &fakeRunner{ruStdout: "/data/projects/one\n/tmp/two\n\n"}
	paths, err := ListRUPaths(context.Background(), runner)
	if err != nil {
		t.Fatalf("list ru paths: %v", err)
	}
	if !reflect.DeepEqual(paths, []string{"/data/projects/one", "/tmp/two"}) {
		t.Fatalf("paths = %+v", paths)
	}

	runner.ruStdout = "relative/path\n"
	_, err = ListRUPaths(context.Background(), runner)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("relative path err = %v, want ErrInvalidRequest", err)
	}
}

func testService(t *testing.T, runner *fakeRunner) *Service {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "projects.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	store, err := NewSQLStore(context.Background(), db)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	service, err := NewService(ServiceConfig{
		Store:  store,
		Runner: runner,
		Now: func() time.Time {
			return time.Date(2026, 5, 4, 0, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return service
}

func testProjectRoot(t *testing.T, withAgentAndManifest bool) string {
	t.Helper()
	root := t.TempDir()
	if withAgentAndManifest {
		writeTestFile(t, filepath.Join(root, "AGENTS.md"), "agent instructions\n")
		writeTestFile(t, filepath.Join(root, "README.md"), "project readme\n")
		writeTestFile(t, filepath.Join(root, "go.mod"), "module example.invalid/project\n\ngo 1.26\n")
	}
	return root
}

func writeTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func testImportRequest(root string, id string, name string, idempotencyKey string) ImportRequest {
	return ImportRequest{
		ProjectCreateRequest: schemas.ProjectCreateRequest{
			Id:       testStringPtr(id),
			Name:     testStringPtr(name),
			RootPath: root,
		},
		IdempotencyKey: idempotencyKey,
	}
}

func testStringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func cleanPath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}
