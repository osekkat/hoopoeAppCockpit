// discovery.go — project-root discovery walk for the bundle assembly
// (hp-rsly eighth slice).
//
// `WalkProjectRoot` orchestrates the deterministic file-discovery
// portion of the existing-codebase context bundle: README + AGENTS.md
// at the project root, architecture-docs under `docs/architecture/`
// and `ARCHITECTURE.md`, the test-layout summary, and the package-
// manifest list. Each surface uses its already-shipped primitive
// (CaptureFile / DetectTestLayout / DetectManifests).
//
// What this slice does NOT do:
//
//   - Bead summaries (BrAdapter integration is hp-rsly residual).
//   - Health hotspots (HealthAdapter integration is hp-rsly residual).
//   - Model-context policy enforcement (§5.5 — secret scan + path rules).
//   - Token-budget hard cap (still a hp-rsly residual).
//
// The caller stitches those layers on top of this walk, then passes
// the resulting bundle through `ComputeContentHash` + `ComputeCacheKey`.
//
// Caps applied here:
//
//   - README + AGENTS.md: DefaultFileSizeCap (50 KB each, per the
//     §7.1 candidate-prompt budget).
//   - Architecture docs: 100 KB total budget shared across all files
//     under `docs/architecture/`. Files are captured alphabetically
//     until the budget is exhausted, then remaining paths are
//     surfaced via `SkippedArchitectureDocs` so the caller can
//     populate the bundle's `Excluded` field.

package bundle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// DefaultArchitectureDocsBudget is the total-bytes ceiling shared
// across the architecture-docs surface. The discovery contract
// allocates a 100 KB envelope per the bead body and the openapi
// schema doc-string. A single oversize file is truncated; multiple
// files are appended in alphabetical order until the remaining
// budget is below the next file's on-disk size, at which point the
// rest are skipped + reported via `SkippedArchitectureDocs`.
const DefaultArchitectureDocsBudget = 100 * 1024

// DiscoveryResult captures the deterministic-walk subset of the bundle.
// Bead/health/policy/token-budget integrations remain follow-up slices.
//
// Fields mirror the `ExistingCodebaseContextBundle` shape so the caller
// can copy them in without translation:
//
//	bundle := &schemas.ExistingCodebaseContextBundle{...}
//	res, _ := bundle.WalkProjectRoot(root)
//	bundle.Readme = res.Readme
//	bundle.AgentsMd = res.AgentsMd
//	bundle.ArchitectureDocs = res.ArchitectureDocs
//	bundle.PackageManifests = res.PackageManifests
//	bundle.TestLayout = res.TestLayout
//	bundle.Excluded = append(bundle.Excluded, res.SkippedArchitectureDocs...)
type DiscoveryResult struct {
	// Readme is the captured README.md, or nil when the project root
	// has no README. Caps at DefaultFileSizeCap.
	Readme *schemas.FileSnapshot

	// AgentsMd is the captured AGENTS.md, or nil when absent. Caps at
	// DefaultFileSizeCap.
	AgentsMd *schemas.FileSnapshot

	// ArchitectureDocs holds files from `docs/architecture/*.md` and
	// `ARCHITECTURE.md`, alphabetically ordered, capped at
	// DefaultArchitectureDocsBudget total bytes.
	ArchitectureDocs []schemas.FileSnapshot

	// PackageManifests is the DetectManifests result: top-level
	// ecosystem manifests (package.json, pyproject.toml, etc.).
	PackageManifests []schemas.ManifestSnapshot

	// TestLayout is the DetectTestLayout result. Always non-nil
	// (DetectTestLayout returns a populated "unknown" summary when
	// no signals match).
	TestLayout *schemas.TestLayoutSummary

	// SkippedArchitectureDocs lists project-root-relative paths the
	// architecture-docs walk hit but couldn't capture under the
	// shared budget. The caller folds these into the bundle's
	// `Excluded` field so the UI's "manage what models see" surface
	// reflects them.
	SkippedArchitectureDocs []string
}

// candidateReadmeNames lists the names the discovery walk treats as
// "the project README." The first existing match wins; the rest are
// ignored. Lowercase + uppercase variants are checked because macOS
// developer setups sometimes mix case.
var candidateReadmeNames = []string{"README.md", "readme.md", "Readme.md"}

// candidateAgentsMdNames mirrors candidateReadmeNames for AGENTS.md.
var candidateAgentsMdNames = []string{"AGENTS.md", "agents.md", "Agents.md"}

// rootArchitectureDocNames is the deterministic list of files at the
// project root that the discovery walk treats as architecture docs.
var rootArchitectureDocNames = []string{"ARCHITECTURE.md", "architecture.md"}

// WalkProjectRoot performs the discovery walk and returns a
// DiscoveryResult. The walk is deterministic: same project tree →
// same result, same field order, same truncation outcomes.
//
// `projectRoot` must be a non-empty absolute or relative path to a
// directory the daemon can read. The walk only reads; it never
// writes back to the project tree.
//
// Errors:
//   - When projectRoot is empty.
//   - When DetectTestLayout / DetectManifests fail (those propagate as-is).
//
// Per-file capture failures (other than ErrFileNotFound, which is
// expected for absent docs) propagate; the caller decides whether to
// surface the error or fall back to "no doc captured." The contract
// is "return a deterministic bundle slice or an error" — never a
// partial bundle with a silently-dropped section.
func WalkProjectRoot(projectRoot string) (*DiscoveryResult, error) {
	if projectRoot == "" {
		return nil, errors.New("planning/bundle: projectRoot is required")
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: abs projectRoot: %w", err)
	}

	out := &DiscoveryResult{
		ArchitectureDocs:        []schemas.FileSnapshot{},
		PackageManifests:        []schemas.ManifestSnapshot{},
		SkippedArchitectureDocs: []string{},
	}

	if snap, err := captureFirstExisting(root, candidateReadmeNames, DefaultFileSizeCap); err != nil {
		return nil, fmt.Errorf("planning/bundle: capture README: %w", err)
	} else {
		out.Readme = snap
	}

	if snap, err := captureFirstExisting(root, candidateAgentsMdNames, DefaultFileSizeCap); err != nil {
		return nil, fmt.Errorf("planning/bundle: capture AGENTS.md: %w", err)
	} else {
		out.AgentsMd = snap
	}

	docs, skipped, err := captureArchitectureDocs(root, DefaultArchitectureDocsBudget)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: capture architecture docs: %w", err)
	}
	out.ArchitectureDocs = docs
	out.SkippedArchitectureDocs = skipped

	manifests, err := DetectManifests(root)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: detect manifests: %w", err)
	}
	out.PackageManifests = manifests

	layout, err := DetectTestLayout(root)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: detect test layout: %w", err)
	}
	out.TestLayout = layout

	return out, nil
}

// captureFirstExisting tries each name under root in order and returns
// the first FileSnapshot that captures successfully. Returns (nil, nil)
// when none of the candidates exist (the absence of all is a normal
// outcome — not an error). Other capture errors propagate.
func captureFirstExisting(root string, names []string, sizeCap int) (*schemas.FileSnapshot, error) {
	for _, name := range names {
		full := filepath.Join(root, name)
		if !fileExists(full) {
			continue
		}
		snap, err := CaptureFile(root, name, sizeCap)
		if err != nil {
			if errors.Is(err, ErrFileNotFound) {
				// Lost between fileExists and CaptureFile (transient
				// race). Try the next candidate instead of failing.
				continue
			}
			return nil, err
		}
		return snap, nil
	}
	return nil, nil
}

// captureArchitectureDocs walks the project's architecture-doc surface
// (`docs/architecture/*.md` + `ARCHITECTURE.md`-like root files) and
// captures them in alphabetical order until the shared `budgetBytes`
// is exhausted. Files that don't fit are appended to `skipped` so the
// caller can surface them in the bundle's `Excluded` list.
func captureArchitectureDocs(root string, budgetBytes int) ([]schemas.FileSnapshot, []string, error) {
	candidates, err := architectureDocCandidates(root)
	if err != nil {
		return nil, nil, err
	}

	out := []schemas.FileSnapshot{}
	skipped := []string{}
	remaining := budgetBytes

	for _, rel := range candidates {
		full := filepath.Join(root, rel)
		stat, err := os.Stat(full)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				// Race between scan + read. Skip silently — the
				// alternative is a hard fail on a transient.
				continue
			}
			return nil, nil, fmt.Errorf("stat %s: %w", rel, err)
		}
		if stat.IsDir() {
			continue
		}
		// Decide capture cap for this file. Use the per-file size cap
		// when remaining > size cap; otherwise, use what's left of
		// the shared budget. A file that doesn't fit at all is
		// recorded as skipped.
		fileSize := int(stat.Size())
		if remaining <= 0 {
			skipped = append(skipped, toPosixPath(rel))
			continue
		}
		cap := fileSize
		if cap > remaining {
			cap = remaining
		}
		if cap > DefaultFileSizeCap {
			cap = DefaultFileSizeCap
		}
		snap, err := CaptureFile(root, rel, cap)
		if err != nil {
			if errors.Is(err, ErrFileNotFound) {
				continue
			}
			return nil, nil, fmt.Errorf("capture %s: %w", rel, err)
		}
		out = append(out, *snap)
		remaining -= snap.SizeBytes
	}

	return out, skipped, nil
}

// architectureDocCandidates returns all .md paths under
// `docs/architecture/` plus root-level architecture-doc files,
// sorted alphabetically (POSIX path order).
func architectureDocCandidates(root string) ([]string, error) {
	candidates := []string{}

	for _, name := range rootArchitectureDocNames {
		if fileExists(filepath.Join(root, name)) {
			candidates = append(candidates, name)
		}
	}

	archDir := filepath.Join(root, "docs", "architecture")
	if dirExists(archDir) {
		entries, err := os.ReadDir(archDir)
		if err != nil {
			return nil, fmt.Errorf("readdir docs/architecture: %w", err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := filepath.Ext(e.Name())
			if ext != ".md" {
				continue
			}
			candidates = append(candidates, filepath.Join("docs", "architecture", e.Name()))
		}
	}

	sort.Strings(candidates)
	return candidates, nil
}
