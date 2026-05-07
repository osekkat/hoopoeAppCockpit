// manifests.go — package-manifest discovery for the bundle assembly
// (hp-rsly sixth slice).
//
// `DetectManifests` scans the project root for ecosystem package
// manifests (package.json, pyproject.toml, Cargo.toml, go.mod, etc.),
// captures each via the shared CaptureFile primitive, classifies it
// into the ManifestSnapshot.Kind enum, and returns the list.
//
// Detection is purely path-based: presence of the file at the project
// root marks the ecosystem present. Sub-tree manifests (workspace
// packages, sub-modules) are NOT walked in this slice — that's a
// follow-up. The current slice already covers the cases the
// candidate-prompt context needs (the project's top-level peer-dep /
// language-version constraints), and avoids fanning out into a
// full-tree walk that would conflict with the discovery walk's
// future cap policy.

package bundle

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/hoopoe-cockpit/hoopoe/packages/schemas/go"
)

// decodeFileSnapshotContent returns the raw textual bytes captured in
// a FileSnapshot (CaptureFile stores them as base64 so the bundle
// envelope is JSON-safe). Used by manifest discovery to populate
// ManifestSnapshot.Raw without round-tripping through the filesystem.
func decodeFileSnapshotContent(snap *schemas.FileSnapshot) (string, error) {
	if snap == nil {
		return "", errors.New("planning/bundle: nil FileSnapshot")
	}
	decoded, err := base64.StdEncoding.DecodeString(snap.ContentB64)
	if err != nil {
		return "", fmt.Errorf("planning/bundle: base64 decode: %w", err)
	}
	return string(decoded), nil
}

// DefaultManifestSizeCap is the per-manifest size ceiling. Manifests
// rarely exceed a few KB; this cap leaves room for monorepo-style
// pyproject.toml files with extensive [tool.X] sections without
// inviting megabyte-scale lockfile bloat.
const DefaultManifestSizeCap = 64 * 1024

// manifestKindByFileName maps a project-root manifest filename to its
// canonical ManifestSnapshotKind. Lockfiles + workspace-helper files
// (e.g., bun.lockb, pnpm-lock.yaml) are intentionally excluded; the
// candidate prompts care about constraints in the top-level manifest,
// not lockfile contents.
var manifestKindByFileName = map[string]schemas.ManifestSnapshotKind{
	"package.json":     schemas.PackageJson,
	"pyproject.toml":   schemas.PyprojectToml,
	"Cargo.toml":       schemas.CargoToml,
	"go.mod":           schemas.GoMod,
	"Gemfile":          schemas.Gemfile,
	"composer.json":    schemas.ComposerJson,
	"mix.exs":          schemas.MixExs,
	"pom.xml":          schemas.PomXml,
}

// candidateManifestNames is the deterministic order DetectManifests
// walks. Sorted alphabetically so the returned slice is stable across
// runs (ContentHash relies on field-order determinism, and this list
// becomes the bundle's PackageManifests order).
var candidateManifestNames = func() []string {
	names := make([]string, 0, len(manifestKindByFileName))
	for n := range manifestKindByFileName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}()

// DetectManifests scans projectRoot for top-level package manifests
// and returns one ManifestSnapshot per detected file. Returns an
// empty (not nil) slice when nothing is detected.
//
// Each returned snapshot carries the manifest's raw contents (capped
// at DefaultManifestSizeCap) and stable Kind. The .Path field is
// POSIX-normalized and project-root-relative.
//
// Errors:
//   - When projectRoot is empty.
//   - When a detected manifest can't be read for a reason other than
//     "doesn't exist" (since fileExists already gated the read).
//
// "Doesn't exist" between the existence check and the read is treated
// as a transient race: the file is silently skipped and the caller
// gets the rest of the manifests instead of a hard fail.
func DetectManifests(projectRoot string) ([]schemas.ManifestSnapshot, error) {
	if projectRoot == "" {
		return nil, errors.New("planning/bundle: projectRoot is required")
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("planning/bundle: abs projectRoot: %w", err)
	}

	out := []schemas.ManifestSnapshot{}
	for _, name := range candidateManifestNames {
		full := filepath.Join(root, name)
		if !fileExists(full) {
			continue
		}
		snap, err := CaptureFile(root, name, DefaultManifestSizeCap)
		if err != nil {
			if errors.Is(err, ErrFileNotFound) {
				// Lost the file between fileExists and CaptureFile —
				// transient race, skip and keep going.
				continue
			}
			return nil, fmt.Errorf("planning/bundle: capture %s: %w", name, err)
		}
		// Decode captured bytes back to raw text. CaptureFile stores
		// b64; manifests are textual, so a round-trip through base64
		// gives us the raw string for ManifestSnapshot.Raw.
		raw, err := decodeFileSnapshotContent(snap)
		if err != nil {
			return nil, fmt.Errorf("planning/bundle: decode %s: %w", name, err)
		}
		out = append(out, schemas.ManifestSnapshot{
			Path: snap.Path,
			Kind: manifestKindByFileName[name],
			Raw:  raw,
		})
	}
	return out, nil
}

// IsKnownManifest reports whether a path's basename matches one of the
// canonical manifest filenames. Future slices use this to gate the
// model-context policy's "share manifests" rule without re-reading
// the file system.
func IsKnownManifest(filename string) bool {
	_, ok := manifestKindByFileName[filepath.Base(filename)]
	return ok
}

// KnownManifestKinds returns the canonical kinds DetectManifests can
// emit, in stable insertion order. Useful for tests + diagnostics
// that need to assert "is the kind enum exhaustive?" without depending
// on map-iteration order.
func KnownManifestKinds() []schemas.ManifestSnapshotKind {
	out := make([]schemas.ManifestSnapshotKind, 0, len(candidateManifestNames))
	for _, n := range candidateManifestNames {
		out = append(out, manifestKindByFileName[n])
	}
	return out
}
