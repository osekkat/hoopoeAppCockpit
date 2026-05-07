package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

func TestDetectManifestsEmptyProjectReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	out, err := DetectManifests(root)
	if err != nil {
		t.Fatalf("DetectManifests: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("len(out) = %d, want 0", len(out))
	}
	if out == nil {
		t.Errorf("out is nil; should be empty slice for stable JSON shape")
	}
}

func TestDetectManifestsEmptyProjectRootRejected(t *testing.T) {
	_, err := DetectManifests("")
	if err == nil {
		t.Fatal("DetectManifests(\"\") should error")
	}
}

func TestDetectManifestsPackageJSON(t *testing.T) {
	root := t.TempDir()
	body := []byte(`{ "name": "demo", "version": "1.2.3" }`)
	if err := writeAtRoot(root, "package.json", body); err != nil {
		t.Fatalf("writeAtRoot: %v", err)
	}

	out, err := DetectManifests(root)
	if err != nil {
		t.Fatalf("DetectManifests: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	got := out[0]
	if got.Kind != schemas.PackageJson {
		t.Errorf("Kind = %q, want package_json", got.Kind)
	}
	if got.Path != "package.json" {
		t.Errorf("Path = %q, want package.json", got.Path)
	}
	if got.Raw != string(body) {
		t.Errorf("Raw mismatch: got %q, want %q", got.Raw, string(body))
	}
}

func TestDetectManifestsAllKnownKinds(t *testing.T) {
	root := t.TempDir()
	files := map[string][]byte{
		"package.json":   []byte(`{}`),
		"pyproject.toml": []byte(`[project]`),
		"Cargo.toml":     []byte(`[package]`),
		"go.mod":         []byte(`module x` + "\n"),
		"Gemfile":        []byte(nil),
		"composer.json":  []byte(`{}`),
		"mix.exs":        []byte(`defmodule X.MixProject do end`),
		"pom.xml":        []byte(`<project/>`),
	}
	// Bypass the empty-bytes special case via a non-nil zero-len for Gemfile.
	files["Gemfile"] = []byte("source 'https://rubygems.org'\n")

	for name, body := range files {
		if err := writeAtRoot(root, name, body); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	out, err := DetectManifests(root)
	if err != nil {
		t.Fatalf("DetectManifests: %v", err)
	}
	if len(out) != len(files) {
		t.Fatalf("len(out) = %d, want %d", len(out), len(files))
	}
	// Confirm each known kind appears exactly once.
	seen := map[schemas.ManifestSnapshotKind]bool{}
	for _, m := range out {
		if seen[m.Kind] {
			t.Errorf("kind %q appears twice", m.Kind)
		}
		seen[m.Kind] = true
	}
	expectedKinds := []schemas.ManifestSnapshotKind{
		schemas.PackageJson,
		schemas.PyprojectToml,
		schemas.CargoToml,
		schemas.GoMod,
		schemas.Gemfile,
		schemas.ComposerJson,
		schemas.MixExs,
		schemas.PomXml,
	}
	for _, k := range expectedKinds {
		if !seen[k] {
			t.Errorf("kind %q missing from out", k)
		}
	}
}

func TestDetectManifestsRawReadBack(t *testing.T) {
	root := t.TempDir()
	body := []byte(`{ "test": "value", "deep": { "nested": true } }`)
	if err := writeAtRoot(root, "package.json", body); err != nil {
		t.Fatalf("writeAtRoot: %v", err)
	}
	out, err := DetectManifests(root)
	if err != nil {
		t.Fatalf("DetectManifests: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("len(out) = %d, want 1", len(out))
	}
	if out[0].Raw != string(body) {
		t.Errorf("Raw mismatch: got %q, want %q", out[0].Raw, string(body))
	}
}

func TestDetectManifestsStableOrder(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"package.json", "go.mod", "Cargo.toml", "pyproject.toml"} {
		if err := writeAtRoot(root, n, []byte("x")); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	a, err := DetectManifests(root)
	if err != nil {
		t.Fatalf("first DetectManifests: %v", err)
	}
	b, err := DetectManifests(root)
	if err != nil {
		t.Fatalf("second DetectManifests: %v", err)
	}
	if len(a) != len(b) {
		t.Fatalf("len mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].Path != b[i].Path {
			t.Fatalf("order drift at i=%d: %v vs %v", i, a[i], b[i])
		}
	}
}

func TestIsKnownManifest(t *testing.T) {
	cases := []struct {
		filename string
		want     bool
	}{
		{"package.json", true},
		{"pyproject.toml", true},
		{"Cargo.toml", true},
		{"go.mod", true},
		{"Gemfile", true},
		{"composer.json", true},
		{"mix.exs", true},
		{"pom.xml", true},
		// Path with directory prefix — should still match by basename.
		{filepath.Join("nested", "deep", "package.json"), true},
		// Lockfiles and other files are NOT manifests.
		{"package-lock.json", false},
		{"yarn.lock", false},
		{"bun.lockb", false},
		{"go.sum", false},
		{"poetry.lock", false},
		{"README.md", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.filename, func(t *testing.T) {
			if got := IsKnownManifest(c.filename); got != c.want {
				t.Errorf("IsKnownManifest(%q) = %v, want %v", c.filename, got, c.want)
			}
		})
	}
}

func TestKnownManifestKindsExhaustive(t *testing.T) {
	want := map[schemas.ManifestSnapshotKind]bool{
		schemas.PackageJson:   true,
		schemas.PyprojectToml: true,
		schemas.CargoToml:     true,
		schemas.GoMod:         true,
		schemas.Gemfile:       true,
		schemas.ComposerJson:  true,
		schemas.MixExs:        true,
		schemas.PomXml:        true,
	}
	got := KnownManifestKinds()
	if len(got) != len(want) {
		t.Errorf("KnownManifestKinds len = %d, want %d", len(got), len(want))
	}
	for _, k := range got {
		if !want[k] {
			t.Errorf("unexpected kind %q", k)
		}
	}
}

func TestDetectManifestsSkipsLockfiles(t *testing.T) {
	root := t.TempDir()
	// Lockfiles are NOT manifests for this surface — only the
	// constraints in the manifest itself matter.
	for _, n := range []string{"package-lock.json", "yarn.lock", "bun.lockb", "go.sum", "poetry.lock"} {
		if err := writeAtRoot(root, n, []byte("dummy")); err != nil {
			t.Fatalf("write %s: %v", n, err)
		}
	}
	out, err := DetectManifests(root)
	if err != nil {
		t.Fatalf("DetectManifests: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("len(out) = %d, want 0 (lockfiles must not appear)", len(out))
	}
}

func TestDecodeFileSnapshotContent(t *testing.T) {
	root := t.TempDir()
	body := []byte("hello, manifest")
	if err := writeAtRoot(root, "Cargo.toml", body); err != nil {
		t.Fatalf("writeAtRoot: %v", err)
	}
	snap, err := CaptureFile(root, "Cargo.toml", 1024)
	if err != nil {
		t.Fatalf("CaptureFile: %v", err)
	}
	got, err := decodeFileSnapshotContent(snap)
	if err != nil {
		t.Fatalf("decodeFileSnapshotContent: %v", err)
	}
	if got != string(body) {
		t.Errorf("decoded = %q, want %q", got, string(body))
	}

	// Nil snapshot is rejected.
	if _, err := decodeFileSnapshotContent(nil); err == nil {
		t.Error("nil snapshot should error")
	}
}

// Helper used by tests in this file (and the testlayout test).
func writeAtRoot(root, rel string, content []byte) error {
	full := filepath.Join(root, rel)
	if err := mkdirAll(filepath.Dir(full)); err != nil {
		return err
	}
	return writeFileBytes(full, content)
}

func mkdirAll(dir string) error {
	return os.MkdirAll(dir, 0o755)
}

func writeFileBytes(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
