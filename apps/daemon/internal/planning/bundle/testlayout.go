// testlayout.go — TestLayoutSummary detection per ecosystem
// (hp-rsly fifth slice).
//
// `DetectTestLayout` inspects the project root for ecosystem markers
// (manifests + config files) and returns a TestLayoutSummary
// describing the detected runner, test-file globs, fixture
// conventions, and coverage-config path.
//
// Detection is heuristic: when no signals match, the returned summary
// reports `runner: "unknown"` and empty pattern lists rather than
// guessing — plan candidates can then ask the user to clarify instead
// of inventing a runner.
//
// Today the helper covers the four ecosystems Hoopoe targets first
// per plan.md §11 (TS/JS, Python, Rust, Go). Other ecosystems
// (Ruby, Elixir, Java) are explicit follow-ups; the heuristics
// table below is the extension point.
//
// Returns:
//   - `*schemas.TestLayoutSummary` with detected fields populated.
//     Always non-nil; empty layout when nothing matched.
//   - error only on filesystem failure (not on absent manifests —
//     a project without a manifest is the "unknown runner" case).

package bundle

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// RunnerUnknown is the canonical sentinel for projects without a
// detected test runner. Mirrors the openapi.yaml description:
// "`unknown` when no runner could be detected with confidence."
const RunnerUnknown = "unknown"

// DetectTestLayout scans `projectRoot` for manifest + config markers
// and returns a populated TestLayoutSummary. The detection rules:
//
//   - package.json present + "vitest" / "jest" / "@bun" devDep hint  →
//     runner = "vitest" / "jest" / "bun:test" (in that priority order
//     when multiple match; the project's own scripts.test entry wins
//     when present).
//   - package.json present without explicit hint → runner = "node:test".
//   - pyproject.toml or setup.cfg present → runner = "pytest" (the
//     dominant Python test runner; refined by future slices).
//   - go.mod present → runner = "go test".
//   - Cargo.toml present → runner = "cargo test".
//   - Otherwise → runner = "unknown".
//
// Path-traversal hardening lives in CaptureFile; this helper only
// reads files from below `projectRoot` resolved with filepath.Join +
// Clean. Empty `projectRoot` is rejected.
func DetectTestLayout(projectRoot string) (*schemas.TestLayoutSummary, error) {
	if projectRoot == "" {
		return nil, errors.New("planning/bundle: projectRoot is required")
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: abs projectRoot: %w", err)
	}

	out := &schemas.TestLayoutSummary{
		Runner:             RunnerUnknown,
		FixtureConventions: []string{},
		TestFilePatterns:   []string{},
	}

	switch {
	case fileExists(filepath.Join(root, "package.json")):
		out.Runner, out.TestFilePatterns = detectJSRunner(root)
		out.FixtureConventions = detectJSFixtureConventions(root)
		if cov := firstExisting(root, jsCoverageConfigs); cov != "" {
			rel := relPath(root, cov)
			out.CoverageConfig = &rel
		}
	case fileExists(filepath.Join(root, "pyproject.toml")) || fileExists(filepath.Join(root, "setup.cfg")):
		out.Runner = "pytest"
		out.TestFilePatterns = []string{"tests/**/test_*.py", "test_*.py", "**/test_*.py"}
		out.FixtureConventions = detectPythonFixtureConventions(root)
		if cov := firstExisting(root, pythonCoverageConfigs); cov != "" {
			rel := relPath(root, cov)
			out.CoverageConfig = &rel
		}
	case fileExists(filepath.Join(root, "go.mod")):
		out.Runner = "go test"
		out.TestFilePatterns = []string{"**/*_test.go"}
		out.FixtureConventions = detectGoFixtureConventions(root)
		// go test integrates coverage natively; the "config" surface is
		// the shell command + flags, not a file. Leave CoverageConfig
		// nil unless a project-specific .coverage.yml shows up.
	case fileExists(filepath.Join(root, "Cargo.toml")):
		out.Runner = "cargo test"
		out.TestFilePatterns = []string{"src/**/*.rs", "tests/**/*.rs"}
		out.FixtureConventions = detectRustFixtureConventions(root)
	}

	return out, nil
}

func fileExists(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !stat.IsDir()
}

func dirExists(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return stat.IsDir()
}

func relPath(root, full string) string {
	r, err := filepath.Rel(root, full)
	if err != nil {
		return full
	}
	return r
}

var (
	jsCoverageConfigs = []string{
		"vitest.config.ts",
		"vitest.config.js",
		"vitest.config.mts",
		"jest.config.ts",
		"jest.config.js",
		"bunfig.toml",
		".codecov.yml",
		"codecov.yml",
	}
	pythonCoverageConfigs = []string{
		".coveragerc",
		"pyproject.toml",
		"setup.cfg",
		".codecov.yml",
		"codecov.yml",
	}
)

func firstExisting(root string, candidates []string) string {
	for _, c := range candidates {
		full := filepath.Join(root, c)
		if fileExists(full) {
			return full
		}
	}
	return ""
}

// detectJSRunner picks the runner based on the strongest signal:
// vitest/jest config presence, then bun:test (bunfig.toml /
// "@hoopoe-cockpit/bun-test" hint), then node:test as the default.
// We deliberately don't open package.json's JSON contents in this
// slice — string-presence is enough for the §7.1 contract and avoids
// pulling a JSON parser path that the future model-context-policy
// slice will need to share. Future slices refine by reading
// scripts.test verbatim.
func detectJSRunner(root string) (string, []string) {
	if fileExists(filepath.Join(root, "vitest.config.ts")) ||
		fileExists(filepath.Join(root, "vitest.config.js")) ||
		fileExists(filepath.Join(root, "vitest.config.mts")) {
		return "vitest", []string{"**/*.test.ts", "**/*.test.tsx", "**/*.test.js", "**/*.spec.ts"}
	}
	if fileExists(filepath.Join(root, "jest.config.ts")) ||
		fileExists(filepath.Join(root, "jest.config.js")) {
		return "jest", []string{"**/*.test.ts", "**/*.test.tsx", "**/*.test.js", "**/__tests__/**/*.ts"}
	}
	if fileExists(filepath.Join(root, "bunfig.toml")) || fileExists(filepath.Join(root, "bun.lockb")) {
		return "bun:test", []string{"**/*.test.ts", "**/*.test.tsx", "**/*.spec.ts", "src/**/*.test.ts"}
	}
	return "node:test", []string{"test/**/*.test.js", "test/**/*.test.mjs", "**/*.test.js"}
}

func detectJSFixtureConventions(root string) []string {
	conv := []string{}
	if dirExists(filepath.Join(root, "__fixtures__")) {
		conv = append(conv, "__fixtures__/")
	}
	if dirExists(filepath.Join(root, "test", "fixtures")) {
		conv = append(conv, "test/fixtures/")
	}
	if dirExists(filepath.Join(root, "tests", "fixtures")) {
		conv = append(conv, "tests/fixtures/")
	}
	return conv
}

func detectPythonFixtureConventions(root string) []string {
	conv := []string{}
	if dirExists(filepath.Join(root, "tests", "fixtures")) {
		conv = append(conv, "tests/fixtures/")
	}
	if dirExists(filepath.Join(root, "test", "fixtures")) {
		conv = append(conv, "test/fixtures/")
	}
	if fileExists(filepath.Join(root, "conftest.py")) {
		conv = append(conv, "conftest.py")
	}
	return conv
}

func detectGoFixtureConventions(root string) []string {
	conv := []string{}
	if dirExists(filepath.Join(root, "testdata")) {
		conv = append(conv, "testdata/")
	}
	// Go convention is per-package testdata/. Future slices may walk to
	// surface every testdata directory; this slice flags the top-level.
	return conv
}

func detectRustFixtureConventions(root string) []string {
	conv := []string{}
	if dirExists(filepath.Join(root, "tests", "fixtures")) {
		conv = append(conv, "tests/fixtures/")
	}
	if dirExists(filepath.Join(root, "tests")) {
		conv = append(conv, "tests/")
	}
	return conv
}
