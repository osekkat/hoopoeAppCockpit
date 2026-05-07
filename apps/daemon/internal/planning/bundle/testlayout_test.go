package bundle

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, root, rel string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte{}, 0o644); err != nil {
		t.Fatalf("touch: %v", err)
	}
}

func mkdir(t *testing.T, root, rel string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, rel), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
}

func TestDetectTestLayoutEmptyProjectIsUnknown(t *testing.T) {
	root := t.TempDir()
	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner != RunnerUnknown {
		t.Errorf("Runner = %q, want unknown", out.Runner)
	}
	if len(out.TestFilePatterns) != 0 {
		t.Errorf("TestFilePatterns = %v, want empty", out.TestFilePatterns)
	}
	if out.CoverageConfig != nil {
		t.Errorf("CoverageConfig = %v, want nil", *out.CoverageConfig)
	}
}

func TestDetectTestLayoutEmptyProjectRoot(t *testing.T) {
	_, err := DetectTestLayout("")
	if err == nil {
		t.Fatal("DetectTestLayout(\"\") should error")
	}
}

func TestDetectTestLayoutVitestProject(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "package.json")
	touch(t, root, "vitest.config.ts")
	mkdir(t, root, "test/fixtures")

	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner != "vitest" {
		t.Errorf("Runner = %q, want vitest", out.Runner)
	}
	if len(out.TestFilePatterns) == 0 {
		t.Error("TestFilePatterns empty for vitest project")
	}
	if out.CoverageConfig == nil || *out.CoverageConfig != "vitest.config.ts" {
		t.Errorf("CoverageConfig = %v, want vitest.config.ts", out.CoverageConfig)
	}
	foundFixture := false
	for _, c := range out.FixtureConventions {
		if c == "test/fixtures/" {
			foundFixture = true
			break
		}
	}
	if !foundFixture {
		t.Errorf("FixtureConventions missing test/fixtures/: %v", out.FixtureConventions)
	}
}

func TestDetectTestLayoutBunTestProject(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "package.json")
	touch(t, root, "bunfig.toml")

	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner != "bun:test" {
		t.Errorf("Runner = %q, want bun:test", out.Runner)
	}
}

func TestDetectTestLayoutJestProject(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "package.json")
	touch(t, root, "jest.config.js")
	mkdir(t, root, "__fixtures__")

	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner != "jest" {
		t.Errorf("Runner = %q, want jest", out.Runner)
	}
	foundFixture := false
	for _, c := range out.FixtureConventions {
		if c == "__fixtures__/" {
			foundFixture = true
		}
	}
	if !foundFixture {
		t.Errorf("FixtureConventions missing __fixtures__/: %v", out.FixtureConventions)
	}
}

func TestDetectTestLayoutNodeTestFallback(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "package.json") // no jest/vitest/bun signals
	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner != "node:test" {
		t.Errorf("Runner = %q, want node:test", out.Runner)
	}
}

func TestDetectTestLayoutPythonProject(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "pyproject.toml")
	touch(t, root, "conftest.py")
	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner != "pytest" {
		t.Errorf("Runner = %q, want pytest", out.Runner)
	}
	if out.CoverageConfig == nil {
		t.Errorf("CoverageConfig nil for python project with pyproject.toml")
	}
	foundConftest := false
	for _, c := range out.FixtureConventions {
		if c == "conftest.py" {
			foundConftest = true
		}
	}
	if !foundConftest {
		t.Errorf("FixtureConventions missing conftest.py: %v", out.FixtureConventions)
	}
}

func TestDetectTestLayoutGoProject(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "go.mod")
	mkdir(t, root, "testdata")
	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner != "go test" {
		t.Errorf("Runner = %q, want go test", out.Runner)
	}
	foundTestdata := false
	for _, c := range out.FixtureConventions {
		if c == "testdata/" {
			foundTestdata = true
		}
	}
	if !foundTestdata {
		t.Errorf("FixtureConventions missing testdata/: %v", out.FixtureConventions)
	}
}

func TestDetectTestLayoutRustProject(t *testing.T) {
	root := t.TempDir()
	touch(t, root, "Cargo.toml")
	mkdir(t, root, "tests")
	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner != "cargo test" {
		t.Errorf("Runner = %q, want cargo test", out.Runner)
	}
	if len(out.FixtureConventions) == 0 {
		t.Error("FixtureConventions empty despite tests/ dir")
	}
}

func TestDetectTestLayoutTSProjectVitestPriority(t *testing.T) {
	// When BOTH vitest.config.ts and bunfig.toml exist, vitest wins
	// per the documented priority order.
	root := t.TempDir()
	touch(t, root, "package.json")
	touch(t, root, "vitest.config.ts")
	touch(t, root, "bunfig.toml")
	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner != "vitest" {
		t.Errorf("Runner = %q, want vitest (priority over bun:test)", out.Runner)
	}
}

func TestDetectTestLayoutPolyglotPicksJSWhenPackageJsonPresent(t *testing.T) {
	// When package.json + go.mod + Cargo.toml all exist, we pick JS
	// (the case-switch hits package.json first since the daemon ships
	// a TS-first project shape; future slices may surface a multi-
	// runner result).
	root := t.TempDir()
	touch(t, root, "package.json")
	touch(t, root, "go.mod")
	touch(t, root, "Cargo.toml")
	out, err := DetectTestLayout(root)
	if err != nil {
		t.Fatalf("DetectTestLayout: %v", err)
	}
	if out.Runner == RunnerUnknown {
		t.Errorf("Runner = unknown, want a JS runner picked")
	}
}
