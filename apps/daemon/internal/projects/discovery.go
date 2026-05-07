package projects

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// discovery.go owns the read-only "what's at this path?" probes the
// project-registry uses to decide readiness gates and import flow:
// Git work-tree shape (origin remote, current branch), supported
// language manifests, AGENTS.md / README.md presence, .beads/ and
// .hoopoe/ presence, and the ru list --paths multi-project enumerator.
//
// hp-j6zr third cut: split out of projects.go to continue the
// "store, import, readiness, RU discovery, file writers" decomposition
// the bead opens against this package. Cuts so far: store.go (#1,
// 9f5b2b4), initialization.go (#2, ae83557), now discovery.go (#3).
// Behavior unchanged — same package, same exported signatures.
// Service.readinessFor + Service.Import + Service.AgentContract still
// drive these probes through same-package access.

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
