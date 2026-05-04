// Package projects owns the daemon-side project registry and import gate.
package projects

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

const (
	SchemaVersion = 1
	defaultVPSID  = "vps_local"
)

var (
	ErrInvalidRequest       = errors.New("projects: invalid request")
	ErrNotFound             = errors.New("projects: not found")
	ErrNotGitRepo           = errors.New("projects: not a git repository")
	ErrMissingOrigin        = errors.New("projects: missing origin remote")
	ErrDetachedHead         = errors.New("projects: detached head")
	ErrCommandFailed        = errors.New("projects: command failed")
	ErrIdempotencyConflict  = errors.New("projects: idempotency key reused with different request")
	ErrProjectJSONMalformed = errors.New("projects: project.json malformed")
)

var supportedLanguageManifests = []string{
	"package.json",
	"pyproject.toml",
	"requirements.txt",
	"Cargo.toml",
	"go.mod",
	"Gemfile",
	"pom.xml",
	"build.gradle",
	"build.gradle.kts",
	"Makefile",
}

// CommandRunner runs argv directly. Implementations must not invoke a shell.
type CommandRunner interface {
	Run(ctx context.Context, dir string, argv []string) (CommandResult, error)
}

type CommandResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, dir string, argv []string) (CommandResult, error) {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return CommandResult{}, fmt.Errorf("%w: empty argv", ErrInvalidRequest)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	result.ExitCode = -1
	return result, err
}

type Manifest struct {
	Name         string `json:"name"`
	RelativePath string `json:"relativePath"`
}

type ToolEnvironment struct {
	AgentsMDRelative *string    `json:"agentsMdRelative"`
	ReadmeRelative   *string    `json:"readmeRelative"`
	Manifests        []Manifest `json:"manifests"`
	HasBeadsDir      bool       `json:"hasBeadsDir"`
	HasHoopoeDir     bool       `json:"hasHoopoeDir"`
}

type GitInfo struct {
	IsGitRepo    bool
	OriginRemote string
	Branch       string
}

type Metadata struct {
	ID           string                        `json:"id"`
	Name         string                        `json:"name"`
	Slug         string                        `json:"slug"`
	RootPath     string                        `json:"rootPath"`
	OriginRemote string                        `json:"originRemote"`
	Branch       string                        `json:"branch"`
	State        schemas.ProjectLifecycleState `json:"state"`
	CreatedAt    string                        `json:"createdAt"`
	UpdatedAt    string                        `json:"updatedAt"`
	Tools        ToolEnvironment               `json:"tools"`
}

type ProjectJSON struct {
	SchemaVersion int      `json:"schemaVersion"`
	Project       Metadata `json:"project"`
}

type ImportRequest struct {
	schemas.ProjectCreateRequest
	IdempotencyKey string `json:"-"`
}

type StoreProject struct {
	ID                    string
	Slug                  string
	Name                  string
	VPSID                 string
	RootPath              string
	OriginRemote          string
	Branch                string
	LifecycleState        schemas.ProjectLifecycleState
	AgentsManifestPresent bool
	HoopoeInitialized     bool
	ToolDetectionDone     bool
	DesktopMirrorPath     string
	ImportedAt            time.Time
	LastActivityAt        time.Time
	Tools                 ToolEnvironment
	SchemaVersion         int
}

type SQLStore struct {
	db *sql.DB
}

func NewSQLStore(ctx context.Context, db *sql.DB) (*SQLStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: nil db", ErrInvalidRequest)
	}
	store := &SQLStore{db: db}
	if err := store.ensureSchema(ctx); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *SQLStore) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			slug TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			vps_id TEXT NOT NULL,
			root_path TEXT NOT NULL UNIQUE,
			origin_remote TEXT NOT NULL,
			branch TEXT NOT NULL,
			lifecycle_state TEXT NOT NULL,
			agents_manifest_present INTEGER NOT NULL,
			hoopoe_initialized INTEGER NOT NULL,
			tool_detection_done INTEGER NOT NULL,
			desktop_mirror_path TEXT NOT NULL DEFAULT '',
			imported_at TEXT NOT NULL,
			last_activity_at TEXT NOT NULL,
			tools_json TEXT NOT NULL,
			schema_version INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS project_idempotency (
			key TEXT PRIMARY KEY,
			request_hash TEXT NOT NULL,
			project_id TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("projects: ensure schema: %w", err)
		}
	}
	return nil
}

func (s *SQLStore) List(ctx context.Context) ([]StoreProject, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects ORDER BY imported_at DESC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("projects: list: %w", err)
	}
	defer rows.Close()
	var out []StoreProject
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("projects: list rows: %w", err)
	}
	return out, nil
}

func (s *SQLStore) Get(ctx context.Context, id string) (StoreProject, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects WHERE id = ?`, strings.TrimSpace(id))
	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StoreProject{}, ErrNotFound
	}
	return project, err
}

func (s *SQLStore) FindByRoot(ctx context.Context, root string) (StoreProject, bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects WHERE root_path = ?`, root)
	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StoreProject{}, false, nil
	}
	if err != nil {
		return StoreProject{}, false, err
	}
	return project, true, nil
}

func (s *SQLStore) Create(ctx context.Context, project StoreProject, idempotencyKey, requestHash string) (StoreProject, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: begin create: %w", err)
	}
	defer tx.Rollback()

	if idempotencyKey != "" {
		existing, err := lookupIdempotency(ctx, tx, idempotencyKey)
		if err != nil {
			return StoreProject{}, err
		}
		if existing != nil {
			if existing.requestHash != requestHash {
				return StoreProject{}, ErrIdempotencyConflict
			}
			out, err := getTx(ctx, tx, existing.projectID)
			if err != nil {
				return StoreProject{}, err
			}
			return out, tx.Commit()
		}
	}

	if existing, ok, err := findByRootTx(ctx, tx, project.RootPath); err != nil {
		return StoreProject{}, err
	} else if ok {
		if idempotencyKey != "" {
			if err := insertIdempotency(ctx, tx, idempotencyKey, requestHash, existing.ID, project.LastActivityAt); err != nil {
				return StoreProject{}, err
			}
		}
		return existing, tx.Commit()
	}

	toolsJSON, err := json.Marshal(project.Tools)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: marshal tools: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects
		(id, slug, name, vps_id, root_path, origin_remote, branch, lifecycle_state,
		agents_manifest_present, hoopoe_initialized, tool_detection_done, desktop_mirror_path,
		imported_at, last_activity_at, tools_json, schema_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		project.ID, project.Slug, project.Name, project.VPSID, project.RootPath, project.OriginRemote,
		project.Branch, string(project.LifecycleState), boolInt(project.AgentsManifestPresent),
		boolInt(project.HoopoeInitialized), boolInt(project.ToolDetectionDone), project.DesktopMirrorPath,
		project.ImportedAt.UTC().Format(time.RFC3339Nano), project.LastActivityAt.UTC().Format(time.RFC3339Nano),
		string(toolsJSON), project.SchemaVersion,
	); err != nil {
		return StoreProject{}, fmt.Errorf("projects: insert: %w", err)
	}
	if idempotencyKey != "" {
		if err := insertIdempotency(ctx, tx, idempotencyKey, requestHash, project.ID, project.LastActivityAt); err != nil {
			return StoreProject{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return StoreProject{}, fmt.Errorf("projects: commit create: %w", err)
	}
	return project, nil
}

type Service struct {
	store  *SQLStore
	runner CommandRunner
	now    func() time.Time
	mu     sync.Mutex
}

type ServiceConfig struct {
	Store  *SQLStore
	Runner CommandRunner
	Now    func() time.Time
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: nil store", ErrInvalidRequest)
	}
	runner := cfg.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Service{store: cfg.Store, runner: runner, now: now}, nil
}

func (s *Service) Import(ctx context.Context, req ImportRequest) (schemas.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	root, err := normalizeRoot(req.RootPath)
	if err != nil {
		return schemas.Project{}, err
	}
	hash := requestHash(req, root)
	if existing, ok, err := s.store.FindByRoot(ctx, root); err != nil {
		return schemas.Project{}, err
	} else if ok && req.IdempotencyKey == "" {
		return toSchemaProject(existing), nil
	}

	git, err := ReadGitInfo(ctx, s.runner, root)
	if err != nil {
		return schemas.Project{}, err
	}
	if !git.IsGitRepo {
		return schemas.Project{}, ErrNotGitRepo
	}
	if git.OriginRemote == "" {
		return schemas.Project{}, ErrMissingOrigin
	}
	if git.Branch == "" {
		return schemas.Project{}, ErrDetachedHead
	}

	before := DetectToolEnvironment(root)
	initResult, err := InitializeHoopoeDir(root, InitializeOptions{
		ID:      req.projectID(),
		Name:    req.projectName(),
		Slug:    req.projectSlug(),
		Now:     s.now,
		Runner:  s.runner,
		Context: ctx,
	})
	if err != nil {
		return schemas.Project{}, err
	}
	if before.HasBeadsDir == false {
		if err := InitializeBeadsIfMissing(ctx, s.runner, root); err != nil {
			return schemas.Project{}, err
		}
	}
	after := DetectToolEnvironment(root)

	ts := s.now().UTC()
	metadata := initResult.Metadata
	vpsID := req.vpsID()
	if vpsID == "" {
		vpsID = defaultVPSID
	}
	project := StoreProject{
		ID:                    metadata.ID,
		Slug:                  metadata.Slug,
		Name:                  metadata.Name,
		VPSID:                 vpsID,
		RootPath:              root,
		OriginRemote:          git.OriginRemote,
		Branch:                git.Branch,
		LifecycleState:        schemas.ProjectLifecycleStateImported,
		AgentsManifestPresent: after.AgentsMDRelative != nil,
		HoopoeInitialized:     after.HasHoopoeDir,
		ToolDetectionDone:     true,
		DesktopMirrorPath:     req.desktopMirrorPath(),
		ImportedAt:            ts,
		LastActivityAt:        ts,
		Tools:                 after,
		SchemaVersion:         SchemaVersion,
	}
	created, err := s.store.Create(ctx, project, strings.TrimSpace(req.IdempotencyKey), hash)
	if err != nil {
		return schemas.Project{}, err
	}
	return toSchemaProject(created), nil
}

func (s *Service) List(ctx context.Context) ([]schemas.Project, error) {
	projects, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]schemas.Project, 0, len(projects))
	for _, project := range projects {
		out = append(out, toSchemaProject(project))
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, id string) (schemas.Project, error) {
	project, err := s.store.Get(ctx, id)
	if err != nil {
		return schemas.Project{}, err
	}
	return toSchemaProject(project), nil
}

func (s *Service) Project(ctx context.Context, id string) (schemas.Project, error) {
	return s.Get(ctx, id)
}

func (s *Service) Readiness(ctx context.Context, id string) (schemas.ProjectReadiness, error) {
	project, err := s.store.Get(ctx, id)
	if err != nil {
		return schemas.ProjectReadiness{}, err
	}
	return s.readinessFor(ctx, project), nil
}

func (s *Service) readinessFor(ctx context.Context, project StoreProject) schemas.ProjectReadiness {
	git, gitErr := ReadGitInfo(ctx, s.runner, project.RootPath)
	tools := DetectToolEnvironment(project.RootPath)
	checks := []schemas.GateCheck{
		gateCheck("git.present", gitErr == nil && git.IsGitRepo, "no git work tree at project root"),
		gateCheck("git.origin", gitErr == nil && git.OriginRemote != "", "v1 requires an origin remote"),
		gateCheck("git.branch", gitErr == nil && git.Branch != "", "detached HEAD or empty branch"),
		gateCheck("agents.md", tools.AgentsMDRelative != nil, "AGENTS.md missing"),
		gateCheck("hoopoe.dir", tools.HasHoopoeDir, ".hoopoe/ is not initialized"),
		gateCheck("tools.detected", len(tools.Manifests) > 0, "no supported language manifest detected"),
	}
	satisfied := true
	blocking := 0
	for _, check := range checks {
		if !check.Ok {
			satisfied = false
			blocking++
		}
	}
	state := project.LifecycleState
	return schemas.ProjectReadiness{
		SchemaVersion:         SchemaVersion,
		ProjectId:             project.ID,
		CheckedAt:             s.now().UTC(),
		CurrentLifecycleState: &state,
		Gates: []schemas.ProjectReadinessGate{{
			Gate:          schemas.ProjectGateProjectImported,
			Satisfied:     satisfied,
			Checks:        checks,
			BlockingCount: &blocking,
		}},
	}
}

type InitializeOptions struct {
	ID      string
	Name    string
	Slug    string
	Now     func() time.Time
	Runner  CommandRunner
	Context context.Context
}

type InitializeResult struct {
	HoopoeDir       string
	ProjectJSONPath string
	Metadata        Metadata
	Created         bool
}

func InitializeHoopoeDir(root string, opts InitializeOptions) (InitializeResult, error) {
	root, err := normalizeRoot(root)
	if err != nil {
		return InitializeResult{}, err
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	git, err := ReadGitInfo(ctx, opts.Runner, root)
	if err != nil {
		return InitializeResult{}, err
	}
	if !git.IsGitRepo {
		return InitializeResult{}, ErrNotGitRepo
	}
	if git.OriginRemote == "" {
		return InitializeResult{}, ErrMissingOrigin
	}
	if git.Branch == "" {
		return InitializeResult{}, ErrDetachedHead
	}

	hoopoeDir := filepath.Join(root, ".hoopoe")
	created := !dirExists(hoopoeDir)
	if err := os.MkdirAll(filepath.Join(hoopoeDir, "plans"), 0o755); err != nil {
		return InitializeResult{}, fmt.Errorf("projects: mkdir .hoopoe: %w", err)
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}
	ts := now().UTC().Format(time.RFC3339Nano)
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = filepath.Base(root)
	}
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = newProjectID()
	}
	slug := strings.TrimSpace(opts.Slug)
	if slug == "" {
		slug = slugify(name)
	}
	metadata := Metadata{
		ID:           id,
		Name:         name,
		Slug:         slug,
		RootPath:     root,
		OriginRemote: git.OriginRemote,
		Branch:       git.Branch,
		State:        schemas.ProjectLifecycleStateImported,
		CreatedAt:    ts,
		UpdatedAt:    ts,
		Tools:        DetectToolEnvironment(root),
	}
	projectJSONPath := filepath.Join(hoopoeDir, "project.json")
	if !fileExists(projectJSONPath) {
		if err := writeProjectJSON(projectJSONPath, metadata); err != nil {
			return InitializeResult{}, err
		}
	} else {
		existing, err := readProjectJSONPath(projectJSONPath)
		if err != nil {
			return InitializeResult{}, err
		}
		metadata = existing.Project
	}
	if err := writeJSONFileIfMissing(filepath.Join(hoopoeDir, "skills.lock.json"), map[string]any{
		"schemaVersion": 1,
		"pins":          map[string]any{},
	}); err != nil {
		return InitializeResult{}, err
	}
	if err := writeJSONFileIfMissing(filepath.Join(hoopoeDir, "model-context-policy.json"), map[string]any{
		"schemaVersion": 1,
		"contextPolicy": map[string]any{
			"includeAuditLog":  false,
			"includeFileGlobs": []string{},
			"excludeFileGlobs": []string{".env*", "**/secrets/**"},
		},
	}); err != nil {
		return InitializeResult{}, err
	}
	return InitializeResult{
		HoopoeDir:       hoopoeDir,
		ProjectJSONPath: projectJSONPath,
		Metadata:        metadata,
		Created:         created,
	}, nil
}

func ReadProjectJSON(root string) (ProjectJSON, error) {
	root, err := normalizeRoot(root)
	if err != nil {
		return ProjectJSON{}, err
	}
	return readProjectJSONPath(filepath.Join(root, ".hoopoe", "project.json"))
}

func readProjectJSONPath(path string) (ProjectJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProjectJSON{}, err
	}
	var parsed ProjectJSON
	if err := json.Unmarshal(data, &parsed); err != nil {
		return ProjectJSON{}, fmt.Errorf("%w: %v", ErrProjectJSONMalformed, err)
	}
	if parsed.SchemaVersion != SchemaVersion {
		return ProjectJSON{}, fmt.Errorf("%w: schemaVersion must be %d", ErrProjectJSONMalformed, SchemaVersion)
	}
	return parsed, nil
}

func InitializeBeadsIfMissing(ctx context.Context, runner CommandRunner, root string) error {
	if dirExists(filepath.Join(root, ".beads")) {
		return nil
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, root, []string{"br", "init"})
	if err != nil {
		return fmt.Errorf("projects: br init: %w", err)
	}
	if result.ExitCode != 0 {
		return commandError{argv: []string{"br", "init"}, result: result}
	}
	return nil
}

func ReadGitInfo(ctx context.Context, runner CommandRunner, root string) (GitInfo, error) {
	root, err := normalizeRoot(root)
	if err != nil {
		return GitInfo{}, err
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	inside, err := runner.Run(ctx, root, []string{"git", "rev-parse", "--is-inside-work-tree"})
	if err != nil {
		return GitInfo{}, err
	}
	if inside.ExitCode != 0 || strings.TrimSpace(string(inside.Stdout)) != "true" {
		return GitInfo{IsGitRepo: false}, nil
	}
	remote, err := runner.Run(ctx, root, []string{"git", "remote", "get-url", "origin"})
	if err != nil {
		return GitInfo{}, err
	}
	branch, err := runner.Run(ctx, root, []string{"git", "branch", "--show-current"})
	if err != nil {
		return GitInfo{}, err
	}
	return GitInfo{
		IsGitRepo:    true,
		OriginRemote: strings.TrimSpace(string(remote.Stdout)),
		Branch:       strings.TrimSpace(string(branch.Stdout)),
	}, nil
}

func DetectToolEnvironment(root string) ToolEnvironment {
	root, err := normalizeRoot(root)
	if err != nil {
		return ToolEnvironment{}
	}
	manifests := make([]Manifest, 0)
	for _, name := range supportedLanguageManifests {
		if fileExists(filepath.Join(root, name)) {
			manifests = append(manifests, Manifest{Name: name, RelativePath: name})
		}
	}
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Name < manifests[j].Name
	})
	return ToolEnvironment{
		AgentsMDRelative: findCaseInsensitive(root, "AGENTS.md"),
		ReadmeRelative:   findCaseInsensitive(root, "README.md"),
		Manifests:        manifests,
		HasBeadsDir:      dirExists(filepath.Join(root, ".beads")),
		HasHoopoeDir:     dirExists(filepath.Join(root, ".hoopoe")),
	}
}

func RUListPathsArgv() []string {
	return []string{"ru", "list", "--paths"}
}

func ListRUPaths(ctx context.Context, runner CommandRunner) ([]string, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	result, err := runner.Run(ctx, "", RUListPathsArgv())
	if err != nil {
		return nil, fmt.Errorf("projects: ru list --paths: %w", err)
	}
	if result.ExitCode != 0 {
		return nil, commandError{argv: RUListPathsArgv(), result: result}
	}
	var paths []string
	for _, line := range strings.Split(string(result.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !filepath.IsAbs(line) {
			return nil, fmt.Errorf("%w: ru returned non-absolute path %q", ErrInvalidRequest, line)
		}
		paths = append(paths, filepath.Clean(line))
	}
	return paths, nil
}

type commandError struct {
	argv   []string
	result CommandResult
}

func (e commandError) Error() string {
	stderr := strings.TrimSpace(string(e.result.Stderr))
	if stderr == "" {
		stderr = strings.TrimSpace(string(e.result.Stdout))
	}
	return fmt.Sprintf("%v: %v exited %d: %s", ErrCommandFailed, e.argv, e.result.ExitCode, stderr)
}

func (e commandError) Unwrap() error {
	return ErrCommandFailed
}

func toSchemaProject(project StoreProject) schemas.Project {
	root := project.RootPath
	importedAt := project.ImportedAt.UTC()
	lastActivityAt := project.LastActivityAt.UTC()
	agents := project.AgentsManifestPresent
	hoopoe := project.HoopoeInitialized
	tools := project.ToolDetectionDone
	repo := schemas.ProjectRepoRef{
		Origin:       project.OriginRemote,
		Branch:       project.Branch,
		VpsClonePath: &root,
	}
	if project.DesktopMirrorPath != "" {
		repo.DesktopMirrorPath = &project.DesktopMirrorPath
	}
	return schemas.Project{
		SchemaVersion:         project.SchemaVersion,
		Id:                    project.ID,
		Slug:                  project.Slug,
		Name:                  project.Name,
		VpsId:                 project.VPSID,
		Repo:                  repo,
		LifecycleState:        project.LifecycleState,
		AgentsManifestPresent: &agents,
		HoopoeInitialized:     &hoopoe,
		ToolDetectionDone:     &tools,
		ImportedAt:            &importedAt,
		LastActivityAt:        &lastActivityAt,
	}
}

func gateCheck(id string, ok bool, detail string) schemas.GateCheck {
	check := schemas.GateCheck{Id: id, Ok: ok}
	if !ok {
		check.Detail = &detail
	}
	return check
}

type idempotencyRecord struct {
	requestHash string
	projectID   string
}

func lookupIdempotency(ctx context.Context, tx *sql.Tx, key string) (*idempotencyRecord, error) {
	var rec idempotencyRecord
	err := tx.QueryRowContext(ctx, `SELECT request_hash, project_id FROM project_idempotency WHERE key = ?`, key).Scan(&rec.requestHash, &rec.projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("projects: lookup idempotency: %w", err)
	}
	return &rec, nil
}

func insertIdempotency(ctx context.Context, tx *sql.Tx, key, requestHash, projectID string, at time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO project_idempotency (key, request_hash, project_id, created_at)
		VALUES (?, ?, ?, ?)`, key, requestHash, projectID, at.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("projects: insert idempotency: %w", err)
	}
	return nil
}

func getTx(ctx context.Context, tx *sql.Tx, id string) (StoreProject, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects WHERE id = ?`, id)
	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StoreProject{}, ErrNotFound
	}
	return project, err
}

func findByRootTx(ctx context.Context, tx *sql.Tx, root string) (StoreProject, bool, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, slug, name, vps_id, root_path, origin_remote, branch,
		lifecycle_state, agents_manifest_present, hoopoe_initialized, tool_detection_done,
		desktop_mirror_path, imported_at, last_activity_at, tools_json, schema_version
		FROM projects WHERE root_path = ?`, root)
	project, err := scanProject(row)
	if errors.Is(err, sql.ErrNoRows) {
		return StoreProject{}, false, nil
	}
	if err != nil {
		return StoreProject{}, false, err
	}
	return project, true, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanProject(row scanner) (StoreProject, error) {
	var project StoreProject
	var lifecycle string
	var importedAt string
	var lastActivityAt string
	var toolsJSON string
	var agents int
	var hoopoe int
	var tools int
	if err := row.Scan(&project.ID, &project.Slug, &project.Name, &project.VPSID, &project.RootPath,
		&project.OriginRemote, &project.Branch, &lifecycle, &agents, &hoopoe, &tools,
		&project.DesktopMirrorPath, &importedAt, &lastActivityAt, &toolsJSON, &project.SchemaVersion); err != nil {
		return StoreProject{}, err
	}
	project.LifecycleState = schemas.ProjectLifecycleState(lifecycle)
	project.AgentsManifestPresent = agents == 1
	project.HoopoeInitialized = hoopoe == 1
	project.ToolDetectionDone = tools == 1
	var err error
	project.ImportedAt, err = time.Parse(time.RFC3339Nano, importedAt)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: parse imported_at: %w", err)
	}
	project.LastActivityAt, err = time.Parse(time.RFC3339Nano, lastActivityAt)
	if err != nil {
		return StoreProject{}, fmt.Errorf("projects: parse last_activity_at: %w", err)
	}
	if err := json.Unmarshal([]byte(toolsJSON), &project.Tools); err != nil {
		return StoreProject{}, fmt.Errorf("projects: decode tools: %w", err)
	}
	return project, nil
}

func normalizeRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", fmt.Errorf("%w: rootPath is required", ErrInvalidRequest)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("%w: resolve rootPath: %v", ErrInvalidRequest, err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("%w: stat rootPath: %v", ErrInvalidRequest, err)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%w: rootPath is not a directory", ErrInvalidRequest)
	}
	return filepath.Clean(abs), nil
}

func writeProjectJSON(path string, metadata Metadata) error {
	body := ProjectJSON{SchemaVersion: SchemaVersion, Project: metadata}
	data, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("projects: write project.json tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("projects: rename project.json: %w", err)
	}
	return nil
}

func writeJSONFileIfMissing(path string, value any) error {
	if fileExists(path) {
		return nil
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("projects: write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func requestHash(req ImportRequest, root string) string {
	canonical := struct {
		ID                      string `json:"id,omitempty"`
		Name                    string `json:"name,omitempty"`
		Slug                    string `json:"slug,omitempty"`
		RootPath                string `json:"rootPath"`
		VPSID                   string `json:"vpsId,omitempty"`
		DesktopMirrorPath       string `json:"desktopMirrorPath,omitempty"`
		AllowNoLanguageManifest bool   `json:"allowNoLanguageManifest,omitempty"`
	}{
		ID:                      req.projectID(),
		Name:                    req.projectName(),
		Slug:                    req.projectSlug(),
		RootPath:                root,
		VPSID:                   req.vpsID(),
		DesktopMirrorPath:       req.desktopMirrorPath(),
		AllowNoLanguageManifest: req.allowNoLanguageManifest(),
	}
	data, _ := json.Marshal(canonical)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r ImportRequest) projectID() string {
	return trimOptionalString(r.Id)
}

func (r ImportRequest) projectName() string {
	return trimOptionalString(r.Name)
}

func (r ImportRequest) projectSlug() string {
	return trimOptionalString(r.Slug)
}

func (r ImportRequest) vpsID() string {
	return trimOptionalString(r.VpsId)
}

func (r ImportRequest) desktopMirrorPath() string {
	return trimOptionalString(r.DesktopMirrorPath)
}

func (r ImportRequest) allowNoLanguageManifest() bool {
	return r.AllowNoLanguageManifest != nil && *r.AllowNoLanguageManifest
}

func trimOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func newProjectID() string {
	var raw [10]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("proj_%d", time.Now().UnixNano())
	}
	return "proj_" + hex.EncodeToString(raw[:])
}

func slugify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = strings.TrimRight(out[:64], "-")
	}
	if out == "" {
		return "project"
	}
	return out
}

func findCaseInsensitive(root, target string) *string {
	for _, name := range []string{target, strings.ToUpper(target), strings.ToLower(target)} {
		if fileExists(filepath.Join(root, name)) {
			value := name
			return &value
		}
	}
	return nil
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
