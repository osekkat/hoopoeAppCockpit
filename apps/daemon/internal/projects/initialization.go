package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hoopoe-cockpit/hoopoe/apps/daemon/internal/modelcontext"
	schemas "github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// initialization.go owns the on-disk import bootstrap for a project root —
// .hoopoe/ scaffolding, project.json read/write, .beads/ initialization,
// and the file-write helpers those flows share.
//
// hp-j6zr second cut: split out of projects.go to continue the
// "store, import, readiness, RU discovery, file writers" decomposition
// the bead opens against this package. The first cut moved StoreProject
// + SQLStore into store.go; this cut moves the import-side initialization
// surface so projects.go is left with Service orchestration + tool
// discovery + agent-contract concerns. Behavior unchanged: same
// package, same exported signatures, same helpers.

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
	if err := modelcontext.WriteDefaultPolicyIfMissing(ctx, filepath.Join(hoopoeDir, "model-context-policy.json")); err != nil {
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

func writeFileIfAbsent(path string, body string, perm os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(body); err != nil {
		_ = file.Close()
		return fmt.Errorf("projects: write %s: %w", filepath.Base(path), err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("projects: close %s: %w", filepath.Base(path), err)
	}
	return nil
}
