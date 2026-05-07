// Package projects owns the daemon-side project registry and import gate.
package projects

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
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

	DefaultAgentsManifestRelativePath = "AGENTS.md"
	AgentContractOpenLabel            = "Open AGENTS.md"
	AgentContractCreateLabel          = "Create AGENTS.md"
	maxAgentContractDraftBytes        = 128 * 1024
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
	ErrAgentContractExists  = errors.New("projects: AGENTS.md already exists")
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

type AgentContractDraftRequest struct {
	ProjectID string
	Body      string
	Actor     string
	At        time.Time
}

// hp-j6zr: StoreProject + SQLStore + CRUD methods moved to store.go.

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

func (s *Service) AgentContract(ctx context.Context, id string) (schemas.ProjectAgentContract, error) {
	project, err := s.store.Get(ctx, id)
	if err != nil {
		return schemas.ProjectAgentContract{}, err
	}
	tools := DetectToolEnvironment(project.RootPath)
	return agentContractFor(project.Name, tools), nil
}

func (s *Service) CreateAgentContractDraft(ctx context.Context, req AgentContractDraftRequest) (schemas.ProjectAgentContract, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	project, err := s.store.Get(ctx, strings.TrimSpace(req.ProjectID))
	if err != nil {
		return schemas.ProjectAgentContract{}, err
	}
	tools := DetectToolEnvironment(project.RootPath)
	if tools.AgentsMDRelative != nil {
		return schemas.ProjectAgentContract{}, ErrAgentContractExists
	}
	body := req.Body
	if strings.TrimSpace(body) == "" {
		body = RecommendedAgentsTemplate(project.Name)
	}
	if strings.Contains(body, "\x00") {
		return schemas.ProjectAgentContract{}, fmt.Errorf("%w: AGENTS.md body contains NUL", ErrInvalidRequest)
	}
	if len([]byte(body)) > maxAgentContractDraftBytes {
		return schemas.ProjectAgentContract{}, fmt.Errorf("%w: AGENTS.md body exceeds %d bytes", ErrInvalidRequest, maxAgentContractDraftBytes)
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	target := filepath.Join(project.RootPath, DefaultAgentsManifestRelativePath)
	if err := writeFileIfAbsent(target, body, 0o644); err != nil {
		if errors.Is(err, os.ErrExist) {
			return schemas.ProjectAgentContract{}, ErrAgentContractExists
		}
		return schemas.ProjectAgentContract{}, err
	}
	updatedTools := DetectToolEnvironment(project.RootPath)
	at := req.At
	if at.IsZero() {
		at = s.now().UTC()
	}
	updated, err := s.store.UpdateToolEnvironment(ctx, project.ID, updatedTools, at)
	if err != nil {
		return schemas.ProjectAgentContract{}, err
	}
	return agentContractFor(updated.Name, updated.Tools), nil
}

func (s *Service) readinessFor(ctx context.Context, project StoreProject) schemas.ProjectReadiness {
	git, gitErr := ReadGitInfo(ctx, s.runner, project.RootPath)
	tools := DetectToolEnvironment(project.RootPath)
	contract := agentContractFor(project.Name, tools)
	checks := []schemas.GateCheck{
		gateCheck("git.present", gitErr == nil && git.IsGitRepo, "no git work tree at project root"),
		gateCheck("git.origin", gitErr == nil && git.OriginRemote != "", "v1 requires an origin remote"),
		gateCheck("git.branch", gitErr == nil && git.Branch != "", "detached HEAD or empty branch"),
		gateCheck("agents.md", contract.Status == schemas.Present, missingAgentContractDetail(contract)),
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

// hp-j6zr: InitializeOptions/InitializeResult, InitializeHoopoeDir,
// ReadProjectJSON/readProjectJSONPath, InitializeBeadsIfMissing, and the
// project-json/file write helpers (writeProjectJSON, writeJSONFileIfMissing,
// writeFileIfAbsent) moved to initialization.go.

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

func RecommendedAgentsTemplate(projectName string) string {
	name := strings.TrimSpace(projectName)
	if name == "" {
		name = "This project"
	}
	return fmt.Sprintf(`# AGENTS.md - %s

## Project Goal

Describe the project goal, success criteria, and the boundaries agents must preserve.

## Coordination

- Use Beads (br) for task status and dependency-aware ready work.
- Use Agent Mail for thread updates and file reservations before editing.
- Include the bead ID in reservation reasons, thread subjects, commits, and handoff notes.
- Push every successful commit promptly.

## Build And Test

- Record the exact build, lint, and test commands agents must run before commit.
- Prefer remote build helpers when the project specifies them.
- Do not mark work complete while required verification is still failing.

## Source Of Truth

- Keep canonical state in the project's native tools and files.
- Do not replace existing project workflows with parallel agent-only state.
- Ask before any destructive filesystem or Git operation.

## Escalation

- If a blocker is outside the bead scope, report it in the project thread with evidence.
- If another agent owns a conflicting file reservation, coordinate before touching it.
`, name)
}

func agentContractFor(projectName string, tools ToolEnvironment) schemas.ProjectAgentContract {
	contract := schemas.ProjectAgentContract{
		DefaultRelativePath:     DefaultAgentsManifestRelativePath,
		ReadRequiredBeforeSwarm: true,
	}
	if tools.AgentsMDRelative != nil && strings.TrimSpace(*tools.AgentsMDRelative) != "" {
		relative := strings.TrimSpace(*tools.AgentsMDRelative)
		contract.Status = schemas.Present
		contract.RelativePath = &relative
		contract.OpenAction = &schemas.ProjectAgentContractAction{
			Id:                 schemas.AgentsOpen,
			Label:              AgentContractOpenLabel,
			TargetRelativePath: relative,
		}
		return contract
	}
	template := RecommendedAgentsTemplate(projectName)
	contract.Status = schemas.Missing
	contract.CreateAction = &schemas.ProjectAgentContractCreateAction{
		Id:                 schemas.AgentsCreate,
		Label:              AgentContractCreateLabel,
		TargetRelativePath: DefaultAgentsManifestRelativePath,
		Template:           template,
		TemplateSha256:     digestString(template),
	}
	return contract
}

func missingAgentContractDetail(contract schemas.ProjectAgentContract) string {
	if contract.CreateAction != nil {
		return fmt.Sprintf("AGENTS.md is missing; surface %s action targeting %s", contract.CreateAction.Id, contract.CreateAction.TargetRelativePath)
	}
	return "AGENTS.md is missing"
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
	contract := agentContractFor(project.Name, project.Tools)
	return schemas.Project{
		SchemaVersion:         project.SchemaVersion,
		Id:                    project.ID,
		Slug:                  project.Slug,
		Name:                  project.Name,
		VpsId:                 project.VPSID,
		Repo:                  repo,
		LifecycleState:        project.LifecycleState,
		AgentsManifestPresent: &agents,
		AgentsContract:        &contract,
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

// hp-j6zr: idempotency helpers + tx scan helpers + scanProject moved to store.go.

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

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
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
